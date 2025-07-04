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

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	autopilot "github.com/k0sproject/k0s/pkg/apis/autopilot/v1beta2"
	"github.com/k0sproject/k0s/pkg/autopilot/controller/plans/core"
	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	"github.com/k0sproject/version"
)

var (
	// errUpgradeNotCompleted is returned when the upgrade is not completed yet so it is needed to retry the status computation later.
	errUpgradeNotCompleted  = errors.New("waiting for plan to complete")
	errUnsupportedPlanState = errors.New("unsupported plan state")
)

// replicaStatusComputer defines an interface for computing the status of a control plane.
// Implementations of this interface will provide logic to compute the control plane
// status based on the upgrade strategy for the controlplane.
type replicaStatusComputer interface {
	compute(*cpv1beta1.K0sControlPlane) error
}

func (c *K0sController) updateStatus(ctx context.Context, kcp *cpv1beta1.K0sControlPlane, cluster *clusterv1.Cluster) error {
	logger := log.FromContext(ctx)

	defer func() {
		c.computeAvailability(ctx, cluster, kcp, logger)
	}()

	kcp.Status.Selector = collections.ControlPlaneSelectorForCluster(cluster.Name).String()

	sc, err := c.newReplicasStatusComputer(ctx, cluster, kcp)
	if err != nil {
		return err
	}
	if sc == nil {
		return nil
	}

	return sc.compute(kcp)
}

func (c *K0sController) newReplicasStatusComputer(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) (replicaStatusComputer, error) {
	logger := log.FromContext(ctx)

	switch kcp.Spec.UpdateStrategy {
	case cpv1beta1.UpdateInPlace:
		kc, err := c.getKubeClient(ctx, cluster)
		if err != nil {
			return nil, err
		}

		result, err := kc.RESTClient().Get().AbsPath("/apis/autopilot.k0sproject.io/v1beta2/plans/autopilot").DoRaw(ctx)
		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("Plan not found, falling back to machine status computer")
				// If the controlplane has not been updated, the calculation of the replica status must be based
				// on the status of the Machines associated to the controlplane instead of the Plan status since
				// it does not exist. At this point it is safe to calculate the state via the Machines because the
				// initial state of the Machine describes the initial state of the controlplane.
				return newMachineStatusComputer(ctx, c.Client, cluster)
			}

			return nil, err
		}

		var plan autopilot.Plan
		if err := yaml.Unmarshal(result, &plan); err != nil {
			return nil, err
		}

		return &planStatus{plan}, nil
	case cpv1beta1.UpdateRecreate:
		return newMachineStatusComputer(ctx, c.Client, cluster)
	default:
		return nil, errors.New("upgrade strategy not found")
	}
}

type planStatus struct {
	plan autopilot.Plan
}

func (ic *planStatus) compute(kcp *cpv1beta1.K0sControlPlane) error {
	logger := log.FromContext(context.Background())

	if len(ic.plan.Spec.Commands) == 0 || len(ic.plan.Status.Commands) == 0 {
		return fmt.Errorf("no plan commands found")
	}

	if ic.plan.Spec.Commands[0].K0sUpdate == nil || ic.plan.Status.Commands[0].K0sUpdate == nil {
		return fmt.Errorf("no plan command for k0s update found")
	}

	// At this point, it is considered that the controlplane status has been computed before using the strategy
	// which takes into account Machines state so we can assume that the only field to compute based on the
	// Plan's state is the version and the updated replicas.
	updatedReplicas := 0
	readyReplicas := 0
	unavailableReplicas := 0
	switch ic.plan.Status.State {
	case core.PlanCompleted:
		// If the Plan is completed, the status of the control plane is updated with the version
		// of the Plan. Otherwise, the status of the control plane remains the same.
		kcp.Status.Version = ic.plan.Spec.Commands[0].K0sUpdate.Version
		// When the update is completed, it is safe to say that the number of updated replicas
		// and ready replicas is as desired.
		updatedReplicas = int(kcp.Spec.Replicas)
		readyReplicas = int(kcp.Spec.Replicas)
	case core.PlanSchedulableWait, core.PlanSchedulable:
		for _, c := range ic.plan.Status.Commands[0].K0sUpdate.Controllers {
			switch c.State {
			case core.SignalCompleted:
				updatedReplicas++
				readyReplicas++
			case core.SignalPending:
				// Controller is still available.
				readyReplicas++
			case core.SignalSent:
				// When the controller state is 'SignalSent', the controlplane is undergoing the
				// update so it cannot be considered as available.
				unavailableReplicas++
			default:
				logger.Info("Unsupported controller state", "state", c.State)
			}
		}
	default:
		// TODO: Surface this error reason as a status.condition for controlplane
		return errUnsupportedPlanState
	}
	kcp.Status.UpdatedReplicas = int32(updatedReplicas)
	kcp.Status.ReadyReplicas = int32(readyReplicas)
	kcp.Status.UnavailableReplicas = int32(unavailableReplicas)

	// If status.updatedReplicas is not equal to desired ones by the spec, the control plane upgrade is not ready
	// so we return an error to retry the status computation later.
	if kcp.Status.UpdatedReplicas != kcp.Spec.Replicas {
		return errUpgradeNotCompleted
	}

	return nil
}

type machineStatus struct {
	machines collections.Machines
}

func newMachineStatusComputer(ctx context.Context, c client.Client, cluster *clusterv1.Cluster) (replicaStatusComputer, error) {
	machines, err := collections.GetFilteredMachinesForCluster(ctx, c, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
	if err != nil {
		return nil, fmt.Errorf("failed to get machines: %w", err)
	}

	ms := &machineStatus{
		machines: machines,
	}

	return ms, nil
}

func (rc *machineStatus) compute(kcp *cpv1beta1.K0sControlPlane) error {
	kcp.Status.Replicas = int32(len(rc.machines))
	readyReplicas := 0
	updatedReplicas := 0
	unavailableReplicas := 0
	// Count the machines in different states
	for _, machine := range rc.machines {
		switch machine.Status.Phase {
		case string(clusterv1.MachinePhaseRunning):
			readyReplicas++
		case string(clusterv1.MachinePhaseProvisioned):
			// If we're running without --enable-worker, the machine will never transition
			// to running state, so we need to count it as ready when it's provisioned
			if !kcp.WorkerEnabled() {
				readyReplicas++
			} else {
				unavailableReplicas++
			}
		case string(clusterv1.MachinePhaseDeleting), string(clusterv1.MachinePhaseDeleted):
			// Do nothing
		default:
			unavailableReplicas++
		}

		if versionMatches(machine, kcp.Spec.Version) {
			updatedReplicas++
		}
	}

	// If some machines are missing, count them as unavailable
	if int(kcp.Spec.Replicas) > rc.machines.Len() {
		unavailableReplicas += int(kcp.Spec.Replicas) - rc.machines.Len()
	}

	kcp.Status.ReadyReplicas = int32(readyReplicas)
	kcp.Status.UpdatedReplicas = int32(updatedReplicas)
	kcp.Status.UnavailableReplicas = int32(unavailableReplicas)

	// Find the lowest version
	lowestMachineVersion, err := minVersion(rc.machines)
	if err != nil {
		log.Log.Error(err, "Failed to get the lowest version")
		return err
	}

	kcp.Status.Version = lowestMachineVersion

	// If kcp has suffix but machines don't, we need to add it to minVersion
	// Otherwise CAPI topology will not be able to match the versions and might try to recreate the machines
	// or restrict the upgrade path
	if strings.Contains(kcp.Spec.Version, "+") && !strings.Contains(lowestMachineVersion, "+") && lowestMachineVersion != "" {
		// Get the suffix from kcp version
		suffix := strings.Split(kcp.Spec.Version, "+")[1]
		kcp.Status.Version = kcp.Status.Version + "+" + suffix
	}

	// If the controlplane spec does NOT have workers enabled
	// we need to mark the controlplane as externally managed
	// Otherwise CAPI assumes it'll find node objects for the machines
	// TODO Check with upstream CAPI folks whether this is the correct approach in this case when
	// we still run the controlplane on Machines
	if !kcp.WorkerEnabled() {
		kcp.Status.ExternalManagedControlPlane = true
	}

	return nil
}

// versionMatches checks if the machine version matches the kcp version taking the possibly missing suffix into account
func versionMatches(machine *clusterv1.Machine, ver string) bool {

	if machine.Spec.Version == nil || *machine.Spec.Version == "" {
		return false
	}

	if *machine.Spec.Version == ver {
		return true
	}

	machineVersion := *machine.Spec.Version
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

func (c *K0sController) computeAvailability(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane, logger logr.Logger) {
	statusAdapter := &K0sControlPlaneStatusAdapter{kcp: kcp}

	computeAvailability(
		ctx,
		c.Client,
		cluster,
		kcp, // K0sControlPlane
		nil, // K0smotronControlPlane (not used)
		statusAdapter,
		"k0smotron",
		logger,
	)
}

func getVersionSuffix(version string) string {
	if strings.Contains(version, "+") {
		return strings.Split(version, "+")[1]
	}
	return ""
}

// Status defines common status operations for control planes
type Status interface {
	SetReady(bool)
	SetInitialized(bool)
	SetControlPlaneInitialized(bool)
	GetReady() bool
	GetInitialized() bool
	GetControlPlaneInitialized() bool
}

// K0sControlPlaneStatusAdapter adapts K0sControlPlane for common status operations
type K0sControlPlaneStatusAdapter struct {
	kcp *cpv1beta1.K0sControlPlane
}

func (a *K0sControlPlaneStatusAdapter) SetReady(ready bool) {
	a.kcp.Status.Ready = ready
}

func (a *K0sControlPlaneStatusAdapter) SetInitialized(initialized bool) {
	a.kcp.Status.Initialized = initialized
}

func (a *K0sControlPlaneStatusAdapter) SetControlPlaneInitialized(controlPlaneInitialized bool) {
	a.kcp.Status.Initialization.ControlPlaneInitialized = controlPlaneInitialized
}

func (a *K0sControlPlaneStatusAdapter) GetReady() bool {
	return a.kcp.Status.Ready
}

func (a *K0sControlPlaneStatusAdapter) GetInitialized() bool {
	return a.kcp.Status.Initialized
}

func (a *K0sControlPlaneStatusAdapter) GetControlPlaneInitialized() bool {
	return a.kcp.Status.Initialization.ControlPlaneInitialized
}

// K0smotronControlPlaneStatusAdapter adapts K0smotronControlPlane for common status operations
type K0smotronControlPlaneStatusAdapter struct {
	kcp *cpv1beta1.K0smotronControlPlane
}

func (a *K0smotronControlPlaneStatusAdapter) SetReady(ready bool) {
	a.kcp.Status.Ready = ready
}

func (a *K0smotronControlPlaneStatusAdapter) SetInitialized(initialized bool) {
	a.kcp.Status.Initialized = initialized
}

func (a *K0smotronControlPlaneStatusAdapter) SetControlPlaneInitialized(controlPlaneInitialized bool) {
	a.kcp.Status.Initialization.ControlPlaneInitialized = controlPlaneInitialized
}

func (a *K0smotronControlPlaneStatusAdapter) GetReady() bool {
	return a.kcp.Status.Ready
}

func (a *K0smotronControlPlaneStatusAdapter) GetInitialized() bool {
	return a.kcp.Status.Initialized
}

func (a *K0smotronControlPlaneStatusAdapter) GetControlPlaneInitialized() bool {
	return a.kcp.Status.Initialization.ControlPlaneInitialized
}

// checkAPIConnectivity performs the actual API connectivity check
func checkAPIConnectivity(ctx context.Context, c client.Client, cluster *clusterv1.Cluster, clientNamespace string) (bool, *corev1.Namespace, error) {
	// Get the CAPI cluster accessor
	remoteClient, err := remote.NewClusterClient(ctx, clientNamespace, c, util.ObjectKey(cluster))
	if err != nil {
		return false, nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// If we can get 'kube-system' namespace, it's safe to say the API is up-and-running
	ns := &corev1.Namespace{}
	nsKey := types.NamespacedName{
		Namespace: "",
		Name:      "kube-system",
	}

	err = remoteClient.Get(pingCtx, nsKey, ns)
	if err != nil {
		return false, nil, nil // API not available, but not an error
	}

	return true, ns, nil
}

// setConditionsOnControlPlane sets conditions on the appropriate control plane type
func setConditionsOnControlPlane(k0sCP *cpv1beta1.K0sControlPlane, k0smotronCP *cpv1beta1.K0smotronControlPlane, conditionType clusterv1.ConditionType, status bool, reason, message string) {
	if k0sCP != nil {
		if status {
			conditions.MarkTrue(k0sCP, conditionType)
		} else {
			conditions.MarkFalse(k0sCP, conditionType, reason, clusterv1.ConditionSeverityWarning, message)
		}
	} else if k0smotronCP != nil {
		if status {
			conditions.MarkTrue(k0smotronCP, conditionType)
		} else {
			conditions.MarkFalse(k0smotronCP, conditionType, reason, clusterv1.ConditionSeverityWarning, message)
		}
	}
}

// computeAvailability checks the API server availability and updates status accordingly
// This function is shared between K0sControlPlane and K0smotronControlPlane controllers
func computeAvailability(
	ctx context.Context,
	c client.Client,
	cluster *clusterv1.Cluster,
	k0sCP *cpv1beta1.K0sControlPlane,
	k0smotronCP *cpv1beta1.K0smotronControlPlane,
	statusAdapter Status,
	clientNamespace string,
	logger logr.Logger,
) {
	logger.Info("Checking workload cluster API availability")

	// 1. First, check API connectivity and update conditions (v1beta2 spec: dynamic update)
	apiAvailable, ns, err := checkAPIConnectivity(ctx, c, cluster, clientNamespace)

	// Conditions are always updated (v1beta2 spec)
	if err != nil {
		logger.Info("Failed to create cluster client", "error", err)
		setConditionsOnControlPlane(
			k0sCP, k0smotronCP,
			cpv1beta1.ControlPlaneReadyCondition,
			false,
			"ClusterClientCreationFailed",
			fmt.Sprintf("Failed to create cluster client: %v", err),
		)
	} else if !apiAvailable {
		logger.Info("Workload cluster API not accessible")
		setConditionsOnControlPlane(
			k0sCP, k0smotronCP,
			cpv1beta1.ControlPlaneReadyCondition,
			false,
			"KubeSystemNamespaceNotAccessible",
			"Failed to get kube-system namespace",
		)
	} else {
		logger.Info("Successfully verified workload cluster API availability")
		setConditionsOnControlPlane(
			k0sCP, k0smotronCP,
			cpv1beta1.ControlPlaneReadyCondition,
			true,
			"",
			"",
		)

		// Set the k0s cluster ID annotation
		annotations.AddAnnotations(cluster, map[string]string{
			cpv1beta1.K0sClusterIDAnnotation: fmt.Sprintf("kube-system:%s", ns.GetUID()),
		})
	}

	// 2. Finally, update Ready/Initialized fields (write-once semantics) Once true, never change
	if statusAdapter.GetReady() && statusAdapter.GetInitialized() && statusAdapter.GetControlPlaneInitialized() {
		logger.Info("Control plane already ready and initialized (v1beta1 fields are write-once)")
		return
	}

	// Only set v1beta1 fields to true if API is available
	if apiAvailable {
		statusAdapter.SetReady(true)
		statusAdapter.SetInitialized(true)
		// Set the new v1beta2 controlPlaneInitialized field to true
		statusAdapter.SetControlPlaneInitialized(true)
	}
}
