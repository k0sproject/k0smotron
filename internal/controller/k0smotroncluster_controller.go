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

package controller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	km "github.com/k0sproject/k0smotron/api/v1beta1"
)

const (
	defaultK0SVersion       = "v1.26.2-k0s.1"
	defaultAPIPort          = 30443
	defaultKonnectivityPort = 30132
)

var patchOpts []client.PatchOption = []client.PatchOption{
	client.FieldOwner("k0smotron-operator"),
	client.ForceOwnership,
}

// K0smotronClusterReconciler reconciles a K0smotronCluster object
type K0smotronClusterReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	ClientSet  *kubernetes.Clientset
	RESTConfig *rest.Config
}

//+kubebuilder:rbac:groups=k0smotron.io,resources=k0smotronclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=k0smotron.io,resources=k0smotronclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=k0smotron.io,resources=k0smotronclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list
// +kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the K0smotronCluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *K0smotronClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var kmc km.K0smotronCluster
	if err := r.Get(ctx, req.NamespacedName, &kmc); err != nil {
		logger.Error(err, "unable to fetch K0smotronCluster")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger.Info("Reconciling")

	logger.Info("Reconciling services")
	if err := r.reconcileServices(ctx, req, kmc); err != nil {
		r.updateStatus(ctx, kmc, "Failed reconciling services")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	if err := r.reconcileCM(ctx, req, kmc); err != nil {
		r.updateStatus(ctx, kmc, "Failed reconciling configmap")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	logger.Info("Reconciling statefulset")
	if err := r.reconcileStatefulSet(ctx, req, kmc); err != nil {
		r.updateStatus(ctx, kmc, "Failed reconciling statefulset")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	if err := r.reconcileKubeConfigSecret(ctx, kmc); err != nil {
		r.updateStatus(ctx, kmc, "Failed reconciling secret")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	r.updateStatus(ctx, kmc, "Reconciliation successful")
	return ctrl.Result{}, nil
}

func (r *K0smotronClusterReconciler) updateStatus(ctx context.Context, kmc km.K0smotronCluster, status string) {
	logger := log.FromContext(ctx)
	kmc.Status.ReconciliationStatus = status
	if err := r.Status().Update(ctx, &kmc); err != nil {
		logger.Error(err, fmt.Sprintf("Unable to update status: %s", status))
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *K0smotronClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&km.K0smotronCluster{}).
		Complete(r)
}
