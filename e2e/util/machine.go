//go:build e2e

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

package util

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type WaitForMachinesInput struct {
	Lister                   framework.Lister
	Namespace                string
	ClusterName              string
	ExpectedReplicas         int
	ExpectedOldMachines      []string
	ExpectedDeletedMachines  []string
	WaitForMachinesIntervals Interval
}

// WaitForMachines waits for machines to reach a well known state defined by number of replicas, a list of machines to exist,
// a list of machines to not exist anymore. The func also check that the state is stable for some time before
// returning the list of new machines.
func WaitForMachines(ctx context.Context, input WaitForMachinesInput) ([]string, []string, error) {
	inClustersNamespaceListOption := client.InNamespace(input.Namespace)
	matchClusterListOption := client.MatchingLabels{
		clusterv1.ClusterNameLabel:         input.ClusterName,
		clusterv1.MachineControlPlaneLabel: "true",
	}

	expectedOldMachines := sets.Set[string]{}.Insert(input.ExpectedOldMachines...)
	expectedDeletedMachines := sets.Set[string]{}.Insert(input.ExpectedDeletedMachines...)
	allMachines := sets.Set[string]{}
	newMachines := sets.Set[string]{}
	machineList := &clusterv1.MachineList{}

	// Waits for the desired set of machines to exist.
	err := wait.PollUntilContextTimeout(ctx, input.WaitForMachinesIntervals.tick, input.WaitForMachinesIntervals.timeout, true, func(ctx context.Context) (done bool, err error) {
		// Gets the list of machines
		if err := input.Lister.List(ctx, machineList, inClustersNamespaceListOption, matchClusterListOption); err != nil {
			return false, err
		}

		allMachines = sets.Set[string]{}
		for i := range machineList.Items {
			allMachines.Insert(machineList.Items[i].Name)
		}

		// Compute new machines (all - old - to be deleted)
		newMachines = allMachines.Clone()
		newMachines.Delete(expectedOldMachines.UnsortedList()...)
		newMachines.Delete(expectedDeletedMachines.UnsortedList()...)

		fmt.Printf(" - expected %d, got %d: %s, of which new %s, must have check: %t, must not have check: %t\n", input.ExpectedReplicas, allMachines.Len(), allMachines.UnsortedList(), newMachines.UnsortedList(), allMachines.HasAll(expectedOldMachines.UnsortedList()...), !allMachines.HasAny(expectedDeletedMachines.UnsortedList()...))

		// Ensures all the expected old machines are still there.
		if !allMachines.HasAll(expectedOldMachines.UnsortedList()...) {
			return false, fmt.Errorf("Got machines: %s, must contain all of: %s", allMachines.UnsortedList(), expectedOldMachines.UnsortedList())
		}

		// Ensures none of the machines to be deleted is still there.
		if allMachines.HasAny(expectedDeletedMachines.UnsortedList()...) {
			return false, fmt.Errorf("Got machines: %s, must not contain any of: %s", allMachines.UnsortedList(), expectedDeletedMachines.UnsortedList())
		}

		if len(allMachines) != input.ExpectedReplicas {
			return false, fmt.Errorf("Got %d machines, must be %d", len(allMachines), input.ExpectedReplicas)
		}

		return true, nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to get the expected list of machines: got %s (expected %d machines, must have %s, must not have %s)",
			allMachines.UnsortedList(), input.ExpectedReplicas, expectedOldMachines.UnsortedList(), expectedDeletedMachines.UnsortedList())
	}

	fmt.Printf("Got %d machines: %s\n", allMachines.Len(), allMachines.UnsortedList())

	// Ensures the desired set of machines is stable (no further machines are created or deleted).
	fmt.Printf("Checking the list of machines is stable\n")
	allMachinesNow := sets.Set[string]{}

	now := time.Now()
	timeForConsideredStable := 30 * time.Second
	for time.Since(now) < timeForConsideredStable {
		// Gets the list of machines
		err := input.Lister.List(ctx, machineList, inClustersNamespaceListOption, matchClusterListOption)
		if err != nil {
			return nil, nil, err
		}

		allMachinesNow = sets.Set[string]{}
		for i := range machineList.Items {
			allMachinesNow.Insert(machineList.Items[i].Name)
		}

		if !allMachines.Equal(allMachinesNow) {
			return nil, nil, fmt.Errorf("Expected list of machines is not stable: got %s, expected %s", allMachinesNow.UnsortedList(), allMachines.UnsortedList())
		}
	}
	return allMachines.UnsortedList(), newMachines.UnsortedList(), nil
}
