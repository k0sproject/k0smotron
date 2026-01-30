package v1beta1

import (
	"github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this version (v1beta2) to the hub version (v1beta2 - self).
func (kcpv1beta1 *K0smotronControlPlane) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0smotronControlPlane)
	dst.ObjectMeta = *kcpv1beta1.ObjectMeta.DeepCopy()

	dst.Spec = (kcpv1beta1.Spec)
	dst.Status = v1beta2.K0smotronControlPlaneStatus{
		Ready:       kcpv1beta1.Status.Ready,
		Initialized: kcpv1beta1.Status.Initialized,
		Initialization: v1beta2.Initialization{
			ControlPlaneInitialized: kcpv1beta1.Status.Initialized,
		},
		ExternalManagedControlPlane: kcpv1beta1.Status.ExternalManagedControlPlane,
		Version:                     kcpv1beta1.Status.Version,
		Replicas:                    kcpv1beta1.Status.Replicas,
		UpdatedReplicas:             kcpv1beta1.Status.UpdatedReplicas,
		ReadyReplicas:               kcpv1beta1.Status.ReadyReplicas,
		UnavailableReplicas:         kcpv1beta1.Status.UnavailableReplicas,
		Selector:                    kcpv1beta1.Status.Selector,
		Conditions:                  kcpv1beta1.Status.Conditions,
	}
	return nil
}

func (kcpv1beta1 *K0smotronControlPlane) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0smotronControlPlane)
	kcpv1beta1.ObjectMeta = *src.ObjectMeta.DeepCopy()
	kcpv1beta1.Spec = (src.Spec)
	kcpv1beta1.Status = K0smotronControlPlaneStatus{
		Ready:       src.Status.Ready,
		Initialized: src.Status.Initialization.ControlPlaneInitialized,
		Initialization: Initialization{
			ControlPlaneInitialized: src.Status.Initialization.ControlPlaneInitialized,
		},
		ExternalManagedControlPlane: src.Status.ExternalManagedControlPlane,
		Version:                     src.Status.Version,
		Replicas:                    src.Status.Replicas,
		UpdatedReplicas:             src.Status.UpdatedReplicas,
		ReadyReplicas:               src.Status.ReadyReplicas,
		UnavailableReplicas:         src.Status.UnavailableReplicas,
		Selector:                    src.Status.Selector,
		Conditions:                  src.Status.Conditions,
	}
	return nil
}
