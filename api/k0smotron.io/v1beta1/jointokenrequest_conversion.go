package v1beta1

import (
	"fmt"

	v2 "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

var _ conversion.Convertible = &JoinTokenRequest{}

// ConvertTo converts this JoinTokenRequest (v1beta1) to the hub version (v1beta2).
func (jtr *JoinTokenRequest) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*v2.JoinTokenRequest)
	if !ok {
		return fmt.Errorf("expected *v2.JoinTokenRequest, got %T", dstRaw)
	}

	dst.ObjectMeta = jtr.ObjectMeta

	dst.Spec = jtr.Spec

	dst.Status.TokenID = jtr.Status.TokenID
	dst.Status.ClusterUID = jtr.Status.ClusterUID
	dst.SetDeprecatedReconciliationStatus(jtr.Status.ReconciliationStatus)

	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this JoinTokenRequest (v1beta1).
// Conditions have no equivalent in v1beta1 and are silently dropped.
func (jtr *JoinTokenRequest) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*v2.JoinTokenRequest)
	if !ok {
		return fmt.Errorf("expected *v2.JoinTokenRequest, got %T", srcRaw)
	}

	jtr.ObjectMeta = src.ObjectMeta

	jtr.Spec = src.Spec

	jtr.Status.TokenID = src.Status.TokenID
	jtr.Status.ClusterUID = src.Status.ClusterUID
	jtr.Status.ReconciliationStatus = src.GetDeprecatedReconciliationStatus()

	return nil
}
