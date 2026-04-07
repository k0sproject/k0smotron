/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta2

import "sigs.k8s.io/controller-runtime/pkg/conversion"

var _ conversion.Hub = &K0sWorkerConfig{}
var _ conversion.Hub = &K0sWorkerConfigTemplate{}
var _ conversion.Hub = &K0sWorkerConfigTemplateList{}
var _ conversion.Hub = &K0sControllerConfig{}

// Hub marks K0sWorkerConfigTemplateList as a conversion hub.
func (*K0sWorkerConfigTemplateList) Hub() {}

// Hub marks K0sWorkerConfig as a conversion hub.
func (*K0sWorkerConfig) Hub() {}

// Hub marks K0sControllerConfig as a conversion hub.
func (*K0sControllerConfig) Hub() {}

// Hub marks K0sWorkerConfigTemplate as a conversion hub.
func (*K0sWorkerConfigTemplate) Hub() {}
