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

package k0smotronio

import (
	"context"
	"errors"
	"fmt"
	"time"

	kutil "github.com/k0sproject/k0smotron/internal/controller/util"
	apps "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
)

var (
	patchOpts []client.PatchOption = []client.PatchOption{
		client.FieldOwner("k0smotron-operator"),
		client.ForceOwnership,
	}
	// ErrNotReady is returned when the statefulset does not have a ready replica.
	ErrNotReady = fmt.Errorf("waiting for the state")
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	SecretCachingClient client.Client
	Scheme              *runtime.Scheme
	ClientSet           *kubernetes.Clientset
	RESTConfig          *rest.Config
	Recorder            record.EventRecorder
}

const (
	clusterUIDLabel  = "k0smotron.io/cluster-uid"
	clusterFinalizer = "k0smotron.io/finalizer"
)

type kmcScope struct {
	// externalOwner is the owner object used to set the owner reference for the external cluster resources.
	externalOwner client.Object
	// client is the client used to interact with the cluster where the controlplane replicas run.
	client client.Client
	// clientSet is the clientset used to interact with the cluster where the controlplane replicas run.
	clienSet *kubernetes.Clientset
	// restConfig is the rest.Config used to interact with the cluster where the controlplane replicas run.
	restConfig *rest.Config
	// secretCachingClient is the client used to cache secrets for certificate generation.
	secretCachingClient client.Client
}

// +kubebuilder:rbac:groups=k0smotron.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=k0smotron.io,resources=clusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=k0smotron.io,resources=clusters/scale,verbs=get;update;patch
// +kubebuilder:rbac:groups=k0smotron.io,resources=clusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;delete;watch
// +kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=list
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=create;update;patch;delete
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Cluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	kmc := &km.Cluster{}
	if err := r.Get(ctx, req.NamespacedName, kmc); err != nil {
		logger.Error(err, "unable to fetch Cluster")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger.Info("Reconciling")

	if finalizerAdded, err := kutil.EnsureFinalizer(ctx, r.Client, kmc, clusterFinalizer); err != nil || finalizerAdded {
		return ctrl.Result{}, err
	}

	kmcScope, err := r.getKmcScope(ctx, kmc)
	if err != nil {
		logger.Error(err, "Error getting kmc scope")
		return ctrl.Result{}, err
	}

	if kmc.Spec.KubeconfigRef != nil {
		// We need to ensure that the external owner exists in the external cluster only if the K0smotron cluster
		// is not being deleted. Otherwise, we would enter an infinite loop trying to ensure the external owner
		// while the K0smotron cluster controller deletes the external owner.
		if !kmc.ObjectMeta.DeletionTimestamp.IsZero() {
			kmcScope.externalOwner, err = kutil.GetExternalOwner(ctx, kmc.Name, kmc.Namespace, kmcScope.client)
			if err != nil {
				if !apierrors.IsNotFound(err) {
					logger.Error(err, "Error getting external owner")
					return ctrl.Result{}, err
				}
			}
		} else {
			kmcScope.externalOwner, err = kutil.EnsureExternalOwner(ctx, kmc.Name, kmc.Namespace, kmcScope.client)
			if err != nil {
				logger.Error(err, "Error ensuring external owner")
				return ctrl.Result{}, err
			}
		}

	}

	patchHelper, err := patch.NewHelper(kmc, r.Client)
	if err != nil {
		logger.Error(err, "Failed to configure the patch helper")
		return ctrl.Result{Requeue: true}, nil
	}

	defer func() {
		err = patchHelper.Patch(ctx, kmc)
		if err != nil {
			logger.Error(err, "Unable to update k0smotron Cluster")
		}
	}()

	if !kmc.ObjectMeta.DeletionTimestamp.IsZero() {
		logger.Info("Cluster is being deleted")
		// If controlplanes run in a different cluster, we need to delete the resources associated with
		// the k0smotron.Cluster by deleting the external owner which owns all the resources associated
		// with the k0smotron.Cluster.
		if kmcScope.externalOwner != nil {
			err := kmcScope.client.Delete(ctx, kmcScope.externalOwner)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("error deleting root configmap: %v", err)
			}
		}

		// Note: owner references cannot be used in this case because JoinTokenRequest can be in a
		// different namespace so we need to list and delete them manually.
		jtrl := &km.JoinTokenRequestList{}
		err := r.List(ctx, jtrl,
			client.MatchingLabels{
				clusterUIDLabel: string(kmc.GetUID()),
			})
		if err != nil {
			logger.Error(err, "Error retrieving JoinTokenRequests resources related to cluster")
			return ctrl.Result{}, nil
		}
		for i := range jtrl.Items {
			err := r.Delete(ctx, &jtrl.Items[i])
			if err != nil {
				logger.Error(err, "Error removing JoinTokenRequests")
				return ctrl.Result{}, nil
			}
		}

		if updated := controllerutil.RemoveFinalizer(kmc, clusterFinalizer); updated {
			logger.Info("Removed finalizer from k0smotron Cluster")
		}

		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling services")
	if err := kmcScope.reconcileServices(ctx, kmc); err != nil {
		kmc.Status.ReconciliationStatus = "Failed reconciling services"
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	if err := kmcScope.reconcileK0sConfig(ctx, kmc, r.Client); err != nil {
		kmc.Status.ReconciliationStatus = "Failed reconciling configmap"
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	if err := kmcScope.reconcileEntrypointCM(ctx, kmc); err != nil {
		kmc.Status.ReconciliationStatus = "Failed reconciling entrypoint configmap"
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	if kmc.Spec.Monitoring.Enabled {
		if err := kmcScope.reconcileMonitoringCM(ctx, kmc); err != nil {
			kmc.Status.ReconciliationStatus = "Failed reconciling prometheus configmap"
			return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
		}
	}

	if kmc.Spec.CertificateRefs == nil {
		if err := kmcScope.ensureCertificates(ctx, kmc); err != nil {
			return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
		}
		kmc.Spec.CertificateRefs = []km.CertificateRef{
			{
				Type: string(secret.ClusterCA),
				Name: secret.Name(kmc.Name, secret.ClusterCA),
			},
			{
				Type: string(secret.FrontProxyCA),
				Name: secret.Name(kmc.Name, secret.FrontProxyCA),
			},
			{
				Type: string(secret.ServiceAccount),
				Name: secret.Name(kmc.Name, secret.ServiceAccount),
			},
			{
				Type: string(secret.EtcdCA),
				Name: secret.Name(kmc.Name, secret.EtcdCA),
			},
		}
	}
	if kmc.Spec.KineDataSourceURL == "" {
		isAPIServerEtcdClientCertRef := false
		for _, cr := range kmc.Spec.CertificateRefs {
			if cr.Type == string(secret.APIServerEtcdClient) {
				isAPIServerEtcdClientCertRef = true
				break
			}
		}
		logger.Info("Reconciling etcd certs")
		err := kmcScope.ensureEtcdCertificates(ctx, kmc)
		if err != nil {
			return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, fmt.Errorf("error generating etcd certificates: %w", err)
		}

		if !isAPIServerEtcdClientCertRef {
			kmc.Spec.CertificateRefs = append(kmc.Spec.CertificateRefs, km.CertificateRef{
				Type: string(secret.APIServerEtcdClient),
				Name: secret.Name(kmc.Name, secret.APIServerEtcdClient),
			})
		}
	}

	if err := kmcScope.reconcilePVC(ctx, kmc); err != nil {
		kmc.Status.ReconciliationStatus = "Failed reconciling PVCs"
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	logger.Info("Reconciling etcd")
	if err := kmcScope.reconcileEtcd(ctx, kmc); err != nil {
		kmc.Status.ReconciliationStatus = fmt.Sprintf("Failed reconciling etcd, %+v", err)
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	logger.Info("Reconciling statefulset")
	if err := kmcScope.reconcileStatefulSet(ctx, kmc); err != nil {
		if errors.Is(err, ErrNotReady) {
			kmc.Status.ReconciliationStatus = err.Error()
			return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
		}

		kmc.Status.ReconciliationStatus = fmt.Sprintf("Failed reconciling statefulset, %+v", err)
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	if err := kmcScope.reconcileKubeConfigSecret(ctx, r.Client, kmc); err != nil {
		kmc.Status.ReconciliationStatus = "Failed reconciling secret"
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	kmc.Status.ReconciliationStatus = "Reconciliation successful"

	return ctrl.Result{}, nil
}

func (scope *kmcScope) ensureCertificates(ctx context.Context, kmc *km.Cluster) error {
	certificates := secret.NewCertificatesForInitialControlPlane(&bootstrapv1.ClusterConfiguration{})

	owner := *metav1.NewControllerRef(kmc, km.GroupVersion.WithKind("Cluster"))
	if scope.externalOwner != nil {
		owner = *kutil.GetExternalControllerRef(scope.externalOwner)
	}

	err := certificates.LookupOrGenerateCached(ctx, scope.secretCachingClient, scope.client, util.ObjectKey(kmc), owner)
	if err != nil {
		return fmt.Errorf("error generating cluster certificates: %w", err)
	}

	return nil
}

func (r *ClusterReconciler) getKmcScope(ctx context.Context, kmc *km.Cluster) (*kmcScope, error) {
	logger := log.FromContext(ctx)

	// By default, the controlplane replicas run in the mothership cluster so we set the
	// clients using the controller's clients.
	kmcScope := &kmcScope{
		client:              r.Client,
		clienSet:            r.ClientSet,
		restConfig:          r.RESTConfig,
		secretCachingClient: r.SecretCachingClient,
	}

	if kmc.Spec.KubeconfigRef != nil {
		var err error
		kmcScope.client, kmcScope.clienSet, kmcScope.restConfig, err = kutil.GetKmcClientFromClusterKubeconfigSecret(ctx, r.Client, kmc.Spec.KubeconfigRef)
		if err != nil {
			logger.Error(err, "Error getting client from cluster kubeconfig reference")
			return nil, err
		}

		kmcScope.secretCachingClient = kmcScope.client
	}

	return kmcScope, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager, opts controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(opts).
		For(&km.Cluster{}).
		Owns(&apps.StatefulSet{}).
		Complete(r)
}
