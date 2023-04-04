/*
Copyright 2023.

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

package v1beta1

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// K0smotronClusterSpec defines the desired state of K0smotronCluster
type K0smotronClusterSpec struct {
	// K0sImage defines the k0s image to be deployed. If empty k0smotron
	// will pick it automatically. Must not include the image tag.
	//+kubebuilder:default=k0sproject/k0s
	K0sImage string `json:"k0sImage,omitempty"`
	// K0sVersion defines the k0s version to be deployed. If empty k0smotron
	// will pick it automatically.
	//+kubebuilder:validation:Optional
	K0sVersion string `json:"k0sVersion,omitempty"`
	// APIPort defines the kubernetes API port. If empty k0smotron
	// will pick it automatically.
	//+kubebuilder:validation:Optional
	APIPort int `json:"apiPort,omitempty"`
	// KonnectivityPort defines the konnectivity port. If empty k0smotron
	// will pick it automatically.
	//+kubebuilder:validation:Optional
	KonnectivityPort int `json:"konnectivityPort,omitempty"`
	// Persistence defines the persistence configuration. If empty k0smotron
	// will use emptyDir as a volume.
	//+kubebuilder:validation:Optional
	Persistence PersistenceSpec `json:"persistence,omitempty"`
}

// K0smotronClusterStatus defines the observed state of K0smotronCluster
type K0smotronClusterStatus struct {
	ReconciliationStatus string `json:"reconciliationStatus"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=kmc

// K0smotronCluster is the Schema for the k0smotronclusters API
type K0smotronCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   K0smotronClusterSpec   `json:"spec,omitempty"`
	Status K0smotronClusterStatus `json:"status"`
}

//+kubebuilder:object:root=true

// K0smotronClusterList contains a list of K0smotronCluster
type K0smotronClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []K0smotronCluster `json:"items"`
}

type PersistenceSpec struct {
	//+kubebuilder:validation:Enum:emptyDir;hostPath;pvc
	//+kubebuilder:default:=emptyDir
	Type string `json:"type"`
	// PersistentVolumeClaim defines the PVC configuration. Will be used as is in case of .spec.persistence.type is pvc.
	//+kubebuilder:validation:Optional
	PersistentVolumeClaim v1.PersistentVolumeClaim `json:"persistentVolumeClaim,omitempty"`
	// HostPath defines the host path configuration. Will be used as is in case of .spec.persistence.type is hostPath.
	//+kubebuilder:validation:Optional
	HostPath string `json:"hostPath,omitempty"`
}

func init() {
	SchemeBuilder.Register(&K0smotronCluster{}, &K0smotronClusterList{})
}

func (kmc *K0smotronCluster) GetDeploymentName() string {
	return fmt.Sprintf("kmc-%s", kmc.Name)
}

func (kmc *K0smotronCluster) GetConfigMapName() string {
	return fmt.Sprintf("kmc-%s-config", kmc.Name)
}

func (kmc *K0smotronCluster) GetNodePortName() string {
	return fmt.Sprintf("kmc-%s-nodeport", kmc.Name)
}

func (kmc *K0smotronCluster) GetVolumeName() string {
	return fmt.Sprintf("kmc-%s", kmc.Name)
}
