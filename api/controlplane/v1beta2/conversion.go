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

// Hub marks K0sControlPlane as a conversion hub.
func (*K0sControlPlane) Hub() {}

// Hub marks K0sControlPlaneTemplate as a conversion hub.
func (*K0sControlPlaneTemplate) Hub() {}

// Hub marks K0smotronControlPlane as a conversion hub.
func (*K0smotronControlPlane) Hub() {}

// Hub marks K0smotronControlPlaneTemplate as a conversion hub.
func (*K0smotronControlPlaneTemplate) Hub() {}
