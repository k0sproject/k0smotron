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

package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/k0sproject/k0smotron/e2e/util"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestWorkloadClusterCAPIInplaceUpgrade(t *testing.T) {
	setupAndRun(t, workloadClusterCAPIInplaceUpgradeSpec)
}

// Validation of the correct operation of k0smotron when the
// K0sControlPlane object is updated. It simulates a typical user workflow that includes:
//
// 1. Creation of a workload cluster.
//   - Ensures the cluster becomes operational.
//
// 2. Updating the control plane version using Inplace upgrade strategy.
//   - Verifies the cluster status aligns with the expected state after the update.
//
// 3. Performing a subsequent control plane version upgrade using Inplace upgrade strategy.
//   - Confirms the cluster status is consistent and desired post-update.
func workloadClusterCAPIInplaceUpgradeSpec(t *testing.T) {
	testName := "workload-capi-inplace-upgrade"

	// Setup a Namespace where to host objects for this spec and create a watcher for the namespace events.
	namespace, _ := util.SetupSpecNamespace(ctx, testName, bootstrapClusterProxy, artifactFolder)

	clusterName := fmt.Sprintf("%s-%s", testName, capiutil.RandomString(6))

	workloadClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
		ClusterctlConfigPath: clusterctlConfigPath,
		KubeconfigPath:       bootstrapClusterProxy.GetKubeconfigPath(),
		// no flavor specified, so it will use the default one "cluster-template"
		Flavor: "inplace-extension",

		Namespace:                namespace.Name,
		ClusterName:              clusterName,
		KubernetesVersion:        e2eConfig.MustGetVariable(KubernetesVersion),
		ControlPlaneMachineCount: ptr.To[int64](3),
		// TODO: make infra provider configurable
		InfrastructureProvider: "docker",
		LogFolder:              filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
		ClusterctlVariables: map[string]string{
			"CLUSTER_NAME":    clusterName,
			"NAMESPACE":       namespace.Name,
			"UPDATE_STRATEGY": "InPlace",
		},
	})
	require.NotNil(t, workloadClusterTemplate)
	fmt.Println(string(workloadClusterTemplate))

	require.Eventually(t, func() bool {
		return bootstrapClusterProxy.CreateOrUpdate(ctx, workloadClusterTemplate) == nil
	}, 10*time.Second, 1*time.Second, "Failed to apply the cluster template")

	cluster, err := util.DiscoveryAndWaitForCluster(ctx, capiframework.DiscoveryAndWaitForClusterInput{
		Getter:    bootstrapClusterProxy.GetClient(),
		Namespace: namespace.Name,
		Name:      clusterName,
	}, util.GetInterval(e2eConfig, testName, "wait-cluster"))
	require.NoError(t, err)

	defer func() {
		util.DumpSpecResourcesAndCleanup(
			ctx,
			testName,
			bootstrapClusterProxy,
			artifactFolder,
			namespace,
			cancelWatches,
			cluster,
			util.GetInterval(e2eConfig, testName, "wait-delete-cluster"),
			skipCleanup,
			clusterctlConfigPath,
		)
	}()

	controlPlane, err := util.DiscoveryAndWaitForControlPlaneInitialized(ctx, capiframework.DiscoveryAndWaitForControlPlaneInitializedInput{
		Lister:  bootstrapClusterProxy.GetClient(),
		Cluster: cluster,
	}, util.GetInterval(e2eConfig, testName, "wait-controllers"))
	require.NoError(t, err)
	// For Inplace upgrades we need to wait for the controlplane to have all the replicas ready before upgrading it again.
	err = util.WaitForControlPlaneToBeReady(ctx, bootstrapClusterProxy.GetClient(), controlPlane, util.GetInterval(e2eConfig, testName, "wait-kube-proxy-upgrade"))
	require.NoError(t, err)

	md, err := util.DiscoveryAndWaitForMachineDeploymentReady(ctx, capiframework.DiscoveryAndWaitForMachineDeploymentsInput{
		Lister:  bootstrapClusterProxy.GetClient(),
		Cluster: cluster,
	})
	require.NoError(t, err)

	existingMachines := &clusterv1.MachineList{}
	err = bootstrapClusterProxy.GetClient().List(ctx, existingMachines, client.InNamespace(namespace.Name))
	require.NoError(t, err)
	existingMachineNames := make(map[string]struct{})
	for _, m := range existingMachines.Items {
		existingMachineNames[m.Name] = struct{}{}
	}

	fmt.Println("Upgrading the Kubernetes control-plane version")
	err = util.UpgradeControlPlaneAndWaitForReadyUpgrade(ctx, util.UpgradeControlPlaneAndWaitForUpgradeInput{
		ClusterProxy:                     bootstrapClusterProxy,
		Cluster:                          cluster,
		ControlPlane:                     controlPlane,
		KubernetesUpgradeVersion:         e2eConfig.MustGetVariable(KubernetesVersionFirstUpgradeTo),
		WaitForKubeProxyUpgradeInterval:  util.GetInterval(e2eConfig, testName, "wait-kube-proxy-upgrade"),
		WaitForControlPlaneReadyInterval: util.GetInterval(e2eConfig, testName, "wait-control-plane"),
	})
	require.NoError(t, err)

	fmt.Println("Upgrading the Kubernetes workers version")
	err = util.UpgradeMachineDeploymentAndWaitForReadyUpgrade(ctx, util.UpgradeMachineDeploymentAndWaitForReadyUpgradeInput{
		Lister:                           bootstrapClusterProxy.GetClient(),
		ClusterProxy:                     bootstrapClusterProxy,
		Cluster:                          cluster,
		MachineDeployment:                md,
		KubernetesUpgradeVersion:         e2eConfig.MustGetVariable(KubernetesVersionFirstUpgradeTo),
		WaitForKubeProxyUpgradeInterval:  util.GetInterval(e2eConfig, testName, "wait-kube-proxy-upgrade"),
		WaitForControlPlaneReadyInterval: util.GetInterval(e2eConfig, testName, "wait-control-plane"),
	})
	require.NoError(t, err)

	fmt.Println("Verifying all previous machines are there after the upgrade")
	updatedMachines := &clusterv1.MachineList{}
	err = bootstrapClusterProxy.GetClient().List(ctx, updatedMachines, client.InNamespace(namespace.Name))
	require.NoError(t, err)

	require.Len(t, existingMachineNames, len(updatedMachines.Items))
	for _, m := range updatedMachines.Items {
		// Check again that all machines have the upgraded version. This ensures all machines
		// have been upgraded and no rollback happened.
		require.Equal(t, m.Spec.Version, e2eConfig.MustGetVariable(KubernetesVersionFirstUpgradeTo), "expected machine %s to have version %s after the upgrade", m.Name, e2eConfig.MustGetVariable(KubernetesVersionFirstUpgradeTo))
		_, exists := existingMachineNames[m.Name]
		require.True(t, exists, "expected machine %s to exist after the upgrade", m.Name)
	}
}
