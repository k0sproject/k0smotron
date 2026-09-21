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

package infrastructure

import (
	"testing"

	infrav1beta2 "github.com/k0sproject/k0smotron/v2/api/infrastructure/v1beta2"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Unstructured so that dropping the Status field fails an assertion rather than the build.
// Converted from Create rather than read back, because the cache lags and the read flakes.
func newTemplate(t *testing.T, ns, name string) *unstructured.Unstructured {
	t.Helper()

	tmpl := &infrav1beta2.RemoteMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: infrav1beta2.RemoteMachineTemplateSpec{
			Template: infrav1beta2.RemoteMachineTemplateResource{
				Spec: infrav1beta2.RemoteMachineTemplateResourceSpec{Pool: "a-pool"},
			},
		},
	}
	require.NoError(t, testEnv.Create(t.Context(), tmpl))

	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tmpl)
	require.NoError(t, err)

	u := &unstructured.Unstructured{Object: raw}
	u.SetGroupVersionKind(infrav1beta2.GroupVersion.WithKind("RemoteMachineTemplate"))

	return u
}

func TestRemoteMachineTemplateStatusPersists(t *testing.T) {
	ns, err := testEnv.CreateNamespace(t.Context(), "test-machine-template-status")
	require.NoError(t, err)

	u := newTemplate(t, ns.Name, "pool-template")
	require.NoError(t, unstructured.SetNestedMap(u.Object, map[string]any{
		"capacity": map[string]any{"cpu": "4", "memory": "8Gi"},
		"nodeInfo": map[string]any{"architecture": "arm64", "operatingSystem": "linux"},
	}, "status"))
	require.NoError(t, testEnv.Status().Update(t.Context(), u))

	// Uncached, since the status write lands on the API server and the cache trails it.
	got := &infrav1beta2.RemoteMachineTemplate{}
	require.NoError(t, testEnv.GetAPIReader().Get(t.Context(),
		client.ObjectKey{Namespace: ns.Name, Name: "pool-template"}, got))

	// The type declared a status subresource with no schema behind it, so the endpoint
	// answered and everything written here was pruned on the way in.
	require.Equal(t, "4", got.Status.Capacity.Cpu().String())
	require.Equal(t, "8Gi", got.Status.Capacity.Memory().String())
	require.Equal(t, infrav1beta2.ArchitectureArm64, got.Status.NodeInfo.Architecture)
	require.Equal(t, "linux", got.Status.NodeInfo.OperatingSystem)
}

// The enum is the only thing stopping a typo reaching the autoscaler.
func TestRemoteMachineTemplateRejectsUnknownArchitecture(t *testing.T) {
	ns, err := testEnv.CreateNamespace(t.Context(), "test-machine-template-arch")
	require.NoError(t, err)

	u := newTemplate(t, ns.Name, "pool-template")
	require.NoError(t, unstructured.SetNestedMap(u.Object, map[string]any{
		"nodeInfo": map[string]any{"architecture": "sparc64"},
	}, "status"))

	require.ErrorContains(t, testEnv.Status().Update(t.Context(), u), "sparc64")
}
