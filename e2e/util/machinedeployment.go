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
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DiscoveryAndWaitForMachineDeploymentReady discovers MachineDeployments for a given Cluster and waits for
// its Machines to be in Running phase.
func DiscoveryAndWaitForMachineDeploymentReady(ctx context.Context, input capiframework.DiscoveryAndWaitForMachineDeploymentsInput) (*clusterv1.MachineDeployment, error) {
	machineDeployments := capiframework.GetMachineDeploymentsByCluster(ctx, capiframework.GetMachineDeploymentsByClusterInput{
		Lister:      input.Lister,
		ClusterName: input.Cluster.Name,
		Namespace:   input.Cluster.Namespace,
	})
	if len(machineDeployments) == 0 {
		return nil, fmt.Errorf("no MachineDeployments found for Cluster %s", klog.KObj(input.Cluster))
	}

	machineDeployment := &clusterv1.MachineDeployment{}
	machineDeployment = machineDeployments[0]

	err := wait.PollUntilContextTimeout(ctx, time.Second*10, time.Minute*5, true, func(ctx context.Context) (done bool, err error) {
		mdMachines := &clusterv1.MachineList{}
		err = input.Lister.List(ctx, mdMachines, client.MatchingLabels(machineDeployment.Spec.Selector.MatchLabels))
		if err != nil {
			return false, fmt.Errorf("failed to list machines for MachineDeployment %s: %w", klog.KObj(machineDeployment), err)
		}
		for _, m := range mdMachines.Items {
			if m.Status.Phase != string(clusterv1.MachinePhaseRunning) {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed waiting for MachineDeployment %s to be upgraded: %w", klog.KObj(machineDeployment), err)
	}

	return machineDeployment, nil
}

// UpgradeMachineDeploymentAndWaitForReadyUpgradeInput is the input type for UpgradeMachineDeploymentAndWaitForReadyUpgrade.
type UpgradeMachineDeploymentAndWaitForReadyUpgradeInput struct {
	Lister                           capiframework.Lister
	ClusterProxy                     capiframework.ClusterProxy
	Cluster                          *clusterv1.Cluster
	MachineDeployment                *clusterv1.MachineDeployment
	KubernetesUpgradeVersion         string
	WaitForKubeProxyUpgradeInterval  Interval
	WaitForControlPlaneReadyInterval Interval
}

// UpgradeMachineDeploymentAndWaitForReadyUpgrade upgrades a MachineDeployment and waits for it to be upgraded.
func UpgradeMachineDeploymentAndWaitForReadyUpgrade(ctx context.Context, input UpgradeMachineDeploymentAndWaitForReadyUpgradeInput) error {
	mgmtClient := input.ClusterProxy.GetClient()

	fmt.Println("Patching the new kubernetes version to MachineDeployment")
	patchHelper, err := patch.NewHelper(input.MachineDeployment, mgmtClient)
	if err != nil {
		return err
	}

	input.MachineDeployment.Spec.Template.Spec.Version = input.KubernetesUpgradeVersion

	err = wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(ctx context.Context) (done bool, err error) {
		return patchHelper.Patch(ctx, input.MachineDeployment) == nil, nil
	})
	if err != nil {
		return fmt.Errorf("failed to patch the new kubernetes version to MachineDeployment %s: %w", klog.KObj(input.MachineDeployment), err)
	}

	fmt.Println("Waiting for MachineDeployment to be upgraded")

	err = wait.PollUntilContextTimeout(ctx, input.WaitForKubeProxyUpgradeInterval.tick, input.WaitForKubeProxyUpgradeInterval.timeout, true, func(ctx context.Context) (done bool, err error) {
		mdMachines := &clusterv1.MachineList{}
		err = input.Lister.List(ctx, mdMachines, client.MatchingLabels(input.MachineDeployment.Spec.Selector.MatchLabels))
		if err != nil {
			return false, fmt.Errorf("failed to list machines for MachineDeployment %s: %w", klog.KObj(input.MachineDeployment), err)
		}

		for _, m := range mdMachines.Items {
			if m.Spec.Version != input.KubernetesUpgradeVersion {
				return false, nil
			}

			if m.Status.Phase != string(clusterv1.MachinePhaseRunning) {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed waiting for MachineDeployment %s to be upgraded: %w", klog.KObj(input.MachineDeployment), err)
	}

	return nil
}
