//go:build e2e

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

package util

import (
	"context"
	"fmt"
	"time"

	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	"k8s.io/apimachinery/pkg/util/wait"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpgradeClusterTopologyAndWaitForReadyUpgradeInput is the input type for UpgradeClusterTopologyAndWaitForReadyUpgrade.
type UpgradeClusterTopologyAndWaitForReadyUpgradeInput struct {
	GetLister                        capiframework.GetLister
	ClusterProxy                     capiframework.ClusterProxy
	Cluster                          *clusterv1.Cluster
	ControlPlane                     *cpv1beta2.K0sControlPlane
	MachineDeployment                *clusterv1.MachineDeployment
	KubernetesUpgradeVersion         string
	WaitForKubeProxyUpgradeInterval  Interval
	WaitForControlPlaneReadyInterval Interval
	Flavor                           string
}

// UpgradeClusterTopologyAndWaitForReadyUpgrade upgrades a Cluster and waits for it to be upgraded.
func UpgradeClusterTopologyAndWaitForReadyUpgrade(ctx context.Context, input UpgradeClusterTopologyAndWaitForReadyUpgradeInput) error {
	mgmtClient := input.ClusterProxy.GetClient()

	// Update the Cluster with the new version and not the K0sControlPlane or K0sControlPlaneTemplate, as the version is
	// set in the Cluster.Spec.Topology.Version for ClusterClass based clusters.
	fmt.Println("Patching the new k0s version in Cluster")
	patchHelper, err := patch.NewHelper(input.Cluster, mgmtClient)
	if err != nil {
		return err
	}

	input.Cluster.Spec.Topology.Version = input.KubernetesUpgradeVersion

	err = wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(ctx context.Context) (done bool, err error) {
		err = patchHelper.Patch(ctx, input.Cluster)
		if err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to patch the new kubernetes version to cluster %s/%s: %w", input.Cluster.Namespace, input.Cluster.Name, err)
	}

	// Wait for the Cluster.Spec.Topology.Version to be propagated to the K0sControlPlane.Spec.Version
	err = WaitClusterTopologyVersionPropagatedInControlplane(ctx, mgmtClient, input.Cluster.Spec.Topology.Version, input.ControlPlane)
	if err != nil {
		return err
	}

	err = WaitForControlPlaneToBeReady(ctx, input.ClusterProxy.GetClient(), input.ControlPlane, input.WaitForControlPlaneReadyInterval)
	if err != nil {
		return err
	}

	// Tolerate the case where the cluster does not have a MachineDeployment, for example when using a ClusterClass with a single control plane node and no worker nodes.
	if input.MachineDeployment != nil {
		md, err := DiscoveryAndWaitForMachineDeploymentReady(ctx, capiframework.DiscoveryAndWaitForMachineDeploymentsInput{
			Lister:  input.ClusterProxy.GetClient(),
			Cluster: input.Cluster,
		})
		if err != nil {
			return fmt.Errorf("failed to discover and wait for machine deployment to be ready: %w", err)
		}
		if md == nil {
			return fmt.Errorf("no machine deployment found for cluster %s", input.Cluster.Name)
		}
	}

	err = WaitForMachinesToBeDeleted(ctx, input.ClusterProxy.GetClient(), input.Cluster.Namespace, input.Cluster.Name, input.WaitForControlPlaneReadyInterval)
	if err != nil {
		return err
	}

	fmt.Println("Waiting for kube-proxy to have the upgraded kubernetes version")
	workloadCluster := input.ClusterProxy.GetWorkloadCluster(ctx, input.Cluster.Namespace, input.Cluster.Name)
	workloadClient := workloadCluster.GetClient()
	return WaitForKubeProxyUpgrade(ctx, WaitForKubeProxyUpgradeInput{
		Getter:            workloadClient,
		KubernetesVersion: input.KubernetesUpgradeVersion,
	}, input.WaitForKubeProxyUpgradeInterval)
}

func WaitClusterTopologyVersionPropagatedInControlplane(ctx context.Context, mgmtClient client.Client, desiredVersion string, controlPlane *cpv1beta2.K0sControlPlane) error {
	err := wait.PollUntilContextTimeout(ctx, time.Second, 1*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		cp := &cpv1beta2.K0sControlPlane{}
		if err := mgmtClient.Get(ctx, client.ObjectKeyFromObject(controlPlane), cp); err != nil {
			return false, err
		}

		if desiredVersion == cp.Spec.Version {
			return true, nil
		}

		return false, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for the topology version propagation to the cluster status: %w", err)
	}

	return nil
}
