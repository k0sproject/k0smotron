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

package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"

	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/failuredomains"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (c *K0sController) reconcileMachines(ctx context.Context, scope *controlplane) (res ctrl.Result, err error) {
	logger := log.FromContext(ctx)

	if res, err := c.preflightChecks(ctx, scope); err != nil || !res.IsZero() {
		return res, err
	}

	// Reconcile k0s version with in-place update strategy, if required.
	if res, err := c.reconcileInplaceK0sVersionUpdate(ctx, scope); err != nil || !res.IsZero() {
		return res, err
	}

	logger.Info("Reconciling control plane machines",
		"active", scope.activeMachines.Len(),
		"upToDate", scope.upToDateMachines.Len(),
		"notUpToDate", scope.notUpToDateMachines.Len(),
		"deleted", scope.deletedMachines.Len(),
		"desired", int(scope.kcp.Spec.Replicas))

	switch {
	case isNeededScaleUp(scope):
		logger.Info("Scaling up control plane")
		if err := c.scaleUp(ctx, scope); err != nil {
			return ctrl.Result{}, fmt.Errorf("error scaling up control plane: %w", err)
		}
	case isNeededScaleDown(scope):
		logger.Info("Scaling down control plane")
		if err := c.scaleDown(ctx, scope); err != nil {
			return ctrl.Result{}, fmt.Errorf("error scaling down control plane: %w", err)
		}
	}

	// Re-initialize the control plane scope to get the updated state after scaling operations or even after remediation or
	// external deletions, to ensure that we are working with the latest state of the cluster and requeue if the desired
	// state is not reached yet.
	updatedScope, err := c.retrieveControlPlaneState(ctx, scope.cluster, scope.kcp)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error re-initializing control plane scope: %w", err)
	}
	*scope = *updatedScope

	// Enqueue until the desired state is reached.
	if !isDesiredStateReached(scope) {
		logger.Info("Desired state not reached yet", "upToDate", scope.upToDateMachines.Len(), "notUpToDate", scope.notUpToDateMachines.Len(), "desired", int(scope.kcp.Spec.Replicas))
		res = ctrl.Result{RequeueAfter: 10 * time.Second, Requeue: true}
		return
	}

	logger.Info("Control plane machines are in the desired state")
	return ctrl.Result{}, nil
}

// isDesiredStateReached checks if the control plane has reached the desired state, which is when the number of up to date machines is
// equal to the desired replicas and there are no not up to date machines.
func isDesiredStateReached(scope *controlplane) bool {
	if scope.upToDateMachines.Len() != int(scope.kcp.Spec.Replicas) {
		return false
	}

	if scope.notUpToDateMachines.Len() > 0 {
		return false
	}

	return true
}

// isNeededScaleDown checks if it's needed to scale down the control plane based on the current state of the cluster and the desired number of replicas.
func isNeededScaleDown(scope *controlplane) bool {
	potentialMachinesCount := scope.activeMachines.Len() - 1
	minimumAllowedMachines := 1
	// Never scale down if that would cause to have zero machines, to avoid having a non-functional control plane.
	if potentialMachinesCount < minimumAllowedMachines {
		return false
	}

	// Reduce the number of control plane machines if there are more machines than the desired replicas,
	// regardless of they are up to date or not, to ensure that we don't end up with more machines than
	// desired during the scaling process.
	if scope.activeMachines.Len() > int(scope.kcp.Spec.Replicas) {
		return true
	}

	// Always scale down machines that are not up to date, to ensure that we don't have machines with old versions running in the cluster.
	return scope.notUpToDateMachines.Len() > 0
}

// isNeededScaleUp checks if it's needed to scale up the control plane based on the current state of the cluster and the desired number of replicas.
func isNeededScaleUp(scope *controlplane) bool {
	if isNeededApplyDeleteFirstStrategy(scope) {
		return false
	}

	potentialMachinesCount := scope.activeMachines.Len() + 1
	maximumAllowedMachines := int(scope.kcp.Spec.Replicas) + 1
	// If we already have the maximum allowed machines, we cannot scale up anymore until some machines are deleted.
	if potentialMachinesCount > maximumAllowedMachines {
		return false
	}

	// Scale up control plane machines if there are less up to date machines than the desired replicas.
	return scope.upToDateMachines.Len() < int(scope.kcp.Spec.Replicas)
}

// isNeededApplyDeleteFirstStrategy checks if it's needed to delete a machine before scaling up the control plane, based on the UpdateStrategy and the current state of the cluster.
func isNeededApplyDeleteFirstStrategy(scope *controlplane) bool {
	// Only if the strategy is UpdateRecreateDeleteFirst.
	if scope.kcp.Spec.UpdateStrategy != cpv1beta2.UpdateRecreateDeleteFirst {
		return false
	}

	// Apply UpdateRecreateDeleteFirst strategy when we already have the maximum (desired) number of machines.
	if scope.activeMachines.Len() < int(scope.kcp.Spec.Replicas) {
		return false
	}

	if scope.notUpToDateMachines.Len() == 0 {
		return false
	}

	if scope.kcp.Spec.Replicas < 3 {
		return false
	}

	return true
}

func (c *K0sController) scaleUp(ctx context.Context, scope *controlplane) error {
	logger := log.FromContext(ctx)
	newMachineName := names.SimpleNameGenerator.GenerateName(fmt.Sprintf("%s-", scope.kcp.Name))

	infraMachine, err := c.createMachineFromTemplate(ctx, newMachineName, scope.cluster, scope.kcp)
	if err != nil {
		return fmt.Errorf("error creating machine from template: %w", err)
	}

	infraRef := clusterv1.ContractVersionedObjectReference{
		Kind:     infraMachine.GetKind(),
		Name:     infraMachine.GetName(),
		APIGroup: clusterv1.GroupVersionInfrastructure.Group,
	}

	selectedFailureDomain := failuredomains.PickFewest(ctx, filterControlPlaneFailureDomains(*scope.cluster), scope.activeMachines, scope.deletedMachines)

	logger.Info("Creating new control plane machine", "name", newMachineName, "failureDomain", selectedFailureDomain)

	machine, err := c.generateMachine(ctx, newMachineName, scope.cluster, scope.kcp, infraRef, selectedFailureDomain)
	if err != nil {
		return fmt.Errorf("error generating machine: %w", err)
	}

	machineK0sConfig, err := getMachineK0sConfig(machine)
	if err != nil {
		return fmt.Errorf("error getting machine k0s config: %w", err)
	}

	err = c.createBootstrapConfig(ctx, machine.Name, machineK0sConfig, scope.kcp, scope.cluster.Name)
	if err != nil {
		return fmt.Errorf("error creating bootstrap config: %w", err)
	}

	err = c.Client.Patch(ctx, machine, client.Apply, &client.PatchOptions{
		FieldManager: "k0smotron",
	})
	if err != nil {
		return fmt.Errorf("error patching machine: %w", err)
	}

	// Remove the annotation tracking that a remediation is in progress.
	// A remediation is completed when the replacement machine has been created above.
	delete(scope.kcp.Annotations, cpv1beta2.RemediationInProgressAnnotation)

	return nil
}

func (c *K0sController) scaleDown(ctx context.Context, scope *controlplane) error {
	logger := log.FromContext(ctx)
	machineToDelete := scope.notUpToDateMachines.Oldest()
	reason := "outdated"

	if machineToDelete == nil {
		// If we need to scale down but there are no machines elegible for deletion, it means that all the machines are up to date but we
		// still have more machines than desired. In this case, we can delete the oldest machine, even if it's up to date.
		machineToDelete = scope.upToDateMachines.Oldest()
		reason = "excess"
	}
	if machineToDelete == nil {
		return fmt.Errorf("no machine found to delete")
	}

	logger.Info("Deleting control plane machine", "machine", machineToDelete.Name, "reason", reason)

	return c.deleteMachine(ctx, machineToDelete.Name, scope.kcp)
}

// preflightChecks performs necessary checks before updating the control plane, ensuring that the cluster is in a healthy state and ready
// for scaling/updating operations. This includes verifying that there are no machines currently being deleted, and that the most recently created machine
// is available before proceeding with scaling.
func (c *K0sController) preflightChecks(ctx context.Context, scope *controlplane) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	err := c.reconcileUnhealthyMachines(ctx, scope)
	if err != nil {
		return ctrl.Result{}, err
	}
	// Before machines scaling, we need to make sure that all manual deletions are fully reconciled, to avoid having machines in a limbo state during
	// the scaling process, which could lead to unexpected behaviors and issues. This includes deleting the k0s node resources for the manually deleted
	// machines and also deleting the machines whose infrastructure has been deleted.
	err = c.reconcileManualDeletions(ctx, scope)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error reconciling manual deletions: %w", err)
	}

	// Wait for the latest created machine to be available before proceeding with scaling if the desired number of replicas is not met,
	// ensuring etcd stability.
	// A machine is considered available if the machines is ready for more than 'machine.spec.mindReadySeconds' seconds. This value
	// is set by the controller when creating the controlplane machine.
	if !c.isLatestMachineReady(ctx, scope) {
		if latest := scope.activeMachines.Newest(); latest != nil {
			logger.Info("Waiting for latest machine to be ready before scaling", "machine", latest.Name)
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second, Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (c *K0sController) isLatestMachineReady(ctx context.Context, scope *controlplane) bool {
	latestCreatedMachine := scope.activeMachines.Newest()
	if latestCreatedMachine == nil {
		return true
	}

	// Ideally, we should rely on the MachineAvailableCondition to check if the machine is ready, but this is not possible when:
	// - Machine does not act as a worker node. See https://github.com/kubernetes-sigs/cluster-api/issues/13692
	// - Controller config is not available to check if the machine is a worker or not.
	// Use a more conservative approach and check if the machine is ready based on ControlNode k0s resource.
	controllerConfig, found := scope.controllerConfigs[latestCreatedMachine.Name]
	if !found || controllerConfig == nil || !controllerConfig.WorkerEnabled() {
		return c.checkMachineIsReady(ctx, latestCreatedMachine.Name, scope.cluster) == nil
	}

	return conditions.IsTrue(latestCreatedMachine, clusterv1.MachineAvailableCondition)
}

func (c *K0sController) reconcileInplaceK0sVersionUpdate(ctx context.Context, scope *controlplane) (ctrl.Result, error) {
	if !conditions.IsTrue(scope.kcp, cpv1beta2.ControlPlaneAvailableCondition) {
		// If the control plane is not available, we cannot proceed with the in-place update, as access to the
		// workload cluster is required to manage the autopilot plan.
		return ctrl.Result{}, nil
	}

	controlplaneRequiresUpdate := scope.hasMachinesWithOnlyVersionOutdated && scope.kcp.Spec.UpdateStrategy == cpv1beta2.UpdateInPlace

	logger := log.FromContext(ctx).WithValues("version", scope.kcp.Spec.Version)

	kubeClient, err := c.getWorkloadClusterClientset(ctx, scope.cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting cluster client set for machine update: %w", err)
	}

	plan, err := getAutopilotPlan(ctx, kubeClient)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if controlplaneRequiresUpdate {
				err = createAutopilotPlan(ctx, kubeClient, scope.kcp, scope.activeMachines)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("error creating autopilot plan: %w", err)
				}
				logger.Info("Autopilot plan created for in-place update")

				c.startUpdateMachineVersions(ctx, kubeClient, scope)

				// Requeue until the autopilot plan is completed, to avoid scaling up or down the control plane
				// while the update is still in progress.
				return ctrl.Result{RequeueAfter: 10 * time.Second, Requeue: true}, nil
			}
			// Update is not required, so we can proceed with the scaling operations.
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("error getting autopilot plan: %w", err)
	}

	completed, err := isAutopilotPlanCompleted(plan, scope.kcp.Spec.Version)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error checking if autopilot plan is completed: %w", err)
	}

	if !completed {
		// Requeue until the autopilot plan is completed, to avoid scaling up or down the control plane
		// while the update is still in progress.
		logger.Info("Autopilot plan is still in progress, requeuing")
		return ctrl.Result{RequeueAfter: 10 * time.Second, Requeue: true}, nil
	}

	// The plan is completed, so the background updateMachineVersions loop, if still running, no
	// longer has anything to wait for. Stop it now instead of leaving it to notice the plan is gone
	// on its next poll, so it can't race with a new plan created for a subsequent version update.
	c.stopUpdateMachineVersions(scope.kcp)
	// Ensure that all machines have the desired version. Update version go routine may have been stopped
	// before all machines were updated, so we need to ensure that all machines are updated to the
	// desired version. At this point, the autopilot plan is completed, so we can safely update the
	// machines to the desired version.
	err = c.ensureMachineVersionsUpdated(ctx, scope)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error ensuring machine versions are updated: %w", err)
	}

	if controlplaneRequiresUpdate {
		// Only delete the last autopilot plan if the control plane requires a new update, preserving it
		// for historical purposes.
		err = deleteAutopilotPlan(ctx, kubeClient)
		if err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("error deleting autopilot plan: %w", err)
		}

		err = createAutopilotPlan(ctx, kubeClient, scope.kcp, scope.activeMachines)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error creating autopilot plan: %w", err)
		}
		logger.Info("Autopilot plan created for in-place update")

		c.startUpdateMachineVersions(ctx, kubeClient, scope)

		// Requeue until the autopilot plan is completed, to avoid scaling up or down the control plane
		// while the update is still in progress.
		return ctrl.Result{RequeueAfter: 10 * time.Second, Requeue: true}, nil
	}

	// Re-initialize the control plane scope to get the updated state after updating the machines. Machines
	// upToDateMachines and notUpToDateMachines control the scaling operations, so we need to ensure that
	// the state is updated after the update is completed.
	updatedScope, err := c.retrieveControlPlaneState(ctx, scope.cluster, scope.kcp)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error re-initializing control plane scope: %w", err)
	}
	*scope = *updatedScope

	logger.Info("Autopilot plan completed and deleted, in-place update finished")
	return ctrl.Result{}, nil
}

func (c *K0sController) reconcileManualDeletions(ctx context.Context, scope *controlplane) error {
	var errs []error

	// Delete k0s resources for manually deleted machines.
	if scope.deletedMachines.Len() > 0 {
		for _, m := range scope.deletedMachines.SortedByCreationTimestamp() {
			err := c.deleteK0sNodeResources(ctx, scope, m)
			if err != nil {
				errs = append(errs, fmt.Errorf("error deleting k0s node resources for machine %s: %w", m.Name, err))
			}
		}
	}

	// Delete k0s node resources and machine resource when infrastructure has been deleted.
	for _, m := range scope.activeMachines {
		if _, exists := scope.infraMachines[m.Name]; !exists {
			err := c.deleteK0sNodeResources(ctx, scope, m)
			if err != nil {
				errs = append(errs, fmt.Errorf("error deleting k0s node resources: %w", err))
			}

			err = c.deleteMachine(ctx, m.Name, scope.kcp)
			if err != nil {
				errs = append(errs, fmt.Errorf("error deleting machine: %w", err))
			}

			// At this point, nothing related to the machine should be left, so we remove the machine
			// from the scope to have a clear state.
			removeMachineFromScope(scope, m.Name)
		}
	}

	if len(errs) > 0 {
		return kerrors.NewAggregate(errs)
	}

	return nil
}

func removeMachineFromScope(scope *controlplane, machineName string) {
	delete(scope.activeMachines, machineName)
	delete(scope.upToDateMachines, machineName)
	delete(scope.notUpToDateMachines, machineName)
	delete(scope.infraMachines, machineName)
	delete(scope.controllerConfigs, machineName)
	delete(scope.deletedMachines, machineName)
}

func (c *K0sController) deleteK0sNodeResources(ctx context.Context, scope *controlplane, machine *clusterv1.Machine) error {
	logger := log.FromContext(ctx)

	if ptr.Deref(scope.kcp.Status.Initialization.ControlPlaneInitialized, false) {
		kubeClient, err := c.getWorkloadClusterClientset(ctx, scope.cluster)
		if err != nil {
			return fmt.Errorf("error getting cluster client set for deletion: %w", err)
		}

		waitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		err = wait.PollUntilContextCancel(waitCtx, 10*time.Second, true, func(fctx context.Context) (bool, error) {
			if err := c.markChildControlNodeToLeave(fctx, machine.Name, kubeClient); err != nil {
				return false, fmt.Errorf("error marking controlnode to leave: %w", err)
			}

			ok, err := c.checkMachineLeft(fctx, machine.Name, kubeClient)
			if err != nil {
				logger.Error(err, "Error checking machine left", "machine", machine.Name)
			}
			return ok, err
		})
		if err != nil {
			return fmt.Errorf("error checking machine left: %w", err)
		}

		err = c.deleteControlNode(ctx, machine.Name, kubeClient)
		if err != nil {
			return fmt.Errorf("error deleting controlnode: %w", err)
		}
	}

	if err := c.removePreTerminateHookAnnotationFromMachine(ctx, machine); err != nil {
		return fmt.Errorf("failed to remove pre-terminate hook from control plane Machine '%s': %w", machine.Name, err)
	}

	return nil
}

func (c *K0sController) removePreTerminateHookAnnotationFromMachine(ctx context.Context, machine *clusterv1.Machine) error {
	if _, exists := machine.Annotations[cpv1beta2.K0ControlPlanePreTerminateHookCleanupAnnotation]; !exists {
		// Nothing to do, the annotation is not set (anymore) on the Machine
		return nil
	}

	log := log.FromContext(ctx)
	log.Info("Removing pre-terminate hook from control plane Machine")

	machineOriginal := machine.DeepCopy()
	delete(machine.Annotations, cpv1beta2.K0ControlPlanePreTerminateHookCleanupAnnotation)
	if err := c.Client.Patch(ctx, machine, client.MergeFrom(machineOriginal)); err != nil {
		return fmt.Errorf("failed to remove pre-terminate hook from control plane Machine: %w", err)
	}

	return nil
}

func (c *K0sController) ensureMachineVersionsUpdated(ctx context.Context, scope *controlplane) error {
	for _, machine := range scope.activeMachines {
		if machine.Spec.Version == scope.kcp.Spec.Version {
			continue
		}

		err := c.updateMachineVersion(ctx, machine, scope.kcp.Spec.Version)
		if err != nil {
			return fmt.Errorf("error updating machine version for %s: %w", machine.Name, err)
		}
	}

	return nil
}

// cancelHandle wraps a context.CancelFunc so it can be stored in and compared by sync.Map, which
// requires comparable values: function values (context.CancelFunc) are not comparable, but
// pointers to this struct are.
type cancelHandle struct {
	cancel context.CancelFunc
}

// startUpdateMachineVersions launches updateMachineVersions in the background with a cancellable
// context, and tracks the cancel function so stopUpdateMachineVersions can stop it early.
func (c *K0sController) startUpdateMachineVersions(ctx context.Context, clientset *kubernetes.Clientset, scope *controlplane) {
	updateCtx, cancel := context.WithCancel(ctx)
	key := client.ObjectKeyFromObject(scope.kcp)
	handle := &cancelHandle{cancel: cancel}
	c.autopilotUpdateCancels.Store(key, handle)

	go func() {
		defer c.autopilotUpdateCancels.CompareAndDelete(key, handle)

		if err := c.updateMachineVersions(updateCtx, clientset, scope); err != nil && !errors.Is(err, context.Canceled) {
			log.FromContext(updateCtx).Error(err, "error updating machine versions for in-place update")
		}
	}()
}

// stopUpdateMachineVersions cancels the running updateMachineVersions goroutine for kcp, if any.
func (c *K0sController) stopUpdateMachineVersions(kcp *cpv1beta2.K0sControlPlane) {
	key := client.ObjectKeyFromObject(kcp)
	if v, ok := c.autopilotUpdateCancels.LoadAndDelete(key); ok {
		v.(*cancelHandle).cancel()
	}
}

func (c *K0sController) updateMachineVersions(ctx context.Context, clientset *kubernetes.Clientset, scope *controlplane) error {
	return wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		plan, err := getAutopilotPlan(ctx, clientset)
		if err != nil {
			return false, err
		}

		commands, found, err := unstructured.NestedSlice(plan.Object, "status", "commands")
		if err != nil {
			return false, fmt.Errorf("error reading status.commands: %w", err)
		}
		if !found || len(commands) == 0 {
			return false, nil
		}

		k0sUpdateCommand, ok := commands[0].(map[string]any)
		if !ok {
			return false, fmt.Errorf("unexpected type for command")
		}

		controllers, found, err := unstructured.NestedSlice(k0sUpdateCommand, "k0supdate", "controllers")
		if err != nil {
			return false, fmt.Errorf("error reading k0supdate.controllers: %w", err)
		}
		if !found {
			return false, nil
		}

		allCompleted := true
		for _, ctrl := range controllers {
			ctrlMap, ok := ctrl.(map[string]any)
			if !ok {
				continue
			}

			name, _, _ := unstructured.NestedString(ctrlMap, "name")
			state, _, _ := unstructured.NestedString(ctrlMap, "state")

			if state != "SignalCompleted" {
				allCompleted = false
				continue
			}

			machine, exists := scope.activeMachines[name]
			if !exists {
				continue
			}

			err := c.updateMachineVersion(ctx, machine, scope.kcp.Spec.Version)
			if err != nil {
				return false, fmt.Errorf("error updating machine version for %s: %w", machine.Name, err)
			}

		}

		return allCompleted, nil
	})
}

func (c *K0sController) updateMachineVersion(ctx context.Context, machine *clusterv1.Machine, version string) error {
	if machine.Spec.Version == version {
		return nil
	}

	patchHelper, err := patch.NewHelper(machine, c)
	if err != nil {
		return fmt.Errorf("error creating patch helper for machine %s: %w", machine.Name, err)
	}

	machine.Spec.Version = version

	if err := patchHelper.Patch(ctx, machine); err != nil {
		return fmt.Errorf("error patching machine %s: %w", machine.Name, err)
	}

	return nil
}
