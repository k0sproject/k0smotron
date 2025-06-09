package k0smotronio

import (
	"context"
	"fmt"
	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// +kubebuilder:webhook:path=/mutate-k0smotron-io-v1beta1-cluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=k0smotron.io,resources=clusters,verbs=create;update,versions=v1beta1,name=mutate-k0smotron-cluster-v1beta1.k0smotron.io,admissionReviewVersions=v1

// ClusterDefaulter is a webhook that sets default values for the Cluster resource.
type ClusterDefaulter struct{}

func (c *ClusterDefaulter) Default(_ context.Context, obj runtime.Object) error {
	kmc, ok := obj.(*km.Cluster)
	if !ok {
		return fmt.Errorf("expected a Cluster object but got %T", obj)
	}

	if kmc.Spec.Version == "" {
		kmc.Spec.Version = km.DefaultK0SVersion
	}

	return nil
}

var _ webhook.CustomDefaulter = &ClusterDefaulter{}

func (c *ClusterDefaulter) SetupK0sControlPlaneWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&km.Cluster{}).
		WithDefaulter(c).
		Complete()
}
