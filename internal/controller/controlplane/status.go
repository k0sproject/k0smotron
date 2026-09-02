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
	"sync"
	"time"

	kutil "github.com/k0sproject/k0smotron/v2/internal/controller/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	"github.com/k0sproject/version"
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

	var allReplicas []*clusterv1.Machine
	allReplicas = append(allReplicas, controlplane.activeMachines.UnsortedList()...)
	allReplicas = append(allReplicas, controlplane.deletedMachines.UnsortedList()...)

	var readyReplicas, availableReplicas, upToDateReplicas int32
	for _, machine := range allReplicas {
		if conditions.IsTrue(machine, clusterv1.MachineReadyCondition) {
			readyReplicas++
		}
		if conditions.IsTrue(machine, clusterv1.MachineAvailableCondition) {
			availableReplicas++
		}
		if conditions.IsTrue(machine, clusterv1.MachineUpToDateCondition) {
			upToDateReplicas++
		}
	}

	controlplane.kcp.Status.ReadyReplicas = new(int32(readyReplicas))
	controlplane.kcp.Status.UpToDateReplicas = new(int32(upToDateReplicas))
	controlplane.kcp.Status.AvailableReplicas = new(int32(availableReplicas))

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
func versionMatches(machine *clusterv1.Machine, ver string) bool {

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

// initialized reports whether the control plane has ever been reachable. Takes the
// latch itself, since the two flavours carry it on different status structs.
func initialized(latch *bool) bool {
	return ptr.Deref(latch, false)
}

// How much failure the cache has to have seen before its word is taken for it. The
// count means probes once connected and connect attempts before that.
const (
	clusterCacheFailureThreshold      = 5
	clusterCacheConnectionGracePeriod = 75 * time.Second
)

// clusterCacheConnectionDown reports whether the cache considers its own connection
// down rather than merely not established yet.
func (c *K0sController) clusterCacheConnectionDown(ctx context.Context, controlplane *controlplane) bool {
	if c.ClusterCache == nil {
		// No second opinion to be had, so the failure that just happened is all the
		// evidence there is.
		return true
	}

	state := c.ClusterCache.GetHealthCheckingState(ctx, client.ObjectKeyFromObject(controlplane.cluster))

	// The cache has given up on its own, which is as definitive as it gets. Waiting the
	// grace period out from a stale success only adds to a verdict already reached.
	if state.ConsecutiveFailures >= clusterCacheFailureThreshold {
		return true
	}

	// A cluster the cache never reached has no probe to go by, and the zero value that
	// comes back means unknown rather than healthy.
	if state.LastProbeSuccessTime.IsZero() {
		return false
	}

	return time.Since(state.LastProbeSuccessTime) > clusterCacheConnectionGracePeriod
}

// availabilityFailureThreshold is how many failed reads have to be seen by this process
// before an outage is reported, whatever the persisted anchor says about the time.
const availabilityFailureThreshold = 2

// availabilityGracePeriod is how long reads have to keep failing before an outage
// is reported. A duration, since this reconcile does not own how often it runs.
const availabilityGracePeriod = 2 * time.Minute

// availabilityCondition decides what the Available condition should say, given what it
// already says. failingSince is when reads started failing and cause names the failure.
func availabilityCondition(existing *metav1.Condition, now, failingSince time.Time, gracePeriod time.Duration, failures int, cause string) metav1.Condition {
	down := metav1.Condition{
		Type:   string(cpv1beta2.ControlPlaneAvailableCondition),
		Status: metav1.ConditionFalse,
		Reason: cpv1beta2.ControlPlaneNotAvailableReason,
		// Written once, on the way in. Refreshing it would rewrite status on every
		// retry whenever the error names a rotating endpoint.
		Message: fmt.Sprintf("the workload cluster API could not be read: %s", cause),
	}

	// Already reported, so a read that is still failing changes nothing. Going back to
	// Unknown would flap, and rewriting the message would churn.
	if existing != nil && existing.Status == metav1.ConditionFalse {
		down.Message = existing.Message

		return down
	}

	// The anchor says how long, and the count says that this process watched it
	// happen. Age alone cannot tell a continued outage from a look that was missed.
	if failures >= availabilityFailureThreshold && !now.Before(failingSince.Add(gracePeriod)) {
		return down
	}

	return metav1.Condition{
		Type:   string(cpv1beta2.ControlPlaneAvailableCondition),
		Status: metav1.ConditionUnknown,
		// One reason for both failure paths. A reason that alternates with the cache
		// connecting and disconnecting would patch status on every retry.
		Reason: cpv1beta2.ControlPlaneConnectionDownReason,
		Message: fmt.Sprintf("the workload cluster API has not been readable since %s",
			failingSince.UTC().Format(time.RFC3339)),
	}
}

// availabilityReporter is what both control plane flavours have in common, so the
// grace period is applied the same way to each.
type availabilityReporter interface {
	client.Object
	conditions.Getter
	conditions.Setter
}

// setUnavailableAfterGracePeriod records that the API could not be read. Without it
// a control plane that goes away keeps reporting the last good state.
func setUnavailableAfterGracePeriod(failures *sync.Map, cp availabilityReporter, cause string) {
	key := availabilityKey(cp)
	existing := conditions.Get(cp, string(cpv1beta2.ControlPlaneAvailableCondition))
	created := cp.GetCreationTimestamp().Time
	now := time.Now()

	// Held in memory rather than read back, so a cached read that has not caught up
	// with the last patch cannot move the anchor forward and restart the window.
	state := availabilityFailures{since: now, seen: 1}

	switch previous, ok := failures.Load(key); {
	case ok:
		state = availabilityFailures{since: previous.(availabilityFailures).since, seen: previous.(availabilityFailures).seen + 1}
	case existing != nil && existing.Status == metav1.ConditionUnknown &&
		existing.Reason == cpv1beta2.ControlPlaneConnectionDownReason &&
		existing.LastTransitionTime.Time.After(created):
		// First failure here, so carry on from the window that was persisted. Only an
		// outage of its own is one, and an anchor older than the object is not evidence.
		state.since = existing.LastTransitionTime.Time
	}

	failures.Store(key, state)

	conditions.Set(cp, availabilityCondition(existing, now, state.since, availabilityGracePeriod, state.seen, cause))
}

// availabilityFailures is what one control plane's failing reads look like so far.
type availabilityFailures struct {
	since time.Time
	seen  int
}

// availabilityKey identifies one control plane across its whole life. The UID keeps a
// recreated one from inheriting the count its dead predecessor left behind.
func availabilityKey(cp client.Object) types.UID {
	return cp.GetUID()
}

// clearAvailabilityFailures forgets the failures counted so far, so a control plane
// has to be watched failing again rather than inheriting an old outage.
func clearAvailabilityFailures(failures *sync.Map, cp client.Object) {
	failures.Delete(availabilityKey(cp))
}

func (c *K0sController) computeAvailability(ctx context.Context, controlplane *controlplane, logger logr.Logger) {
	logger.Info("Computed status", "status", controlplane.kcp.Status)

	// Assumed unavailable while nothing has reached this control plane yet, so the
	// condition says so rather than leaving CAPI to infer it from the contract field.
	everReached := initialized(controlplane.kcp.Status.Initialization.ControlPlaneInitialized)
	if !everReached {
		conditions.Set(controlplane.kcp, metav1.Condition{
			Type:   string(cpv1beta2.ControlPlaneAvailableCondition),
			Status: metav1.ConditionUnknown,
			Reason: cpv1beta2.ControlPlaneAvailableUnknownReason,
			// Written before the read below, which is the only thing that can say
			// otherwise and is also what latches initialization.
			Message: "the workload cluster API has not been reached yet",
		})
	}

	// Check if the control plane is ready by connecting to the API server
	// and checking if the control plane is initialized
	logger.Info("Pinging the workload cluster API")
	// Get the CAPI cluster accessor
	client, err := kutil.GetControllerRuntimeClient(ctx, c.Client, c.ClusterCache, controlplane.kcp, client.ObjectKeyFromObject(controlplane.cluster))
	if err != nil {
		logger.Info("Failed to get cluster client", "error", err)

		if !everReached {
			return
		}

		// A cache that is not connected yet is also normal for a while after this
		// controller starts, and it is the one failure the cache can speak to.
		if errors.Is(err, kutil.ErrClusterCacheNotConnected) && !c.clusterCacheConnectionDown(ctx, controlplane) {
			// The last known state is kept here, and a cluster that never connects
			// produces no cache event either, so ask for another look.
			controlplane.availabilityUndecided = true

			return
		}

		setUnavailableAfterGracePeriod(&c.availabilityFailures, controlplane.kcp, err.Error())

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

		if !everReached {
			return
		}

		// The cache probe only asks for the API root, which a server with
		// broken storage still answers, so this read decides on its own.
		setUnavailableAfterGracePeriod(&c.availabilityFailures, controlplane.kcp, err.Error())

		return
	}
	logger.Info("Successfully pinged the workload cluster API")

	clearAvailabilityFailures(&c.availabilityFailures, controlplane.kcp)

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

const (
	// availabilityRetryInterval is how soon an unsettled control plane is looked at
	// again.
	availabilityRetryInterval = 20 * time.Second
	// availabilityPollInterval is how often a healthy looking control plane is read
	// again, since the cluster cache watches an endpoint this may not read through.
	availabilityPollInterval = time.Minute
)

// requeueResult decides the result to hand back. An error goes back on its own,
// since controller-runtime ignores a result given alongside one and says so loudly.
func requeueResult(err error, res ctrl.Result, controlplane *controlplane) ctrl.Result {
	if err != nil {
		return ctrl.Result{}
	}

	// The scale and in place logic ask for their own shorter intervals, so nothing
	// below has to reason about replicas.
	if !res.IsZero() {
		return res
	}

	return ctrl.Result{RequeueAfter: availabilityRequeueAfter(controlplane)}
}

// availabilityRequeueAfter reports how long to wait before looking again, or zero to wait
// for an event. Availability is only observed here, so even a settled control plane polls.
func availabilityRequeueAfter(controlplane *controlplane) time.Duration {
	kcp := controlplane.kcp

	// A deleting control plane drives its own requeue, and its status fields were
	// deliberately left alone.
	if !kcp.DeletionTimestamp.IsZero() {
		return 0
	}

	if controlplane.availabilityUndecided {
		return availabilityRetryInterval
	}

	if !conditions.IsTrue(kcp, string(cpv1beta2.ControlPlaneAvailableCondition)) {
		return availabilityRetryInterval
	}

	return availabilityPollInterval
}

func getVersionSuffix(version string) string {
	if strings.Contains(version, "+") {
		return strings.Split(version, "+")[1]
	}
	return ""
}
