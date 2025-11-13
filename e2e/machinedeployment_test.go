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
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/k0sproject/k0smotron/e2e/util"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	clusterv2 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestMachinedeployment(t *testing.T) {
	setupAndRun(t, func(t *testing.T) {
		testName := "machinedeployment"

		// Setup a Namespace where to host objects for this spec and create a watcher for the namespace events.
		namespace, _ := util.SetupSpecNamespace(ctx, testName, bootstrapClusterProxy, artifactFolder)

		clusterName := fmt.Sprintf("%s-%s", testName, capiutil.RandomString(6))

		workloadClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
			ClusterctlConfigPath: clusterctlConfigPath,
			KubeconfigPath:       bootstrapClusterProxy.GetKubeconfigPath(),
			// select cluster templates
			Flavor: "machinedeployment",

			Namespace:                namespace.Name,
			ClusterName:              clusterName,
			KubernetesVersion:        "v1.32.2",
			ControlPlaneMachineCount: ptr.To[int64](1),
			// TODO: make infra provider configurable
			InfrastructureProvider: "docker",
			LogFolder:              filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
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

		err = util.DiscoveryAndWaitForK0smotronControlPlaneInitialized(ctx, capiframework.DiscoveryAndWaitForControlPlaneInitializedInput{
			Lister:  bootstrapClusterProxy.GetClient(),
			Cluster: cluster,
		}, util.GetInterval(e2eConfig, testName, "wait-control-plane"))
		require.NoError(t, err)

		err = util.WaitForK0smotronControlPlaneToBeReady(ctx, bootstrapClusterProxy.GetClient(), clusterName, namespace.Name, util.GetInterval(e2eConfig, testName, "wait-control-plane"))
		require.NoError(t, err)

		fmt.Print("Verifying K0smotronControlPlane version format\n")
		verifyK0smotronControlPlaneVersionFormat(ctx, t, bootstrapClusterProxy, clusterName, namespace.Name)

		// Get the kubeconfig for the workload cluster
		workloadClusterKubeconfig := getWorkloadClusterKubeconfig(ctx, t, bootstrapClusterProxy, clusterName, namespace.Name)

		fmt.Print("Waiting for MachineDeployment to be ready\n")
		require.Eventually(t, func() bool {
			md := &clusterv2.MachineDeployment{}
			err := bootstrapClusterProxy.GetClient().Get(ctx, client.ObjectKey{
				Namespace: namespace.Name,
				Name:      clusterName,
			}, md)
			if err != nil {
				return false
			}
			return md.Status.ReadyReplicas == 2
		}, 5*time.Minute, 10*time.Second, "MachineDeployment failed to become ready")

		fmt.Print("Verifying worker nodes are ready in the workload cluster\n")
		verifyWorkerNodesReady(ctx, t, workloadClusterKubeconfig, 2)

		fmt.Print("MachineDeployment test completed successfully\n")
	})
}

func verifyK0smotronControlPlaneVersionFormat(ctx context.Context, t *testing.T, clusterProxy capiframework.ClusterProxy, clusterName, namespace string) {
	kcp := &unstructured.Unstructured{}
	kcp.SetAPIVersion("controlplane.cluster.x-k8s.io/v1beta1")
	kcp.SetKind("K0smotronControlPlane")

	require.Eventually(t, func() bool {
		err := clusterProxy.GetClient().Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      clusterName,
		}, kcp)
		if err != nil {
			return false
		}

		status, found, err := unstructured.NestedMap(kcp.Object, "status")
		if err != nil || !found {
			return false
		}

		version, found, err := unstructured.NestedString(status, "version")
		if err != nil || !found {
			return false
		}

		if version != "v1.32.2" {
			t.Errorf("Expected version %s, but got: %s", "v1.32.2", version)
			return false
		}

		return true
	}, 3*time.Minute, 10*time.Second, "K0smotronControlPlane version format verification failed")
}

func getWorkloadClusterKubeconfig(ctx context.Context, t *testing.T, clusterProxy capiframework.ClusterProxy, clusterName, namespace string) *rest.Config {
	kubeconfigSecret := &corev1.Secret{}
	require.Eventually(t, func() bool {
		err := clusterProxy.GetClient().Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s-kubeconfig", clusterName),
		}, kubeconfigSecret)
		return err == nil
	}, 2*time.Minute, 10*time.Second, "Failed to get kubeconfig secret")

	kubeconfig, ok := kubeconfigSecret.Data["value"]
	require.True(t, ok, "kubeconfig secret should contain 'value' key")

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	require.NoError(t, err, "Failed to parse kubeconfig")

	return restConfig
}

func verifyWorkerNodesReady(ctx context.Context, t *testing.T, kubeconfig *rest.Config, expectedNodes int) {
	clientset, err := kubernetes.NewForConfig(kubeconfig)
	require.NoError(t, err, "Failed to create Kubernetes clientset")

	require.Eventually(t, func() bool {
		nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return false
		}

		readyCount := 0
		for _, node := range nodes.Items {
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
					readyCount++
					break
				}
			}
		}

		return readyCount == expectedNodes
	}, 5*time.Minute, 10*time.Second, "Expected number of worker nodes not ready")
}
