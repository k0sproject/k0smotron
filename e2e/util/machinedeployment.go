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

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DiscoveryAndWaitForMachineDeploymentReady discovers MachineDeployments for a given Cluster with the desired version and waits for it to be ready.
func DiscoveryAndWaitForMachineDeploymentReady(ctx context.Context, input capiframework.DiscoveryAndWaitForMachineDeploymentsInput) (*clusterv1.MachineDeployment, error) {
	var desiredMachineDeployment *clusterv1.MachineDeployment

	err := wait.PollUntilContextTimeout(ctx, time.Second*5, time.Minute*5, true, func(ctx context.Context) (done bool, err error) {
		machineDeployments := capiframework.GetMachineDeploymentsByCluster(ctx, capiframework.GetMachineDeploymentsByClusterInput{
			Lister:      input.Lister,
			ClusterName: input.Cluster.Name,
			Namespace:   input.Cluster.Namespace,
		})
		if len(machineDeployments) == 0 {
			return false, fmt.Errorf("no MachineDeployments found for Cluster %s/%s", input.Cluster.Namespace, input.Cluster.Name)
		}

		for _, md := range machineDeployments {
			if md.Spec.Template.Spec.Version == input.Cluster.Spec.Topology.Version {
				desiredMachineDeployment = md
				return true, nil
			}
		}

		return false, nil
	})

	err = WaitForMachineDeploymentToBeReady(ctx, input.Lister, desiredMachineDeployment, Interval{
		tick:    time.Second * 5,
		timeout: time.Minute * 5,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to wait for MachineDeployment %s/%s to be ready: %w", desiredMachineDeployment.Namespace, desiredMachineDeployment.Name, err)
	}

	return desiredMachineDeployment, nil
}

// UpgradeMachineDeploymentAndWaitForReadyUpgrade upgrades a MachineDeployment and waits for it to be upgraded.
func WaitForMachineDeploymentToBeReady(ctx context.Context, lister capiframework.Lister, machineDeployment *clusterv1.MachineDeployment, waitInterval Interval) error {
	fmt.Println("Waiting for MachineDeployment to be upgraded")

	err := wait.PollUntilContextTimeout(ctx, waitInterval.tick, waitInterval.timeout, true, func(ctx context.Context) (done bool, err error) {
		mdMachines := &clusterv1.MachineList{}
		err = lister.List(ctx, mdMachines, client.MatchingLabels(machineDeployment.Spec.Selector.MatchLabels))
		if err != nil {
			return false, fmt.Errorf("failed to list machines for MachineDeployment %s: %w", klog.KObj(machineDeployment), err)
		}

		for _, m := range mdMachines.Items {
			if m.Spec.Version != machineDeployment.Spec.Template.Spec.Version {
				return false, nil
			}

			if m.Status.Phase != string(clusterv1.MachinePhaseRunning) {
				return false, nil
			}

			if !conditions.IsTrue(&m, clusterv1.AvailableCondition) {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed waiting for MachineDeployment %s to be ready: %w", klog.KObj(machineDeployment), err)
	}

	return nil
}
