package v1beta2

import "sigs.k8s.io/controller-runtime/pkg/conversion"

var _ conversion.Hub = &Cluster{}
var _ conversion.Hub = &JoinTokenRequest{}

// Hub marks Cluster as a conversion hub.
func (*Cluster) Hub() {}

// Hub marks JoinTokenRequest as a conversion hub.
func (*JoinTokenRequest) Hub() {}
