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
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/k0sproject/k0smotron/internal/cloudinit"
	kutil "github.com/k0sproject/k0smotron/internal/util"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"

	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/secret"

	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/log"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	capiutil "sigs.k8s.io/cluster-api/util"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Controller struct {
	client.Client
	Scheme     *runtime.Scheme
	ClientSet  *kubernetes.Clientset
	RESTConfig *rest.Config
}

type Scope struct {
	Config      *bootstrapv1.K0sWorkerConfig
	ConfigOwner *bsutil.ConfigOwner
	Cluster     *clusterv1.Cluster
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0sworkerconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0sworkerconfigs/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=exp.cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;events;configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := log.FromContext(ctx).WithValues("k0sconfig", req.NamespacedName)
	log.Info("Reconciling K0sConfig")

	// Lookup the config object
	config := &bootstrapv1.K0sWorkerConfig{}
	if err := r.Get(ctx, req.NamespacedName, config); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("K0sConfig not found")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get config")
		return ctrl.Result{}, err
	}

	// Look up the owner of this config if there is one
	configOwner, err := bsutil.GetConfigOwner(ctx, r.Client, config)
	if apierrors.IsNotFound(err) {
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
	cluster, err := capiutil.GetClusterByName(ctx, r.Client, configOwner.GetNamespace(), configOwner.ClusterName())
	if err != nil {
		if errors.Is(err, capiutil.ErrNoCluster) {
			log.Info(fmt.Sprintf("%s does not belong to a cluster yet, waiting until it's part of a cluster", configOwner.GetKind()))
			return ctrl.Result{}, nil
		}

		if apierrors.IsNotFound(err) {
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
		Config:      config,
		ConfigOwner: configOwner,
		Cluster:     cluster,
	}

	// TODO Check if the secret is already present etc. to bail out early

	log.Info("Finding the token secret")
	// Get the token from a secret
	token, err := r.getK0sToken(ctx, scope)
	if err != nil {
		log.Error(err, "Failed to get token")
		return ctrl.Result{}, err
	}

	log.Info("Creating bootstrap data")

	// Create the bootstrap data
	files := []cloudinit.File{
		{
			Path:        "/etc/k0s.token",
			Permissions: "0644",
			Content:     token,
		},
	}

	files = append(files, config.Spec.Files...)
	downloadCommands := createDownloadCommands(config)
	installCmd := createInstallCmd(config)

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
					Kind:       "K0sWorkerConfig",
					Name:       scope.Config.Name,
					UID:        scope.Config.UID,
					Controller: pointer.Bool(true),
				},
			},
		},
		Data: map[string][]byte{
			"value": bootstrapData,
		},
		Type: clusterv1.ClusterSecretType,
	}

	if err := r.Client.Patch(ctx, bootstrapSecret, client.Apply, &client.PatchOptions{FieldManager: "k0s-bootstrap"}); err != nil {
		log.Error(err, "Failed to patch bootstrap secret")
		return ctrl.Result{}, err
	}

	log.Info("Bootstrap secret created", "secret", bootstrapSecret.Name)

	// Set the status to ready
	scope.Config.Status.Ready = true
	scope.Config.Status.DataSecretName = pointer.String(bootstrapSecret.Name)
	if err := r.Status().Update(ctx, scope.Config); err != nil {
		log.Error(err, "Failed to patch config status")
		return ctrl.Result{}, err
	}

	log.Info("Reconciled succesfully")

	return ctrl.Result{}, nil
}

func (r *Controller) getK0sToken(ctx context.Context, scope *Scope) (string, error) {
	if scope.Cluster.Spec.ControlPlaneEndpoint.IsZero() {
		return "", errors.New("control plane endpoint is not set")

	}
	childClient, err := kutil.LoadChildClusterKubeClient(ctx, scope.Cluster, r.Client)
	if err != nil {
		return "", fmt.Errorf("failed to create child cluster client: %w", err)
	}

	// Create the token using the child cluster client
	tokenID := kutil.RandomString(6)
	tokenSecret := kutil.RandomString(16)
	token := fmt.Sprintf("%s.%s", tokenID, tokenSecret)
	if err := childClient.Create(ctx, &corev1.Secret{
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
	if err := certificates.Lookup(ctx, r.Client, capiutil.ObjectKey(scope.Cluster)); err != nil {
		return "", fmt.Errorf("failed to lookup CA certificates: %w", err)
	}
	ca := certificates.GetByPurpose(secret.ClusterCA)
	if ca.KeyPair == nil {
		return "", errors.New("failed to get CA certificate key pair")

	}

	joinToken, err := kutil.CreateK0sJoinToken(ca.KeyPair.Cert, token, fmt.Sprintf("https://%s:%d", scope.Cluster.Spec.ControlPlaneEndpoint.Host, scope.Cluster.Spec.ControlPlaneEndpoint.Port), "kubelet-bootstrap")
	if err != nil {
		return "", fmt.Errorf("failed to create join token: %w", err)
	}
	return joinToken, nil
}

func createInstallCmd(config *bootstrapv1.K0sWorkerConfig) string {
	installCmd := []string{
		"k0s install worker --token-file /etc/k0s.token"}
	if config.Spec.Args != nil && len(config.Spec.Args) > 0 {
		installCmd = append(installCmd, config.Spec.Args...)
	}
	return strings.Join(installCmd, " ")
}

func createDownloadCommands(config *bootstrapv1.K0sWorkerConfig) []string {
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

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1.K0sWorkerConfig{}).
		Complete(r)
}
