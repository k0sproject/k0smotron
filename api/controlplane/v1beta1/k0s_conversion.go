package v1beta1

import (
	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

var _ conversion.Convertible = &K0sControlPlane{}
var _ conversion.Convertible = &K0sControlPlaneTemplate{}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2).
func (kcpv1beta1 *K0sControlPlane) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0sControlPlane)
	dst.ObjectMeta = *kcpv1beta1.ObjectMeta.DeepCopy()

	dst.Spec = k0sControlPlaneSpecV1beta1ToV1beta2(kcpv1beta1.Spec)
	dst.Status = v1beta2.K0sControlPlaneStatus{
		Ready:                       kcpv1beta1.Status.Ready,
		Initialization:              v1beta2.Initialization{ControlPlaneInitialized: kcpv1beta1.Status.Initialized},
		ExternalManagedControlPlane: kcpv1beta1.Status.ExternalManagedControlPlane,
		Replicas:                    kcpv1beta1.Status.Replicas,
		Version:                     kcpv1beta1.Status.Version,
		Selector:                    kcpv1beta1.Status.Selector,
		UnavailableReplicas:         kcpv1beta1.Status.UnavailableReplicas,
		ReadyReplicas:               kcpv1beta1.Status.ReadyReplicas,
		UpdatedReplicas:             kcpv1beta1.Status.UpdatedReplicas,
		Conditions:                  kcpv1beta1.Status.Conditions,
	}
	return nil
}

func k0sControlPlaneSpecV1beta1ToV1beta2(spec K0sControlPlaneSpec) v1beta2.K0sControlPlaneSpec {
	configSpec := bootstrapv1.ConvertK0sConfigSpecV1beta1ToV1beta2(&spec.K0sConfigSpec)
	return v1beta2.K0sControlPlaneSpec{
		K0sConfigSpec:            *configSpec.DeepCopy(),
		MachineTemplate:          spec.MachineTemplate.DeepCopy(),
		Replicas:                 spec.Replicas,
		UpdateStrategy:           spec.UpdateStrategy,
		Version:                  spec.Version,
		KubeconfigSecretMetadata: spec.KubeconfigSecretMetadata,
	}
}

// ConvertFrom converts from the hub version (v1beta2) to this version.
func (kcpv1beta1 *K0sControlPlane) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0sControlPlane)
	kcpv1beta1.ObjectMeta = *src.ObjectMeta.DeepCopy()

	configSpec := bootstrapv1.ConvertK0sConfigSpecV1beta2ToV1beta1(&src.Spec.K0sConfigSpec)
	kcpv1beta1.Spec = K0sControlPlaneSpec{
		K0sConfigSpec:            *configSpec.DeepCopy(),
		MachineTemplate:          src.Spec.MachineTemplate.DeepCopy(),
		Replicas:                 src.Spec.Replicas,
		UpdateStrategy:           src.Spec.UpdateStrategy,
		Version:                  src.Spec.Version,
		KubeconfigSecretMetadata: src.Spec.KubeconfigSecretMetadata,
	}
	kcpv1beta1.Status = K0sControlPlaneStatus{
		Ready:       src.Status.Ready,
		Initialized: src.Status.Initialization.ControlPlaneInitialized,
		Initialization: Initialization{
			ControlPlaneInitialized: src.Status.Initialization.ControlPlaneInitialized,
		},
		ExternalManagedControlPlane: src.Status.ExternalManagedControlPlane,
		Replicas:                    src.Status.Replicas,
		Version:                     src.Status.Version,
		Selector:                    src.Status.Selector,
		UnavailableReplicas:         src.Status.UnavailableReplicas,
		ReadyReplicas:               src.Status.ReadyReplicas,
		UpdatedReplicas:             src.Status.UpdatedReplicas,
		Conditions:                  src.Status.Conditions,
	}

	return nil
}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2 - self).
func (kcpv1beta1 *K0sControlPlaneTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0sControlPlaneTemplate)
	dst.ObjectMeta = *kcpv1beta1.ObjectMeta.DeepCopy()

	configSpec := bootstrapv1.ConvertK0sConfigSpecV1beta1ToV1beta2(&kcpv1beta1.Spec.Template.Spec.K0sConfigSpec)
	dst.Spec = v1beta2.K0sControlPlaneTemplateSpec{
		Template: v1beta2.K0sControlPlaneTemplateResource{
			ObjectMeta: kcpv1beta1.Spec.Template.ObjectMeta,
			Spec: v1beta2.K0sControlPlaneTemplateResourceSpec{
				K0sConfigSpec:   *configSpec.DeepCopy(),
				MachineTemplate: kcpv1beta1.Spec.Template.Spec.MachineTemplate.DeepCopy(),
				Version:         kcpv1beta1.Spec.Template.Spec.Version,
				UpdateStrategy:  kcpv1beta1.Spec.Template.Spec.UpdateStrategy,
			},
		},
	}
	return nil
}

// ConvertFrom converts from the hub version (v1beta2 - self) to this version.
func (kcpv1beta1 *K0sControlPlaneTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0sControlPlaneTemplate)
	kcpv1beta1.ObjectMeta = *src.ObjectMeta.DeepCopy()
	configSpec := bootstrapv1.ConvertK0sConfigSpecV1beta2ToV1beta1(&src.Spec.Template.Spec.K0sConfigSpec)
	kcpv1beta1.Spec = K0sControlPlaneTemplateSpec{
		Template: K0sControlPlaneTemplateResource{
			ObjectMeta: src.Spec.Template.ObjectMeta,
			Spec: K0sControlPlaneTemplateResourceSpec{
				K0sConfigSpec:   *configSpec.DeepCopy(),
				MachineTemplate: src.Spec.Template.Spec.MachineTemplate.DeepCopy(),
				Version:         src.Spec.Template.Spec.Version,
				UpdateStrategy:  src.Spec.Template.Spec.UpdateStrategy,
			},
		},
	}
	return nil
}
