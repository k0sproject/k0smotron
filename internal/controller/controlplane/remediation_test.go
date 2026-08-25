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
