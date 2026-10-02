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
//nolint:revive
package util

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testClusterConfig(spec map[string]interface{}) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetAPIVersion("k0s.k0sproject.io/v1beta1")
	u.SetKind("ClusterConfig")
	u.SetName("k0s")
	u.SetNamespace("kube-system")
	_ = unstructured.SetNestedMap(u.Object, spec, "spec")
	return u
}

func TestReconcileDynamicConfigStripsControllerOnlyFields(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "k0s.k0sproject.io", Version: "v1beta1", Kind: "ClusterConfig"}
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind("ClusterConfigList"), &unstructured.UnstructuredList{})

	existing := testClusterConfig(map[string]interface{}{
		"storage": map[string]interface{}{"type": "etcd"},
		"network": map[string]interface{}{
			"controlPlaneLoadBalancing": map[string]interface{}{"enabled": true},
			"podCIDR":                   "10.244.0.0/16",
		},
	})
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&existing).Build()

	incoming := testClusterConfig(map[string]interface{}{
		"storage": map[string]interface{}{"type": "kine"},
		"network": map[string]interface{}{
			"controlPlaneLoadBalancing": map[string]interface{}{"enabled": false},
			"podCIDR":                   "10.245.0.0/16",
		},
	})

	require.NoError(t, ReconcileDynamicConfig(context.Background(), fakeClient, incoming))

	var got unstructured.Unstructured
	got.SetAPIVersion("k0s.k0sproject.io/v1beta1")
	got.SetKind("ClusterConfig")
	require.NoError(t, fakeClient.Get(context.Background(), client.ObjectKey{Name: "k0s", Namespace: "kube-system"}, &got))

	// storage and controlPlaneLoadBalancing are removed from the patch payload
	// before it's sent, so a merge patch leaves the live object's existing
	// values for those fields untouched rather than overwriting them with the
	// incoming ones.
	storageType, _, err := unstructured.NestedString(got.Object, "spec", "storage", "type")
	require.NoError(t, err)
	require.Equal(t, "etcd", storageType, "storage should be left untouched, not overwritten with the incoming value")

	cplbEnabled, _, err := unstructured.NestedBool(got.Object, "spec", "network", "controlPlaneLoadBalancing", "enabled")
	require.NoError(t, err)
	require.True(t, cplbEnabled, "controlPlaneLoadBalancing should be left untouched, not overwritten with the incoming value")

	podCIDR, _, err := unstructured.NestedString(got.Object, "spec", "network", "podCIDR")
	require.NoError(t, err)
	require.Equal(t, "10.245.0.0/16", podCIDR, "fields other than the stripped ones should still be applied")
}
