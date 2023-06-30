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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/secret"

	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	kapi "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type K0sController struct {
	client.Client
	Scheme     *runtime.Scheme
	ClientSet  *kubernetes.Clientset
	RESTConfig *rest.Config
}

// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=K0sControlPlanes/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=K0sControlPlanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch;update;pacth

func (c *K0sController) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {

	log := log.FromContext(ctx).WithValues("controlplane", req.NamespacedName)
	log.Info("Reconciling K0sControlPlane")

	kcp := &cpv1beta1.K0sControlPlane{}
	if err := c.Get(ctx, req.NamespacedName, kcp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get K0sControlPlane")
		return ctrl.Result{}, err
	}

	if !kcp.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("K0sControlPlane is being deleted, no action needed")
		return ctrl.Result{}, nil
	}

	cluster, err := capiutil.GetOwnerCluster(ctx, c.Client, kcp.ObjectMeta)
	if err != nil {
		log.Error(err, "Failed to get owner cluster")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info("Waiting for Cluster Controller to set OwnerRef on K0sControlPlane")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("cluster", cluster.Name)

	if annotations.IsPaused(cluster, kcp) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	// TODO: This is in pretty much all CAPI controllers, but AFAIK we do not need this as we're running stuff on mothership
	// if !cluster.Status.InfrastructureReady {
	// 	log.Info("Waiting for Cluster Infrastructure to be ready")
	// 	return ctrl.Result{}, nil
	// }

	if err = c.ensureCertificates(ctx, cluster, kcp); err != nil {
		log.Error(err, "Failed to ensure certificates")
		return ctrl.Result{}, err
	}

	res, err = c.reconcile(ctx, cluster, kcp)
	if err != nil {
		return res, err
	}

	// TODO: We need to have bit more detailed status and conditions handling
	kcp.Status.Ready = true
	kcp.Status.ExternalManagedControlPlane = true
	kcp.Status.Inititalized = true
	kcp.Status.ControlPlaneReady = true
	err = c.Status().Update(ctx, kcp)

	return res, err

}

func (c *K0sController) reconcile(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) (ctrl.Result, error) {
	kcluster := kapi.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kapi.GroupVersion.String(),
			Kind:       "Cluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kcp.Name,
			Namespace: kcp.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: cpv1beta1.GroupVersion.String(),
					Kind:       "K0sControlPlane",
					Name:       kcp.Name,
					UID:        kcp.UID,
				},
			},
		},
		Spec: kcp.Spec,
	}

	if err := c.Client.Patch(ctx, &kcluster, client.Apply, &client.PatchOptions{
		FieldManager: "k0smotron",
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (c *K0sController) ensureCertificates(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) error {
	certificates := secret.NewCertificatesForInitialControlPlane(&bootstrapv1.ClusterConfiguration{})
	return certificates.LookupOrGenerate(ctx, c.Client, util.ObjectKey(cluster), *metav1.NewControllerRef(kcp, cpv1beta1.GroupVersion.WithKind("K0sControlPlane")))
}

// SetupWithManager sets up the controller with the Manager.
func (c *K0sController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cpv1beta1.K0sControlPlane{}).
		Complete(c)
}
