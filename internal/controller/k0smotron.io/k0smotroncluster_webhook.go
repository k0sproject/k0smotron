package k0smotronio

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

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

	return c.ValidateClusterSpec(&kmc.Spec)
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

	return c.ValidateClusterSpec(&kmc.Spec)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Cluster.
func (c ClusterValidator) ValidateDelete(_ context.Context, _ runtime.Object) (warnings admission.Warnings, err error) {
	return nil, nil
}

// ValidateClusterSpec validates the ClusterSpec and returns any warnings or errors.
func (c ClusterValidator) ValidateClusterSpec(kcs *km.ClusterSpec) (warnings admission.Warnings, err error) {
	if kcs.Ingress != nil {
		warn, err := kcs.Ingress.Validate(kcs.Version)
		warnings = append(warnings, warn...)
		if err != nil {
			return nil, err
		}

		if kcs.Ingress.Deploy != nil && *kcs.Ingress.Deploy && len(kcs.Ingress.Annotations) == 0 {
			warnings = append(warnings, "ingress annotations are not set, make sure that the ingress controller supports tls passthrough")
		}
	}

	return warnings, nil
}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the type Cluster.
func (c ClusterDefaulter) Default(_ context.Context, obj runtime.Object) error {
	kmc, ok := obj.(*km.Cluster)
	if !ok {
		return fmt.Errorf("expected a Cluster object but got %T", obj)
	}

	return c.DefaultClusterSpec(&kmc.Spec)
}

// DefaultClusterSpec sets default values for the ClusterSpec.
func (c *ClusterDefaulter) DefaultClusterSpec(kmcSpec *km.ClusterSpec) error {
	if kmcSpec.Replicas == 0 {
		kmcSpec.Replicas = 1
	}

	if kmcSpec.Version == "" {
		kmcSpec.Version = km.DefaultK0SVersion
	} else {
		// Ensure we store the version with "+k0s.". That way we can standardize the k0s version format.
		// Needed sanitization will be done in runtime when building image references and not required
		// by the user.
		kmcSpec.Version = strings.ReplaceAll(kmcSpec.Version, "-k0s.", "+k0s.")
		// Always append the default k0s version if not present.
		if !strings.Contains(kmcSpec.Version, "+k0s.") {
			kmcSpec.Version = fmt.Sprintf("%s+%s", kmcSpec.Version, km.DefaultK0SSuffix)
		}
	}

	if kmcSpec.Service.Type == "" {
		kmcSpec.Service.Type = corev1.ServiceTypeClusterIP
		kmcSpec.Service.APIPort = 30443
		kmcSpec.Service.KonnectivityPort = 30132
	}

	if kmcSpec.Etcd.Image == "" {
		kmcSpec.Etcd.Image = km.DefaultEtcdImage
	}

	if kmcSpec.Ingress != nil {
		if kmcSpec.Ingress.Deploy == nil {
			kmcSpec.Ingress.Deploy = ptr.To(true)
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
