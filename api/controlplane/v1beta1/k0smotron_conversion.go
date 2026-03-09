package v1beta1

import (
	"fmt"

	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	"github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	kmcv1beta1 "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
)

// ConvertTo converts this version (v1beta2) to the hub version (v1beta2 - self).
func (k *K0smotronControlPlane) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0smotronControlPlane)
	dst.ObjectMeta = *k.ObjectMeta.DeepCopy()

	dst.Spec = kmcv1beta1.ClusterSpecToV2(k.Spec)
	dst.Status = v1beta2.K0smotronControlPlaneStatus{
		Initialization: v1beta2.Initialization{
			ControlPlaneInitialized: &k.Status.Initialized,
		},
		ExternalManagedControlPlane: k.Status.ExternalManagedControlPlane,
		Version:                     k.Status.Version,
		Replicas:                    ptr.To(k.Status.Replicas),
		UpToDateReplicas:            ptr.To(k.Status.UpdatedReplicas),
		ReadyReplicas:               ptr.To(k.Status.ReadyReplicas),
		AvailableReplicas:           ptr.To(k.Status.Replicas - k.Status.UnavailableReplicas),
		Selector:                    k.Status.Selector,
		Conditions:                  k.Status.Conditions,
	}
	return nil
}

// ConvertTo converts this K0smotronControlPlaneTemplate (v1beta1) to the hub version (v1beta2).
func (k *K0smotronControlPlaneTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0smotronControlPlaneTemplate)
	dst.ObjectMeta = *k.ObjectMeta.DeepCopy()
	dst.Spec = v1beta2.K0smotronControlPlaneTemplateSpec{
		Template: v1beta2.K0smotronControlPlaneTemplateResource{
			ObjectMeta: k.Spec.Template.ObjectMeta,
			Spec:       kmcv1beta1.ClusterSpecToV2(k.Spec.Template.Spec),
		},
	}
	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this K0smotronControlPlaneTemplate (v1beta1).
func (k *K0smotronControlPlaneTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0smotronControlPlaneTemplate)
	k.ObjectMeta = *src.ObjectMeta.DeepCopy()
	spec, err := kmcv1beta1.ClusterSpecFromV2(src.Spec.Template.Spec)
	if err != nil {
		return err
	}
	k.Spec = K0smotronControlPlaneTemplateSpec{
		Template: K0smotronControlPlaneTemplateResource{
			ObjectMeta: src.Spec.Template.ObjectMeta,
			Spec:       spec,
		},
	}
	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this version (v1beta1).
func (k *K0smotronControlPlane) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0smotronControlPlane)
	k.ObjectMeta = *src.ObjectMeta.DeepCopy()
	v1beta1Spec, err := kmcv1beta1.ClusterSpecFromV2(src.Spec)
	if err != nil {
		return fmt.Errorf("failed to convert K0smotronControlPlane.Spec: %w", err)
	}
	k.Spec = v1beta1Spec
	k.Status = K0smotronControlPlaneStatus{
		Ready:       ptr.Deref(src.Status.ReadyReplicas, 0) > 0,
		Initialized: ptr.Deref(src.Status.Initialization.ControlPlaneInitialized, false),
		Initialization: Initialization{
			ControlPlaneInitialized: ptr.Deref(src.Status.Initialization.ControlPlaneInitialized, false),
		},
		ExternalManagedControlPlane: src.Status.ExternalManagedControlPlane,
		Version:                     src.Status.Version,
		Replicas:                    ptr.Deref(src.Status.Replicas, 0),
		UpdatedReplicas:             ptr.Deref(src.Status.UpToDateReplicas, 0),
		ReadyReplicas:               ptr.Deref(src.Status.ReadyReplicas, 0),
		UnavailableReplicas:         ptr.Deref(src.Status.Replicas, 0) - ptr.Deref(src.Status.AvailableReplicas, 0),
		Selector:                    src.Status.Selector,
		Conditions:                  src.Status.Conditions,
	}
	return nil
}
