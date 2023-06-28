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
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Controller struct {
	client.Client
	Scheme     *runtime.Scheme
	ClientSet  *kubernetes.Clientset
	RESTConfig *rest.Config
}

type Scope struct {
	Config *cpv1beta1.K0smotronControlPlane
	// ConfigOwner *bsutil.ConfigOwner
	Cluster *clusterv1.Cluster
}

// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=k0smotroncontrolplanes/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=k0smotroncontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch;update;pacth

func (c *Controller) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {

	log := log.FromContext(ctx).WithValues("controlplane", req.NamespacedName)
	log.Info("Reconciling K0smotronControlPlane")

	kcp := &cpv1beta1.K0smotronControlPlane{}
	if err := c.Get(ctx, req.NamespacedName, kcp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get K0smotronControlPlane")
		return ctrl.Result{}, err
	}

	if !kcp.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("K0smotronControlPlane is being deleted, no action needed")
		return ctrl.Result{}, nil
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
	err = c.waitExternalAddress(ctx, cluster, kcp)
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

// watchExternalAddress watches the external address of the control plane and updates the status accordingly
func (c *Controller) waitExternalAddress(ctx context.Context, cluster *clusterv1.Cluster, _ *cpv1beta1.K0smotronControlPlane) error {
	log := log.FromContext(ctx).WithValues("cluster", cluster.Name)
	log.Info("Waiting for external address to be set")
	err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 3*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		k0smoCluster := &kapi.Cluster{}
		if err := c.Client.Get(ctx, capiutil.ObjectKey(cluster), k0smoCluster); err != nil {
			log.Error(err, "Failed to get k0smotron Cluster")
			return false, err
		}
		if k0smoCluster.Spec.ExternalAddress == "" {
			log.Info("Waiting for external address to be set")
			return false, nil
		}
		// Get the external address of the control plane
		host := k0smoCluster.Spec.ExternalAddress
		port := k0smoCluster.Spec.Service.APIPort
		// Update the Clusters endpoint if needed
		if cluster.Spec.ControlPlaneEndpoint.Host != host || cluster.Spec.ControlPlaneEndpoint.Port != int32(port) {

			// Get the infrastructure cluster object
			infraCluster := &unstructured.Unstructured{}
			infraCluster.SetGroupVersionKind(cluster.Spec.InfrastructureRef.GroupVersionKind())
			if err := c.Client.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Spec.InfrastructureRef.Name}, infraCluster); err != nil {
				log.Error(err, "Failed to get infrastructure cluster")
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
			if err := c.Client.Update(ctx, cluster); err != nil {
				log.Error(err, "Failed to update Cluster endpoint")
				return false, err
			}
			return true, nil
		}
		return true, nil
	})

	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) reconcile(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0smotronControlPlane) (ctrl.Result, error) {
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
	}
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
					Kind:       "K0smotronControlPlane",
					Name:       kcp.Name,
					UID:        kcp.UID,
				},
			},
		},
		Spec: kcp.Spec.ClusterSpec,
	}

	if err := c.Client.Patch(ctx, &kcluster, client.Apply, &client.PatchOptions{
		FieldManager: "k0smotron",
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (c *Controller) ensureCertificates(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0smotronControlPlane) error {
	certificates := secret.NewCertificatesForInitialControlPlane(&bootstrapv1.ClusterConfiguration{})
	return certificates.LookupOrGenerate(ctx, c.Client, util.ObjectKey(cluster), *metav1.NewControllerRef(kcp, cpv1beta1.GroupVersion.WithKind("K0smotronControlPlane")))
}

// SetupWithManager sets up the controller with the Manager.
func (c *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cpv1beta1.K0smotronControlPlane{}).
		Complete(c)
}
