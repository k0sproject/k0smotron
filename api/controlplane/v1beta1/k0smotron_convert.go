package v1beta1

import (
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this version (v1beta2) to the hub version (v1beta2 - self).
func (kcpv1beta1 *K0smotronControlPlane) ConvertTo(dstRaw conversion.Hub) error {
	//dst := dstRaw.(*v1beta2.K0smotronControlPlane)
	//dst.ObjectMeta = kcpv1beta1.ObjectMeta
	//
	//dst.Spec = (kcpv1beta1.Spec)
	return nil
}

func (kcpv1beta1 *K0smotronControlPlane) ConvertFrom(srcRaw conversion.Hub) error {
	return nil
}
