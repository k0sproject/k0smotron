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
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	"github.com/stretchr/testify/require"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func remediationMachine(name string, healthy bool, deleting bool) *clusterv1.Machine {
	m := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Status:     clusterv1.MachineStatus{NodeRef: clusterv1.MachineNodeReference{Name: name}},
	}

	if healthy {
		conditions.Set(m, metav1.Condition{
			Type:               clusterv1.MachineHealthCheckSucceededCondition,
			Status:             metav1.ConditionTrue,
			Reason:             clusterv1.MachineHealthCheckSucceededReason,
			LastTransitionTime: metav1.Now(),
		})
	} else {
		// Both conditions false is what marks a machine as needing the owner to
		// remediate it.
		for _, t := range []string{clusterv1.MachineHealthCheckSucceededCondition, clusterv1.MachineOwnerRemediatedCondition} {
			conditions.Set(m, metav1.Condition{
				Type:               t,
				Status:             metav1.ConditionFalse,
				Reason:             clusterv1.MachineHealthCheckHasRemediateAnnotationReason,
				LastTransitionTime: metav1.Now(),
			})
		}
	}

	if deleting {
		m.DeletionTimestamp = new(metav1.NewTime(time.Unix(1, 0)))
		m.Finalizers = []string{"test"}
	}

	return m
}

// TestReconcileUnhealthyMachinesWaitsForDeletions covers the guard that holds
// remediation back while another control plane machine is going away.
func TestReconcileUnhealthyMachinesWaitsForDeletions(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, cpv1beta2.AddToScheme(scheme))

	unhealthy := remediationMachine("cp-0", false, false)
	healthy := remediationMachine("cp-1", true, false)
	goingAway := remediationMachine("cp-2", true, true)

	kcp := &cpv1beta2.K0sControlPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"},
	}
	kcp.Status.Initialization.ControlPlaneInitialized = new(true)

	scope := &controlplane{
		cluster:        &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"}},
		kcp:            kcp,
		activeMachines: collections.FromMachines(unhealthy, healthy),
		// retrieveControlPlaneState splits these two sets by deletion timestamp, so
		// a deleting machine only ever appears here.
		deletedMachines: collections.FromMachines(goingAway),
	}

	require.Empty(t, scope.activeMachines.Filter(collections.HasDeletionTimestamp),
		"the fixture must mirror production, where active machines are never deleting")

	c := &K0sController{
		Client: fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(unhealthy, healthy, kcp).
			WithStatusSubresource(unhealthy, healthy, kcp).Build(),
	}

	require.NoError(t, c.reconcileUnhealthyMachines(context.Background(), scope))

	require.NotContains(t, scope.kcp.Annotations, cpv1beta2.RemediationInProgressAnnotation,
		"remediation must not start while another control plane machine is being deleted")

	got := conditions.Get(unhealthy, string(clusterv1.MachineOwnerRemediatedCondition))
	require.NotNil(t, got)
	require.Equal(t, metav1.ConditionFalse, got.Status)
	require.Contains(t, got.Message, "deletion")
}

// TestReconcileUnhealthyMachinesRemediatesWhenNothingIsDeleting is the other
// direction, since a guard that always fires would also pass the test above.
func TestReconcileUnhealthyMachinesRemediatesWhenNothingIsDeleting(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, cpv1beta2.AddToScheme(scheme))

	unhealthy := remediationMachine("cp-0", false, false)
	healthy := remediationMachine("cp-1", true, false)

	kcp := &cpv1beta2.K0sControlPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"},
	}
	kcp.Status.Initialization.ControlPlaneInitialized = new(true)

	scope := &controlplane{
		cluster:         &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"}},
		kcp:             kcp,
		activeMachines:  collections.FromMachines(unhealthy, healthy),
		deletedMachines: collections.Machines{},
	}

	c := &K0sController{
		Client: fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(unhealthy, healthy, kcp).
			WithStatusSubresource(unhealthy, healthy, kcp).Build(),
	}

	require.NoError(t, c.reconcileUnhealthyMachines(context.Background(), scope))

	require.Contains(t, scope.kcp.Annotations, cpv1beta2.RemediationInProgressAnnotation,
		"with nothing being deleted the unhealthy machine has to be remediated")

	err := c.Get(context.Background(), client.ObjectKeyFromObject(unhealthy), &clusterv1.Machine{})
	require.True(t, apierrors.IsNotFound(err), "the unhealthy machine should have been deleted")
}

// TestReconcileUnhealthyMachinesReportsAFailedConditionPatch covers the deferred patch being the
// only way the machine's condition reaches a user, so losing it quietly leaves them nothing to read.
func TestReconcileUnhealthyMachinesReportsAFailedConditionPatch(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, cpv1beta2.AddToScheme(scheme))

	unhealthy := remediationMachine("cp-0", false, false)

	kcp := &cpv1beta2.K0sControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"}}
	kcp.Status.Initialization.ControlPlaneInitialized = new(true)

	c := &K0sController{
		Client: fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(unhealthy, kcp).
			WithStatusSubresource(unhealthy, kcp).
			WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(context.Context, client.Client, string, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
					return errors.New("conflict on the machine")
				},
			}).Build(),
	}

	// A single replica is one of the preflight refusals, so the condition carrying the reason is
	// all this reconcile produces and the patch is what delivers it.
	err := c.reconcileUnhealthyMachines(context.Background(), &controlplane{
		cluster:         &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"}},
		kcp:             kcp,
		activeMachines:  collections.FromMachines(unhealthy),
		deletedMachines: collections.Machines{},
	})

	require.Error(t, err, "a condition nobody can read is not a successful reconcile")
	require.Contains(t, err.Error(), "failed to patch control plane Machine")
	require.Contains(t, err.Error(), "conflict on the machine", "the cause has to survive the wrap")
}

// TestReconcileUnhealthyMachinesToleratesTheRemediatedMachineBeingGone covers the other half. The
// remediated machine is deleted before the patch runs, so a missing machine is the success case.
func TestReconcileUnhealthyMachinesToleratesTheRemediatedMachineBeingGone(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, cpv1beta2.AddToScheme(scheme))

	unhealthy := remediationMachine("cp-0", false, false)
	healthy := remediationMachine("cp-1", true, false)

	kcp := &cpv1beta2.K0sControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "kcp", Namespace: "default"}}
	kcp.Status.Initialization.ControlPlaneInitialized = new(true)

	patched := 0
	c := &K0sController{
		Client: fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(unhealthy, healthy, kcp).
			WithStatusSubresource(unhealthy, healthy, kcp).
			WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, cl client.Client, _ string, obj client.Object, p client.Patch, opts ...client.SubResourcePatchOption) error {
					patched++

					return cl.Status().Patch(ctx, obj, p, opts...)
				},
			}).Build(),
	}

	require.NoError(t, c.reconcileUnhealthyMachines(context.Background(), &controlplane{
		cluster:         &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"}},
		kcp:             kcp,
		activeMachines:  collections.FromMachines(unhealthy, healthy),
		deletedMachines: collections.Machines{},
	}), "remediating a machine deletes it, so the patch that follows finding nothing is the happy path")

	require.Positive(t, patched, "the patch has to have been attempted, or this proves nothing")
	require.Contains(t, kcp.Annotations, cpv1beta2.RemediationInProgressAnnotation,
		"the remediation still has to have happened")
}

// TestReconcileUnhealthyMachinesClearsAStaleMarker covers the wedge where the marker
// outlives its remediation, since the only place that removes it runs on scale up.
func TestReconcileUnhealthyMachinesClearsAStaleMarker(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, cpv1beta2.AddToScheme(scheme))

	newScope := func(converged bool) (*controlplane, *clusterv1.Machine) {
		unhealthy := remediationMachine("cp-0", false, false)
		healthy := remediationMachine("cp-1", true, false)
		active := collections.FromMachines(unhealthy, healthy)

		kcp := &cpv1beta2.K0sControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "kcp",
				Namespace:   "default",
				Annotations: map[string]string{cpv1beta2.RemediationInProgressAnnotation: "true"},
			},
			Spec: cpv1beta2.K0sControlPlaneSpec{Replicas: 2},
		}
		kcp.Status.Initialization.ControlPlaneInitialized = new(true)

		scope := &controlplane{
			cluster:         &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"}},
			kcp:             kcp,
			activeMachines:  active,
			deletedMachines: collections.Machines{},
		}
		if converged {
			scope.upToDateMachines = active
			scope.notUpToDateMachines = collections.Machines{}
		} else {
			// A replacement is still owed, so the marker is doing its job.
			scope.upToDateMachines = collections.FromMachines(healthy)
			scope.notUpToDateMachines = collections.FromMachines(unhealthy)
		}

		return scope, unhealthy
	}

	controller := func(scope *controlplane, machines ...*clusterv1.Machine) *K0sController {
		objs := []client.Object{scope.kcp}
		for _, m := range machines {
			objs = append(objs, m)
		}

		return &K0sController{
			Client: fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(objs...).WithStatusSubresource(objs...).Build(),
		}
	}

	// Deleting the machine is the only thing remediation does that the fixture does not,
	// so that is what says whether it ran or was skipped.
	remediated := func(t *testing.T, c *K0sController, name string) bool {
		t.Helper()

		err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: name}, &clusterv1.Machine{})
		if err == nil {
			return false
		}
		require.True(t, apierrors.IsNotFound(err))

		return true
	}

	t.Run("a converged control plane owes no replacement, so the marker goes", func(t *testing.T) {
		scope, unhealthy := newScope(true)
		c := controller(scope, unhealthy)

		require.NoError(t, c.reconcileUnhealthyMachines(context.Background(), scope))

		require.True(t, remediated(t, c, unhealthy.Name),
			"a marker left behind by a finished remediation must not block the next one")
	})

	t.Run("a replacement still owed keeps the marker and skips", func(t *testing.T) {
		scope, unhealthy := newScope(false)
		c := controller(scope, unhealthy)

		require.NoError(t, c.reconcileUnhealthyMachines(context.Background(), scope))

		require.False(t, remediated(t, c, unhealthy.Name),
			"remediation must not start while the last one is still owed a replacement")
		require.Contains(t, scope.kcp.Annotations, cpv1beta2.RemediationInProgressAnnotation)
	})
}
