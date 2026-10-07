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
	"strings"
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

	// The marker's content is what the replacement inherits, so presence alone is not enough.
	marked, ok := remediationDataFrom(scope.kcp.Annotations, cpv1beta2.RemediationInProgressAnnotation)
	require.True(t, ok, "the marker has to carry which machine is being remediated")
	require.Equal(t, unhealthy.Name, marked.Machine)
	require.Equal(t, int32(0), marked.RetryCount, "a machine that replaced nothing starts a sequence")

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

// TestRemediationDataRoundTrip covers the annotation payload, including the literal "true" that
// older versions wrote, which has to read as no history rather than as an error.
func TestRemediationDataRoundTrip(t *testing.T) {
	stamp := metav1.NewTime(time.Unix(1700000000, 0).UTC())

	marshalled := remediationData{Machine: "cp-0", Timestamp: stamp, RetryCount: 2}.marshal()

	tests := []struct {
		name   string
		value  map[string]string
		want   remediationData
		wantOK bool
	}{
		{name: "no annotations", value: nil},
		{name: "annotation absent", value: map[string]string{"other": "x"}},
		{
			// The value every k0smotron before this change wrote.
			name:  "the legacy marker",
			value: map[string]string{cpv1beta2.RemediationForAnnotation: "true"},
		},
		{name: "not json at all", value: map[string]string{cpv1beta2.RemediationForAnnotation: "{oops"}},
		{
			// A timestamp so this reaches the machine check rather than stopping at the time one.
			name:  "json without a machine",
			value: map[string]string{cpv1beta2.RemediationForAnnotation: `{"timestamp":"2023-11-14T22:13:20Z","retryCount":3}`},
		},
		{
			// status requires the time, so reporting a payload without one makes the API server
			// reject the whole control plane patch on every reconcile.
			name:  "json without a timestamp",
			value: map[string]string{cpv1beta2.RemediationForAnnotation: `{"machine":"cp-0","retryCount":3}`},
		},
		{
			// Anyone can write this annotation, and every bound below is one status enforces.
			name:  "a machine name longer than status allows",
			value: map[string]string{cpv1beta2.RemediationForAnnotation: `{"machine":"` + strings.Repeat("a", 254) + `","timestamp":"2023-11-14T22:13:20Z"}`},
		},
		{
			name:  "a negative retry count",
			value: map[string]string{cpv1beta2.RemediationForAnnotation: `{"machine":"cp-0","timestamp":"2023-11-14T22:13:20Z","retryCount":-1}`},
		},
		{
			// Left out so the increment below cannot carry it past what an int32 holds.
			name:  "a retry count at the int32 ceiling",
			value: map[string]string{cpv1beta2.RemediationForAnnotation: `{"machine":"cp-0","timestamp":"2023-11-14T22:13:20Z","retryCount":2147483647}`},
		},
		{
			name:   "a machine name exactly at the limit is kept",
			value:  map[string]string{cpv1beta2.RemediationForAnnotation: `{"machine":"` + strings.Repeat("a", 253) + `","timestamp":"2023-11-14T22:13:20Z","retryCount":0}`},
			want:   remediationData{Machine: strings.Repeat("a", 253), Timestamp: stamp},
			wantOK: true,
		},
		{
			name:   "written by this version",
			value:  map[string]string{cpv1beta2.RemediationForAnnotation: marshalled},
			want:   remediationData{Machine: "cp-0", Timestamp: stamp, RetryCount: 2},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := remediationDataFrom(tt.value, cpv1beta2.RemediationForAnnotation)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want.Machine, got.Machine)
			require.Equal(t, tt.want.RetryCount, got.RetryCount)

			// Compared as an instant, since a round trip through RFC3339 comes back in the local
			// zone and the location alone would fail a struct comparison.
			require.True(t, tt.want.Timestamp.Time.Equal(got.Timestamp.Time),
				"want %s, got %s", tt.want.Timestamp, got.Timestamp)
		})
	}
}

// TestRemediationInProgressFor covers the retry count, which is the only part of the payload that
// is derived rather than copied. The window is what separates a retry from an unrelated failure.
func TestRemediationInProgressFor(t *testing.T) {
	withLineage := func(name string, age time.Duration, retries int32) *clusterv1.Machine {
		data := remediationData{
			Machine:    "cp-replaced",
			Timestamp:  metav1.NewTime(time.Now().Add(-age)),
			RetryCount: retries,
		}
		value := data.marshal()

		return &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{cpv1beta2.RemediationForAnnotation: value},
		}}
	}

	tests := []struct {
		name string
		in   *clusterv1.Machine
		want int32
	}{
		{
			name: "a machine that replaced nothing starts a sequence",
			in:   &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: "cp-0"}},
			want: 0,
		},
		{
			name: "a replacement failing inside the window continues the sequence",
			in:   withLineage("cp-1", time.Minute, 1),
			want: 2,
		},
		{
			// Long enough healthy that this is a fresh problem rather than the same one again.
			name: "a replacement failing outside the window starts over",
			in:   withLineage("cp-2", minHealthyPeriod+time.Minute, 4),
			want: 0,
		},
		{
			name: "the legacy marker cannot be read, so the sequence starts",
			in: &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{
				Name:        "cp-3",
				Annotations: map[string]string{cpv1beta2.RemediationForAnnotation: "true"},
			}},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := remediationInProgressFor(tt.in)
			require.Equal(t, tt.in.Name, got.Machine, "the marker names the machine being remediated")
			require.Equal(t, tt.want, got.RetryCount)
			require.False(t, got.Timestamp.IsZero())
		})
	}
}

// TestRemediationLineageSurvivesARetry covers the write and read halves agreeing across a cycle.
// Each is otherwise tested against a hand built annotation rather than against the other's output.
func TestRemediationLineageSurvivesARetry(t *testing.T) {
	unhealthy := &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: "cp-0"}}

	first := remediationInProgressFor(unhealthy)
	require.Equal(t, int32(0), first.RetryCount, "a machine that replaced nothing starts a sequence")

	kcp := &cpv1beta2.K0sControlPlane{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{cpv1beta2.RemediationInProgressAnnotation: first.marshal()},
	}}

	replacement := &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: "cp-1"}}
	carryRemediationLineage(kcp, replacement)
	require.Contains(t, replacement.Annotations, cpv1beta2.RemediationForAnnotation)

	// The replacement fails inside the window, so the sequence continues rather than restarting.
	second := remediationInProgressFor(replacement)
	require.Equal(t, "cp-1", second.Machine)
	require.Equal(t, int32(1), second.RetryCount)

	// A third, to show the count climbs across the wire instead of resetting each hop.
	kcp.Annotations[cpv1beta2.RemediationInProgressAnnotation] = second.marshal()
	third := &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: "cp-2"}}
	carryRemediationLineage(kcp, third)
	require.Equal(t, int32(2), remediationInProgressFor(third).RetryCount)
}
