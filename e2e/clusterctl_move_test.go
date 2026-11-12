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

package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/k0sproject/k0smotron/v2/e2e/mothership"
	"github.com/k0sproject/k0smotron/v2/e2e/util"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/bootstrap"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestClusterctlMove(t *testing.T) {
	setupAndRun(t, clusterctlMoveSpec)
}

func clusterctlMoveSpec(t *testing.T) {
	testName := "clusterctl-move"

	namespace, cancelNamespaceWatches := util.SetupSpecNamespace(ctx, testName, bootstrapClusterProxy, artifactFolder)
	clusterName := fmt.Sprintf("%s-%s", testName, capiutil.RandomString(6))

	workloadClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
		ClusterctlConfigPath: clusterctlConfigPath,
		KubeconfigPath:       bootstrapClusterProxy.GetKubeconfigPath(),
		Flavor:               "",

		Namespace:                namespace.Name,
		ClusterName:              clusterName,
		KubernetesVersion:        e2eConfig.MustGetVariable(KubernetesVersion),
		ControlPlaneMachineCount: new(int64(1)),
		InfrastructureProvider:   "docker",
		LogFolder:                filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
		ClusterctlVariables: map[string]string{
			"CLUSTER_NAME": clusterName,
			"NAMESPACE":    namespace.Name,
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

	// Create the target management cluster.
	targetName := fmt.Sprintf("%s-target-%s", testName, capiutil.RandomString(6))
	targetLogFolder := filepath.Join(artifactFolder, "clusters", targetName)
	targetProvider := bootstrap.CreateKindBootstrapClusterAndLoadImages(ctx, bootstrap.CreateKindBootstrapClusterAndLoadImagesInput{
		Name:               targetName,
		KubernetesVersion:  e2eConfig.MustGetVariable(KubernetesVersionManagement),
		RequiresDockerSock: e2eConfig.HasDockerProvider(),
		Images:             e2eConfig.Images,
		IPFamily:           e2eConfig.MustGetVariable(IPFamily),
		LogFolder:          filepath.Join(targetLogFolder, "logs-kind"),
	})
	require.NotNil(t, targetProvider, "Failed to create the target management cluster")

	scheme, err := initScheme()
	require.NoError(t, err)
	targetProxy := capiframework.NewClusterProxy(targetName, targetProvider.GetKubeconfigPath(), scheme)
	require.NotNil(t, targetProxy)

	// Registered after the target exists so it runs first (LIFO): the cluster is deleted from wherever it
	// ended up (the target once moved) before the target management cluster is disposed.
	moved := false
	t.Cleanup(func() {
		cleanupProxy := bootstrapClusterProxy
		if moved {
			cleanupProxy = targetProxy
		}
		util.DumpSpecResourcesAndCleanup(ctx, testName, cleanupProxy, artifactFolder, namespace, cancelNamespaceWatches, cluster,
			util.GetInterval(e2eConfig, testName, "wait-delete-cluster"), skipCleanup, clusterctlConfigPath)
		if moved && !skipCleanup {
			// The namespace in the source cluster is left behind by clusterctl move.
			capiframework.DeleteNamespace(ctx, capiframework.DeleteNamespaceInput{
				Deleter: bootstrapClusterProxy.GetClient(),
				Name:    namespace.Name,
			})
		}
		if !skipCleanup {
			targetProxy.Dispose(ctx)
			targetProvider.Dispose(ctx)
		}
	})

	// The source control plane has to be healthy before it is moved.
	controlPlane, err := util.DiscoveryAndWaitForControlPlaneInitialized(ctx, capiframework.DiscoveryAndWaitForControlPlaneInitializedInput{
		Lister:  bootstrapClusterProxy.GetClient(),
		Cluster: cluster,
	}, util.GetInterval(e2eConfig, testName, "wait-control-plane"))
	require.NoError(t, err)
	err = util.WaitForControlPlaneToBeReady(ctx, bootstrapClusterProxy.GetClient(), controlPlane, util.GetInterval(e2eConfig, testName, "wait-control-plane"))
	require.NoError(t, err)

	err = mothership.InitAndWatchControllerLogs(watchesCtx, clusterctl.InitManagementClusterAndWatchControllerLogsInput{
		ClusterProxy:              targetProxy,
		ClusterctlConfigPath:      clusterctlConfigPath,
		InfrastructureProviders:   e2eConfig.InfrastructureProviders(),
		DisableMetricsCollection:  true,
		BootstrapProviders:        []string{"k0sproject-k0smotron"},
		ControlPlaneProviders:     []string{"k0sproject-k0smotron"},
		RuntimeExtensionProviders: []string{"k0sproject-k0smotron"},
		LogFolder:                 targetLogFolder,
	}, util.GetInterval(e2eConfig, "bootstrap", "wait-deployment-available"))
	require.NoError(t, err, "Failed to init the target management cluster")

	fmt.Println("Moving the workload cluster to the target management cluster")
	moved = true
	err = util.MoveClusterAndWait(ctx, util.MoveClusterAndWaitInput{
		SourceProxy:          bootstrapClusterProxy,
		TargetProxy:          targetProxy,
		Cluster:              cluster,
		Namespace:            namespace.Name,
		ArtifactFolder:       artifactFolder,
		ClusterctlConfigPath: clusterctlConfigPath,
		WaitForCluster:       util.GetInterval(e2eConfig, testName, "wait-cluster"),
		WaitForControlPlane:  util.GetInterval(e2eConfig, testName, "wait-control-plane"),
	})
	require.NoError(t, err)

	fmt.Println("Verifying the target management cluster can reach the workload cluster API")
	require.Eventually(t, func() bool {
		c := &clusterv1.Cluster{}
		if err := targetProxy.GetClient().Get(ctx, client.ObjectKeyFromObject(cluster), c); err != nil {
			return false
		}
		return conditions.IsTrue(c, clusterv1.ClusterRemoteConnectionProbeCondition)
	}, 2*time.Minute, 10*time.Second, "RemoteConnectionProbe is not true after move")

	fmt.Println("Verifying CA and kubeconfig secrets exist in the target management cluster")
	for _, suffix := range []string{"ca", "kubeconfig"} {
		secret := &corev1.Secret{}
		err := targetProxy.GetClient().Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: fmt.Sprintf("%s-%s", clusterName, suffix)}, secret)
		require.NoError(t, err, "secret %s-%s missing in target", clusterName, suffix)
	}

	fmt.Println("Verifying the cluster is gone from the source management cluster")
	err = bootstrapClusterProxy.GetClient().Get(ctx, client.ObjectKeyFromObject(cluster), cluster.DeepCopy())
	require.True(t, apierrors.IsNotFound(err), "Cluster still present in source management cluster: %v", err)
}
