package v1beta2

// Hub marks K0sControlPlane as a conversion hub.
func (*K0sControlPlane) Hub() {}

// Hub marks K0sControlPlaneTemplate as a conversion hub.
func (*K0sControlPlaneTemplate) Hub() {}

// Hub marks K0smotronControlPlane as a conversion hub.
func (*K0smotronControlPlane) Hub() {}

// Hub marks K0smotronControlPlaneTemplate as a conversion hub.
func (*K0smotronControlPlaneTemplate) Hub() {}
