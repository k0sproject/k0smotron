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

	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	e2eutil "github.com/k0sproject/k0smotron/e2e/util"
	"github.com/k0sproject/k0smotron/internal/util"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/cluster-api/test/framework"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestControlplaneConditions(t *testing.T) {
	setupAndRun(t, controlplaneConditionsSpec)
}

func controlplaneConditionsSpec(t *testing.T) {
	testName := "kcp-conditions"

	// Setup a Namespace where to host objects for this spec and create a watcher for the namespace events.
	testNamespace, testCancelWatches := framework.CreateNamespaceAndWatchEvents(ctx, framework.CreateNamespaceAndWatchEventsInput{
		Creator:   bootstrapClusterProxy.GetClient(),
		ClientSet: bootstrapClusterProxy.GetClientSet(),
		Name:      testName,
		LogFolder: filepath.Join(artifactFolder, "clusters", "bootstrap"),
	})

	workloadClusterName := fmt.Sprintf("%s-workload-%s", testName, util.RandomString(6))
	workloadClusterNamespace := testNamespace.Name

	workloadClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
		ClusterctlConfigPath:     clusterctlConfigPath,
		KubeconfigPath:           bootstrapClusterProxy.GetKubeconfigPath(),
		InfrastructureProvider:   "docker",
		Flavor:                   "",
		Namespace:                workloadClusterNamespace,
		ClusterName:              workloadClusterName,
		KubernetesVersion:        e2eConfig.MustGetVariable(KubernetesVersion),
		ControlPlaneMachineCount: ptr.To[int64](1),
		LogFolder:                filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
		ClusterctlVariables: map[string]string{
			"CLUSTER_NAME":    workloadClusterName,
			"NAMESPACE":       workloadClusterNamespace,
			"UPDATE_STRATEGY": "InPlace",
		},
	})

	// Apply the cluster template yaml
	require.Eventually(t, func() bool {
		return bootstrapClusterProxy.CreateOrUpdate(ctx, workloadClusterTemplate) == nil
	}, 10*time.Second, 1*time.Second, "Failed to apply the cluster template")

	// Wait for the cluster to be ready using the utility function
	cluster, err := e2eutil.DiscoveryAndWaitForCluster(ctx, capiframework.DiscoveryAndWaitForClusterInput{
		Getter:    bootstrapClusterProxy.GetClient(),
		Namespace: workloadClusterNamespace,
		Name:      workloadClusterName,
	}, e2eutil.GetInterval(e2eConfig, testName, "wait-cluster"))
	require.NoError(t, err)

	defer func() {
		e2eutil.DumpSpecResourcesAndCleanup(
			ctx,
			testName,
			bootstrapClusterProxy,
			artifactFolder,
			testNamespace,
			testCancelWatches,
			cluster,
			e2eutil.GetInterval(e2eConfig, testName, "wait-delete-cluster"),
			skipCleanup,
			clusterctlConfigPath,
		)

		testCancelWatches()
	}()

	// Wait for the control plane to be initialized
	controlPlane, err := e2eutil.DiscoveryAndWaitForControlPlaneInitialized(ctx, capiframework.DiscoveryAndWaitForControlPlaneInitializedInput{
		Lister:  bootstrapClusterProxy.GetClient(),
		Cluster: cluster,
	}, e2eutil.GetInterval(e2eConfig, testName, "wait-controllers"))
	require.NoError(t, err)

	// Wait for the control plane to be ready
	err = e2eutil.WaitForControlPlaneToBeReady(ctx, bootstrapClusterProxy.GetClient(), controlPlane, e2eutil.GetInterval(e2eConfig, testName, "wait-control-plane"))
	require.NoError(t, err)

	// Test: Verify that ControlPlaneReadyCondition transitions to True when cluster is ready
	require.Eventually(t, func() bool {
		if err := bootstrapClusterProxy.GetClient().Get(ctx, client.ObjectKeyFromObject(controlPlane), controlPlane); err != nil {
			return false
		}
		return conditions.IsTrue(controlPlane, cpv1beta1.ControlPlaneReadyCondition)
	}, 5*time.Minute, 10*time.Second, "ControlPlaneReadyCondition should transition to True")

	// Test: Verify that the condition has the correct final status
	condition := conditions.Get(controlPlane, cpv1beta1.ControlPlaneReadyCondition)
	require.NotNil(t, condition, "ControlPlaneReadyCondition should exist")
	require.Equal(t, corev1.ConditionTrue, condition.Status, "ControlPlaneReadyCondition should be True")

	// Test: Verify that the status is ready
	require.True(t, controlPlane.Status.Ready, "K0smotronControlPlane should be ready")
	require.True(t, controlPlane.Status.Initialized, "K0smotronControlPlane should be initialized")
}
