package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	bootstrapv1beta1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	bootstrapv1beta2 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	controlplanev1beta2 "github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	"github.com/k0sproject/k0smotron/internal/controller/util"
	"github.com/k0sproject/k0smotron/internal/provisioner"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	"sigs.k8s.io/cluster-api/controllers/external"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Reconciler is responsible for reconciling the K0sConfig resource.
type Reconciler struct {
	Client                client.Client
	SecretCachingClient   client.Client
	workloadClusterClient client.Client
}

// scope contains the information required to generate the bootstrap data
// for a machine.
type scope struct {
	Config           *bootstrapv1beta2.K0sConfig
	ConfigOwner      *bsutil.ConfigOwner
	Cluster          *clusterv1.Cluster
	WorkerEnabled    bool
	machines         collections.Machines
	provisioner      provisioner.Provisioner
	installArgs      []string
	isIngressEnabled bool
	// In cases where the control plane is being bootstrapped with an external cluster reference,
	// the client used to interact with the workload cluster is stored here.
	client              client.Client
	secretCachingClient client.Client
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, opts controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(opts).
		For(&bootstrapv1beta2.K0sConfig{}).
		// K0sControllerConfig shouldn't be called for reconciliation.
		//
		// Watches(
		// 	&bootstrapv1beta1.K0sControllerConfig{},
		// 	handler.EnqueueRequestsFromMapFunc(nil),
		// ).
		Watches(
			&bootstrapv1beta1.K0sWorkerConfig{},
			handler.EnqueueRequestsFromMapFunc(r.convertK0sWorkerConfigToK0sConfig),
		).
		Complete(r)
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0sconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0sconfigs/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0sworkerconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0sworkerconfigs/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=exp.cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;events;configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=k0smotroncontrolplanes/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=k0smotroncontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=k0scontrolplanes/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=k0scontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

// Reconcile reconciles the K0sConfig resource and generates the bootstrap data secret for the machine if it is not already created.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := log.FromContext(ctx).WithValues("K0sConfig", req.NamespacedName)
	log.Info("Reconciling K0sConfig")

	// Lookup the config object
	config := &bootstrapv1beta2.K0sConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, config); err != nil {
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

	// If the K0sConfig does not have a version set, use the machine's version.
	if config.Spec.Version == "" && machine.Spec.Version != "" {
		config.Spec.Version = machine.Spec.Version
	}
	// If the version does not contain the k0s suffix, append it.
	if config.Spec.Version != "" {
		// When machine is created by CAPI, for example by using a clusterclass template, the version
		// of the cluster may contain '-k0s.' instead of '+k0s.', so we need to replace it first.
		config.Spec.Version = strings.Replace(config.Spec.Version, "-k0s.", "+k0s.", 1)
		if !strings.Contains(config.Spec.Version, "+k0s.") {
			config.Spec.Version = fmt.Sprintf("%s+%s", config.Spec.Version, defaultK0sSuffix)
		}
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

	if config.Status.Initialization.DataSecretCreated != nil && *config.Status.Initialization.DataSecretCreated {
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
		err := conditions.SetSummaryCondition(config, config, string(bootstrapv1beta2.ConfigReadyCondition),
			conditions.ForConditionTypes{string(bootstrapv1beta2.DataSecretAvailableCondition)},
			// Using a custom merge strategy to override reasons applied during merge and to ignore some
			// info message so the ready condition aggregation in other resources is less noisy.
			conditions.CustomMergeStrategy{
				MergeStrategy: conditions.DefaultMergeStrategy(
					// Use custom reasons.
					conditions.ComputeReasonFunc(conditions.GetDefaultComputeMergeReasonFunc(
						bootstrapv1beta2.ConfigNotReadyReason,
						bootstrapv1beta2.ConfigReadyUnknownReason,
						bootstrapv1beta2.ConfigReadyReason,
					)),
				),
			},
		)
		if err != nil {
			log.Error(err, "Failed to set summary condition")
		}

		// TODO: Once v1beta1 support is removed, we can remove the mimicStatusForV1beta1 function.
		// Machine controllers others than the control plane controller, which is managed by k0smotron,
		// can be configured with deprecated v1beta1 K0sWorkerConfig, so we need to mimic the status
		// of v1beta2 K0sConfig to v1beta1 K0sWorkerConfig for a proper reconciliation of the machine
		// node, e.g. mark the config.status.dataSecretName to the name of the created bootstrap secret
		// in the K0sWorkerConfig so the infrastructure controlelr can provision the machine.
		err = mimicStatusForV1beta1WorkerConfigs(ctx, r.Client, config)
		if err != nil {
			log.Error(err, "Failed to mimic status for v1beta1")
		}

		err = patchHelper.Patch(ctx, config)
		if err != nil {
			log.Error(err, "Failed to patch K0sWorkerConfig status")
		}
	}()

	// Ignore deleted K0sCofigs.
	if !config.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	if cluster.Spec.ControlPlaneEndpoint.IsZero() {
		log.Info("control plane endpoint is not set")
		conditions.Set(config, metav1.Condition{
			Type:    string(bootstrapv1beta2.DataSecretAvailableCondition),
			Status:  metav1.ConditionFalse,
			Reason:  bootstrapv1beta2.WaitingForControlPlaneInitializationReason,
			Message: "Control plane endpoint is not set",
		})
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 30}, nil
	}

	scope := &scope{
		Config:      config,
		ConfigOwner: configOwner,
		Cluster:     cluster,
		provisioner: getProvisioner(&config.Spec.Provisioner),
	}
	err = r.setClientScope(ctx, cluster, scope)
	if err != nil {
		conditions.Set(config, metav1.Condition{
			Type:    string(bootstrapv1beta2.DataSecretAvailableCondition),
			Status:  metav1.ConditionFalse,
			Reason:  bootstrapv1beta2.InternalErrorReason,
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}

	var bootstrapData []byte
	if configOwner.IsControlPlaneMachine() {
		bootstrapData, err = r.generateBootstrapDataForControlPlane(ctx, scope)
		if err != nil {
			conditions.Set(config, metav1.Condition{
				Type:    string(bootstrapv1beta2.DataSecretAvailableCondition),
				Status:  metav1.ConditionFalse,
				Reason:  bootstrapv1beta2.DataSecretGenerationFailedReason,
				Message: err.Error(),
			})
			return ctrl.Result{}, err
		}
	} else {
		// Control plane needs to be ready because worker needs to use controlplane API to retrieve a join token.
		if scope.Cluster.Spec.ControlPlaneEndpoint.IsZero() || !conditions.IsTrue(scope.Cluster, string(clusterv1.ClusterControlPlaneInitializedCondition)) {
			conditions.Set(scope.Config, metav1.Condition{
				Type:    string(bootstrapv1beta2.DataSecretAvailableCondition),
				Status:  metav1.ConditionFalse,
				Reason:  bootstrapv1beta2.WaitingForControlPlaneInitializationReason,
				Message: "Control plane is not ready yet",
			})
			return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5}, nil
		}

		bootstrapData, err = r.generateBootstrapDataForWorker(ctx, scope)
		if err != nil {
			conditions.Set(config, metav1.Condition{
				Type:    string(bootstrapv1beta2.DataSecretAvailableCondition),
				Status:  metav1.ConditionFalse,
				Reason:  bootstrapv1beta2.DataSecretGenerationFailedReason,
				Message: err.Error(),
			})
			return ctrl.Result{}, err
		}
	}

	bootstrapDataSecret, err := r.createBootstrapSecret(ctx, scope, bootstrapData)
	if err != nil {
		conditions.Set(config, metav1.Condition{
			Type:    string(bootstrapv1beta2.DataSecretAvailableCondition),
			Status:  metav1.ConditionFalse,
			Reason:  bootstrapv1beta2.DataSecretGenerationFailedReason,
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}
	conditions.Set(config, metav1.Condition{
		Type:    string(bootstrapv1beta2.DataSecretAvailableCondition),
		Status:  metav1.ConditionTrue,
		Reason:  bootstrapv1beta2.ConfigSecretAvailableReason,
		Message: "Bootstrap secret created",
	})

	// Notify the controllers waiting for the bootstrap data that it is already available.
	config.Status.Initialization.DataSecretCreated = ptr.To(true)
	config.Status.DataSecretName = ptr.To(bootstrapDataSecret.Name)

	return res, nil
}

// createBootstrapSecret creates a bootstrap secret.
func (r *Reconciler) createBootstrapSecret(ctx context.Context, scope *scope, bootstrapData []byte) (*corev1.Secret, error) {
	format := scope.provisioner.GetFormat()

	// Initialize labels with cluster-name label
	labels := map[string]string{
		clusterv1.ClusterNameLabel: scope.Cluster.Name,
		util.ComponentLabel:        util.ComponentBootstrap,
	}

	// Copy labels from secretMetadata if specified
	if scope.Config.Spec.SecretMetadata != nil && scope.Config.Spec.SecretMetadata.Labels != nil {
		maps.Copy(labels, scope.Config.Spec.SecretMetadata.Labels)
	}

	// Copy annotations from secretMetadata if specified
	annotations := map[string]string{}
	if scope.Config.Spec.SecretMetadata != nil && scope.Config.Spec.SecretMetadata.Annotations != nil {
		maps.Copy(annotations, scope.Config.Spec.SecretMetadata.Annotations)
	}

	bootstrapSecret := &corev1.Secret{
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
					APIVersion: bootstrapv1beta2.GroupVersion.String(),
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

	if err := r.Client.Patch(ctx, bootstrapSecret, client.Apply, &client.PatchOptions{FieldManager: "k0s-bootstrap"}); err != nil {
		return nil, err
	}

	return bootstrapSecret, nil
}

// setClientScope set the cluster client scope depending on the control plane configuration. By default, it uses the management cluster
// client if there is no external cluster reference provided.
func (r *Reconciler) setClientScope(ctx context.Context, cluster *clusterv1.Cluster, scope *scope) error {
	log := log.FromContext(ctx)

	scope.client = r.Client
	scope.secretCachingClient = r.SecretCachingClient

	uControlPlane, err := external.GetObjectFromContractVersionedRef(ctx, r.Client, cluster.Spec.ControlPlaneRef, cluster.Namespace)
	if err != nil {
		return err
	}

	// Only K0smotronControlPlane might store controlplane certificates in an external cluster. Otherwise, certificates are store in mothership.
	if uControlPlane.GetKind() == "K0smotronControlPlane" {
		kcp := &controlplanev1beta2.K0smotronControlPlane{}
		key := client.ObjectKey{
			Namespace: uControlPlane.GetNamespace(),
			Name:      uControlPlane.GetName(),
		}
		if err := r.Client.Get(ctx, key, kcp); err != nil {
			log.Error(err, "Failed to get K0smotronControlPlane")
			return err
		}

		scope.isIngressEnabled = kcp.Spec.Ingress != nil

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

func (r *Reconciler) convertK0sWorkerConfigToK0sConfig(ctx context.Context, o client.Object) []ctrl.Request {
	k0sWorkerConfig := o.(*bootstrapv1beta1.K0sWorkerConfig)

	labels := k0sWorkerConfig.Labels
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[util.DeprecatedBootstrapResouce] = "K0sWorkerConfig"

	dst := &v1beta2.K0sConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: bootstrapv1beta2.GroupVersion.String(),
			Kind:       "K0sConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            k0sWorkerConfig.Name,
			Namespace:       k0sWorkerConfig.Namespace,
			Labels:          labels,
			Annotations:     k0sWorkerConfig.Annotations,
			OwnerReferences: k0sWorkerConfig.OwnerReferences,
		},
		Spec: k0sWorkerConfigV1beta1ToV1beta2Spec(k0sWorkerConfig.Spec),
	}

	err := r.Client.Patch(ctx, dst, client.Apply, &client.PatchOptions{FieldManager: "k0s-bootstrap"})
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to patch K0sConfig")
	}

	return []ctrl.Request{
		{
			NamespacedName: client.ObjectKey{Namespace: dst.Namespace, Name: dst.Name},
		},
	}
}

func k0sWorkerConfigV1beta1ToV1beta2Spec(spec bootstrapv1beta1.K0sWorkerConfigSpec) v1beta2.K0sConfigSpec {
	res := v1beta2.K0sConfigSpec{
		Provisioner: v1beta2.ProvisionerSpec{
			// Default to CloudInit, will be overridden below if Ignition is set
			Type: provisioner.CloudInitProvisioningFormat,
		},
		K0sInstallDir:     spec.K0sInstallDir,
		Version:           spec.Version,
		UseSystemHostname: spec.UseSystemHostname,
		Files:             spec.Files,
		Args:              spec.Args,
		PreK0sCommands:    spec.PreStartCommands,
		PostK0sCommands:   spec.PostStartCommands,
		PreInstalledK0s:   spec.PreInstalledK0s,
		DownloadURL:       spec.DownloadURL,
		SecretMetadata:    spec.SecretMetadata,
		WorkingDir:        spec.WorkingDir,
	}
	if spec.Ignition != nil {
		res.Provisioner = v1beta2.ProvisionerSpec{
			Type:     provisioner.IgnitionProvisioningFormat,
			Ignition: spec.Ignition,
		}
	}
	if spec.CustomUserDataRef != nil {
		res.Provisioner.CustomUserDataRef = spec.CustomUserDataRef
	}

	return res
}

func mimicStatusForV1beta1WorkerConfigs(ctx context.Context, c client.Client, config *v1beta2.K0sConfig) error {

	var workerConfigs bootstrapv1beta1.K0sWorkerConfigList
	err := c.List(ctx, &workerConfigs, client.MatchingLabels{
		util.DeprecatedBootstrapResouce: "K0sWorkerConfig",
	},
	)
	if err != nil {
		return err
	}

	for _, workerConfig := range workerConfigs.Items {
		if workerConfig.Namespace == config.Namespace && workerConfig.Name == config.Name {
			patchHelper, err := patch.NewHelper(&workerConfig, c)
			if err != nil {
				return err
			}

			workerConfig.Status = bootstrapv1beta1.K0sWorkerConfigStatus{
				Ready:          *config.Status.Initialization.DataSecretCreated,
				Conditions:     config.GetConditions(),
				DataSecretName: config.Status.DataSecretName,
			}

			if err := patchHelper.Patch(ctx, &workerConfig); err != nil {
				return err
			}

			break
		}
	}

	return nil
}
