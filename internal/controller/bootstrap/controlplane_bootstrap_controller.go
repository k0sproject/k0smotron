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
	"fmt"
	kutil "github.com/k0sproject/k0smotron/internal/util"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/yaml"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/k0sproject/k0smotron/internal/cloudinit"
	"github.com/pkg/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	kubeadmbootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"

	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/secret"

	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/log"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	capiutil "sigs.k8s.io/cluster-api/util"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ControlPlaneController struct {
	client.Client
	Scheme     *runtime.Scheme
	ClientSet  *kubernetes.Clientset
	RESTConfig *rest.Config
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0scontrollerconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0scontrollerconfigs/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=exp.cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;events;configmaps,verbs=get;list;watch;create;update;patch;delete

func (c *ControlPlaneController) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := log.FromContext(ctx).WithValues("K0sControllerConfig", req.NamespacedName)
	log.Info("Reconciling K0sControllerConfig")

	// Lookup the config object
	config := &bootstrapv1.K0sControllerConfig{}
	if err := c.Get(ctx, req.NamespacedName, config); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("K0sControllerConfig not found")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get config")
		return ctrl.Result{}, err
	}

	// Look up the owner of this config if there is one
	configOwner, err := bsutil.GetConfigOwner(ctx, c.Client, config)
	if apierrors.IsNotFound(errors.Cause(err)) {
		// Could not find the owner yet, this is not an error and will rereconcile when the owner gets set.
		log.Info("Owner not found yet, waiting until it is set")
		return ctrl.Result{}, nil
	}
	if err != nil {
		log.Error(err, "Failed to get owner")
		return ctrl.Result{}, err
	}
	if configOwner == nil {
		log.Info("Owner is nil, waiting until it is set")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("kind", configOwner.GetKind(), "version", configOwner.GetResourceVersion(), "name", configOwner.GetName())

	// Lookup the cluster the config owner is associated with
	cluster, err := capiutil.GetClusterByName(ctx, c.Client, configOwner.GetNamespace(), configOwner.ClusterName())
	if err != nil {
		if errors.Cause(err) == capiutil.ErrNoCluster {
			log.Info(fmt.Sprintf("%s does not belong to a cluster yet, waiting until it's part of a cluster", configOwner.GetKind()))
			return ctrl.Result{}, nil
		}

		if apierrors.IsNotFound(errors.Cause(err)) {
			log.Info("Cluster does not exist yet, waiting until it is created")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Could not get cluster with metadata")
		return ctrl.Result{}, err
	}

	if annotations.IsPaused(cluster, config) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	scope := &Scope{
		ConfigOwner: configOwner,
		Cluster:     cluster,
	}

	// TODO Check if the secret is already present etc. to bail out early

	log.Info("Creating bootstrap data")
	var files []cloudinit.File

	if strings.HasSuffix(config.Name, "-0") {

		// Create the bootstrap data
		certs, ca, err := c.getCerts(ctx, scope)
		if err != nil {
			log.Error(err, "Failed to create certs")
			return ctrl.Result{}, err
		}
		files = append(files, certs...)

		// Create the token using the child cluster client
		tokenID := kutil.RandomString(6)
		tokenSecret := kutil.RandomString(16)
		token := fmt.Sprintf("%s.%s", tokenID, tokenSecret)
		tokenFile, err := createToken(tokenID, tokenSecret)
		if err != nil {
			log.Error(err, "Failed to create token")
			return ctrl.Result{}, err
		}

		err = c.createKubeconfig(ctx, scope, ca, token)
		if err != nil {
			log.Error(err, "Failed to create kubeconfig")
			return ctrl.Result{}, err
		}

		files = append(files, cloudinit.File{
			Path:        "/var/lib/k0s/manifests/k0smontron/token.yaml",
			Permissions: "0644",
			Content:     tokenFile,
		})
		files = append(files, config.Spec.Files...)
	}

	downloadCommands := createCPDownloadCommands(config)
	var installCmd string

	if strings.HasSuffix(config.Name, "-0") {
		installCmd = createCPInstallCmd(config)
	} else {
		tokenFile := "/tmp/join-token"
		installCmd = createCPInstallCmdWithJoinToken(config, tokenFile)
	}

	commands := config.Spec.PreStartCommands
	commands = append(commands, downloadCommands...)
	commands = append(commands, installCmd, "k0s start")
	commands = append(commands, config.Spec.PostStartCommands...)
	// Create the sentinel file as the last step so we know all previous _stuff_ has completed
	// https://cluster-api.sigs.k8s.io/developer/providers/bootstrap.html#sentinel-file
	commands = append(commands, "mkdir -p /run/cluster-api && touch /run/cluster-api/bootstrap-success.complete")

	ci := &cloudinit.CloudInit{
		Files:   files,
		RunCmds: commands,
	}

	// Create the bootstrap data
	bootstrapData, err := ci.AsBytes()
	if err != nil {
		return ctrl.Result{}, err
	}
	// Create the secret containing the bootstrap data
	bootstrapSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: scope.Cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: bootstrapv1.GroupVersion.String(),
					Kind:       "K0sControllerConfig",
					Name:       config.Name,
					UID:        config.UID,
					Controller: pointer.Bool(true),
				},
			},
		},
		Data: map[string][]byte{
			"value": bootstrapData,
		},
		Type: clusterv1.ClusterSecretType,
	}

	if err := c.Client.Patch(ctx, bootstrapSecret, client.Apply, &client.PatchOptions{FieldManager: "k0s-bootstrap"}); err != nil {
		log.Error(err, "Failed to patch bootstrap secret")
		return ctrl.Result{}, err
	}

	log.Info("Bootstrap secret created", "secret", bootstrapSecret.Name)

	// Set the status to ready
	config.Status.Ready = true
	config.Status.DataSecretName = pointer.String(bootstrapSecret.Name)
	if err := c.Status().Update(ctx, config); err != nil {
		log.Error(err, "Failed to patch config status")
		return ctrl.Result{}, err
	}

	log.Info("Reconciled succesfully")

	return ctrl.Result{}, nil
}

func (c *ControlPlaneController) getCerts(ctx context.Context, scope *Scope) ([]cloudinit.File, *secret.Certificate, error) {
	var files []cloudinit.File
	certificates := secret.NewCertificatesForInitialControlPlane(&kubeadmbootstrapv1.ClusterConfiguration{
		CertificatesDir: "/var/lib/k0s/pki",
	})
	err := certificates.Lookup(ctx, c.Client, util.ObjectKey(scope.Cluster))
	if err != nil {
		return nil, nil, err
	}
	ca := certificates.GetByPurpose(secret.ClusterCA)
	for _, cert := range certificates.AsFiles() {
		files = append(files, cloudinit.File{
			Path:        cert.Path,
			Permissions: "0644",
			Content:     cert.Content,
		})
	}

	return files, ca, nil
}

func createToken(tokenID, tokenSecret string) (string, error) {
	bootstrapToken := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
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
	}
	b, err := yaml.Marshal(bootstrapToken)
	return string(b), err
}

func (c *ControlPlaneController) createKubconfigSecret(ctx context.Context, scope *Scope, ca *secret.Certificate, token string) error {
	const k0sContextName = "k0s"
	const userName = "kubelet-bootstrap"
	kubeconfig, err := clientcmd.Write(clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{k0sContextName: {
			Server:                   fmt.Sprintf("https://%s:%d", scope.Cluster.Spec.ControlPlaneEndpoint.Host, scope.Cluster.Spec.ControlPlaneEndpoint.Port),
			CertificateAuthorityData: ca.KeyPair.Cert,
		}},
		Contexts: map[string]*clientcmdapi.Context{k0sContextName: {
			Cluster:  k0sContextName,
			AuthInfo: userName,
		}},
		CurrentContext: k0sContextName,
		AuthInfos: map[string]*clientcmdapi.AuthInfo{userName: {
			Token: token,
		}},
	})
	kubeconfigSecret := corev1.Secret{
		// The dynamic c.Client needs TypeMeta to be set
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      scope.Cluster.Name + "-kubeconfig",
			Namespace: scope.Cluster.Namespace,
		},
		StringData: map[string]string{"value": string(kubeconfig)},
	}

	if err = ctrl.SetControllerReference(scope.Cluster, &kubeconfigSecret, c.Scheme); err != nil {
		return err
	}

	return c.Client.Patch(ctx, &kubeconfigSecret, client.Apply, client.FieldOwner("k0smotron-operator"), client.ForceOwnership)
}

func (c *ControlPlaneController) createKubeconfig(ctx context.Context, scope *Scope, ca *secret.Certificate, token string) error {
	if scope.Cluster.Spec.ControlPlaneEndpoint.IsZero() {
		return errors.New("control plane endpoint is not set")
	}

	return c.createKubconfigSecret(ctx, scope, ca, token)
}

func (c *ControlPlaneController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1.K0sControllerConfig{}).
		Complete(c)
}

func createCPDownloadCommands(config *bootstrapv1.K0sControllerConfig) []string {
	if config.Spec.PreInstalledK0s {
		return nil
	}

	if config.Spec.DownloadURL != "" {
		return []string{
			fmt.Sprintf("curl -sSfL %s -o /usr/local/bin/k0s", config.Spec.DownloadURL),
			"chmod +x /usr/local/bin/k0s",
		}
	}

	// Figure out version to download if download URL is not set
	if config.Spec.Version != "" {
		return []string{fmt.Sprintf("curl -sSfL https://get.k0s.sh | K0S_VERSION=%s sh", config.Spec.Version)}
	}

	return []string{"curl -sSfL https://get.k0s.sh | sh"}
}

func createCPInstallCmd(config *bootstrapv1.K0sControllerConfig) string {
	installCmd := []string{
		"k0s install controller"}
	if config.Spec.Args != nil && len(config.Spec.Args) > 0 {
		installCmd = append(installCmd, config.Spec.Args...)
	}
	return strings.Join(installCmd, " ")
}

func createCPInstallCmdWithJoinToken(config *bootstrapv1.K0sControllerConfig, tokenPath string) string {
	installCmd := []string{
		"k0s install controller"}
	installCmd = append(installCmd, "--token-file", tokenPath)
	if config.Spec.Args != nil && len(config.Spec.Args) > 0 {
		installCmd = append(installCmd, config.Spec.Args...)
	}
	return strings.Join(installCmd, " ")
}
