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

package infrastructure

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/paused"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	infrastructure "github.com/k0sproject/k0smotron/v2/api/infrastructure/v1beta2"
)

// ClusterController is responsible for reconciling the RemoteCluster resource,
// which represents a remote cluster that is being managed by k0smotron.
type ClusterController struct {
	client.Client
	Scheme           *runtime.Scheme
	WatchFilterValue string
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=remoteclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=remoteclusters/status,verbs=get;list;watch;create;update;patch;delete

// Reconcile reconciles the RemoteCluster resource and ensures it is in a ready state.
func (r *ClusterController) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := log.FromContext(ctx).WithValues("remotecluster", req.NamespacedName)
	log.Info("Reconciling RemoteCluster")

	rc := &infrastructure.RemoteCluster{}
	if err := r.Get(ctx, req.NamespacedName, rc); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("RemoteCluster not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get RemoteCluster")
		return ctrl.Result{}, err
	}

	cluster, err := util.GetOwnerCluster(ctx, r.Client, rc.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info("Waiting for Cluster Controller to set OwnerRef on RemoteCluster")
		return ctrl.Result{}, nil
	}

	if isPaused, requeue, err := paused.EnsurePausedCondition(ctx, r.Client, cluster, rc); err != nil || isPaused || requeue {
		return ctrl.Result{}, err
	}

	// Nothing really to do, except put the cluster in a ready state
	if rc.ObjectMeta.DeletionTimestamp.IsZero() {
		rc.Status.Initialization.Provisioned = new(true)
		conditions.Set(rc, metav1.Condition{
			Type:   infrastructure.RemoteClusterReadyCondition,
			Status: metav1.ConditionTrue,
			Reason: infrastructure.RemoteClusterReadyReason,
		})
		if err := r.Status().Update(ctx, rc); err != nil {
			log.Error(err, "Failed to update RemoteCluster status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterController) SetupWithManager(ctx context.Context, mgr ctrl.Manager, opts controller.Options) error {
	predicateLog := ctrl.LoggerFrom(ctx).WithValues("controller", "remotecluster")
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(opts).
		For(&infrastructure.RemoteCluster{}).
		WithEventFilter(predicates.ResourceHasFilterLabel(mgr.GetScheme(), predicateLog, r.WatchFilterValue)).
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(util.ClusterToInfrastructureMapFunc(ctx, infrastructure.GroupVersion.WithKind("RemoteCluster"), mgr.GetClient(), &infrastructure.RemoteCluster{})),
			builder.WithPredicates(
				predicates.ClusterPausedTransitions(mgr.GetScheme(), predicateLog),
			)).
		Complete(r)
}
