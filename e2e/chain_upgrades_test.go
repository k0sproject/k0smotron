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

	"github.com/k0sproject/k0smotron/v2/e2e/util"
	"github.com/stretchr/testify/require"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiutil "sigs.k8s.io/cluster-api/util"
)

func TestChainUpgrade(t *testing.T) {
	setupAndRun(t, chainUpgradesSpec)
}

func chainUpgradesSpec(t *testing.T) {
	testName := "chain-upgrades"

	// Setup a Namespace where to host objects for this spec and create a watcher for the namespace events.
	namespace, _ := util.SetupSpecNamespace(ctx, testName, bootstrapClusterProxy, artifactFolder)

	clusterName := fmt.Sprintf("%s-%s", testName, capiutil.RandomString(6))

	workloadClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
		ClusterctlConfigPath:     clusterctlConfigPath,
		KubeconfigPath:           bootstrapClusterProxy.GetKubeconfigPath(),
		Flavor:                   "chain-upgrades",
		Namespace:                namespace.Name,
		ClusterName:              clusterName,
		KubernetesVersion:        e2eConfig.MustGetVariable(KubernetesVersion),
		ControlPlaneMachineCount: new(int64(3)),
		// TODO: make infra provider configurable
		InfrastructureProvider: "docker",
		LogFolder:              filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
		ClusterctlVariables: map[string]string{
			"CLUSTER_NAME": clusterName,
			"NAMESPACE":    namespace.Name,
			"K0S_VERSION":  e2eConfig.MustGetVariable(K0SVersionFromUpgradeChain),
		},
	})
	require.NotNil(t, workloadClusterTemplate)

	require.Eventually(t, func() bool {
		return bootstrapClusterProxy.CreateOrUpdate(ctx, workloadClusterTemplate) == nil
	}, 10*time.Second, 1*time.Second, "Failed to apply the cluster template")

	cluster, err := util.DiscoveryAndWaitForCluster(ctx, capiframework.DiscoveryAndWaitForClusterInput{
		Getter:    bootstrapClusterProxy.GetClient(),
		Namespace: namespace.Name,
		Name:      clusterName,
	}, util.GetInterval(e2eConfig, testName, "wait-cluster"))
	require.NoError(t, err)

	t.Cleanup(func() {
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
	})

	controlPlane, err := util.DiscoveryAndWaitForControlPlaneInitialized(ctx, capiframework.DiscoveryAndWaitForControlPlaneInitializedInput{
		Lister:  bootstrapClusterProxy.GetClient(),
		Cluster: cluster,
	}, util.GetInterval(e2eConfig, testName, "wait-controllers"))
	require.NoError(t, err)

	err = util.WaitForControlPlaneReplicasWithDesiredState(ctx, bootstrapClusterProxy.GetClient(), controlPlane, util.GetInterval(e2eConfig, testName, "wait-control-plane"))
	require.NoError(t, err)

	fmt.Println("Upgrading the Kubernetes control-plane version")
	err = util.UpgradeClusterTopologyAndWaitForReadyUpgrade(ctx, util.UpgradeClusterTopologyAndWaitForReadyUpgradeInput{
		ClusterProxy:                    bootstrapClusterProxy,
		Cluster:                         cluster,
		ControlPlane:                    controlPlane,
		Chained:                         true,
		KubernetesUpgradeVersion:        e2eConfig.MustGetVariable(K0SVersionToUpgradeChain),
		WaitForKubeProxyUpgradeInterval: util.GetInterval(e2eConfig, testName, "wait-kube-proxy-upgrade"),
		WaitForControlPlaneReplicatedWithDesiredStateInterval: util.GetInterval(e2eConfig, testName, "wait-control-plane"),
	})
	require.NoError(t, err)
}
