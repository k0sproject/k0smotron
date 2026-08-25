//go:build !envtest

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
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructure "github.com/k0sproject/k0smotron/v2/api/infrastructure/v1beta2"
)

// TestMergedMap covers a RemoteMachine authored without labels or annotations,
// which is the standalone path where the destination map is nil.
func TestMergedMap(t *testing.T) {
	for _, tc := range []struct {
		name string
		dst  map[string]string
		src  map[string]string
		want map[string]string
	}{
		{
			name: "a nil destination takes the source instead of panicking",
			src:  map[string]string{"a": "1"},
			want: map[string]string{"a": "1"},
		},
		{
			name: "nothing to copy leaves a nil destination alone",
			want: nil,
		},
		{
			name: "nothing to copy leaves an existing destination alone",
			dst:  map[string]string{"a": "1"},
			want: map[string]string{"a": "1"},
		},
		{
			name: "the source wins on a shared key",
			dst:  map[string]string{"a": "1", "b": "2"},
			src:  map[string]string{"a": "9"},
			want: map[string]string{"a": "9", "b": "2"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, mergedMap(tc.dst, tc.src))
		})
	}

	t.Run("the result never shares storage with the source", func(t *testing.T) {
		src := map[string]string{"a": "1"}

		got := mergedMap(nil, src)
		got["b"] = "2"

		require.Equal(t, map[string]string{"a": "1"}, src,
			"handing back the source would let a later write reach the pooled machine")
	})
}

// TestReservePooledMachineCopiesMetadataOntoBareRemoteMachine covers a hand
// authored RemoteMachine, which carries no labels or annotations to copy into.
func TestReservePooledMachineCopiesMetadataOntoBareRemoteMachine(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, infrastructure.AddToScheme(scheme))

	pooled := &infrastructure.PooledRemoteMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pooled",
			Namespace:   "default",
			Labels:      map[string]string{"pool": "a"},
			Annotations: map[string]string{"note": "from the pool"},
		},
		Spec: infrastructure.PooledRemoteMachineSpec{
			Pool: "a",
			Machine: infrastructure.PooledMachineSpec{
				Address: "10.0.0.1",
				Port:    22,
				User:    "root",
			},
		},
	}

	// A RemoteMachine created by hand rather than by CAPI, so both metadata maps
	// are nil.
	rm := &infrastructure.RemoteMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "rm", Namespace: "default"},
		Spec:       infrastructure.RemoteMachineSpec{Pool: "a"},
	}
	require.Nil(t, rm.Labels, "the fixture must have no labels or this proves nothing")
	require.Nil(t, rm.Annotations, "the fixture must have no annotations or this proves nothing")

	c := &RemoteMachineController{
		Client: fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(pooled, rm).WithStatusSubresource(pooled).Build(),
	}

	require.NoError(t, c.reservePooledMachineAndPopulateRemoteMachine(context.Background(), rm))

	require.Equal(t, "10.0.0.1", rm.Spec.Address)
	require.Equal(t, map[string]string{"pool": "a"}, rm.Labels)
	require.Equal(t, map[string]string{"note": "from the pool"}, rm.Annotations)
}
