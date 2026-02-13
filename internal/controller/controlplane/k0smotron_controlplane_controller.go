/*
Copyright 2023.

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

package controlplane

import (
	"context"
	"errors"
	"fmt"
	"k8s.io/utils/ptr"
	"reflect"
	"strings"
	"time"

	"github.com/imdario/mergo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	bootstrapv1 "sigs.k8s.io/cluster-api/api/bootstrap/kubeadm/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cpv1beta2 "github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	kapi "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	"github.com/k0sproject/k0smotron/internal/controller/util"
	"github.com/k0sproject/k0smotron/internal/exec"
	"github.com/k0sproject/version"
)

const (
	// AnnotationKeyManagedBy is the annotation key that indicates which controller manages the infrastructure object
	AnnotationKeyManagedBy = "cluster.x-k8s.io/managed-by"

	// AnnotationValueManagedByK0smotron is the value for the managed-by annotation
	AnnotationValueManagedByK0smotron = "k0smotron"
)

// K0smotronController is the controller for K0smotronControlPlane objects.
type K0smotronController struct {
	client.Client
	SecretCachingClient client.Client
	Scheme              *runtime.Scheme
	ClientSet           *kubernetes.Clientset
	RESTConfig          *rest.Config
}

// Scope defines the basic context for the reconciliation of a K0smotronControlPlane object.
type Scope struct {
	Config *cpv1beta2.K0smotronControlPlane
	// ConfigOwner *bsutil.ConfigOwner
	Cluster *clusterv1.Cluster
}

type kmcScope struct {
	// externalOwner is the owner object used to set the owner reference for the external cluster resources.
	externalOwner metav1.Object
	// client is the client used to interact with the cluster where the controlplane replicas run.
	client client.Client
	// clientSet is the clientset used to interact with the cluster where the controlplane replicas run.
	clientSet *kubernetes.Clientset
	// restConfig is the rest.Config used to interact with the cluster where the controlplane replicas run.
	restConfig *rest.Config

	secretCachingClient client.Client
}

// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=k0smotroncontrolplanes/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=k0smotroncontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

// Reconcile reconciles the K0smotronControlPlane to the desired state.
func (c *K0smotronController) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := log.FromContext(ctx).WithValues("controlplane", req.NamespacedName)
	log.Info("Reconciling K0smotronControlPlane")

	kcp := &cpv1beta2.K0smotronControlPlane{}
	if err := c.Get(ctx, req.NamespacedName, kcp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get K0smotronControlPlane")
		return ctrl.Result{}, err
	}

	if finalizerAdded, err := util.EnsureFinalizer(ctx, c.Client, kcp, cpv1beta2.K0smotronControlPlaneFinalizer); err != nil || finalizerAdded {
		return ctrl.Result{}, err
	}

	cluster, err := capiutil.GetOwnerCluster(ctx, c.Client, kcp.ObjectMeta)
	if err != nil {
		log.Error(err, "Failed to get owner cluster")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info("Waiting for Cluster Controller to set OwnerRef on K0smotronControlPlane")
		return ctrl.Result{}, nil
	}

	// Configure patch helpers for resources updated during the reconcile loop.
	kcpPatchHelper, err := patch.NewHelper(kcp, c.Client)
	if err != nil {
		log.Error(err, "Failed to configure K0smotronControlPlane patch helper")
		return ctrl.Result{Requeue: true}, nil
	}
	clusterPatchHelper, err := patch.NewHelper(cluster, c.Client)
	if err != nil {
		log.Error(err, "Failed to configure Cluster patch helper")
		return ctrl.Result{Requeue: true}, nil
	}

	log = log.WithValues("cluster", cluster.Name)

	if annotations.IsPaused(cluster, kcp) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	kmcScope, err := c.getKmcScope(ctx, kcp)
	if err != nil {
		log.Error(err, "Error getting kmc scope")
		return ctrl.Result{}, err
	}

	// If the controlplane replicas run in a different cluster, we need to ensure the external owner is created
	// for garbage collection purposes. Only if the K0smotronControlPlane is not being deleted, otherwise an infinite
	// loop would be created creating the root owner - deleting it - creating it again.
	if kcp.Spec.KubeconfigRef != nil && kcp.ObjectMeta.DeletionTimestamp.IsZero() {
		// Ensure the namespace exists in the remote cluster before creating any resources
		if err := util.EnsureNamespaceExists(ctx, kmcScope.client, cluster.Namespace); err != nil {
			log.Error(err, "Error ensuring namespace exists in remote cluster")
			return ctrl.Result{}, err
		}

		kmcScope.externalOwner, err = util.EnsureExternalOwner(ctx, cluster.Name, cluster.Namespace, kmcScope.client)
		if err != nil {
			log.Error(err, "Error ensuring external owner")
			return ctrl.Result{}, err
		}
	}

	defer func() {
		derr := c.computeStatus(ctx, cluster, kcp, kmcScope)
		if derr != nil {
			if errors.Is(derr, ErrNotReady) {
				res = ctrl.Result{RequeueAfter: 10 * time.Second, Requeue: true}
			} else {
				log.Error(derr, "Failed to update K0smotronControlPlane status")
				err = derr
				return
			}
		}

		derr = kcpPatchHelper.Patch(ctx, kcp)
		if derr != nil {
			log.Error(derr, "Failed to patch K0smotronControlPlane")
			err = kerrors.NewAggregate([]error{err, derr})
		}

		derr = clusterPatchHelper.Patch(ctx, cluster)
		if derr != nil {
			log.Error(err, "Failed to update Cluster endpoint")
			err = kerrors.NewAggregate([]error{err, derr})
		}

		if err != nil {
			// We shouldn't proceed with Infrastructure patching
			// if we couldn't update the Cluster, K0smotronControlPlane object(s)
			return
		}

		derr = c.patchInfrastructureStatus(ctx, cluster, kcp.Status.Initialization.ControlPlaneInitialized)
		if derr != nil {
			log.Error(derr, "Failed to patch Infrastructure object status")
			err = kerrors.NewAggregate([]error{err, derr})
		}
	}()

	if !kcp.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("Reconcile K0smotronControlPlane deletion")
		return c.reconcileDelete(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, kcp)
	}

	res, err = c.reconcile(ctx, cluster, kcp, kmcScope)
	if err != nil {
		log.Error(err, "Reconciliation failed")
		return res, err
	}
	// Requeue is needed when the k0smotron Cluster has just been created. k0smotron Cluster controller needs to take action
	// before the ControlPlane reconciliation can continue.
	if !res.IsZero() {
		return res, err
	}

	err = c.ensureExternalAddress(ctx, kcp, cluster)
	if err != nil {
		return res, err
	}

	kcp.Status.ExternalManagedControlPlane = true

	return res, err
}

// ensureExternalAddress watches the external address of the control plane and updates the status accordingly
func (c *K0smotronController) ensureExternalAddress(ctx context.Context, kcp *cpv1beta2.K0smotronControlPlane, cluster *clusterv1.Cluster) error {
	log := log.FromContext(ctx).WithValues("cluster", cluster.Name)
	log.Info("Starting to wait for external address")
	startTime := time.Now()
	err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 3*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		k0smoCluster := &kapi.Cluster{}
		if err := c.Client.Get(ctx, capiutil.ObjectKey(cluster), k0smoCluster); err != nil {
			log.Error(err, "Failed to get k0smotron Cluster")
			return false, err
		}

		var (
			host string
			port int
		)
		if kcp.Spec.Ingress != nil {
			host = k0smoCluster.Spec.Ingress.APIHost
			port = int(k0smoCluster.Spec.Ingress.Port)
			if port == 0 {
				port = 443
			}
		} else {
			if k0smoCluster.Spec.ExternalAddress == "" {
				elapsed := time.Since(startTime).Round(time.Second)
				log.Info("External address not yet available", "elapsed", elapsed)
				return false, nil
			}
			log.Info("External address found", "address", k0smoCluster.Spec.ExternalAddress, "elapsed", time.Since(startTime).Round(time.Second))

			// Get the external address of the control plane
			host = k0smoCluster.Spec.ExternalAddress
			port = k0smoCluster.Spec.Service.APIPort
		}
		// Update the Clusters endpoint if needed
		if cluster.Spec.InfrastructureRef.IsDefined() && (cluster.Spec.ControlPlaneEndpoint.Host != host || cluster.Spec.ControlPlaneEndpoint.Port != int32(port)) {
			infraCluster, err := external.GetObjectFromContractVersionedRef(ctx, c, cluster.Spec.InfrastructureRef, cluster.Namespace)
			if err != nil {
				log.Error(err, "Failed to get infrastructure cluster from versioned ref")
				return false, err
			}
			log.Info("Found infrastructure cluster")
			newEndpoint := map[string]interface{}{
				"host": host,
				"port": int64(port),
			}
			err = unstructured.SetNestedMap(infraCluster.Object, newEndpoint, "spec", "controlPlaneEndpoint")
			if err != nil {
				log.Error(err, "Failed to set controlPlaneEndpoint in infrastructure cluster")
				return false, err
			}
			if err := c.Client.Update(ctx, infraCluster); err != nil {
				log.Error(err, "Failed to update infrastructure cluster")
				return false, err
			}
			log.Info("Updated infrastructure cluster", "host", host, "port", port)
			cluster.Spec.ControlPlaneEndpoint.Host = host
			cluster.Spec.ControlPlaneEndpoint.Port = int32(port)

			return true, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	return nil
}

func (c *K0smotronController) reconcile(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta2.K0smotronControlPlane, scope *kmcScope) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if kcp.Spec.CertificateRefs == nil {
		kcp.Spec.CertificateRefs = []kapi.CertificateRef{
			{
				Type: string(secret.ClusterCA),
				Name: secret.Name(cluster.Name, secret.ClusterCA),
			},
			{
				Type: string(secret.FrontProxyCA),
				Name: secret.Name(cluster.Name, secret.FrontProxyCA),
			},
			{
				Type: string(secret.ServiceAccount),
				Name: secret.Name(cluster.Name, secret.ServiceAccount),
			},
			{
				Type: string(secret.EtcdCA),
				Name: secret.Name(cluster.Name, secret.EtcdCA),
			},
		}

		if err := ensureCertificates(ctx, cluster, kcp, scope); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to ensure certificates for K0smotronControlPlane %s/%s: %w", kcp.Namespace, kcp.Name, err)
		}
	}

	var err error
	kcp.Spec.K0sConfig, err = enrichK0sConfigWithClusterData(cluster, kcp.Spec.K0sConfig)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to enrich k0s config with cluster data for K0smotronControlPlane %s/%s", kcp.Namespace, kcp.Name)
	}

	desiredK0smotronCluster := kapi.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kapi.GroupVersion.String(),
			Kind:       "Cluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: cpv1beta2.GroupVersion.String(),
					Kind:       "K0smotronControlPlane",
					Name:       kcp.Name,
					UID:        kcp.UID,
				},
			},
		},
		Spec: kcp.Spec,
	}

	var foundCluster kapi.Cluster
	err = c.Client.Get(ctx, types.NamespacedName{Name: desiredK0smotronCluster.Name, Namespace: desiredK0smotronCluster.Namespace}, &foundCluster)
	if err != nil && apierrors.IsNotFound(err) {
		if err := c.Client.Create(ctx, &desiredK0smotronCluster); err != nil {
			return ctrl.Result{}, err
		}

		logger.Info("Requeuing because k0smotron Cluster has just been created")
		return ctrl.Result{RequeueAfter: 5 * time.Second, Requeue: true}, nil
	}

	if kcp.Spec.ExternalAddress == "" {
		kcp.Spec.ExternalAddress = foundCluster.Spec.ExternalAddress
	}

	isClusterSpecSynced, err := isClusterSpecSynced(foundCluster.Spec, kcp.Spec)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error comparing cluster spec between k0smotron.Cluster and k0smotronControlPlane: %w", err)
	}
	if !isClusterSpecSynced {
		patchHelper, err := patch.NewHelper(&foundCluster, c.Client)
		if err != nil {
			return ctrl.Result{}, err
		}

		// Modidy current Cluster specification with the desired one.
		foundCluster.Spec = kcp.Spec

		return ctrl.Result{}, patchHelper.Patch(ctx, &foundCluster)
	}

	return ctrl.Result{}, nil
}

// isClusterSpecSynced compares ClusterSpecs while accounting for expected changes. The K0smotron Cluster controller may add additional data to the spec,
// so we need to account for that possibility where appropriate.
func isClusterSpecSynced(kmcSpec, kcpSpec kapi.ClusterSpec) (bool, error) {
	overridenKmcSpec := kmcSpec.DeepCopy()
	// The definition in K0smotronControlPlane takes precedence, as the K0smotron Cluster is created based on it. For this reason, mergo.WithOverride is
	// used to ensure those values override existing ones.
	err := mergo.Merge(overridenKmcSpec, kcpSpec, mergo.WithOverride)
	if err != nil {
		return false, err
	}
	// K0smotron Cluster controller can add additional certificate references, such as 'apiserver-etcd-client'. Therefore, we explicitly reset the
	// certificate references based on the original Cluster definition.
	overridenKmcSpec.CertificateRefs = kmcSpec.CertificateRefs

	// K0smotron Cluster controller will add additional SANs, such as those related to the externaladdress or to the service that exposes the controlplanes.
	// Therefore, we explicitly reset the SANs references based on the original Cluster definition.
	if kmcSpec.K0sConfig != nil {
		kmcSans, found, err := unstructured.NestedSlice(kmcSpec.K0sConfig.Object, "spec", "api", "sans")
		if found && err == nil {
			err = unstructured.SetNestedField(overridenKmcSpec.K0sConfig.Object, kmcSans, "spec", "api", "sans")
			if err != nil {
				return false, fmt.Errorf("failed to set api.sans in K0smotronCluster spec: %w", err)
			}
		}
	}

	return reflect.DeepEqual(kmcSpec, *overridenKmcSpec), nil
}

func ensureCertificates(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta2.K0smotronControlPlane, scope *kmcScope) error {
	certificates := secret.NewCertificatesForInitialControlPlane(&bootstrapv1.ClusterConfiguration{})

	owner := *metav1.NewControllerRef(kcp, cpv1beta2.GroupVersion.WithKind("K0smotronControlPlane"))
	if scope.externalOwner != nil {
		owner = *util.GetExternalControllerRef(scope.externalOwner)
	}

	return certificates.LookupOrGenerateCached(ctx, scope.secretCachingClient, scope.client, capiutil.ObjectKey(cluster), owner)
}

func (c *K0smotronController) computeStatus(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta2.K0smotronControlPlane, scope *kmcScope) error {
	var kmc kapi.Cluster
	err := c.Client.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, &kmc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The cluster is not yet created.
			return nil
		}

		return err
	}

	contolPlanePods := &corev1.PodList{}
	opts := &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"cluster.x-k8s.io/cluster-name":  cluster.Name,
			"cluster.x-k8s.io/control-plane": "true",
		}),
	}
	err = scope.client.List(ctx, contolPlanePods, opts)
	if err != nil {
		return err
	}

	kcp.Status.Replicas = ptr.To(int32(len(contolPlanePods.Items)))

	var upToDateReplicas, readyReplicas, unavailableReplicas, availableReplicas int

	desiredVersionStr := kcp.Spec.Version
	if !strings.Contains(desiredVersionStr, "-k0s.") && !strings.Contains(desiredVersionStr, "+k0s.") {
		// Use default k0s suffix format when spec version does not contain either -k0s. or +k0s.
		desiredVersionStr = fmt.Sprintf("%s+%s", desiredVersionStr, kapi.DefaultK0SSuffix)
	}
	desiredVersion, err := version.NewVersion(desiredVersionStr)
	if err != nil {
		return err
	}
	minimumVersion := *desiredVersion

	for _, pod := range contolPlanePods.Items {
		isPodReady := false
		for _, c := range pod.Status.Conditions {
			// readiness probe in pod will propagate pod status Ready = True if k0s service is running successfully.
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				isPodReady = true
				break
			}
		}
		if isPodReady {
			readyReplicas++
			availableReplicas++
		} else {
			unavailableReplicas++
			// if pod is unavailable subsequent checks do not apply
			continue
		}

		currentVersion, err := scope.getK0sVersionRunningInPod(ctx, &pod)
		if err != nil {
			return err
		}
		// Align version format in spec to the current version format for comparison. DO NOT modify version format in spec.
		currentVersion, err = alignToSpecVersionFormat(desiredVersion, currentVersion)
		if err != nil {
			return err
		}

		if desiredVersion.Equal(currentVersion) {
			upToDateReplicas++
		}

		if currentVersion.LessThan(&minimumVersion) {
			minimumVersion = *currentVersion
		}
	}

	kcp.Status.UpToDateReplicas = ptr.To(int32(upToDateReplicas))
	kcp.Status.ReadyReplicas = ptr.To(int32(readyReplicas))
	kcp.Status.AvailableReplicas = ptr.To(ptr.Deref(kcp.Status.Replicas, 0) - int32(unavailableReplicas))

	if ptr.Deref(kcp.Status.ReadyReplicas, 0) > 0 {
		kcp.Status.Version = minimumVersion.String()
	}

	c.computeAvailability(ctx, cluster, kcp)

	// if no replicas are yet available or the desired version is not in the current state of the
	// control plane, the reconciliation is requeued waiting for the desired replicas to become available.
	// Additionally, if the ControlPlaneReadyCondition is false (e.g., due to DNS resolution failures),
	// we should also requeue to retry the connection.
	if unavailableReplicas > 0 ||
		desiredVersion.String() != kcp.Status.Version ||
		!conditions.IsTrue(kcp, string(cpv1beta2.ControlPlaneAvailableCondition)) {
		return ErrNotReady
	}

	return nil
}

// alignToSpecVersionFormat ensures that the currentVersion format matches the desiredVersion format.
func alignToSpecVersionFormat(specVersion, currentVersion *version.Version) (*version.Version, error) {
	specVersionUsesDefaultK0SSuffix := strings.Contains(specVersion.String(), "+k0s.")
	currentVersionUsesDefaultK0SSuffix := strings.Contains(currentVersion.String(), "+k0s.")

	isFormatAligned := (specVersionUsesDefaultK0SSuffix && currentVersionUsesDefaultK0SSuffix) ||
		(!specVersionUsesDefaultK0SSuffix && !currentVersionUsesDefaultK0SSuffix)

	if isFormatAligned {
		return currentVersion, nil
	}

	currentVersionStr := currentVersion.String()
	if !specVersionUsesDefaultK0SSuffix {
		// Spec version format is like "vX.Y.Z-k0s.0".
		// Current version format is like "vX.Y.Z+k0s.0".
		// Convert currentVersion to match it.
		currentVersionAlignedStr := strings.Replace(currentVersionStr, "+k0s.", "-k0s.", 1)
		return version.NewVersion(currentVersionAlignedStr)
	}
	// Spec version format is like "vX.Y.Z+k0s.0".
	// Current version format is like "vX.Y.Z-k0s.0".
	// Convert currentVersion to match it.
	currentVersionAlignedStr := strings.Replace(currentVersionStr, "-k0s.", "+k0s.", 1)
	return version.NewVersion(currentVersionAlignedStr)
}

// computeAvailability checks if the control plane is ready by connecting to the API server
// and checking if the control plane is initialized
func (c *K0smotronController) computeAvailability(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta2.K0smotronControlPlane) {
	logger := log.FromContext(ctx).WithValues("cluster", cluster.Name)
	// Check if the control plane is ready by connecting to the API server
	// and checking if the control plane is initialized
	logger.Info("Pinging the workload cluster API")

	// Get the CAPI cluster accessor
	client, err := remote.NewClusterClient(ctx, "k0smotron", c.Client, capiutil.ObjectKey(cluster))
	if err != nil {
		logger.Info("Failed to create cluster client", "error", err)
		conditions.Set(kcp, metav1.Condition{
			Type:    string(cpv1beta2.ControlPlaneAvailableCondition),
			Status:  metav1.ConditionFalse,
			Reason:  "ClusterClientCreationFailed",
			Message: err.Error(),
		})
		return
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// If we can get 'kube-system' namespace, it's safe to say the API is up-and-running
	ns := &corev1.Namespace{}
	nsKey := types.NamespacedName{
		Namespace: "",
		Name:      "kube-system",
	}
	err = client.Get(pingCtx, nsKey, ns)
	if err != nil {
		logger.Info("Failed to get workload cluster namespace", "error", err)
		conditions.Set(kcp, metav1.Condition{
			Type:    string(cpv1beta2.ControlPlaneAvailableCondition),
			Status:  metav1.ConditionFalse,
			Reason:  "KubeSystemNamespaceNotAccessible",
			Message: err.Error(),
		})
		return
	}

	logger.Info("Successfully verified workload cluster API availability")

	// Set condition for successful API access
	conditions.Set(kcp, metav1.Condition{
		Type:   string(cpv1beta2.ControlPlaneAvailableCondition),
		Status: metav1.ConditionTrue,
		Reason: cpv1beta2.ControlPlaneAvailableReason,
	})

	kcp.Status.Initialization.ControlPlaneInitialized = ptr.To(true)

	// Set the k0s cluster ID annotation
	annotations.AddAnnotations(cluster, map[string]string{
		cpv1beta2.K0sClusterIDAnnotation: fmt.Sprintf("kube-system:%s", ns.GetUID()),
	})
}

func (scope *kmcScope) getK0sVersionRunningInPod(ctx context.Context, pod *corev1.Pod) (*version.Version, error) {
	currentVersionOutput, err := exec.PodExecCmdOutput(ctx, scope.clientSet, scope.restConfig, pod.GetName(), pod.GetNamespace(), "k0s version")
	if err != nil {
		return nil, err
	}
	currentVersionStr, _ := strings.CutSuffix(currentVersionOutput, "\n")
	return version.NewVersion(currentVersionStr)
}

// patchInfrastructureStatus updates the ready status of the infrastructure object referenced by the cluster
func (c *K0smotronController) patchInfrastructureStatus(ctx context.Context, cluster *clusterv1.Cluster, ready *bool) error {
	log := log.FromContext(ctx).WithValues("cluster", cluster.Name)

	infraObj, err := external.GetObjectFromContractVersionedRef(ctx, c, cluster.Spec.InfrastructureRef, cluster.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get Infrastructure object: %w", err)
	}

	// Check if the object has the required annotation
	annotations := infraObj.GetAnnotations()
	if annotations == nil || annotations[AnnotationKeyManagedBy] != AnnotationValueManagedByK0smotron {
		return nil
	}

	// Check current status.ready value
	currentReady, found, err := unstructured.NestedBool(infraObj.Object, "status", "ready")
	if err != nil {
		return fmt.Errorf("failed to get ready status: %w", err)
	}

	// Only patch if status is not set or different from desired value
	if !found || currentReady != ptr.Deref(ready, false) {
		log.Info("Patching Infrastructure object status", "ready", ready)

		// Apply the patch
		err = c.Client.Status().Patch(
			ctx,
			infraObj,
			client.RawPatch(
				types.MergePatchType,
				fmt.Appendf(nil, `{"status": {"ready": %t}}`, ptr.Deref(ready, false)),
			),
		)
		if err != nil {
			return fmt.Errorf("failed to patch Infrastructure object: %w", err)
		}

		log.Info("Successfully patched Infrastructure object status")
	}

	return nil
}

// reconcileDelete handles the deletion of the K0smotronControlPlane object when the controlplane replicas runs in a different cluster.
func (c *K0smotronController) reconcileDelete(ctx context.Context, key types.NamespacedName, kcp *cpv1beta2.K0smotronControlPlane) (res ctrl.Result, err error) {

	defer func() {
		if err != nil && apierrors.IsNotFound(err) {
			// The cluster is already deleted. Remove the finalizer from the K0smotronControlPlane object for complete cleanup.
			controllerutil.RemoveFinalizer(kcp, cpv1beta2.K0smotronControlPlaneFinalizer)
			err = nil
		}
	}()

	kmc := &kapi.Cluster{}
	err = c.Client.Get(ctx, key, kmc)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !kmc.GetDeletionTimestamp().IsZero() {
		// Requeue the reconciliation to wait for the complete deletion of the k0smotron Cluster meaning all dependent resources are deleted.
		return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
	}

	if err := c.Client.Delete(ctx, kmc); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (c *K0smotronController) getKmcScope(ctx context.Context, kcp *cpv1beta2.K0smotronControlPlane) (*kmcScope, error) {
	logger := log.FromContext(ctx)

	// By default, the controlplane replicas run in the mothership cluster so we set the
	// clients using the controller's clients.
	kmcScope := &kmcScope{
		client:     c.Client,
		clientSet:  c.ClientSet,
		restConfig: c.RESTConfig,
	}

	if kcp.Spec.KubeconfigRef != nil {
		var err error
		kmcScope.client, kmcScope.clientSet, kmcScope.restConfig, err = util.GetKmcClientFromClusterKubeconfigSecret(ctx, c.Client, kcp.Spec.KubeconfigRef)
		if err != nil {
			logger.Error(err, "Error getting client from cluster kubeconfig reference")
			return nil, err
		}

		kmcScope.secretCachingClient = kmcScope.client
	}

	return kmcScope, nil
}

// SetupWithManager sets up the controller with the Manager.
func (c *K0smotronController) SetupWithManager(mgr ctrl.Manager, opts controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(opts).
		For(&cpv1beta2.K0smotronControlPlane{}).
		Owns(&kapi.Cluster{}, builder.MatchEveryOwner).
		Complete(c)
}
