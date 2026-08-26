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

package controlplane

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/collections"
)

func failureDomainMachine(name, domain string) *clusterv1.Machine {
	return &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       clusterv1.MachineSpec{FailureDomain: domain},
	}
}

// TestNextFailureDomain covers where a new control plane machine is placed. The
// signal that matters is how the up to date machines are spread.
func TestNextFailureDomain(t *testing.T) {
	clusterWithDomains := func(names ...string) *clusterv1.Cluster {
		cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"}}
		for _, name := range names {
			cluster.Status.FailureDomains = append(cluster.Status.FailureDomains,
				clusterv1.FailureDomain{Name: name, ControlPlane: new(true)})
		}

		return cluster
	}

	t.Run("an empty control plane can go anywhere", func(t *testing.T) {
		scope := &controlplane{
			cluster:          clusterWithDomains("fd-a"),
			activeMachines:   collections.Machines{},
			deletedMachines:  collections.Machines{},
			upToDateMachines: collections.Machines{},
		}

		require.Equal(t, "fd-a", nextFailureDomain(context.Background(), scope))
	})

	t.Run("no failure domains means no choice to make", func(t *testing.T) {
		scope := &controlplane{
			cluster:          &clusterv1.Cluster{},
			activeMachines:   collections.Machines{},
			deletedMachines:  collections.Machines{},
			upToDateMachines: collections.Machines{},
		}

		require.Empty(t, nextFailureDomain(context.Background(), scope))
	})

	// A domain holding only a machine on its way out has no up to date machine, so
	// it is where the replacement belongs.
	t.Run("a domain losing its machine is preferred over one already up to date", func(t *testing.T) {
		upToDate := failureDomainMachine("cp-a", "fd-a")
		goingAway := failureDomainMachine("cp-b", "fd-b")

		scope := &controlplane{
			cluster:          clusterWithDomains("fd-a", "fd-b"),
			activeMachines:   collections.FromMachines(upToDate),
			deletedMachines:  collections.FromMachines(goingAway),
			upToDateMachines: collections.FromMachines(upToDate),
		}

		require.Equal(t, "fd-b", nextFailureDomain(context.Background(), scope),
			"counting deleting machines as the priority signal would pick fd-a here")
	})

	// This one pins the machine total rather than the spread, since a domain is
	// still occupied until its machine is really gone.
	t.Run("machines on their way out still occupy their domain", func(t *testing.T) {
		leaving := failureDomainMachine("cp-a", "fd-a")
		alsoLeaving := failureDomainMachine("cp-b", "fd-a")
		outdated := failureDomainMachine("cp-c", "fd-b")

		scope := &controlplane{
			cluster:          clusterWithDomains("fd-a", "fd-b"),
			activeMachines:   collections.FromMachines(outdated),
			deletedMachines:  collections.FromMachines(leaving, alsoLeaving),
			upToDateMachines: collections.Machines{},
		}

		require.Equal(t, "fd-b", nextFailureDomain(context.Background(), scope),
			"counting only the active machines would pick fd-a and put a third machine there")
	})

	// Guards against the two collections being passed the other way round, which
	// the helper cannot detect for us.
	t.Run("the spread signal is not the machine total", func(t *testing.T) {
		outdatedOne := failureDomainMachine("cp-a", "fd-a")
		outdatedTwo := failureDomainMachine("cp-b", "fd-a")
		current := failureDomainMachine("cp-c", "fd-b")

		scope := &controlplane{
			cluster:          clusterWithDomains("fd-a", "fd-b"),
			activeMachines:   collections.FromMachines(outdatedOne, outdatedTwo, current),
			deletedMachines:  collections.Machines{},
			upToDateMachines: collections.FromMachines(current),
		}

		require.Equal(t, "fd-a", nextFailureDomain(context.Background(), scope),
			"fd-a holds no up to date machine, so the next one belongs there")
	})

	// Worker only domains are not eligible, however empty they look.
	t.Run("a domain that is not for control planes is never chosen", func(t *testing.T) {
		occupied := failureDomainMachine("cp-a", "fd-a")

		cluster := clusterWithDomains("fd-a")
		cluster.Status.FailureDomains = append(cluster.Status.FailureDomains,
			clusterv1.FailureDomain{Name: "fd-workers", ControlPlane: new(false)})

		scope := &controlplane{
			cluster:          cluster,
			activeMachines:   collections.FromMachines(occupied),
			deletedMachines:  collections.Machines{},
			upToDateMachines: collections.FromMachines(occupied),
		}

		require.Equal(t, "fd-a", nextFailureDomain(context.Background(), scope),
			"skipping the control plane filter would pick the emptier worker domain")
	})

	t.Run("with the same spread the emptier domain wins", func(t *testing.T) {
		first := failureDomainMachine("cp-a", "fd-a")
		second := failureDomainMachine("cp-b", "fd-a")

		scope := &controlplane{
			cluster:          clusterWithDomains("fd-a", "fd-b"),
			activeMachines:   collections.FromMachines(first, second),
			deletedMachines:  collections.Machines{},
			upToDateMachines: collections.Machines{},
		}

		require.Equal(t, "fd-b", nextFailureDomain(context.Background(), scope))
	})
}
