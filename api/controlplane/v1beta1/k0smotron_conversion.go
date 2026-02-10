package v1beta1

import (
	"github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this version (v1beta2) to the hub version (v1beta2 - self).
func (k *K0smotronControlPlane) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.K0smotronControlPlane)
	dst.ObjectMeta = *k.ObjectMeta.DeepCopy()

	dst.Spec = k.Spec
	dst.Status = v1beta2.K0smotronControlPlaneStatus{
		Initialization: v1beta2.Initialization{
			ControlPlaneInitialized: k.Status.Initialized,
		},
		ExternalManagedControlPlane: k.Status.ExternalManagedControlPlane,
		Version:                     k.Status.Version,
		Replicas:                    k.Status.Replicas,
		UpToDateReplicas:            &k.Status.UpdatedReplicas,
		ReadyReplicas:               k.Status.ReadyReplicas,
		AvailableReplicas:           ptr.To(k.Status.Replicas - k.Status.UnavailableReplicas),
		Selector:                    k.Status.Selector,
		Conditions:                  k.Status.Conditions,
	}
	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this version (v1beta1).
func (k *K0smotronControlPlane) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.K0smotronControlPlane)
	k.ObjectMeta = *src.ObjectMeta.DeepCopy()
	k.Spec = (src.Spec)
	k.Status = K0smotronControlPlaneStatus{
		Ready:       src.Status.ReadyReplicas > 0,
		Initialized: src.Status.Initialization.ControlPlaneInitialized,
		Initialization: Initialization{
			ControlPlaneInitialized: src.Status.Initialization.ControlPlaneInitialized,
		},
		ExternalManagedControlPlane: src.Status.ExternalManagedControlPlane,
		Version:                     src.Status.Version,
		Replicas:                    src.Status.Replicas,
		UpdatedReplicas:             *src.Status.UpToDateReplicas,
		ReadyReplicas:               src.Status.ReadyReplicas,
		UnavailableReplicas:         src.Status.Replicas - *src.Status.AvailableReplicas,
		Selector:                    src.Status.Selector,
		Conditions:                  src.Status.Conditions,
	}
	return nil
}
