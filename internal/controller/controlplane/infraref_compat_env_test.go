//go:build envtest

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

package controlplane

import (
	"fmt"
	"testing"
	"time"

	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// readBack fetches a freshly created object. testEnv reads through the manager's cache, which
// trails the write, so a plain Get is a coin flip that reports NotFound on the loser.
func readBack(t *testing.T, created *unstructured.Unstructured) *unstructured.Unstructured {
	t.Helper()

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(created.GroupVersionKind())

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.NoError(c, testEnv.Get(t.Context(), client.ObjectKeyFromObject(created), got))
	}, 10*time.Second, 100*time.Millisecond, "the created object never became visible")

	return got
}

// A fake client validates against neither the CRD schema nor the webhooks, so only a real API
// server answers this. Unstructured on purpose, a compiled fixture would fail the build instead.
func controlPlaneManifest(namespace string, machineTemplate map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": cpv1beta2.GroupVersion.String(),
		"kind":       "K0sControlPlane",
		"metadata": map[string]any{
			"name":      fmt.Sprintf("kcp-compat-%s", util.RandomString(6)),
			"namespace": namespace,
		},
		"spec": map[string]any{
			"replicas":        int64(1),
			"version":         "v1.30.0+k0s.0",
			"updateStrategy":  string(cpv1beta2.UpdateRecreate),
			"k0sConfigSpec":   map[string]any{},
			"machineTemplate": machineTemplate,
		},
	}}
}

func infraRefFields(name string) map[string]any {
	return map[string]any{
		"apiVersion": "infrastructure.cluster.x-k8s.io/v1beta1",
		"kind":       "GenericInfrastructureMachineTemplate",
		"name":       name,
	}
}

// TestDeprecatedInfrastructureRefStillApplies covers a manifest written against the released API.
// The field moved into machineTemplate.spec, and anything already deployed names it flat.
func TestDeprecatedInfrastructureRefStillApplies(t *testing.T) {
	ns, err := testEnv.CreateNamespace(t.Context(), "test-infraref-compat")
	require.NoError(t, err)

	// testEnv is shared for the whole package run, so these outlive the test. The package context
	// rather than t.Context(), which is already cancelled by the time a cleanup runs.
	t.Cleanup(func() {
		require.NoError(t, testEnv.Cleanup(ctx, ns))
	})

	t.Run("a flat manifest is accepted and migrated onto the nested field", func(t *testing.T) {
		kcp := controlPlaneManifest(ns.Name, map[string]any{
			"infrastructureRef": infraRefFields("infra-flat"),
		})
		require.NoError(t, testEnv.Create(t.Context(), kcp),
			"a manifest written against the released API has to keep working")

		got := readBack(t, kcp)

		nested, found, err := unstructured.NestedString(got.Object, "spec", "machineTemplate", "spec", "infrastructureRef", "name")
		require.NoError(t, err)
		require.True(t, found, "the defaulting webhook has to carry the deprecated field across")
		require.Equal(t, "infra-flat", nested)

		deprecated, found, err := unstructured.NestedString(got.Object, "spec", "machineTemplate", "infrastructureRef", "name")
		require.NoError(t, err)
		require.True(t, found, "removing what the user wrote makes their next apply put it back")
		require.Equal(t, "infra-flat", deprecated)
	})

	t.Run("a nested manifest is accepted and nothing is written backwards", func(t *testing.T) {
		kcp := controlPlaneManifest(ns.Name, map[string]any{
			"spec": map[string]any{"infrastructureRef": infraRefFields("infra-nested")},
		})
		require.NoError(t, testEnv.Create(t.Context(), kcp))

		got := readBack(t, kcp)

		_, found, err := unstructured.NestedMap(got.Object, "spec", "machineTemplate", "infrastructureRef")
		require.NoError(t, err)
		require.False(t, found, "the deprecated field is not something the controller should start setting")
	})

	// A legacy manifest rotates by editing the only field it knows, and filling the nested one
	// once then leaving it alone wedged exactly that. This is the regression, not a nicety.
	t.Run("a rotation through the deprecated field carries across", func(t *testing.T) {
		kcp := controlPlaneManifest(ns.Name, map[string]any{
			"infrastructureRef": infraRefFields("infra-before"),
		})
		require.NoError(t, testEnv.Create(t.Context(), kcp))

		rotated := readBack(t, kcp)
		require.NoError(t, unstructured.SetNestedMap(rotated.Object,
			infraRefFields("infra-after"), "spec", "machineTemplate", "infrastructureRef"))
		require.NoError(t, testEnv.Update(t.Context(), rotated),
			"the only field a legacy manifest knows has to stay writable")

		// Polled on the value rather than on existence, since the object is already in the cache
		// from the create and a plain read hands back the copy from before the update.
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			nested, found, err := unstructured.NestedString(readBack(t, kcp).Object,
				"spec", "machineTemplate", "spec", "infrastructureRef", "name")
			assert.NoError(c, err)
			assert.True(c, found)
			assert.Equal(c, "infra-after", nested,
				"the controller reads the nested field, so a stale one clones the template the user moved off")
		}, 10*time.Second, 100*time.Millisecond)
	})

	t.Run("naming no template at all is refused", func(t *testing.T) {
		kcp := controlPlaneManifest(ns.Name, map[string]any{})

		err := testEnv.Create(t.Context(), kcp)
		require.Error(t, err, "the schema stopped requiring it, so admission has to")
		require.ErrorContains(t, err, "is required")
	})
}
