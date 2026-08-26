//go:build !envtest

/*
Copyright 2025.

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
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	"github.com/stretchr/testify/require"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
)

// TestReconcileInplaceK0sVersionUpdateWhenUnavailable covers the gate that runs
// before any workload cluster call, so no client is needed here.
func TestReconcileInplaceK0sVersionUpdateWhenUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name         string
		strategy     cpv1beta2.UpdateStrategy
		onlyVersion  bool
		wantRequeue  bool
		wantedReason string
	}{
		{
			name:         "an in place update in flight must hold the machines still",
			strategy:     cpv1beta2.UpdateInPlace,
			onlyVersion:  true,
			wantRequeue:  true,
			wantedReason: "scaling here would recreate the machines the update is upgrading where they are",
		},
		{
			name:         "with nothing to update the scaling logic still runs",
			strategy:     cpv1beta2.UpdateInPlace,
			wantRequeue:  false,
			wantedReason: "bring up needs the scaling logic to create the first machines",
		},
		{
			name:         "a recreating strategy is expected to replace machines",
			strategy:     cpv1beta2.UpdateRecreate,
			onlyVersion:  true,
			wantRequeue:  false,
			wantedReason: "recreation is what the user asked for",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kcp := &cpv1beta2.K0sControlPlane{
				Spec: cpv1beta2.K0sControlPlaneSpec{UpdateStrategy: tc.strategy},
			}
			conditions.Set(kcp, metav1.Condition{
				Type:   string(cpv1beta2.ControlPlaneAvailableCondition),
				Status: metav1.ConditionFalse,
				Reason: cpv1beta2.ControlPlaneNotAvailableReason,
			})

			scope := &controlplane{
				kcp:                                kcp,
				cluster:                            &clusterv1.Cluster{},
				hasMachinesWithOnlyVersionOutdated: tc.onlyVersion,
			}

			res, err := (&K0sController{}).reconcileInplaceK0sVersionUpdate(context.Background(), scope)

			require.NoError(t, err)
			require.Equal(t, tc.wantRequeue, !res.IsZero(), tc.wantedReason)
		})
	}
}

// TestReconcileInplaceK0sVersionUpdateHoldsOnlyTheRollout covers the gate letting
// through anything that changes the machine count, so nothing can be livelocked.
func TestReconcileInplaceK0sVersionUpdateHoldsOnlyTheRollout(t *testing.T) {
	newScope := func(active int, replicas int32) *controlplane {
		kcp := &cpv1beta2.K0sControlPlane{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: cpv1beta2.K0sControlPlaneSpec{
				UpdateStrategy: cpv1beta2.UpdateInPlace,
				Replicas:       replicas,
			},
		}
		conditions.Set(kcp, metav1.Condition{
			Type:   string(cpv1beta2.ControlPlaneAvailableCondition),
			Status: metav1.ConditionFalse,
			Reason: cpv1beta2.ControlPlaneNotAvailableReason,
		})

		machines := collections.Machines{}
		for i := range active {
			machines.Insert(&clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("cp-%d", i),
				Namespace: "default",
			}})
		}

		return &controlplane{
			kcp:                                kcp,
			cluster:                            &clusterv1.Cluster{},
			hasMachinesWithOnlyVersionOutdated: true,
			activeMachines:                     machines,
			upToDateMachines:                   collections.Machines{},
			deletedMachines:                    collections.Machines{},
		}
	}

	t.Run("a rollout with the right machine count is held", func(t *testing.T) {
		res, err := (&K0sController{}).reconcileInplaceK0sVersionUpdate(context.Background(), newScope(3, 3))

		require.NoError(t, err)
		require.False(t, res.IsZero(),
			"falling through would replace machines the update should upgrade in place")
	})

	t.Run("being short of machines is let through", func(t *testing.T) {
		// Remediation deleted one, or an operator raised replicas. Either way the
		// scale up is the only thing that can fix it.
		res, err := (&K0sController{}).reconcileInplaceK0sVersionUpdate(context.Background(), newScope(2, 3))

		require.NoError(t, err)
		require.True(t, res.IsZero(),
			"holding here livelocks a control plane that cannot recover on its own")
	})

	t.Run("having too many machines is let through", func(t *testing.T) {
		// An operator lowered replicas, or wants a wedged machine gone.
		res, err := (&K0sController{}).reconcileInplaceK0sVersionUpdate(context.Background(), newScope(3, 1))

		require.NoError(t, err)
		require.True(t, res.IsZero(), "an operator has to be able to remove a machine")
	})
}
