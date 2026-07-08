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

package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	kutil "github.com/k0sproject/k0smotron/v2/internal/controller/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv2 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	"github.com/k0sproject/version"
)

var (
	errUnsupportedPlanState = errors.New("unsupported plan state")
)

func (c *K0sController) updateStatus(ctx context.Context, controlplane *controlplane) (err error) {
	logger := log.FromContext(ctx)

	defer func() {
		if err != nil {
			if errors.Is(err, kutil.ErrNotReady) {
				logger.Info("Skipping availability computation since the control plane is not ready yet")
				return
			}
		}
		// The availability of a controlplane is computed in the same way regardless of the type of strategy followed for its upgrade.
		c.computeAvailability(ctx, controlplane, logger)
	}()

	controlplane.kcp.Status.Selector = collections.ControlPlaneSelectorForCluster(controlplane.cluster.Name).String()

	return computeReplicas(controlplane)
}

func computeReplicas(controlplane *controlplane) error {
	controlplane.kcp.Status.Replicas = new(int32(len(controlplane.activeMachines)))
	readyReplicas := 0
	unavailableReplicas := 0
	// Count the machines in different states
	for _, machine := range controlplane.activeMachines {
		switch machine.Status.Phase {
		case string(clusterv2.MachinePhaseRunning):
			readyReplicas++
		case string(clusterv2.MachinePhaseProvisioned):
			// If we're running without --enable-worker, the machine will never transition
			// to running state, so we need to count it as ready when it's provisioned
			if !controlplane.kcp.WorkerEnabled() {
				readyReplicas++
			} else {
				unavailableReplicas++
			}
		case string(clusterv2.MachinePhaseDeleting), string(clusterv2.MachinePhaseDeleted):
			// Do nothing
		default:
			unavailableReplicas++
		}
	}

	// If some machines are missing, count them as unavailable
	if int(controlplane.kcp.Spec.Replicas) > controlplane.activeMachines.Len() {
		unavailableReplicas += int(controlplane.kcp.Spec.Replicas) - controlplane.activeMachines.Len()
	}

	controlplane.kcp.Status.ReadyReplicas = new(int32(readyReplicas))
	controlplane.kcp.Status.UpToDateReplicas = new(int32(controlplane.upToDateMachines.Len()))
	controlplane.kcp.Status.AvailableReplicas = new(int32(controlplane.activeMachines.Len() - unavailableReplicas))

	// Find the lowest version
	lowestMachineVersion, err := minVersion(controlplane.activeMachines)
	if err != nil {
		log.Log.Error(err, "Failed to get the lowest version")
		return err
	}

	controlplane.kcp.Status.Version = lowestMachineVersion

	// If kcp has suffix but machines don't, we need to add it to minVersion
	// Otherwise CAPI topology will not be able to match the versions and might try to recreate the machines
	// or restrict the upgrade path
	if strings.Contains(controlplane.kcp.Spec.Version, "+") && !strings.Contains(lowestMachineVersion, "+") && lowestMachineVersion != "" {
		// Get the suffix from kcp version
		suffix := strings.Split(controlplane.kcp.Spec.Version, "+")[1]
		controlplane.kcp.Status.Version = controlplane.kcp.Status.Version + "+" + suffix
	}

	// If the controlplane spec does NOT have workers enabled
	// we need to mark the controlplane as externally managed
	// Otherwise CAPI assumes it'll find node objects for the machines
	// TODO Check with upstream CAPI folks whether this is the correct approach in this case when
	// we still run the controlplane on Machines
	if !controlplane.kcp.WorkerEnabled() {
		controlplane.kcp.Status.ExternalManagedControlPlane = new(true)
	}

	setScalingConditions(controlplane)

	return nil
}

func setScalingConditions(controlplane *controlplane) {
	upToDateReplicas := controlplane.upToDateMachines.Len()

	if upToDateReplicas < int(controlplane.kcp.Spec.Replicas) {
		conditions.Set(controlplane.kcp, metav1.Condition{
			Type:   string(cpv1beta2.K0sControlPlaneScalingUpCondition),
			Status: metav1.ConditionTrue,
			Reason: cpv1beta2.K0sControlPlaneScalingUpReason,
			Message: fmt.Sprintf("Control plane is scaling up: %d/%d",
				upToDateReplicas, controlplane.kcp.Spec.Replicas),
		})
	} else {
		conditions.Set(controlplane.kcp, metav1.Condition{
			Type:   string(cpv1beta2.K0sControlPlaneScalingUpCondition),
			Status: metav1.ConditionFalse,
			Reason: cpv1beta2.K0sControlPlaneNotScalingUpReason,
		})
	}

	if upToDateReplicas > int(controlplane.kcp.Spec.Replicas) {
		conditions.Set(controlplane.kcp, metav1.Condition{
			Type:   string(cpv1beta2.K0sControlPlaneScalingDownCondition),
			Status: metav1.ConditionTrue,
			Reason: cpv1beta2.K0sControlPlaneScalingDownReason,
			Message: fmt.Sprintf("Control plane is scaling down: %d/%d",
				upToDateReplicas, controlplane.kcp.Spec.Replicas),
		})
	} else {
		conditions.Set(controlplane.kcp, metav1.Condition{
			Type:   string(cpv1beta2.K0sControlPlaneScalingDownCondition),
			Status: metav1.ConditionFalse,
			Reason: cpv1beta2.K0sControlPlaneNotScalingDownReason,
		})
	}
}

// versionMatches checks if the machine version matches the kcp version taking the possibly missing suffix into account
func versionMatches(machine *clusterv2.Machine, ver string) bool {

	if machine.Spec.Version == "" {
		return false
	}

	if machine.Spec.Version == ver {
		return true
	}

	machineVersion := machine.Spec.Version
	kcpVersion := ver

	// If either of the versions is missing the suffix, we need to add it
	// But take the suffix from kcp version if present
	kcpSuffix := getVersionSuffix(kcpVersion)
	if kcpSuffix == "" {
		kcpSuffix = "k0s.0"
		kcpVersion = kcpVersion + "+" + kcpSuffix
	}

	if machineSuffix := getVersionSuffix(machineVersion); machineSuffix == "" {
		machineVersion = machineVersion + "+" + kcpSuffix
	}

	// Compare the versions
	vMachine := version.MustParse(machineVersion)
	vKCP := version.MustParse(kcpVersion)

	return vKCP.Equal(vMachine)
}

func (c *K0sController) computeAvailability(ctx context.Context, controlplane *controlplane, logger logr.Logger) {
	logger.Info("Computed status", "status", controlplane.kcp.Status)
	// Check if the control plane is ready by connecting to the API server
	// and checking if the control plane is initialized
	logger.Info("Pinging the workload cluster API")
	// Get the CAPI cluster accessor
	client, err := kutil.GetControllerRuntimeClient(ctx, c.Client, c.ClusterCache, controlplane.kcp, client.ObjectKeyFromObject(controlplane.cluster))
	if err != nil {
		logger.Info("Failed to get cluster client", "error", err)
		return
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// If we can get 'kube-system' namespace, it's safe to say the API is up-and-running
	ns := &corev1.Namespace{}
	nsKey := types.NamespacedName{
		Namespace: "",
		Name:      "kube-system",
	}
	err = client.Get(pingCtx, nsKey, ns)
	if err != nil {
		logger.Info("Failed to ping the workload cluster API", "error", err)
		return
	}
	logger.Info("Successfully pinged the workload cluster API")
	// Set the conditions
	conditions.Set(controlplane.kcp, metav1.Condition{
		Type:   string(cpv1beta2.ControlPlaneAvailableCondition),
		Status: metav1.ConditionTrue,
		Reason: cpv1beta2.ControlPlaneAvailableReason,
	})
	controlplane.kcp.Status.Initialization.ControlPlaneInitialized = new(true)

	// Set the k0s cluster ID annotation
	annotations.AddAnnotations(controlplane.cluster, map[string]string{
		cpv1beta2.K0sClusterIDAnnotation: fmt.Sprintf("kube-system:%s", ns.GetUID()),
	})
}

// needsRequeue checks if the control plane needs to be requeued based on its status. It returns true if the control plane is not available
// or if the number of up-to-date replicas is not equal to the desired number of replicas.
func needsRequeue(kcp *cpv1beta2.K0sControlPlane) bool {
	if !conditions.IsTrue(kcp, string(cpv1beta2.ControlPlaneAvailableCondition)) {
		return true
	}

	if *kcp.Status.UpToDateReplicas != kcp.Spec.Replicas {
		return true
	}

	return false
}

func getVersionSuffix(version string) string {
	if strings.Contains(version, "+") {
		return strings.Split(version, "+")[1]
	}
	return ""
}
