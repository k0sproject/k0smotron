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
	"os"

	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	"github.com/k0sproject/k0smotron/e2e/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
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
var _ = When("upgrading a workload cluster", Ordered, func() {
	var (
		workloadCluster *clusterv1.Cluster
		controlPlane    *cpv1beta1.K0sControlPlane
	)

	It("Should create a workload cluster", func() {
		workloadYaml, err := os.ReadFile("")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(managementClusterProxy.CreateOrUpdate(ctx, workloadYaml)).ShouldNot(HaveOccurred())

		workloadCluster = capiframework.DiscoveryAndWaitForCluster(ctx, capiframework.DiscoveryAndWaitForClusterInput{
			Getter:    managementClusterProxy.GetClient(),
			Namespace: "default",
			Name:      "docker-test-cluster",
		})

		controlPlane = util.DiscoveryAndWaitForControlPlaneInitialized(ctx, capiframework.DiscoveryAndWaitForControlPlaneInitializedInput{
			Lister:  managementClusterProxy.GetClient(),
			Cluster: workloadCluster,
		})

		util.WaitForControlPlaneAndMachinesReady(ctx, managementClusterProxy.GetClient(), controlPlane)
	})

	It("Should upgrade a workload cluster controlplane version", func() {
		util.UpgradeControlPlaneAndWaitForUpgrade(ctx, util.UpgradeControlPlaneAndWaitForUpgradeInput{
			ClusterProxy:             managementClusterProxy,
			Cluster:                  workloadCluster,
			ControlPlane:             controlPlane,
			KubernetesUpgradeVersion: "v1.30.2+k0s.0",
		})
	})

	It("Should update a worload cluster controlplane version again", func() {
		util.UpgradeControlPlaneAndWaitForUpgrade(ctx, util.UpgradeControlPlaneAndWaitForUpgradeInput{
			ClusterProxy:             managementClusterProxy,
			Cluster:                  workloadCluster,
			ControlPlane:             controlPlane,
			KubernetesUpgradeVersion: "v1.31.2+k0s.0",
		})
	})

})
