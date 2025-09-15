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
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiutil "sigs.k8s.io/cluster-api/util"
)

func TestIgnitionProvisioning(t *testing.T) {
	setupAndRun(t, ignitionProvisioningSpec)
}

// Validation of the correct operation of k0smotron when using Ignition provisioning.
func ignitionProvisioningSpec(t *testing.T) {
	testName := "ignition"

	// Setup a Namespace where to host objects for this spec and create a watcher for the namespace events.
	namespace, _ := util.SetupSpecNamespace(ctx, testName, bootstrapClusterProxy, artifactFolder)

	clusterName := fmt.Sprintf("%s-%s", testName, capiutil.RandomString(6))

	// A SSH is not really needed for using AWS, but for debugging purposes it is useful to have it configured.
	SSHPublicKey := e2eConfig.GetVariable(SSHPublicKey)
	if SSHPublicKey == "" {
		t.Fatal("SSH public key is not set")
	}

	workloadClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
		ClusterctlConfigPath: clusterctlConfigPath,
		KubeconfigPath:       bootstrapClusterProxy.GetKubeconfigPath(),
		Flavor:               "ignition",

		Namespace:                namespace.Name,
		ClusterName:              clusterName,
		KubernetesVersion:        e2eConfig.GetVariable(KubernetesVersion),
		ControlPlaneMachineCount: ptr.To[int64](3),
		// CAPD doesn't support ignition, so we use AWS as infrastructure provider
		InfrastructureProvider: "aws",
		LogFolder:              filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
		ClusterctlVariables: map[string]string{
			"CLUSTER_NAME":   clusterName,
			"NAMESPACE":      namespace.Name,
			"SSH_PUBLIC_KEY": SSHPublicKey,
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
		)
	}()

	_, err = util.DiscoveryAndWaitForControlPlaneInitialized(ctx, capiframework.DiscoveryAndWaitForControlPlaneInitializedInput{
		Lister:  bootstrapClusterProxy.GetClient(),
		Cluster: cluster,
	}, util.GetInterval(e2eConfig, testName, "wait-controllers"))
	require.NoError(t, err)
	fmt.Println("Control plane is initialized")

	waitMachineInterval := util.GetInterval(e2eConfig, testName, "wait-machines")
	err = util.WaitForWorkerMachine(ctx, util.WaitForWorkersMachineInput{
		Lister:    bootstrapClusterProxy.GetClient(),
		Namespace: namespace.Name,
		// TODO: Once another higher-level resource is used to set machines, get configuration about resource replicas here.
		ExpectedWorkers:          1,
		ClusterName:              clusterName,
		WaitForMachinesIntervals: waitMachineInterval,
	})
	require.NoError(t, err)
	fmt.Println("Worker nodes are ready!")
	fmt.Println("Cluster is ready!")
}
