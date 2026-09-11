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

package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// pagedNodes serves the given pages in order and records the options it was called
// with, so a test can see both the page size and the continue token.
func pagedNodes(pages ...[]corev1.Node) (listNodes, *[]metav1.ListOptions) {
	seen := &[]metav1.ListOptions{}
	call := 0

	return func(_ context.Context, opts metav1.ListOptions) (*corev1.NodeList, error) {
		*seen = append(*seen, opts)

		list := &corev1.NodeList{Items: pages[call]}
		if call < len(pages)-1 {
			list.Continue = "next"
		}
		call++

		return list, nil
	}, seen
}

func node(name string, mutate func(*corev1.Node)) corev1.Node {
	n := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if mutate != nil {
		mutate(&n)
	}

	return n
}

func machineWithProviderID(name, providerID string, addresses ...string) *clusterv1.Machine {
	m := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       clusterv1.MachineSpec{ProviderID: providerID},
	}
	for _, a := range addresses {
		m.Status.Addresses = append(m.Status.Addresses, clusterv1.MachineAddress{Address: a})
	}

	return m
}

func TestNodeForMachineMatching(t *testing.T) {
	machine := machineWithProviderID("machine1", "test://1", "10.0.0.1")

	t.Run("by name", func(t *testing.T) {
		list, _ := pagedNodes([]corev1.Node{node("other", nil), node("machine1", nil)})

		found, providerIDSet, err := nodeForMachine(context.Background(), list, machine)
		require.NoError(t, err)
		require.False(t, providerIDSet)
		require.NotNil(t, found)
		require.Equal(t, "machine1", found.Name)
	})

	t.Run("by machine name label", func(t *testing.T) {
		list, _ := pagedNodes([]corev1.Node{node("node1", func(n *corev1.Node) {
			n.Labels = map[string]string{machineNameNodeLabel: "machine1"}
		})})

		found, _, err := nodeForMachine(context.Background(), list, machine)
		require.NoError(t, err)
		require.NotNil(t, found)
		require.Equal(t, "node1", found.Name)
	})

	t.Run("by address", func(t *testing.T) {
		list, _ := pagedNodes([]corev1.Node{node("node1", func(n *corev1.Node) {
			n.Status.Addresses = []corev1.NodeAddress{{Address: "10.0.0.1"}}
		})})

		found, _, err := nodeForMachine(context.Background(), list, machine)
		require.NoError(t, err)
		require.NotNil(t, found)
		require.Equal(t, "node1", found.Name)
	})

	t.Run("a loopback address is not a match", func(t *testing.T) {
		loopback := machineWithProviderID("machine1", "test://1", "127.0.0.1")
		list, _ := pagedNodes([]corev1.Node{node("node1", func(n *corev1.Node) {
			n.Status.Addresses = []corev1.NodeAddress{{Address: "127.0.0.1"}}
		})})

		found, providerIDSet, err := nodeForMachine(context.Background(), list, loopback)
		require.NoError(t, err)
		require.False(t, providerIDSet)
		require.Nil(t, found)
	})

	t.Run("the providerID already being set reports nothing to do", func(t *testing.T) {
		list, _ := pagedNodes([]corev1.Node{node("node1", func(n *corev1.Node) {
			n.Spec.ProviderID = "test://1"
		}), node("machine1", nil)})

		found, providerIDSet, err := nodeForMachine(context.Background(), list, machine)
		require.NoError(t, err)
		require.True(t, providerIDSet)
		require.Nil(t, found)
	})

	t.Run("the first match wins and the rest are left alone", func(t *testing.T) {
		// Both match by address, so scanning on would hand back the later one.
		byAddress := func(n *corev1.Node) {
			n.Status.Addresses = []corev1.NodeAddress{{Address: "10.0.0.1"}}
		}
		list, seen := pagedNodes(
			[]corev1.Node{node("node1", byAddress), node("node2", byAddress)},
			[]corev1.Node{node("node3", byAddress)},
		)

		found, _, err := nodeForMachine(context.Background(), list, machine)
		require.NoError(t, err)
		require.NotNil(t, found)
		require.Equal(t, "node1", found.Name)
		require.Len(t, *seen, 1, "a match on the first page must not ask for the second")
	})
}

func TestNodeForMachinePaging(t *testing.T) {
	machine := machineWithProviderID("machine1", "test://1")

	t.Run("the continue token is followed", func(t *testing.T) {
		list, seen := pagedNodes(
			[]corev1.Node{node("node1", nil)},
			[]corev1.Node{node("node2", nil)},
			[]corev1.Node{node("machine1", nil)},
		)

		found, _, err := nodeForMachine(context.Background(), list, machine)
		require.NoError(t, err)
		require.NotNil(t, found)
		require.Equal(t, "machine1", found.Name)

		require.Len(t, *seen, 3)
		require.Equal(t, int64(nodeListPageSize), (*seen)[0].Limit, "the list has to be bounded")
		require.Empty(t, (*seen)[0].Continue)
		require.Equal(t, "next", (*seen)[1].Continue)
		require.Equal(t, "next", (*seen)[2].Continue)
	})

	t.Run("no match anywhere", func(t *testing.T) {
		list, seen := pagedNodes([]corev1.Node{node("node1", nil)}, []corev1.Node{node("node2", nil)})

		found, providerIDSet, err := nodeForMachine(context.Background(), list, machine)
		require.NoError(t, err)
		require.False(t, providerIDSet)
		require.Nil(t, found)
		require.Len(t, *seen, 2, "every page has to be read before giving up")
	})

	t.Run("a failed page is reported", func(t *testing.T) {
		list := func(_ context.Context, _ metav1.ListOptions) (*corev1.NodeList, error) {
			return nil, errors.New("boom")
		}

		_, _, err := nodeForMachine(context.Background(), list, machine)
		require.ErrorContains(t, err, "boom")
	})
}
