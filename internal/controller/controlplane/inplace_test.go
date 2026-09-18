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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	bootstrapv2 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	"github.com/stretchr/testify/require"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimev1 "sigs.k8s.io/cluster-api/api/runtime/v1beta2"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
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

// TestTriggerCAPIInplaceVersionUpdateOrdering pins the order the contract requires, so a
// pending hook is never observed against objects that do not yet know about the update.
func TestTriggerCAPIInplaceVersionUpdateOrdering(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, bootstrapv2.AddToScheme(scheme))

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{Name: "cp-0", Namespace: "default"},
		Spec:       clusterv1.MachineSpec{Version: "v1.31.0+k0s.0", ClusterName: "c"},
	}
	bootstrapConfig := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cp-0", Namespace: "default"},
	}
	infraMachine := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "infrastructure.cluster.x-k8s.io/v1beta2",
		"kind":       "RemoteMachine",
		"metadata":   map[string]any{"name": "cp-0", "namespace": "default"},
	}}

	// Never delegates, because the fake client panics applying a merge patch to a
	// K0sControllerConfig, whose spec embeds a pointer.
	var patched []string
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(_ context.Context, _ client.WithWatch, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
				var label string
				switch obj.(type) {
				case *unstructured.Unstructured:
					label = "infraMachine"
				case *bootstrapv2.K0sControllerConfig:
					label = "bootstrapConfig"
				case *clusterv1.Machine:
					label = "machine"
				default:
					return fmt.Errorf("unexpected patch on %T", obj)
				}
				// Recorded per patch, so the hook cannot hide on an earlier one.
				if _, ok := obj.GetAnnotations()[runtimev1.PendingHooksAnnotation]; ok {
					label += "+hook"
				}
				patched = append(patched, label)

				return nil
			},
		}).Build()

	require.NoError(t, triggerCAPIInplaceVersionUpdate(
		t.Context(), cl, "v1.32.0+k0s.0", machine, infraMachine, bootstrapConfig))

	// The bare machine first is the latch, which records that a trigger started, and the hook
	// last is what the machine controller acts on.
	require.Equal(t, []string{"machine", "infraMachine", "bootstrapConfig", "machine+hook"}, patched)

	require.Contains(t, machine.Annotations, runtimev1.PendingHooksAnnotation)
	require.Contains(t, machine.Annotations, clusterv1.UpdateInProgressAnnotation)
	require.Contains(t, infraMachine.GetAnnotations(), clusterv1.UpdateInProgressAnnotation)
	require.Contains(t, bootstrapConfig.Annotations, clusterv1.UpdateInProgressAnnotation)
	require.Equal(t, "v1.32.0+k0s.0", machine.Spec.Version)
}

// TestPendingHookList covers the annotation format, which is upstream's to define since its
// machine controller is the reader. Defect 33 was this being assigned rather than added to.
func TestPendingHookList(t *testing.T) {
	const other = "AnotherHook"

	tests := []struct {
		name    string
		start   map[string]string
		want    string
		wantHas bool
	}{
		{
			name: "no annotations at all",
			want: updateMachineHook(),
		},
		{
			name:  "an empty list",
			start: map[string]string{runtimev1.PendingHooksAnnotation: ""},
			want:  updateMachineHook(),
		},
		{
			// The defect. Assigning here dropped a hook another controller was waiting on.
			name:  "a hook someone else is waiting on",
			start: map[string]string{runtimev1.PendingHooksAnnotation: other},
			want:  other + "," + updateMachineHook(),
		},
		{
			name:    "already listed, so nothing changes",
			start:   map[string]string{runtimev1.PendingHooksAnnotation: updateMachineHook()},
			want:    updateMachineHook(),
			wantHas: true,
		},
		{
			name:    "listed among others, deduplicated and sorted",
			start:   map[string]string{runtimev1.PendingHooksAnnotation: updateMachineHook() + "," + other + "," + updateMachineHook()},
			want:    other + "," + updateMachineHook(),
			wantHas: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: "cp-0", Annotations: tt.start}}

			require.Equal(t, tt.wantHas, hasPendingHook(m, updateMachineHook()))

			markHookPending(m, updateMachineHook())
			require.Equal(t, tt.want, m.Annotations[runtimev1.PendingHooksAnnotation])
			require.True(t, hasPendingHook(m, updateMachineHook()), "the hook has to be tracked once marked")

			// Marking twice is what re-entrancy does, and it must not grow the list.
			markHookPending(m, updateMachineHook())
			require.Equal(t, tt.want, m.Annotations[runtimev1.PendingHooksAnnotation])
		})
	}
}

// TestMachinesToCompleteTrigger covers which Machines are treated as having an unfinished trigger.
// The states either side of it matter as much as the state itself.
func TestMachinesToCompleteTrigger(t *testing.T) {
	machine := func(name string, latched, hooked bool) *clusterv1.Machine {
		m := &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{}}}
		if latched {
			m.Annotations[clusterv1.UpdateInProgressAnnotation] = ""
		}
		if hooked {
			markHookPending(m, updateMachineHook())
		}

		return m
	}

	tests := []struct {
		name string
		in   *clusterv1.Machine
		want bool
	}{
		{name: "untouched", in: machine("cp-0", false, false), want: false},
		{name: "trigger stopped after the latch", in: machine("cp-1", true, false), want: true},
		{name: "trigger finished", in: machine("cp-2", true, true), want: false},
		{
			// Completion clears the latch before the hook, so this is a Machine finishing rather
			// than one that never started. Re-triggering it would restart a completed update.
			name: "completion in progress",
			in:   machine("cp-3", false, true),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := &controlplane{activeMachines: collections.FromMachines(tt.in)}
			require.Equal(t, tt.want, machinesToCompleteTrigger(scope).Has(tt.in))
		})
	}
}

// TestTriggerCAPIInplaceVersionUpdateCompletesAfterTheLatch covers re-entrancy. A Machine already
// carrying the latch gets the rest of the trigger, and the version still lands with the hook.
func TestTriggerCAPIInplaceVersionUpdateCompletesAfterTheLatch(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, bootstrapv2.AddToScheme(scheme))

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cp-0", Namespace: "default",
			Annotations: map[string]string{clusterv1.UpdateInProgressAnnotation: ""},
		},
		Spec: clusterv1.MachineSpec{Version: "v1.31.0+k0s.0", ClusterName: "c"},
	}
	bootstrapConfig := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cp-0", Namespace: "default"},
	}
	infraMachine := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "infrastructure.cluster.x-k8s.io/v1beta2",
		"kind":       "RemoteMachine",
		"metadata":   map[string]any{"name": "cp-0", "namespace": "default"},
	}}

	var patched []string
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(_ context.Context, _ client.WithWatch, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
				switch obj.(type) {
				case *unstructured.Unstructured:
					patched = append(patched, "infraMachine")
				case *bootstrapv2.K0sControllerConfig:
					patched = append(patched, "bootstrapConfig")
				case *clusterv1.Machine:
					patched = append(patched, "machine+hook")
				default:
					return fmt.Errorf("unexpected patch on %T", obj)
				}

				return nil
			},
		}).Build()

	require.NoError(t, triggerCAPIInplaceVersionUpdate(
		t.Context(), cl, "v1.32.0+k0s.0", machine, infraMachine, bootstrapConfig))

	// No second latch patch, since the latch is already there.
	require.Equal(t, []string{"infraMachine", "bootstrapConfig", "machine+hook"}, patched)
	require.True(t, hasPendingHook(machine, updateMachineHook()))
	require.Equal(t, "v1.32.0+k0s.0", machine.Spec.Version)
}

// TestTriggerCAPIInplaceVersionUpdateHoldsTheVersionUntilTheHook covers the property the whole
// ordering exists for. A failure before the hook must not leave the version already bumped, since
// a bumped version is what made the Machine unselectable and stranded it for good.
func TestTriggerCAPIInplaceVersionUpdateHoldsTheVersionUntilTheHook(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, bootstrapv2.AddToScheme(scheme))

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{Name: "cp-0", Namespace: "default"},
		Spec:       clusterv1.MachineSpec{Version: "v1.31.0+k0s.0", ClusterName: "c"},
	}
	bootstrapConfig := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cp-0", Namespace: "default"},
	}
	infraMachine := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "infrastructure.cluster.x-k8s.io/v1beta2",
		"kind":       "RemoteMachine",
		"metadata":   map[string]any{"name": "cp-0", "namespace": "default"},
	}}

	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(_ context.Context, _ client.WithWatch, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
				if _, ok := obj.(*unstructured.Unstructured); ok {
					return fmt.Errorf("infra machine write failed")
				}

				return nil
			},
		}).Build()

	require.Error(t, triggerCAPIInplaceVersionUpdate(
		t.Context(), cl, "v1.32.0+k0s.0", machine, infraMachine, bootstrapConfig))

	require.Equal(t, "v1.31.0+k0s.0", machine.Spec.Version, "the version cannot move before the hook")
	require.False(t, hasPendingHook(machine, updateMachineHook()), "nothing may act on a half marked machine")

	// The latch is what a later reconcile finds it by, since an unbumped version alone would not
	// distinguish it from a machine nobody has started on.
	require.Contains(t, machine.Annotations, clusterv1.UpdateInProgressAnnotation)
	require.True(t, machinesToCompleteTrigger(&controlplane{
		activeMachines: collections.FromMachines(machine),
	}).Has(machine))
}
