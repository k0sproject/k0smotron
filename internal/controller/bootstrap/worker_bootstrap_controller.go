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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/controllers/remote"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	"github.com/k0sproject/k0smotron/internal/controller/util"
	"github.com/k0sproject/k0smotron/internal/provisioner"
	kutil "github.com/k0sproject/k0smotron/internal/util"
)

const (
	defaultK0sSuffix = "k0s.0"

	machineNameNodeLabel = "k0smotron.io/machine-name"
)

type Controller struct {
	client.Client
	SecretCachingClient client.Client
	Scheme              *runtime.Scheme
	ClientSet           *kubernetes.Clientset
	RESTConfig          *rest.Config
	// workloadClusterClient is used during testing to inject a fake client
	workloadClusterClient client.Client
}

type Scope struct {
	Config              *bootstrapv1.K0sWorkerConfig
	ConfigOwner         *bsutil.ConfigOwner
	Cluster             *clusterv1.Cluster
	client              client.Client
	secretCachingClient client.Client
	provisioner         provisioner.Provisioner
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0sworkerconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0sworkerconfigs/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=exp.cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch
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

	machine := &clusterv1.Machine{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(configOwner.Object, machine); err != nil {
		return ctrl.Result{}, fmt.Errorf("error converting %s to Machine: %w", configOwner.GetKind(), err)
	}

	// If the K0sWorkerConfig does not have a version set, use the machine's version.
	if config.Spec.Version == "" && machine.Spec.Version != nil {
		config.Spec.Version = *machine.Spec.Version
	}
	// If the version does not contain the k0s suffix, append it.
	if config.Spec.Version != "" && !strings.Contains(config.Spec.Version, "+k0s.") {
		config.Spec.Version = fmt.Sprintf("%s+%s", config.Spec.Version, defaultK0sSuffix)
	}

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

	if config.Status.Ready {
		// Bootstrapdata field is ready to be consumed, skipping the generation of the bootstrap data secret
		log.Info("Bootstrapdata already created, reconciled succesfully")
		return ctrl.Result{}, nil
	}

	patchHelper, err := patch.NewHelper(config, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		// Always report the status of the bootsrap data secret generation.
		conditions.SetSummary(config,
			conditions.WithConditions(
				bootstrapv1.DataSecretAvailableCondition,
			),
		)

		err := patchHelper.Patch(ctx, config)
		if err != nil {
			log.Error(err, "Failed to patch K0sWorkerConfig status")
		}
	}()

	scope := &Scope{
		Config:      config,
		ConfigOwner: configOwner,
		Cluster:     cluster,
		provisioner: getProvisioner(config.Spec.Ignition),
	}
	err = r.setClientScope(ctx, cluster, scope)
	if err != nil {
		conditions.MarkFalse(config, bootstrapv1.DataSecretAvailableCondition, bootstrapv1.DataSecretGenerationFailedReason, clusterv1.ConditionSeverityError, "%s", err.Error())
		return ctrl.Result{}, err
	}

	// Control plane needs to be ready because worker needs to use controlplane API to retrieve a join token.
	if scope.Cluster.Spec.ControlPlaneEndpoint.IsZero() || !scope.Cluster.Status.ControlPlaneReady {
		conditions.MarkFalse(config, bootstrapv1.DataSecretAvailableCondition, bootstrapv1.WaitingForControlPlaneInitializationReason, clusterv1.ConditionSeverityInfo, "")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5}, nil
	}

	log.Info("Generating bootstrap data")
	bootstrapData, err := r.generateBootstrapDataForWorker(ctx, log, scope)
	if err != nil {
		conditions.MarkFalse(config, bootstrapv1.DataSecretAvailableCondition, bootstrapv1.DataSecretGenerationFailedReason, clusterv1.ConditionSeverityError, "%s", err.Error())
		return ctrl.Result{}, err
	}

	// Create the secret containing the bootstrap data
	bootstrapSecret := createBootstrapSecret(scope, bootstrapData, scope.provisioner.GetFormat())

	if err := r.Client.Patch(ctx, bootstrapSecret, client.Apply, &client.PatchOptions{FieldManager: "k0s-bootstrap"}); err != nil {
		log.Error(err, "Failed to patch bootstrap secret")
		conditions.MarkFalse(config, bootstrapv1.DataSecretAvailableCondition, bootstrapv1.DataSecretGenerationFailedReason, clusterv1.ConditionSeverityError, "%s", err.Error())
		return ctrl.Result{}, err
	}
	conditions.MarkTrue(config, bootstrapv1.DataSecretAvailableCondition)
	log.Info("Bootstrap secret created", "secret", bootstrapSecret.Name)

	// Set the status to ready
	scope.Config.Status.Ready = true
	scope.Config.Status.DataSecretName = ptr.To(bootstrapSecret.Name)

	log.Info("Reconciled succesfully")

	return ctrl.Result{}, nil
}

func (r *Controller) generateBootstrapDataForWorker(ctx context.Context, log logr.Logger, scope *Scope) ([]byte, error) {
	log.Info("Finding the token secret")
	token, err := r.getK0sToken(ctx, scope)
	if err != nil {
		log.Error(err, "Failed to get token")
		return nil, err
	}

	files := []provisioner.File{
		{
			Path:        "/etc/k0s.token",
			Permissions: "0600",
			Content:     token,
		},
	}

	resolvedFiles, err := resolveFiles(ctx, r.Client, scope.Cluster, scope.Config.Spec.Files)
	if err != nil {
		return nil, err
	}
	files = append(files, resolvedFiles...)

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

	commands := scope.Config.Spec.PreStartCommands
	commands = append(commands, downloadCommands...)
	commandsMap[provisioner.VarK0sDownloadCommands] = strings.Join(downloadCommands, " && ")
	commands = append(commands, installCmd, startCmd)
	commandsMap[provisioner.VarK0sInstallCommand] = installCmd
	commandsMap[provisioner.VarK0sStartCommand] = startCmd
	commands = append(commands, scope.Config.Spec.PostStartCommands...)
	// Create the sentinel file as the last step so we know all previous _stuff_ has completed
	// https://cluster-api.sigs.k8s.io/developer/providers/contracts/bootstrap-config#sentinel-file
	commands = append(commands, "mkdir -p /run/cluster-api && touch /run/cluster-api/bootstrap-success.complete")

	var (
		customUserData string
		vars           map[provisioner.VarName]string
	)
	if scope.Config.Spec.CustomUserDataRef != nil {
		customUserData, err = resolveContentFromFile(ctx, r.Client, scope.Cluster, scope.Config.Spec.CustomUserDataRef)
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

func (r *Controller) getK0sToken(ctx context.Context, scope *Scope) (string, error) {
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

	joinToken, err := kutil.CreateK0sJoinToken(ca.KeyPair.Cert, token, fmt.Sprintf("https://%s:%d", scope.Cluster.Spec.ControlPlaneEndpoint.Host, scope.Cluster.Spec.ControlPlaneEndpoint.Port), "kubelet-bootstrap")
	if err != nil {
		return "", fmt.Errorf("failed to create join token: %w", err)
	}
	return joinToken, nil
}

// createBootstrapSecret creates a bootstrap secret for the worker node
func createBootstrapSecret(scope *Scope, bootstrapData []byte, format string) *corev1.Secret {
	// Initialize labels with cluster-name label
	labels := map[string]string{
		clusterv1.ClusterNameLabel: scope.Cluster.Name,
	}

	// Copy labels from secretMetadata if specified
	if scope.Config.Spec.SecretMetadata != nil && scope.Config.Spec.SecretMetadata.Labels != nil {
		for k, v := range scope.Config.Spec.SecretMetadata.Labels {
			labels[k] = v
		}
	}

	// Copy annotations from secretMetadata if specified
	annotations := map[string]string{}
	if scope.Config.Spec.SecretMetadata != nil && scope.Config.Spec.SecretMetadata.Annotations != nil {
		for k, v := range scope.Config.Spec.SecretMetadata.Annotations {
			annotations[k] = v
		}
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        scope.Config.Name,
			Namespace:   scope.Config.Namespace,
			Labels:      labels,
			Annotations: annotations,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: bootstrapv1.GroupVersion.String(),
					Kind:       scope.Config.Kind,
					Name:       scope.Config.Name,
					UID:        scope.Config.UID,
					Controller: ptr.To(true),
				},
			},
		},
		Data: map[string][]byte{
			"value":  bootstrapData,
			"format": []byte(format),
		},
		Type: clusterv1.ClusterSecretType,
	}
}

// setClientScope set the cluster client scope depending on the control plane configuration. By default, it uses the management cluster
// client if there is no external cluster reference provided.
func (r *Controller) setClientScope(ctx context.Context, cluster *clusterv1.Cluster, scope *Scope) error {
	log := log.FromContext(ctx)

	scope.client = r.Client
	scope.secretCachingClient = r.SecretCachingClient

	uControlPlane, err := external.Get(ctx, r.Client, cluster.Spec.ControlPlaneRef)
	if err != nil {
		return err
	}

	// Only K0smotronControlPlane might store controlplane certificates in an external cluster. Otherwise, certificates are store in mothership.
	if uControlPlane.GetKind() == "K0smotronControlPlane" {
		kcp := &cpv1beta1.K0smotronControlPlane{}
		key := client.ObjectKey{
			Namespace: uControlPlane.GetNamespace(),
			Name:      uControlPlane.GetName(),
		}
		if err := r.Client.Get(ctx, key, kcp); err != nil {
			log.Error(err, "Failed to get K0smotronControlPlane")
			return err
		}

		if kcp.Spec.KubeconfigRef != nil {
			var err error
			scope.client, _, _, err = util.GetKmcClientFromClusterKubeconfigSecret(ctx, r.Client, kcp.Spec.KubeconfigRef)
			if err != nil {
				log.Error(err, "Error getting client from cluster kubeconfig reference")
				return err
			}
			scope.secretCachingClient = scope.client
		}
	}

	return nil
}

func createInstallCmd(scope *Scope) string {
	k0sPath := filepath.Join(scope.Config.Spec.K0sInstallDir, "k0s")
	installCmd := []string{
		fmt.Sprintf("%s install worker --token-file /etc/k0s.token", k0sPath),
	}
	installCmd = append(installCmd, mergeExtraArgs(scope.Config.Spec.Args, scope.ConfigOwner, true, scope.Config.Spec.UseSystemHostname)...)
	return strings.Join(installCmd, " ")
}

// SetupWithManager sets up the controller with the Manager.
func (r *Controller) SetupWithManager(mgr ctrl.Manager, opts controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(opts).
		For(&bootstrapv1.K0sWorkerConfig{}).
		Complete(r)
}
