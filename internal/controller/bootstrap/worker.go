/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/cluster-api/controllers/remote"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bootstrapv2 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	"github.com/k0sproject/k0smotron/internal/controller/util"
	"github.com/k0sproject/k0smotron/internal/provisioner"
	kutil "github.com/k0sproject/k0smotron/internal/util"
)

const (
	defaultK0sSuffix = "k0s.0"

	// MachineNameNodeLabel is the label used to link the worker node to the Machine resource.
	MachineNameNodeLabel = "k0smotron.io/machine-name"
)

func (r *Reconciler) generateBootstrapDataForWorker(ctx context.Context, scope *scope) ([]byte, error) {
	log := log.FromContext(ctx)

	log.Info("Finding the token secret")
	token, err := r.getK0sToken(ctx, scope)
	if err != nil {
		log.Error(err, "Failed to get token")
		return nil, err
	}

	files := []provisioner.File{
		{
			Path:        getJoinTokenPath(scope.Config.Spec.WorkingDir),
			Permissions: "0600",
			Content:     token,
		},
	}

	resolvedFiles, err := resolveFiles(ctx, r.Client, scope.Cluster, scope.Config.Spec.Files)
	if err != nil {
		return nil, err
	}
	files = append(files, resolvedFiles...)

	if scope.isIngressEnabled {
		resolveCertsForIngress, err := r.resolveFilesForIngress(ctx, scope)
		if err != nil {
			return nil, err
		}
		files = append(files, resolveCertsForIngress...)
	}

	commandsMap := make(map[provisioner.VarName]string)

	downloadCommands, err := util.DownloadCommands(scope.Config.Spec.PreInstalledK0s, scope.Config.Spec.DownloadURL, scope.Config.Spec.Version, scope.Config.Spec.K0sInstallDir)
	if err != nil {
		return nil, fmt.Errorf("error generating download commands: %w", err)
	}
	installCmd := createInstallCmd(scope)

	startCmd := `(command -v systemctl > /dev/null 2>&1 && systemctl start k0sworker) || ` + // systemd
		`(command -v rc-service > /dev/null 2>&1 && rc-service k0sworker start) || ` + // OpenRC
		`(command -v service > /dev/null 2>&1 && service k0sworker start) || ` + // SysV
		`(echo "Not a supported init system"; false)`

	ingressCommands := createIngressCommands(scope)
	commands := scope.Config.Spec.PreK0sCommands
	commands = append(commands, downloadCommands...)
	commandsMap[provisioner.VarK0sDownloadCommands] = strings.Join(downloadCommands, " && ")
	commands = append(commands, ingressCommands...)
	commands = append(commands, installCmd, startCmd)
	commandsMap[provisioner.VarK0sInstallCommand] = installCmd
	commandsMap[provisioner.VarK0sStartCommand] = startCmd
	commands = append(commands, scope.Config.Spec.PostK0sCommands...)
	// Create the sentinel file as the last step so we know all previous _stuff_ has completed
	// https://cluster-api.sigs.k8s.io/developer/providers/contracts/bootstrap-config#sentinel-file
	commands = append(commands, "mkdir -p /run/cluster-api && touch /run/cluster-api/bootstrap-success.complete")

	var (
		customUserData string
		vars           map[provisioner.VarName]string
	)
	if scope.Config.Spec.Provisioner.CustomUserDataRef != nil {
		customUserData, err = resolveContentFromFile(ctx, r.Client, scope.Cluster, scope.Config.Spec.Provisioner.CustomUserDataRef)
		if err != nil {
			return nil, fmt.Errorf("error extracting the contents of the provided custom worker user data: %w", err)
		}
		vars = commandsMap
	}

	return scope.provisioner.ToProvisionData(&provisioner.InputProvisionData{
		Files:          files,
		Commands:       commands,
		CustomUserData: customUserData,
		Vars:           vars,
	})
}

func (r *Reconciler) getK0sToken(ctx context.Context, scope *scope) (string, error) {
	// Check if the workload cluster client is already set. This client is used for testing purposes to inject a fake client.
	client := r.workloadClusterClient
	if client == nil {
		var err error
		client, err = remote.NewClusterClient(ctx, "k0smotron", r.Client, capiutil.ObjectKey(scope.Cluster))
		if err != nil {
			return "", fmt.Errorf("failed to create child cluster client: %w", err)
		}
	}

	// Create the token using the child cluster client
	tokenID := kutil.RandomString(6)
	tokenSecret := kutil.RandomString(16)
	token := fmt.Sprintf("%s.%s", tokenID, tokenSecret)
	if err := client.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("bootstrap-token-%s", tokenID),
			Namespace: "kube-system",
		},
		Type: corev1.SecretTypeBootstrapToken,
		StringData: map[string]string{
			"token-id":     tokenID,
			"token-secret": tokenSecret,
			// TODO We need bit shorter time for the token
			"expiration":                       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			"usage-bootstrap-api-auth":         "true",
			"description":                      "Worker bootstrap token generated by k0smotron",
			"usage-bootstrap-authentication":   "true",
			"usage-bootstrap-api-worker-calls": "true",
		},
	}); err != nil {
		return "", fmt.Errorf("failed to create token secret: %w", err)
	}

	certificates := secret.NewCertificatesForWorker("")
	if err := certificates.LookupCached(ctx, scope.secretCachingClient, scope.client, capiutil.ObjectKey(scope.Cluster)); err != nil {
		return "", fmt.Errorf("failed to lookup CA certificates: %w", err)
	}
	ca := certificates.GetByPurpose(secret.ClusterCA)
	if ca.KeyPair == nil {
		return "", errors.New("failed to get CA certificate key pair")
	}

	var joinToken string
	joinURL := fmt.Sprintf("https://%s:%d", scope.Cluster.Spec.ControlPlaneEndpoint.Host, scope.Cluster.Spec.ControlPlaneEndpoint.Port)
	// if scope.ingressSpec != nil {
	// 	joinURL = fmt.Sprintf("https://%s:%d", scope.ingressSpec.APIHost, scope.ingressSpec.Port)
	// }

	joinToken, err := kutil.CreateK0sJoinToken(ca.KeyPair.Cert, token, joinURL, "kubelet-bootstrap")
	if err != nil {
		return "", fmt.Errorf("failed to create join token: %w", err)
	}
	return joinToken, nil
}

func createIngressCommands(scope *scope) []string {
	if !scope.isIngressEnabled {
		return []string{}
	}

	return []string{
		"mkdir -p /etc/haproxy/certs",
		"cat /etc/haproxy/certs/server.crt /etc/haproxy/certs/server.key > /etc/haproxy/certs/server.pem",
		"chmod 666 /etc/haproxy/certs/server.pem",
	}
}

func (r *Reconciler) resolveFilesForIngress(ctx context.Context, scope *scope) ([]provisioner.File, error) {
	resolvedFiles, err := resolveFiles(ctx, r.Client, scope.Cluster, []bootstrapv2.File{
		{
			File: provisioner.File{
				Path: "/etc/haproxy/certs/ca.crt",
			},
			ContentFrom: &bootstrapv2.ContentSource{
				SecretRef: &bootstrapv2.ContentSourceRef{
					Name: secret.Name(scope.Cluster.Name, secret.ClusterCA),
					Key:  "tls.crt",
				},
			},
		},
		{
			File: provisioner.File{
				Path: "/etc/haproxy/certs/server.crt",
			},
			ContentFrom: &bootstrapv2.ContentSource{
				SecretRef: &bootstrapv2.ContentSourceRef{
					Name: secret.Name(scope.Cluster.Name, "ingress-haproxy"),
					Key:  "tls.crt",
				},
			},
		},
		{
			File: provisioner.File{
				Path: "/etc/haproxy/certs/server.key",
			},
			ContentFrom: &bootstrapv2.ContentSource{
				SecretRef: &bootstrapv2.ContentSourceRef{
					Name: secret.Name(scope.Cluster.Name, "ingress-haproxy"),
					Key:  "tls.key",
				},
			},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to resolve files for ingress integration: %w", err)
	}

	return resolvedFiles, nil
}

func createInstallCmd(scope *scope) string {
	k0sPath := filepath.Join(scope.Config.Spec.K0sInstallDir, "k0s")
	installCmd := []string{
		fmt.Sprintf("%s install worker --token-file %s", k0sPath, getJoinTokenPath(scope.Config.Spec.WorkingDir)),
	}
	installCmd = append(installCmd, mergeExtraArgs(scope.Config.Spec.Args, scope.ConfigOwner, true, scope.Config.Spec.UseSystemHostname)...)
	return strings.Join(installCmd, " ")
}

func getJoinTokenPath(workingDir string) string {
	if workingDir == "" {
		return "/etc/k0s.token"
	}
	return filepath.Join(workingDir, "k0s.token")
}
