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
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/k0smotron/v2/e2e/util"
	"github.com/k0sproject/k0smotron/v2/internal/autopilot"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/wait"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimev1 "sigs.k8s.io/cluster-api/api/runtime/v1beta2"
	capiframework "sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	testName := "workload-inplace-upgrade"

	require.NotEmpty(t, flavor, "a flavor between InPlace, InPlaceCAPI, Recreate or RecreateDeleteFirst needs to be specified for this test")

	// Setup a Namespace where to host objects for this spec and create a watcher for the namespace events.
	namespace, _ := util.SetupSpecNamespace(ctx, testName, bootstrapClusterProxy, artifactFolder)

	clusterName := fmt.Sprintf("%s-%s", testName, capiutil.RandomString(6))

	workloadClusterTemplate := clusterctl.ConfigCluster(ctx, clusterctl.ConfigClusterInput{
		ClusterctlConfigPath:     clusterctlConfigPath,
		KubeconfigPath:           bootstrapClusterProxy.GetKubeconfigPath(),
		Flavor:                   strings.ToLower(flavor),
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

	// For Inplace upgrades we need to wait for the controlplane to have all the replicas ready before upgrading it again.
	if strings.HasPrefix(strings.ToLower(flavor), "inplace") {
		err = util.WaitForControlPlaneToBeReady(ctx, bootstrapClusterProxy.GetClient(), controlPlane, util.GetInterval(e2eConfig, testName, "wait-kube-proxy-upgrade"))
		require.NoError(t, err)
	}

	var md *clusterv1.MachineDeployment
	if flavor == "InPlaceCAPI" {
		// Only the InPlaceCAPI flavor supports worker nodes updates.
		md, err = util.DiscoveryAndWaitForMachineDeploymentReady(ctx, capiframework.DiscoveryAndWaitForMachineDeploymentsInput{
			Lister:  bootstrapClusterProxy.GetClient(),
			Cluster: cluster,
		})
		require.NoError(t, err)
		require.NotNil(t, md)

	}

	preMachines := &clusterv1.MachineList{}
	err = bootstrapClusterProxy.GetClient().List(ctx, preMachines,
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels{clusterv1.ClusterNameLabel: cluster.Name},
	)
	require.NoError(t, err)

	upgradeChecks := newControlPlaneUpgradeChecks(preMachines)

	fmt.Println("Upgrading the Kubernetes control-plane version")
	err = util.UpgradeControlPlaneAndWaitForReadyUpgrade(ctx, util.UpgradeControlPlaneAndWaitForUpgradeInput{
		ClusterProxy:                     bootstrapClusterProxy,
		Cluster:                          cluster,
		ControlPlane:                     controlPlane,
		KubernetesUpgradeVersion:         e2eConfig.MustGetVariable(KubernetesVersionFirstUpgradeTo),
		WaitForKubeProxyUpgradeInterval:  util.GetInterval(e2eConfig, testName, "wait-kube-proxy-upgrade"),
		WaitForControlPlaneReadyInterval: util.GetInterval(e2eConfig, testName, "wait-control-plane"),
		DuringUpgradeCheck:               upgradeChecks.DuringUpgradeCheck,
		PostUpgradeCheck:                 upgradeChecks.PostUpgradeCheck,
	})
	require.NoError(t, err)

	if flavor == "InPlaceCAPI" {
		// Only the InPlaceCAPI flavor supports worker nodes updates.
		fmt.Println("Upgrading the Kubernetes worker version")
		err = util.UpgradeMachineDeploymentAndWaitForReadyUpgrade(ctx, util.UpgradeMachineDeploymentAndWaitForReadyUpgradeInput{
			ClusterProxy:                            bootstrapClusterProxy,
			Cluster:                                 cluster,
			MachineDeployment:                       md,
			KubernetesUpgradeVersion:                e2eConfig.MustGetVariable(KubernetesVersionFirstUpgradeTo),
			WaitForMachineDeploymentUpgradeInterval: util.GetInterval(e2eConfig, testName, "wait-md-upgrade"),
		})
		require.NoError(t, err)
	}

	preMachines = &clusterv1.MachineList{}
	err = bootstrapClusterProxy.GetClient().List(ctx, preMachines,
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels{clusterv1.ClusterNameLabel: cluster.Name},
	)
	require.NoError(t, err)

	upgradeChecks = newControlPlaneUpgradeChecks(preMachines)

	fmt.Println("Upgrading the Kubernetes control-plane version again")
	err = util.UpgradeControlPlaneAndWaitForReadyUpgrade(ctx, util.UpgradeControlPlaneAndWaitForUpgradeInput{
		ClusterProxy:                     bootstrapClusterProxy,
		Cluster:                          cluster,
		ControlPlane:                     controlPlane,
		KubernetesUpgradeVersion:         e2eConfig.MustGetVariable(KubernetesVersionSecondUpgradeTo),
		WaitForKubeProxyUpgradeInterval:  util.GetInterval(e2eConfig, testName, "wait-kube-proxy-upgrade"),
		WaitForControlPlaneReadyInterval: util.GetInterval(e2eConfig, testName, "wait-control-plane"),
		DuringUpgradeCheck:               upgradeChecks.DuringUpgradeCheck,
		PostUpgradeCheck:                 upgradeChecks.PostUpgradeCheck,
	})
	require.NoError(t, err)

	if flavor == "InPlaceCAPI" {
		// Only the InPlaceCAPI flavor supports worker nodes updates.
		fmt.Println("Upgrading the Kubernetes worker version again")
		err = util.UpgradeMachineDeploymentAndWaitForReadyUpgrade(ctx, util.UpgradeMachineDeploymentAndWaitForReadyUpgradeInput{
			ClusterProxy:                            bootstrapClusterProxy,
			Cluster:                                 cluster,
			MachineDeployment:                       md,
			KubernetesUpgradeVersion:                e2eConfig.MustGetVariable(KubernetesVersionSecondUpgradeTo),
			WaitForMachineDeploymentUpgradeInterval: util.GetInterval(e2eConfig, testName, "wait-md-upgrade"),
		})
		require.NoError(t, err)
	}
}

// upgradeChecks bundles the strategy-specific checks that are injected into the
// control plane upgrade helper.
type upgradeChecks struct {
	DuringUpgradeCheck func(ctx context.Context, input util.UpgradeControlPlaneAndWaitForUpgradeInput) (func() error, error)
	PostUpgradeCheck   func(ctx context.Context, input util.UpgradeControlPlaneAndWaitForUpgradeInput) error
}

// newControlPlaneUpgradeChecks returns the strategy-specific checks for the
// currently selected flavor. In-place flavors verify which update path was used
// and that no control plane machine is recreated; recreate flavors verify that
// the machines are replaced and the desired count is preserved.
func newControlPlaneUpgradeChecks(preMachines *clusterv1.MachineList) upgradeChecks {
	return upgradeChecks{
		DuringUpgradeCheck: func(ctx context.Context, input util.UpgradeControlPlaneAndWaitForUpgradeInput) (func() error, error) {
			switch {
			case strings.EqualFold(flavor, "InPlace"):
				return startStandaloneInPlaceCheck(ctx, input), nil
			case strings.EqualFold(flavor, "InPlaceCAPI"):
				return startCAPIInPlaceCheck(ctx, input), nil
			case strings.EqualFold(flavor, "RecreateDeleteFirst"):
				return startRecreateDeleteFirstMachineCountCheck(ctx, input), nil
			default:
				return nil, nil
			}
		},
		PostUpgradeCheck: func(ctx context.Context, input util.UpgradeControlPlaneAndWaitForUpgradeInput) error {
			return validateControlPlaneMachineRollout(ctx, input, preMachines)
		},
	}
}

// validateControlPlaneMachineRollout verifies update-strategy specific machine expectations
// after a control plane upgrade. For in-place upgrades no control plane Machine must be
// recreated. For recreate based strategies the final control plane machines must be new
// machines and their count must match the desired number of replicas.
func validateControlPlaneMachineRollout(ctx context.Context, input util.UpgradeControlPlaneAndWaitForUpgradeInput, preMachines *clusterv1.MachineList) error {
	postMachines := &clusterv1.MachineList{}
	if err := input.ClusterProxy.GetClient().List(ctx, postMachines,
		client.InNamespace(input.Cluster.Namespace),
		client.MatchingLabels{clusterv1.ClusterNameLabel: input.Cluster.Name},
	); err != nil {
		return fmt.Errorf("failed to list control plane machines after upgrade: %w", err)
	}

	desiredReplicas := int(input.ControlPlane.Spec.Replicas)
	preControlPlaneMachines := controlPlaneMachineNamesByUID(preMachines)
	postControlPlaneMachines := controlPlaneMachineNamesByUID(postMachines)

	fmt.Printf("Control plane machines after upgrade: %d (desired %d)\n", len(postControlPlaneMachines), desiredReplicas)

	switch {
	case strings.HasPrefix(strings.ToLower(flavor), "inplace"):
		if len(preControlPlaneMachines) != len(postControlPlaneMachines) {
			return fmt.Errorf("in-place upgrade recreated control plane machines: expected %d machines, got %d", len(preControlPlaneMachines), len(postControlPlaneMachines))
		}
		for uid := range postControlPlaneMachines {
			if _, ok := preControlPlaneMachines[uid]; !ok {
				return fmt.Errorf("in-place upgrade recreated control plane machine %s", postControlPlaneMachines[uid])
			}
		}
		fmt.Println("In-place upgrade did not recreate any control plane machine")
	case strings.EqualFold(flavor, "Recreate"), strings.EqualFold(flavor, "RecreateDeleteFirst"):
		if len(postControlPlaneMachines) != desiredReplicas {
			return fmt.Errorf("recreate upgrade ended with %d control plane machines, expected %d", len(postControlPlaneMachines), desiredReplicas)
		}
		var reusedMachines []string
		for uid := range postControlPlaneMachines {
			if name, ok := preControlPlaneMachines[uid]; ok {
				reusedMachines = append(reusedMachines, name)
			}
		}
		if len(reusedMachines) > 0 {
			return fmt.Errorf("recreate upgrade reused %d control plane machine(s): %s", len(reusedMachines), strings.Join(reusedMachines, ", "))
		}
		fmt.Println("Recreate upgrade replaced all control plane machines and kept the desired count")
	default:
		return fmt.Errorf("unsupported update strategy for machine rollout validation: %s", flavor)
	}

	return nil
}

// controlPlaneMachineNamesByUID returns the control plane machine names keyed by
// their UID for the given machine list.
func controlPlaneMachineNamesByUID(machines *clusterv1.MachineList) map[string]string {
	byUID := make(map[string]string, len(machines.Items))
	for i := range machines.Items {
		m := machines.Items[i]
		if m.Labels[clusterv1.MachineControlPlaneLabel] == "true" {
			byUID[string(m.GetUID())] = m.Name
		}
	}
	return byUID
}

// startStandaloneInPlaceCheck verifies that the standalone in-place update path is
// used: an autopilot Plan must appear in the workload cluster and no control plane
// Machine may carry the CAPI in-place update annotations while the upgrade runs.
func startStandaloneInPlaceCheck(ctx context.Context, input util.UpgradeControlPlaneAndWaitForUpgradeInput) func() error {
	checkCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	planSeen := false

	workloadClientSet := input.ClusterProxy.GetWorkloadCluster(ctx, input.Cluster.Namespace, input.Cluster.Name).GetClientSet()

	go func() {
		done <- wait.PollUntilContextCancel(checkCtx, time.Second, true, func(fctx context.Context) (bool, error) {
			if err := failOnInPlaceUpdateAnnotations(fctx, input); err != nil {
				return false, err
			}
			plan, err := autopilot.GetPlan(fctx, workloadClientSet)
			if err != nil {
				// The plan has not been created yet; keep polling.
				return false, nil
			}
			targetVersion, err := autopilot.GetPlanTargetVersion(plan)
			if err != nil {
				return false, err
			}
			if targetVersion != input.KubernetesUpgradeVersion {
				return false, fmt.Errorf("standalone in-place upgrade created an autopilot plan for version %s, expected %s", targetVersion, input.KubernetesUpgradeVersion)
			}
			planSeen = true
			return true, nil
		})
	}()

	return func() error {
		cancel()
		err := <-done
		if err == nil {
			return nil
		}
		if errors.Is(err, context.Canceled) {
			if planSeen {
				return nil
			}
			return errors.New("standalone in-place upgrade did not create an autopilot plan in the workload cluster")
		}
		return err
	}
}

// failOnInPlaceUpdateAnnotations returns an error if any control plane Machine
// carries the CAPI in-place update annotations.
func failOnInPlaceUpdateAnnotations(ctx context.Context, input util.UpgradeControlPlaneAndWaitForUpgradeInput) error {
	machines := &clusterv1.MachineList{}
	if err := input.ClusterProxy.GetClient().List(ctx, machines,
		client.InNamespace(input.Cluster.Namespace),
		client.MatchingLabels{clusterv1.ClusterNameLabel: input.Cluster.Name},
	); err != nil {
		return err
	}
	for i := range machines.Items {
		m := machines.Items[i]
		if m.Labels[clusterv1.MachineControlPlaneLabel] != "true" {
			continue
		}
		if hasInPlaceUpdateAnnotations(m.Annotations) {
			return fmt.Errorf("standalone in-place upgrade unexpectedly used CAPI in-place update annotations on machine %s", m.Name)
		}
	}
	return nil
}

// startCAPIInPlaceCheck verifies that the runtime extension in-place update path is
// used: at least one control plane Machine must carry the CAPI in-place update
// annotations while the upgrade runs.
func startCAPIInPlaceCheck(ctx context.Context, input util.UpgradeControlPlaneAndWaitForUpgradeInput) func() error {
	checkCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)

	go func() {
		done <- wait.PollUntilContextCancel(checkCtx, time.Second, true, func(fctx context.Context) (bool, error) {
			machines := &clusterv1.MachineList{}
			if err := input.ClusterProxy.GetClient().List(fctx, machines,
				client.InNamespace(input.Cluster.Namespace),
				client.MatchingLabels{clusterv1.ClusterNameLabel: input.Cluster.Name},
			); err != nil {
				return false, err
			}
			for i := range machines.Items {
				m := machines.Items[i]
				if m.Labels[clusterv1.MachineControlPlaneLabel] == "true" && hasInPlaceUpdateAnnotations(m.Annotations) {
					return true, nil
				}
			}
			return false, nil
		})
	}()

	return func() error {
		cancel()
		err := <-done
		if errors.Is(err, context.Canceled) {
			return errors.New("InPlaceCAPI upgrade did not trigger the runtime extension: no control plane machine had CAPI in-place update annotations")
		}
		return err
	}
}

// hasInPlaceUpdateAnnotations reports whether the given Machine annotations
// indicate an in-place update triggered through the CAPI runtime extension.
func hasInPlaceUpdateAnnotations(annotations map[string]string) bool {
	if _, ok := annotations[clusterv1.UpdateInProgressAnnotation]; ok {
		return true
	}
	_, ok := annotations[runtimev1.PendingHooksAnnotation]
	return ok
}

// startRecreateDeleteFirstMachineCountCheck starts a background check that makes sure
// the RecreateDeleteFirst strategy never exceeds the desired number of control plane machines
// while the upgrade is in progress. The returned stop function waits for the check to finish
// and returns its error, if any.
func startRecreateDeleteFirstMachineCountCheck(ctx context.Context, input util.UpgradeControlPlaneAndWaitForUpgradeInput) func() error {
	checkCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	desiredReplicas := int(input.ControlPlane.Spec.Replicas)

	go func() {
		done <- wait.PollUntilContextCancel(checkCtx, time.Second, true, func(fctx context.Context) (bool, error) {
			machineList := &clusterv1.MachineList{}
			if err := input.ClusterProxy.GetClient().List(fctx, machineList,
				client.InNamespace(input.Cluster.Namespace),
				client.MatchingLabels{clusterv1.ClusterNameLabel: input.Cluster.Name},
			); err != nil {
				return false, err
			}
			count := 0
			for _, m := range machineList.Items {
				if m.Labels[clusterv1.MachineControlPlaneLabel] == "true" {
					count++
				}
			}
			if count > desiredReplicas {
				return false, fmt.Errorf("RecreateDeleteFirst upgrade exceeded desired control plane machine count: got %d, expected max %d", count, desiredReplicas)
			}
			return false, nil
		})
	}()

	return func() error {
		cancel()
		err := <-done
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}
