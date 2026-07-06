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
	"bytes"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/k0sproject/k0smotron/v2/e2e/util"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/test/framework"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiutil "sigs.k8s.io/cluster-api/util"
	utilyaml "sigs.k8s.io/cluster-api/util/yaml"
)

const (
	clusterclassNamespacePlaceholder = "clusterclass-namespace-placeholder"
	clusterNamePlaceholder           = "cluster-name-placeholder"
)

func TestWorkloadClusterUpgrade(t *testing.T) {
	setupAndRun(t, workloadClusterUpgradeSpec)
}

// Validation of the correct operation of k0smotron when the
// K0sControlPlane object is updated. It simulates a typical user workflow that includes:
//
// 1. Creation of a workload cluster.
//   - Ensures the cluster becomes operational.
//
// 2. Updating the control plane version using the selected (flavor) upgrade strategy.
//   - Verifies the cluster status aligns with the expected state after the update.
//
// 3. Performing a subsequent control plane version upgrade using the selected (flavor) upgrade strategy.
//   - Confirms the cluster status is consistent and desired post-update.
func workloadClusterUpgradeSpec(t *testing.T) {
	testName := "workload-upgrade"

	require.NotEmpty(t, flavor, "a flavor between InPlace, Recreate or RecreateDeleteFirst needs to be specified for this test")

	// Setup a Namespace where to host objects for this spec and create a watcher for the namespace events.
	clusterClassNamespace, _ := util.SetupSpecNamespace(ctx, testName, bootstrapClusterProxy, artifactFolder)

	clusterClassName := fmt.Sprintf("%s-%s", testName, capiutil.RandomString(6))

	clusterWithCCTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
		ClusterctlConfigPath:     clusterctlConfigPath,
		KubeconfigPath:           bootstrapClusterProxy.GetKubeconfigPath(),
		Flavor:                   "clusterclass",
		Namespace:                clusterclassNamespacePlaceholder,
		ClusterName:              clusterClassName,
		KubernetesVersion:        e2eConfig.MustGetVariable(KubernetesVersion),
		ControlPlaneMachineCount: new(int64(3)),
		// TODO: make infra provider configurable
		InfrastructureProvider: "docker",
		LogFolder:              filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
		ClusterctlVariables: map[string]string{
			"K0S_VERSION":            e2eConfig.MustGetVariable(K0sVersion),
			"CLUSTERCLASS_NAMESPACE": clusterClassNamespace.Name,
			"UPDATE_STRATEGY":        flavor,
		},
	})
	require.NotNil(t, clusterWithCCTemplate)
	fmt.Println(string(clusterWithCCTemplate))

	clusterClassTemplate, clusterTemplate, err := extractClusterClassAndClusterFromTemplate(clusterWithCCTemplate)
	require.NoError(t, err)
	require.NotNil(t, clusterClassTemplate)
	require.NotNil(t, clusterTemplate)

	clusterClassTemplate = bytes.ReplaceAll(clusterClassTemplate, []byte(clusterclassNamespacePlaceholder), []byte(clusterClassNamespace.Name))
	require.Eventually(t, func() bool {
		return bootstrapClusterProxy.CreateOrUpdate(ctx, clusterClassTemplate) == nil
	}, 10*time.Second, 1*time.Second, "Failed to apply the clusterclass template")

	wg := &sync.WaitGroup{}
	errCh := make(chan error)
	for i := 0; i < nWorkloads; i++ {
		clusterName := fmt.Sprintf("cluster-%d", i)
		wg.Add(1)
		go func(clusterName string) {
			defer wg.Done()

			clusterNamespace, _ := framework.CreateNamespaceAndWatchEvents(ctx, framework.CreateNamespaceAndWatchEventsInput{
				Creator:   bootstrapClusterProxy.GetClient(),
				ClientSet: bootstrapClusterProxy.GetClientSet(),
				Name:      clusterName,
				LogFolder: filepath.Join(artifactFolder, "clusters", bootstrapClusterProxy.GetName()),
			})

			customClusterTemplate := bytes.ReplaceAll(clusterTemplate, []byte(clusterNamePlaceholder), []byte(clusterName))
			customClusterTemplate = bytes.ReplaceAll(customClusterTemplate, []byte(clusterclassNamespacePlaceholder), []byte(clusterNamespace.Name))

			err := bootstrapClusterProxy.CreateOrUpdate(ctx, customClusterTemplate)
			if err != nil {
				errCh <- fmt.Errorf("failed to apply the cluster template for cluster %s: %w", clusterName, err)
				return
			}

			cluster, err := util.DiscoveryAndWaitForCluster(ctx, capiframework.DiscoveryAndWaitForClusterInput{
				Getter:    bootstrapClusterProxy.GetClient(),
				Namespace: clusterNamespace.Name,
				Name:      clusterName,
			}, util.GetInterval(e2eConfig, testName, "wait-cluster"))
			if err != nil {
				errCh <- fmt.Errorf("failed to discover and wait for cluster %s: %w", clusterName, err)
				return
			}

			t.Cleanup(func() {
				util.DumpSpecResourcesAndCleanup(
					ctx,
					testName,
					bootstrapClusterProxy,
					artifactFolder,
					clusterNamespace,
					cluster,
					util.GetInterval(e2eConfig, testName, "wait-delete-cluster"),
					skipCleanup,
					clusterctlConfigPath,
				)
			})

			err = checkUpgradeOnCluster(&checkUpgradeOnClusterInput{
				testName:        testName,
				clusterTemplate: customClusterTemplate,
				cluster:         cluster,
				wg:              wg,
			})
			if err != nil {
				errCh <- err
			}
		}(clusterName)

	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("All clusters have been upgraded successfully")
	case err := <-errCh:
		t.Fatalf("Error upgrading clusters: %v", err)
	case <-ctx.Done():
		t.Fatalf("Test context cancelled: %v", ctx.Err())
	}
}

type checkUpgradeOnClusterInput struct {
	testName        string
	clusterTemplate []byte
	cluster         *clusterv1.Cluster
	wg              *sync.WaitGroup
}

func checkUpgradeOnCluster(input *checkUpgradeOnClusterInput) error {
	controlPlane, err := util.DiscoveryAndWaitForControlPlaneInitialized(ctx, capiframework.DiscoveryAndWaitForControlPlaneInitializedInput{
		Lister:  bootstrapClusterProxy.GetClient(),
		Cluster: input.cluster,
	}, util.GetInterval(e2eConfig, input.testName, "wait-controllers"))
	if err != nil {
		return fmt.Errorf("failed to discover and wait for control plane to be initialized: %w", err)
	}

	// For Inplace upgrades we need to wait for the controlplane to have all the replicas ready before upgrading it again.
	if flavor == "InPlace" {
		err = util.WaitForControlPlaneToBeReady(ctx, bootstrapClusterProxy.GetClient(), controlPlane, util.GetInterval(e2eConfig, input.testName, "wait-kube-proxy-upgrade"))
		if err != nil {
			return fmt.Errorf("failed to wait for control plane to be ready: %w", err)
		}
	}

	fmt.Printf("Upgrading the Kubernetes control-plane version to %s in cluster %s/%s\n", e2eConfig.MustGetVariable(K0sVersionFirstUpgradeTo), input.cluster.Namespace, input.cluster.Name)
	err = util.UpgradeClusterTopologyAndWaitForReadyUpgrade(ctx, util.UpgradeClusterTopologyAndWaitForReadyUpgradeInput{
		ClusterProxy:                     bootstrapClusterProxy,
		Cluster:                          input.cluster,
		ControlPlane:                     controlPlane,
		GetLister:                        bootstrapClusterProxy.GetClient(),
		KubernetesUpgradeVersion:         e2eConfig.MustGetVariable(K0sVersionFirstUpgradeTo),
		WaitForKubeProxyUpgradeInterval:  util.GetInterval(e2eConfig, input.testName, "wait-kube-proxy-upgrade"),
		WaitForControlPlaneReadyInterval: util.GetInterval(e2eConfig, input.testName, "wait-control-plane"),
	})
	if err != nil {
		return fmt.Errorf("failed to upgrade cluster topology to k0s version %s and wait for ready upgrade: %w", e2eConfig.MustGetVariable(K0sVersionFirstUpgradeTo), err)
	}

	fmt.Printf("Upgrading the Kubernetes control-plane version to %s in cluster %s/%s\n", e2eConfig.MustGetVariable(K0sVersionSecondUpgradeTo), input.cluster.Namespace, input.cluster.Name)
	err = util.UpgradeClusterTopologyAndWaitForReadyUpgrade(ctx, util.UpgradeClusterTopologyAndWaitForReadyUpgradeInput{
		ClusterProxy:                     bootstrapClusterProxy,
		Cluster:                          input.cluster,
		ControlPlane:                     controlPlane,
		GetLister:                        bootstrapClusterProxy.GetClient(),
		KubernetesUpgradeVersion:         e2eConfig.MustGetVariable(K0sVersionSecondUpgradeTo),
		WaitForKubeProxyUpgradeInterval:  util.GetInterval(e2eConfig, input.testName, "wait-kube-proxy-upgrade"),
		WaitForControlPlaneReadyInterval: util.GetInterval(e2eConfig, input.testName, "wait-control-plane"),
	})
	if err != nil {
		return fmt.Errorf("failed to upgrade cluster topology to k0s version %s and wait for ready upgrade: %w", e2eConfig.MustGetVariable(K0sVersionSecondUpgradeTo), err)
	}

	return nil
}

func extractClusterClassAndClusterFromTemplate(manifestYAML []byte) ([]byte, []byte, error) {
	objs, err := utilyaml.ToUnstructured(manifestYAML)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert clusterclass and cluster yaml to unstructured: %w", err)
	}
	clusterObjs := []unstructured.Unstructured{}
	clusterClassAndTemplates := []unstructured.Unstructured{}
	for _, obj := range objs {
		if obj.GroupVersionKind().GroupKind() == clusterv1.GroupVersion.WithKind("Cluster").GroupKind() {
			clusterObjs = append(clusterObjs, obj)
		} else if obj.GroupVersionKind().GroupKind() == corev1.SchemeGroupVersion.WithKind("ConfigMap").GroupKind() {
			clusterObjs = append(clusterObjs, obj)
		} else {
			clusterClassAndTemplates = append(clusterClassAndTemplates, obj)
		}
	}
	clusterObjsYAML, err := utilyaml.FromUnstructured(clusterObjs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert cluster unstructured to yaml: %w", err)
	}
	clusterClassYAML, err := utilyaml.FromUnstructured(clusterClassAndTemplates)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert clusterclass and templates unstructured to yaml: %w", err)
	}
	return clusterClassYAML, clusterObjsYAML, nil
}
