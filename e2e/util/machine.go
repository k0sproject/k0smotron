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
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type WaitForMachinesInput struct {
	Lister                   framework.Lister
	Namespace                string
	ClusterName              string
	ExpectedReplicas         int
	ExpectedOldMachines      map[string]string
	ExpectedDeletedMachines  map[string]string
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

	allMachines := make(map[string]string)
	newMachines := make(map[string]string)
	machineList := &clusterv1.MachineList{}

	// Waits for the desired set of machines to exist.
	err := wait.PollUntilContextTimeout(ctx, input.WaitForMachinesIntervals.tick, input.WaitForMachinesIntervals.timeout, true, func(ctx context.Context) (done bool, err error) {
		// Gets the list of machines
		if err := input.Lister.List(ctx, machineList, inClustersNamespaceListOption, matchClusterListOption); err != nil {
			return false, err
		}

		allMachines = make(map[string]string)
		for i := range machineList.Items {
			allMachines[string(machineList.Items[i].GetUID())] = machineList.Items[i].Name
		}

		// Compute new machines (all - old - to be deleted)
		newMachines = make(map[string]string)
		for k, v := range allMachines {
			newMachines[k] = v
		}
		for k := range input.ExpectedOldMachines {
			delete(newMachines, k)
		}
		for k := range input.ExpectedDeletedMachines {
			delete(newMachines, k)
		}

		// Ensures all the expected old machines are still there.
		for k := range input.ExpectedOldMachines {
			if _, ok := allMachines[k]; !ok {
				return false, nil
			}
		}

		// Ensures none of the machines to be deleted is still there.
		for k := range input.ExpectedDeletedMachines {
			if _, ok := allMachines[k]; ok {
				return false, nil
			}
		}

		if len(allMachines) != input.ExpectedReplicas {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to get the expected list of machines: got %v (expected %d machines, must have %v, must not have %v)",
			allMachines, input.ExpectedReplicas, input.ExpectedOldMachines, input.ExpectedDeletedMachines)
	}

	fmt.Printf("Got %d machines: %v\n", len(allMachines), allMachines)

	// Ensures the desired set of machines is stable (no further machines are created or deleted).
	fmt.Printf("Checking the list of machines is stable\n")
	allMachinesNow := make(map[string]string)

	now := time.Now()
	timeForConsideredStable := 30 * time.Second
	for time.Since(now) < timeForConsideredStable {
		// Gets the list of machines
		err := input.Lister.List(ctx, machineList, inClustersNamespaceListOption, matchClusterListOption)
		if err != nil {
			return nil, nil, err
		}

		allMachinesNow = make(map[string]string)
		for i := range machineList.Items {
			allMachinesNow[string(machineList.Items[i].GetUID())] = machineList.Items[i].Name
		}

		if !reflect.DeepEqual(allMachines, allMachinesNow) {
			return nil, nil, fmt.Errorf("Expected list of machines is not stable: got %v, expected %v", allMachinesNow, allMachines)
		}
	}

	allMachinesNames := []string{}
	for _, v := range allMachines {
		allMachinesNames = append(allMachinesNames, v)
	}

	newMachinesNames := []string{}
	for _, v := range newMachines {
		newMachinesNames = append(newMachinesNames, v)
	}
	return allMachinesNames, newMachinesNames, nil
}

type WaitForWorkersMachineInput struct {
	Lister                   framework.Lister
	ExpectedWorkers          int
	Namespace                string
	ClusterName              string
	WaitForMachinesIntervals Interval
}

// WaitForMachine waits for worker machine to join the cluster. Checks machine.spec has the proper providerId set.
func WaitForWorkerMachine(ctx context.Context, input WaitForWorkersMachineInput) error {
	inClustersNamespaceListOption := client.InNamespace(input.Namespace)
	matchClusterGetOption := client.MatchingLabels{
		clusterv1.ClusterNameLabel: input.ClusterName,
	}

	machineList := &clusterv1.MachineList{}

	// Waits for the desired worker machine to exist.
	err := wait.PollUntilContextTimeout(ctx, input.WaitForMachinesIntervals.tick, input.WaitForMachinesIntervals.timeout, true, func(ctx context.Context) (done bool, err error) {
		// Gets the worker machine
		if err := input.Lister.List(ctx, machineList, inClustersNamespaceListOption, matchClusterGetOption); err != nil {
			return false, err
		}

		currentWorkerMachines := 0
		for _, m := range machineList.Items {
			isWorker := true
			for k, _ := range m.GetLabels() {
				if k == clusterv1.MachineControlPlaneLabel {
					isWorker = false
					break
				}
			}

			if isWorker {
				mProviderId := m.Spec.ProviderID
				if mProviderId == "" {
					continue
				}

				currentWorkerMachines++
			}
		}
		if input.ExpectedWorkers != currentWorkerMachines {
			return false, fmt.Errorf("expected worker machines is %d but got %d", input.ExpectedWorkers, currentWorkerMachines)
		}

		return true, nil
	})
	if err != nil {
		return fmt.Errorf("Failed to get the expected worker machine: %w", err)
	}

	return nil
}
