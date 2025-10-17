package k0smotronio

import (
	"context"
	"fmt"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
)

// +kubebuilder:webhook:path=/mutate-k0smotron-io-v1beta1-cluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=k0smotron.io,resources=clusters,verbs=create;update,versions=v1beta1,name=mutate-k0smotron-cluster-v1beta1.k0smotron.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-k0smotron-io-v1beta1-cluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=k0smotron.io,resources=clusters,verbs=create;update,versions=v1beta1,name=validate-k0smotron-cluster-v1beta1.k0smotron.io,admissionReviewVersions=v1

// ClusterDefaulter is a webhook that sets default values for the Cluster resource.
type ClusterDefaulter struct{}

// ClusterValidator is a webhook that validates the Cluster resource.
type ClusterValidator struct{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Cluster.
func (c ClusterValidator) ValidateCreate(_ context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	kmc, ok := obj.(*km.Cluster)
	if !ok {
		return nil, fmt.Errorf("expected a Cluster object but got %T", obj)
	}

	return c.validateCluster(kmc)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Cluster.
func (c ClusterValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (warnings admission.Warnings, err error) {
	_, ok := oldObj.(*km.Cluster)
	if !ok {
		return nil, fmt.Errorf("expected a Cluster object but got %T", oldObj)
	}

	kmc, ok := newObj.(*km.Cluster)
	if !ok {
		return nil, fmt.Errorf("expected a Cluster object but got %T", newObj)
	}

	return c.validateCluster(kmc)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Cluster.
func (c ClusterValidator) ValidateDelete(_ context.Context, _ runtime.Object) (warnings admission.Warnings, err error) {
	return nil, nil
}

func (c ClusterValidator) validateCluster(kmc *km.Cluster) (warnings admission.Warnings, err error) {
	if kmc.Spec.Ingress != nil {
		err := kmc.Spec.Ingress.Validate(kmc.Spec.Version)
		if err != nil {
			return nil, err
		}
	}

	return warnings, nil
}

func (c *ClusterDefaulter) Default(_ context.Context, obj runtime.Object) error {
	kmc, ok := obj.(*km.Cluster)
	if !ok {
		return fmt.Errorf("expected a Cluster object but got %T", obj)
	}

	if kmc.Spec.Replicas == 0 {
		kmc.Spec.Replicas = 1
	}

	if kmc.Spec.Version == "" {
		kmc.Spec.Version = km.DefaultK0SVersion
	}

	if kmc.Spec.Service.Type == "" {
		kmc.Spec.Service.Type = corev1.ServiceTypeClusterIP
		kmc.Spec.Service.APIPort = 30443
		kmc.Spec.Service.KonnectivityPort = 30132
	}

	if kmc.Spec.Etcd.Image == "" {
		kmc.Spec.Etcd.Image = km.DefaultEtcdImage
	}

	if kmc.Spec.Ingress != nil {
		if kmc.Spec.Ingress.Deploy == nil {
			kmc.Spec.Ingress.Deploy = ptr.To(true)
		}
	}

	return nil
}

var _ webhook.CustomDefaulter = &ClusterDefaulter{}
var _ webhook.CustomValidator = &ClusterValidator{}

// SetupK0sControlPlaneWebhookWithManager sets up the webhook with the manager.
func SetupK0sControlPlaneWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&km.Cluster{}).
		WithDefaulter(&ClusterDefaulter{}).
		WithValidator(&ClusterValidator{}).
		Complete()
}
