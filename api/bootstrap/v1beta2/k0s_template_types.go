/*


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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	conflictingFileSourceMsg  = "only one of content or contentFrom may be specified for a single file"
	conflictingContentFromMsg = "only one of contentFrom.secretKeyRef or contentFrom.configMapKeyRef may be specified for a single file"
	pathConflictMsg           = "path property must be unique among all files"
	noContentMsg              = "either content or contentFrom must be specified for a file"
)

func init() {
	SchemeBuilder.Register(&K0sWorkerConfigTemplate{}, &K0sWorkerConfigTemplateList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:conversion-gen=./api/bootstrap/v1beta2
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1beta2"
// +kubebuilder:metadata:labels="cluster.x-k8s.io/provider=bootstrap-k0smotron"

type K0sWorkerConfigTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec K0sWorkerConfigTemplateSpec `json:"spec,omitempty"`
}

func (*K0sWorkerConfigTemplate) Hub() {}

type K0sWorkerConfigTemplateSpec struct {
	Template K0sWorkerConfigTemplateResource `json:"template,omitempty"`
}

type K0sWorkerConfigTemplateResource struct {
	// +kubebuilder:validation:Optional
	ObjectMeta metav1.ObjectMeta   `json:"metadata,omitempty"`
	Spec       K0sWorkerConfigSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

type K0sWorkerConfigTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0sWorkerConfigTemplate `json:"items"`
}

// Hub marks K0sWorkerConfigTemplateList as a conversion hub.
func (*K0sWorkerConfigTemplateList) Hub() {}
