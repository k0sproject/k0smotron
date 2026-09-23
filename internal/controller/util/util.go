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
package util

import (
	"maps"
	"sort"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
)

// ComponentLabel is the well-known Kubernetes recommended label key for identifying
// the component within the architecture (e.g. "etcd", "control-plane").
const ComponentLabel = "app.kubernetes.io/component"

// Component label values for app.kubernetes.io/component (and legacy "component").
// Use these constants so component names are defined in one place.
const (
	ComponentBootstrap     = "bootstrap"
	ComponentClusterConfig = "cluster-config"
	ComponentConfig        = "config"
	ComponentControlPlane  = "control-plane"
	ComponentEntrypoint    = "entrypoint"
	ComponentEtcd          = "etcd"
	ComponentIngress       = "ingress"
	ComponentJointoken     = "jointoken"
	ComponentKubeconfig    = "kubeconfig"
	ComponentMonitoring    = "monitoring"
	ComponentTelemetry     = "telemetry"
	ComponentTunneling     = "tunneling"
)

// DefaultK0smotronClusterLabels returns the default labels (app, cluster).
func DefaultK0smotronClusterLabels(kmc *km.Cluster) map[string]string {
	return map[string]string{
		"app":     "k0smotron",
		"cluster": kmc.Name,
	}
}

// LabelsForK0smotronCluster returns base labels (app, cluster, user labels) with legacy component=cluster.
// Use when building custom label sets or for immutable selectors (StatefulSet, Deployment).
// For component-specific metadata (ConfigMap, Secret, etc.), use LabelsForK0smotronComponent.
func LabelsForK0smotronCluster(kmc *km.Cluster) map[string]string {
	labels := DefaultK0smotronClusterLabels(kmc)
	maps.Copy(labels, kmc.Labels)
	maps.Copy(labels, kmc.Spec.KubeconfigSecretMetadata.Labels)
	labels["component"] = "cluster"
	return labels
}

// LabelsForK0smotronComponent adds app.kubernetes.io/component to base labels.
// The legacy "component" label value from LabelsForK0smotronCluster is preserved.
// Do not use for immutable selectors (StatefulSet, Deployment); use LabelsForK0smotronControlPlane or LabelsForEtcdK0smotronCluster for those.
func LabelsForK0smotronComponent(kmc *km.Cluster, component string) map[string]string {
	labels := LabelsForK0smotronCluster(kmc)
	labels[ComponentLabel] = component
	return labels
}

// LabelsForK0smotronControlPlane returns labels for K0smotron control plane resources (selector-safe, same as main).
func LabelsForK0smotronControlPlane(kmc *km.Cluster) map[string]string {
	labels := LabelsForK0smotronCluster(kmc)
	labels["cluster.x-k8s.io/control-plane"] = "true"
	return labels
}

// LabelsForEtcdK0smotronCluster returns labels for K0smotron etcd resources (selector-safe, same as main).
func LabelsForEtcdK0smotronCluster(kmc *km.Cluster) map[string]string {
	labels := LabelsForK0smotronCluster(kmc)
	labels["component"] = "etcd"
	return labels
}

// AnnotationsForK0smotronCluster returns annotations for K0smotron cluster resources,
// including user-defined annotations from the cluster spec.
func AnnotationsForK0smotronCluster(kmc *km.Cluster) map[string]string {
	if kmc.Annotations == nil {
		kmc.Annotations = make(map[string]string)
	}
	maps.Copy(kmc.Annotations, kmc.Spec.KubeconfigSecretMetadata.Annotations)
	return kmc.Annotations
}

// AddToExistingSans merges original sans list with a new sans slice avoiding duplicated values.
func AddToExistingSans(existing []string, newSan []string) []string {
	uniques := make(map[string]struct{})
	for _, val := range existing {
		uniques[val] = struct{}{}
	}
	for _, val := range newSan {
		uniques[val] = struct{}{}
	}
	finalSans := make([]string, 0, len(uniques))
	for key := range uniques {
		finalSans = append(finalSans, key)
	}

	// Sort the sans to ensure stable output order
	sort.Strings(finalSans)

	return finalSans
}
