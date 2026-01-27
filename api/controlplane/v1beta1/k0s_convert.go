package v1beta1

import (
	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	"github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this version (v1beta2) to the hub version (v1beta2 - self).
func (kcpv1beta1 *K0sControlPlane) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0sControlPlane)
	dst.ObjectMeta = kcpv1beta1.ObjectMeta

	dst.Spec = k0sControlPlaneSpecV1beta1ToV1beta2(kcpv1beta1.Spec)
	return nil
}

func k0sControlPlaneSpecV1beta1ToV1beta2(spec K0sControlPlaneSpec) v1beta2.K0sControlPlaneSpec {
	configSpec := bootstrapv1.ConvertK0sConfigSpecV1beta1ToV1beta2(&spec.K0sConfigSpec)
	return v1beta2.K0sControlPlaneSpec{
		K0sConfigSpec:            *configSpec,
		MachineTemplate:          spec.MachineTemplate,
		Replicas:                 spec.Replicas,
		UpdateStrategy:           spec.UpdateStrategy,
		Version:                  spec.Version,
		KubeconfigSecretMetadata: spec.KubeconfigSecretMetadata,
	}
}

// ConvertFrom converts from the hub version (v1beta2 - self) to this version.
func (kcpv1beta1 *K0sControlPlane) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0sControlPlane)
	kcpv1beta1.ObjectMeta = src.ObjectMeta
	configSpec := bootstrapv1.ConvertK0sConfigSpecV1beta2ToV1beta1(&src.Spec.K0sConfigSpec)
	kcpv1beta1.Spec = K0sControlPlaneSpec{
		K0sConfigSpec:            *configSpec,
		MachineTemplate:          kcpv1beta1.Spec.MachineTemplate,
		Replicas:                 kcpv1beta1.Spec.Replicas,
		UpdateStrategy:           kcpv1beta1.Spec.UpdateStrategy,
		Version:                  kcpv1beta1.Spec.Version,
		KubeconfigSecretMetadata: kcpv1beta1.Spec.KubeconfigSecretMetadata,
	}
	return nil
}

//// ConvertTo converts this version (v1beta2) to the hub version (v1beta2 - self).
//func (kcpv1beta2 *K0sControlPlaneList) ConvertTo(dstRaw conversion.Hub) error {
//	dst := dstRaw.(*v1beta2.K0sControlPlaneList)
//	dst.ListMeta = kcpv1beta2.ListMeta
//	for _, item := range kcpv1beta2.Items {
//		converted := v1beta2.K0sControlPlane{}
//		if err := item.ConvertTo(&converted); err != nil {
//			return err
//		}
//		dst.Items = append(dst.Items, converted)
//	}
//	return nil
//}
//
//// ConvertFrom converts from the hub version (v1beta2 - self) to this version.
//func (kcpv1beta1 *K0sControlPlaneList) ConvertFrom(srcRaw conversion.Hub) error {
//	src := srcRaw.(*v1beta2.K0sControlPlaneList)
//	kcpv1beta1.ListMeta = src.ListMeta
//	for _, item := range src.Items {
//		converted := K0sControlPlane{}
//		if err := converted.ConvertFrom(&item); err != nil {
//			return err
//		}
//		kcpv1beta1.Items = append(kcpv1beta1.Items, converted)
//	}
//	return nil
//}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2 - self).
func (kcpv1beta1 *K0sControlPlaneTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0sControlPlaneTemplate)
	dst.ObjectMeta = kcpv1beta1.ObjectMeta
	configSpec := bootstrapv1.ConvertK0sConfigSpecV1beta1ToV1beta2(&kcpv1beta1.Spec.Template.Spec.K0sConfigSpec)
	dst.Spec = v1beta2.K0sControlPlaneTemplateSpec{
		Template: v1beta2.K0sControlPlaneTemplateResource{
			ObjectMeta: kcpv1beta1.Spec.Template.ObjectMeta,
			Spec: v1beta2.K0sControlPlaneTemplateResourceSpec{
				K0sConfigSpec:   *configSpec,
				MachineTemplate: kcpv1beta1.Spec.Template.Spec.MachineTemplate,
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
	kcpv1beta1.ObjectMeta = src.ObjectMeta
	configSpec := bootstrapv1.ConvertK0sConfigSpecV1beta2ToV1beta1(&src.Spec.Template.Spec.K0sConfigSpec)
	kcpv1beta1.Spec = K0sControlPlaneTemplateSpec{
		Template: K0sControlPlaneTemplateResource{
			ObjectMeta: src.Spec.Template.ObjectMeta,
			Spec: K0sControlPlaneTemplateResourceSpec{
				K0sConfigSpec:   *configSpec,
				MachineTemplate: src.Spec.Template.Spec.MachineTemplate,
				Version:         src.Spec.Template.Spec.Version,
				UpdateStrategy:  src.Spec.Template.Spec.UpdateStrategy,
			},
		},
	}
	return nil
}
