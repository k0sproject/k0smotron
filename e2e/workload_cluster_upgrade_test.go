/*
Copyright 2024.

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
	"time"

	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	"github.com/k0sproject/k0smotron/e2e/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiutil "sigs.k8s.io/cluster-api/util"
)

// Validation of the correct operation of k0smotron when the
// K0sControlPlane object is updated. It simulates a typical user workflow that includes:
//
// 1. Creation of a workload cluster.
//   - Ensures the cluster becomes operational.
//
// 2. Updating the control plane version.
//   - Verifies the cluster status aligns with the expected state after the update.
//
// 3. Performing a subsequent control plane version update.
//   - Confirms the cluster status is consistent and desired post-update.
var _ = Describe("When testing workload cluster upgrade", Ordered, func() {
	var (
		specName     = "workload-upgrade"
		controlPlane *cpv1beta1.K0sControlPlane
		namespace    *corev1.Namespace
		cluster      *clusterv1.Cluster
	)

	BeforeEach(func() {
		Expect(e2eConfig.Variables).To(HaveKey(KubernetesVersion))
		Expect(e2eConfig.Variables).To(HaveKey(KubernetesVersionFirstUpgradeTo))

		// Setup a Namespace where to host objects for this spec and create a watcher for the namespace events.
		namespace, _ = capiframework.SetupSpecNamespace(ctx, specName, managementClusterProxy, artifactFolder, nil)
	})

	AfterEach(func() {
		// Dumps all the resources in the spec namespace, then cleanups the cluster object and the spec namespace itself.
		capiframework.DumpSpecResourcesAndCleanup(ctx, specName, managementClusterProxy, artifactFolder, namespace, cancelWatches, cluster, e2eConfig.GetIntervals, skipCleanup)

	})

	It("Should create and upgrade a workload cluster", func() {

		clusterName := fmt.Sprintf("%s-%s", specName, capiutil.RandomString(6))

		By("Creating a workload cluster")
		workloadClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
			ClusterctlConfigPath: clusterctlConfigPath,
			KubeconfigPath:       managementClusterProxy.GetKubeconfigPath(),
			// select cluster templates
			Flavor: "ooc",

			Namespace:                namespace.Name,
			ClusterName:              clusterName,
			KubernetesVersion:        e2eConfig.GetVariable(KubernetesVersion),
			ControlPlaneMachineCount: ptr.To[int64](3),
			// TODO: make infra provider configurable
			InfrastructureProvider: "docker",
			LogFolder:              filepath.Join(artifactFolder, "clusters", managementClusterProxy.GetName()),
			ClusterctlVariables: map[string]string{
				"CLUSTER_NAME": clusterName,
				"NAMESPACE":    namespace.Name,
			},
		})
		Expect(workloadClusterTemplate).ToNot(BeNil(), "Failed to get the cluster template")

		// Periodically try to apply cluster template because CAPI or K0smotron pods may not be running yet.
		Eventually(func() error {
			return managementClusterProxy.CreateOrUpdate(ctx, workloadClusterTemplate)
		}, 10*time.Second).Should(Succeed(), "Failed to apply the cluster template")

		cluster = capiframework.DiscoveryAndWaitForCluster(ctx, capiframework.DiscoveryAndWaitForClusterInput{
			Getter:    managementClusterProxy.GetClient(),
			Namespace: namespace.Name,
			Name:      clusterName,
		})

		controlPlane = util.DiscoveryAndWaitForControlPlaneInitialized(ctx, capiframework.DiscoveryAndWaitForControlPlaneInitializedInput{
			Lister:  managementClusterProxy.GetClient(),
			Cluster: cluster,
		})

		By("Upgrading the Kubernetes control-plane version")
		util.UpgradeControlPlaneAndWaitForUpgrade(ctx, util.UpgradeControlPlaneAndWaitForUpgradeInput{
			ClusterProxy:             managementClusterProxy,
			Cluster:                  cluster,
			ControlPlane:             controlPlane,
			KubernetesUpgradeVersion: e2eConfig.GetVariable(KubernetesVersionFirstUpgradeTo),
		})

		By("Upgrading the Kubernetes control-plane version again")
		util.UpgradeControlPlaneAndWaitForUpgrade(ctx, util.UpgradeControlPlaneAndWaitForUpgradeInput{
			ClusterProxy:             managementClusterProxy,
			Cluster:                  cluster,
			ControlPlane:             controlPlane,
			KubernetesUpgradeVersion: e2eConfig.GetVariable(KubernetesVersionSecondUpgradeTo),
		})
	})

})
