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
	"github.com/stretchr/testify/require"
	apps "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func mustScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(scheme))
	require.NoError(t, apps.AddToScheme(scheme))
	require.NoError(t, batchv1.AddToScheme(scheme))
	return scheme
}

func TestApplyComponentPatches_EmptyPatches_NoChange(t *testing.T) {
	scheme := mustScheme(t)
	svc := &v1.Service{
		TypeMeta: metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{ComponentLabel: "control-plane"},
		},
	}
	err := ApplyComponentPatches(scheme, svc, nil)
	require.NoError(t, err)
	err = ApplyComponentPatches(scheme, svc, []km.ComponentPatch{})
	require.NoError(t, err)
}

func TestApplyComponentPatches_NoMatchingPatch_NoChange(t *testing.T) {
	scheme := mustScheme(t)
	svc := &v1.Service{
		TypeMeta: metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{ComponentLabel: "control-plane"},
		},
		Spec: v1.ServiceSpec{Type: v1.ServiceTypeClusterIP},
	}
	patches := []km.ComponentPatch{
		{
			ResourceType: "StatefulSet",
			Component:    "control-plane",
			Type:         km.JSONPatchType,
			Patch:        `[{"op":"replace","path":"/spec/replicas","value":2}]`,
		},
		{
			ResourceType: "Service",
			Component:    "etcd",
			Type:         km.JSONPatchType,
			Patch:        `[{"op":"replace","path":"/spec/type","value":"NodePort"}]`,
		},
	}
	err := ApplyComponentPatches(scheme, svc, patches)
	require.NoError(t, err)
	assert.Equal(t, v1.ServiceTypeClusterIP, svc.Spec.Type)
}

func TestApplyComponentPatches_JSONPatch_Applied(t *testing.T) {
	scheme := mustScheme(t)
	svc := &v1.Service{
		TypeMeta: metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{ComponentLabel: "control-plane"},
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeClusterIP,
			Ports: []v1.ServicePort{
				{Name: "api", Port: 6443},
			},
		},
	}
	patches := []km.ComponentPatch{
		{
			ResourceType: "Service",
			Component:    "control-plane",
			Type:         km.JSONPatchType,
			Patch:        `[{"op":"add","path":"/metadata/annotations","value":{"custom":"true"}}]`,
		},
	}
	err := ApplyComponentPatches(scheme, svc, patches)
	require.NoError(t, err)
	assert.Equal(t, "true", svc.Annotations["custom"])
}

func TestApplyComponentPatches_MergePatch_Applied(t *testing.T) {
	scheme := mustScheme(t)
	cm := &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{ComponentLabel: "cluster-config"},
		},
		Data: map[string]string{"key": "original"},
	}
	patches := []km.ComponentPatch{
		{
			ResourceType: "ConfigMap",
			Component:    "cluster-config",
			Type:         km.MergePatchType,
			Patch:        `{"data":{"key":"patched","extra":"value"}}`,
		},
	}
	err := ApplyComponentPatches(scheme, cm, patches)
	require.NoError(t, err)
	assert.Equal(t, "patched", cm.Data["key"])
	assert.Equal(t, "value", cm.Data["extra"])
}

func TestApplyComponentPatches_StrategicMergePatch_Applied(t *testing.T) {
	scheme := mustScheme(t)
	sts := &apps.StatefulSet{
		TypeMeta: metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{ComponentLabel: "control-plane"},
		},
		Spec: apps.StatefulSetSpec{
			Replicas:    ptr(int32(1)),
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"app": "k0smotron"}},
			Template:    v1.PodTemplateSpec{},
			ServiceName: "test",
		},
	}
	patches := []km.ComponentPatch{
		{
			ResourceType: "StatefulSet",
			Component:    "control-plane",
			Type:         km.StrategicMergePatchType,
			Patch:        `{"spec":{"replicas":3}}`,
		},
	}
	err := ApplyComponentPatches(scheme, sts, patches)
	require.NoError(t, err)
	require.NotNil(t, sts.Spec.Replicas)
	assert.Equal(t, int32(3), *sts.Spec.Replicas)
}

func TestApplyComponentPatches_InvalidJSONPatch_ReturnsError(t *testing.T) {
	scheme := mustScheme(t)
	svc := &v1.Service{
		TypeMeta: metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{ComponentLabel: "control-plane"},
		},
	}
	patches := []km.ComponentPatch{
		{
			ResourceType: "Service",
			Component:    "control-plane",
			Type:         km.JSONPatchType,
			Patch:        `[{"op":"replace","path":"/nonexistent","value":1}]`,
		},
	}
	err := ApplyComponentPatches(scheme, svc, patches)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apply patch")
}

func TestApplyComponentPatches_MergePatch_YAML_Applied(t *testing.T) {
	scheme := mustScheme(t)
	svc := &v1.Service{
		TypeMeta: metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{ComponentLabel: "control-plane"},
		},
	}
	patches := []km.ComponentPatch{
		{
			ResourceType: "Service",
			Component:    "control-plane",
			Type:         km.MergePatchType,
			Patch: `metadata:
  annotations:
    example.com/managed-by: "k0smotron-customized"
`,
		},
	}
	err := ApplyComponentPatches(scheme, svc, patches)
	require.NoError(t, err)
	assert.Equal(t, "k0smotron-customized", svc.Annotations["example.com/managed-by"])
}

func TestApplyComponentPatches_StrategicMergePatch_YAML_Applied(t *testing.T) {
	scheme := mustScheme(t)
	sts := &apps.StatefulSet{
		TypeMeta: metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{ComponentLabel: "control-plane"},
		},
		Spec: apps.StatefulSetSpec{
			Replicas:    ptr(int32(1)),
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"app": "k0smotron"}},
			Template:    v1.PodTemplateSpec{},
			ServiceName: "test",
		},
	}
	patches := []km.ComponentPatch{
		{
			ResourceType: "StatefulSet",
			Component:    "control-plane",
			Type:         km.StrategicMergePatchType,
			Patch: `spec:
  replicas: 3
`,
		},
	}
	err := ApplyComponentPatches(scheme, sts, patches)
	require.NoError(t, err)
	require.NotNil(t, sts.Spec.Replicas)
	assert.Equal(t, int32(3), *sts.Spec.Replicas)
}

func TestApplyComponentPatches_UnknownPatchType_ReturnsError(t *testing.T) {
	scheme := mustScheme(t)
	svc := &v1.Service{
		TypeMeta: metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test",
			Labels: map[string]string{ComponentLabel: "control-plane"},
		},
	}
	patches := []km.ComponentPatch{
		{
			ResourceType: "Service",
			Component:    "control-plane",
			Type:         km.PatchType("invalid"),
			Patch:        `{}`,
		},
	}
	err := ApplyComponentPatches(scheme, svc, patches)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown patch type")
}

func ptr(i int32) *int32 { return &i }
