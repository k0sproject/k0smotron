package v1beta2

import "sigs.k8s.io/controller-runtime/pkg/conversion"

var _ conversion.Hub = &K0sWorkerConfig{}
var _ conversion.Hub = &K0sWorkerConfigList{}
var _ conversion.Hub = &K0sWorkerConfigTemplate{}
var _ conversion.Hub = &K0sWorkerConfigTemplateList{}
var _ conversion.Hub = &K0sControllerConfig{}
var _ conversion.Hub = &K0sControllerConfigList{}

// Hub marks K0sWorkerConfigTemplateList as a conversion hub.
func (*K0sWorkerConfigTemplateList) Hub() {}

// Hub marks K0sWorkerConfig as a conversion hub.
func (*K0sWorkerConfig) Hub() {}

// Hub marks K0sWorkerConfigList as a conversion hub.
func (*K0sWorkerConfigList) Hub() {}

// Hub marks K0sControllerConfig as a conversion hub.
func (*K0sControllerConfig) Hub() {}

// Hub marks K0sControllerConfigList as a conversion hub.
func (*K0sControllerConfigList) Hub() {}
