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

package util

import (
	"testing"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDefaultK0smotronClusterLabels(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "mycluster"},
	}
	got := DefaultK0smotronClusterLabels(kmc)
	want := map[string]string{
		"app":     "k0smotron",
		"cluster": "mycluster",
	}
	assert.Equal(t, want, got)
}

func TestLabelsForK0smotronCluster_LegacyComponent(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "mycluster"},
		Spec:       km.ClusterSpec{},
	}
	got := LabelsForK0smotronCluster(kmc)
	assert.Equal(t, "k0smotron", got["app"])
	assert.Equal(t, "mycluster", got["cluster"])
	assert.Equal(t, "cluster", got["component"], "LabelsForK0smotronCluster sets legacy component=cluster for selector compatibility")
	_, hasAppComponent := got[ComponentLabel]
	assert.False(t, hasAppComponent, "LabelsForK0smotronCluster must not set app.kubernetes.io/component")
}

func TestLabelsForK0smotronComponent_AddsAppComponentOnly(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "mycluster"},
		Spec:       km.ClusterSpec{},
	}
	got := LabelsForK0smotronComponent(kmc, ComponentConfig)
	assert.Equal(t, "k0smotron", got["app"])
	assert.Equal(t, "mycluster", got["cluster"])
	assert.Equal(t, "cluster", got["component"], "LabelsForK0smotronComponent preserves legacy component from base")
	assert.Equal(t, ComponentConfig, got[ComponentLabel])
}

func TestLabelsForK0smotronControlPlane(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "mycluster"},
		Spec:       km.ClusterSpec{},
	}
	got := LabelsForK0smotronControlPlane(kmc)
	assert.Equal(t, "k0smotron", got["app"])
	assert.Equal(t, "mycluster", got["cluster"])
	assert.Equal(t, "cluster", got["component"], "selector-safe: legacy component=cluster")
	_, hasAppComponent := got[ComponentLabel]
	assert.False(t, hasAppComponent, "selector does not include app.kubernetes.io/component")
	assert.Equal(t, "true", got["cluster.x-k8s.io/control-plane"])
}

func TestLabelsForEtcdK0smotronCluster(t *testing.T) {
	kmc := &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "mycluster"},
		Spec:       km.ClusterSpec{},
	}
	got := LabelsForEtcdK0smotronCluster(kmc)
	assert.Equal(t, "k0smotron", got["app"])
	assert.Equal(t, "mycluster", got["cluster"])
	assert.Equal(t, ComponentEtcd, got["component"])
	_, hasAppComponent := got[ComponentLabel]
	assert.False(t, hasAppComponent, "selector does not include app.kubernetes.io/component")
}
