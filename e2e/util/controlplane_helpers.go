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

package util

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/klog/v2"

	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/util/patch"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func WaitForControlPlaneToBeReady(ctx context.Context, client crclient.Client, controlplane *cpv1beta1.K0sControlPlane) {
	Eventually(func() (bool, error) {
		key := crclient.ObjectKey{
			Namespace: controlplane.Namespace,
			Name:      controlplane.Name,
		}
		if err := client.Get(ctx, key, controlplane); err != nil {
			return false, errors.Wrapf(err, "failed to get controlplane")
		}

		desiredReplicas := controlplane.Spec.Replicas
		statusReplicas := controlplane.Status.Replicas
		updatedReplicas := controlplane.Status.UpdatedReplicas
		readyReplicas := controlplane.Status.ReadyReplicas
		unavailableReplicas := controlplane.Status.UnavailableReplicas

		if statusReplicas != desiredReplicas ||
			updatedReplicas != desiredReplicas ||
			readyReplicas != desiredReplicas ||
			unavailableReplicas > 0 {
			return false, nil
		}

		return true, nil
	}, "20m").Should(BeTrue())
}

// UpgradeControlPlaneAndWaitForUpgradeInput is the input type for UpgradeControlPlaneAndWaitForUpgrade.
type UpgradeControlPlaneAndWaitForUpgradeInput struct {
	ClusterProxy             capiframework.ClusterProxy
	Cluster                  *clusterv1.Cluster
	ControlPlane             *cpv1beta1.K0sControlPlane
	KubernetesUpgradeVersion string
}

// UpgradeControlPlaneAndWaitForUpgrade upgrades a KubeadmControlPlane and waits for it to be upgraded.
func UpgradeControlPlaneAndWaitForUpgrade(ctx context.Context, input UpgradeControlPlaneAndWaitForUpgradeInput) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for UpgradeControlPlaneAndWaitForUpgrade")
	Expect(input.ClusterProxy).ToNot(BeNil(), "Invalid argument. input.ClusterProxy can't be nil when calling UpgradeControlPlaneAndWaitForUpgrade")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling UpgradeControlPlaneAndWaitForUpgrade")
	Expect(input.ControlPlane).ToNot(BeNil(), "Invalid argument. input.ControlPlane can't be nil when calling UpgradeControlPlaneAndWaitForUpgrade")
	Expect(input.KubernetesUpgradeVersion).ToNot(BeNil(), "Invalid argument. input.KubernetesUpgradeVersion can't be empty when calling UpgradeControlPlaneAndWaitForUpgrade")

	mgmtClient := input.ClusterProxy.GetClient()

	Logf("Patching the new kubernetes version to KCP")
	patchHelper, err := patch.NewHelper(input.ControlPlane, mgmtClient)
	Expect(err).ToNot(HaveOccurred())

	input.ControlPlane.Spec.Version = input.KubernetesUpgradeVersion

	Eventually(func() error {
		return patchHelper.Patch(ctx, input.ControlPlane)
	}).Should(Succeed(), "Failed to patch the new kubernetes version to controlplane %s", klog.KObj(input.ControlPlane))

	// TODO: avoid check node conditions because "NodeHealthy" is "False" due to
	// NodeMemoryPressure and NodePIDPressure is False

	// Logf("Waiting for control-plane machines to have the upgraded kubernetes version")
	//capiframework.WaitForControlPlaneMachinesToBeUpgraded(ctx, capiframework.WaitForControlPlaneMachinesToBeUpgradedInput{
	// 	Lister:                   mgmtClient,
	// 	Cluster:                  input.Cluster,
	// 	MachineCount:             int(input.ControlPlane.Spec.Replicas),
	// 	KubernetesUpgradeVersion: input.KubernetesUpgradeVersion,
	// }, "10m")

	WaitForControlPlaneToBeReady(ctx, input.ClusterProxy.GetClient(), input.ControlPlane)

	Logf("Waiting for kube-proxy to have the upgraded kubernetes version")
	workloadCluster := input.ClusterProxy.GetWorkloadCluster(ctx, input.Cluster.Namespace, input.Cluster.Name)
	workloadClient := workloadCluster.GetClient()
	WaitForKubeProxyUpgrade(ctx, WaitForKubeProxyUpgradeInput{
		Getter:            workloadClient,
		KubernetesVersion: input.KubernetesUpgradeVersion,
	}, "10m")
}

func DiscoveryAndWaitForControlPlaneInitialized(ctx context.Context, input capiframework.DiscoveryAndWaitForControlPlaneInitializedInput, intervals ...interface{}) *cpv1beta1.K0sControlPlane {
	Expect(ctx).NotTo(BeNil(), "ctx is required for DiscoveryAndWaitForControlPlaneInitialized")
	Expect(input.Lister).ToNot(BeNil(), "Invalid argument. input.Lister can't be nil when calling DiscoveryAndWaitForControlPlaneInitialized")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling DiscoveryAndWaitForControlPlaneInitialized")

	var controlPlane *cpv1beta1.K0sControlPlane
	Eventually(func(g Gomega) {
		controlPlane = getK0sControlPlaneByCluster(ctx, GetK0sControlPlaneByClusterInput{
			Lister:      input.Lister,
			ClusterName: input.Cluster.Name,
			Namespace:   input.Cluster.Namespace,
		})
		g.Expect(controlPlane).ToNot(BeNil())
	}, "10s", "1s").Should(Succeed(), "Couldn't get the control plane for the cluster %s", klog.KObj(input.Cluster))

	Logf("Waiting for the first control plane machine managed by %s to be provisioned", klog.KObj(controlPlane))
	WaitForOneK0sControlPlaneMachineToExist(ctx, WaitForOneK0sControlPlaneMachineToExistInput{
		Lister:       input.Lister,
		Cluster:      input.Cluster,
		ControlPlane: controlPlane,
	}, intervals...)

	return controlPlane
}

type GetK0sControlPlaneByClusterInput struct {
	Lister      Lister
	ClusterName string
	Namespace   string
}

func getK0sControlPlaneByCluster(ctx context.Context, input GetK0sControlPlaneByClusterInput) *cpv1beta1.K0sControlPlane {
	controlPlaneList := &cpv1beta1.K0sControlPlaneList{}
	Eventually(func() error {
		return input.Lister.List(ctx, controlPlaneList, byClusterOptions(input.ClusterName, input.Namespace)...)
	}).Should(Succeed(), "Failed to list KubeadmControlPlane object for Cluster %s", klog.KRef(input.Namespace, input.ClusterName))
	Expect(len(controlPlaneList.Items)).ToNot(BeNumerically(">", 1), "Cluster %s should not have more than 1 KubeadmControlPlane object", klog.KRef(input.Namespace, input.ClusterName))
	if len(controlPlaneList.Items) == 1 {
		return &controlPlaneList.Items[0]
	}
	return nil
}

// byClusterOptions returns a set of ListOptions that allows to identify all the objects belonging to a Cluster.
func byClusterOptions(name, namespace string) []crclient.ListOption {
	return []crclient.ListOption{
		crclient.InNamespace(namespace),
		crclient.MatchingLabels{
			clusterv1.ClusterNameLabel: name,
		},
	}
}

type WaitForOneK0sControlPlaneMachineToExistInput struct {
	Lister       Lister
	Cluster      *clusterv1.Cluster
	ControlPlane *cpv1beta1.K0sControlPlane
}

// WaitForOneK0sControlPlaneMachineToExist will wait until all control plane machines have node refs.
func WaitForOneK0sControlPlaneMachineToExist(ctx context.Context, input WaitForOneK0sControlPlaneMachineToExistInput, intervals ...interface{}) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for WaitForOneKubeadmControlPlaneMachineToExist")
	Expect(input.Lister).ToNot(BeNil(), "Invalid argument. input.Getter can't be nil when calling WaitForOneKubeadmControlPlaneMachineToExist")
	Expect(input.ControlPlane).ToNot(BeNil(), "Invalid argument. input.ControlPlane can't be nil when calling WaitForOneKubeadmControlPlaneMachineToExist")

	By("Waiting for one control plane node to exist")
	inClustersNamespaceListOption := crclient.InNamespace(input.Cluster.Namespace)
	// ControlPlane labels
	matchClusterListOption := crclient.MatchingLabels{
		clusterv1.MachineControlPlaneLabel: "",
		clusterv1.ClusterNameLabel:         input.Cluster.Name,
	}

	Eventually(func() (bool, error) {
		machineList := &clusterv1.MachineList{}
		if err := input.Lister.List(ctx, machineList, inClustersNamespaceListOption, matchClusterListOption); err != nil {
			Logf("Failed to list the machines: %+v", err)
			return false, err
		}
		count := 0
		for _, machine := range machineList.Items {
			if machine.Status.NodeRef != nil {
				count++
			}
		}
		return count > 0, nil
	}, "10m").Should(BeTrue(), "No Control Plane machines came into existence. ")
}

type WaitForKubeProxyUpgradeInput struct {
	Getter            Getter
	KubernetesVersion string
}

// WaitForKubeProxyUpgrade waits until kube-proxy version matches with the kubernetes version.
func WaitForKubeProxyUpgrade(ctx context.Context, input WaitForKubeProxyUpgradeInput, intervals ...interface{}) {
	By("Ensuring kube-proxy has the correct image")

	// this desired version is sticky to the k0s naming on the kube-proxy image
	versionPrefix := strings.Split(input.KubernetesVersion, "+")[0]
	wantKubeProxyImage := fmt.Sprintf("quay.io/k0sproject/kube-proxy:%s", versionPrefix)

	Eventually(func() (bool, error) {
		ds := &appsv1.DaemonSet{}

		if err := input.Getter.Get(ctx, crclient.ObjectKey{Name: "kube-proxy", Namespace: metav1.NamespaceSystem}, ds); err != nil {
			return false, err
		}

		if strings.HasPrefix(ds.Spec.Template.Spec.Containers[0].Image, wantKubeProxyImage) {
			return true, nil
		}

		return false, nil
	}, intervals...).Should(BeTrue())
}
