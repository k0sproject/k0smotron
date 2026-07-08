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

	"github.com/go-logr/logr"
	bootstrapv2 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	"github.com/k0sproject/k0smotron/v2/internal/autopilot"
	"github.com/k0sproject/k0smotron/v2/internal/featuregate"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	runtimev1 "sigs.k8s.io/cluster-api/api/runtime/v1beta2"
	runtimecatalog "sigs.k8s.io/cluster-api/exp/runtime/catalog"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

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

	plan, err := autopilot.GetPlan(ctx, kubeClient)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if controlplaneRequiresUpdate {
				err := c.runInplaceUpdate(ctx, kubeClient, scope, logger)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("error running in-place update: %w", err)
				}

				// Requeue until the autopilot plan is completed, to avoid scaling up or down the control plane
				// while the update is still in progress.
				return ctrl.Result{RequeueAfter: 10 * time.Second, Requeue: true}, nil
			}
			// Update is not required, so we can proceed with the scaling operations.
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("error getting autopilot plan: %w", err)
	}

	completed, err := autopilot.IsPlanCompleted(plan)
	if err != nil {
		if errors.Is(err, autopilot.ErrUnexpectedPlanState) {
			return ctrl.Result{}, fmt.Errorf("autopilot plan is in an unexpected state: %w", err)
		}

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

	if controlplaneRequiresUpdate {
		// Only delete the last autopilot plan if the control plane requires a new update, preserving it
		// for historical purposes.
		err = autopilot.DeletePlan(ctx, kubeClient)
		if err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("error deleting autopilot plan: %w", err)
		}

		err = c.runInplaceUpdate(ctx, kubeClient, scope, logger)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error running in-place update: %w", err)
		}

		// Requeue until the autopilot plan is completed, to avoid scaling up or down the control plane
		// while the update is still in progress.
		return ctrl.Result{RequeueAfter: 10 * time.Second, Requeue: true}, nil
	}

	// Ensure that all machines have the desired version. Update version go routine may have been stopped
	// before all machines were updated, so we need to ensure that all machines are updated to the
	// desired version. At this point, the autopilot plan is completed, so we can safely update the
	// machines to the desired version.
	err = c.ensureMachineVersionsUpdated(ctx, scope)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error ensuring machine versions are updated: %w", err)
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

func (c *K0sController) runInplaceUpdate(ctx context.Context, workloadClientset *kubernetes.Clientset, scope *controlplane, logger logr.Logger) error {
	isK0smotronExtensionForInplaceUpdateDeployed, err := isK0smotronExtensionForInplaceUpdateDeployed(ctx, c.Client)
	if err != nil {
		logger.Info("error checking if k0smotron extension for in-place updates is deployed", "error", err)
	}

	if featuregate.IsEnabled(featuregate.InPlaceUpdates) && isK0smotronExtensionForInplaceUpdateDeployed {
		machineToUpdate, infraMachineToUpdate, bootstrapConfigToUpdate, err := retrieveOldestMachineOutOfDate(scope)
		if err != nil {
			return fmt.Errorf("error retrieving machine to update: %w", err)
		}

		logger.Info("Starting CAPI in-place update", "machine", machineToUpdate.Name)
		err = triggerCAPIInplaceVersionUpdate(ctx, c.Client, scope.kcp.Spec.Version, machineToUpdate, infraMachineToUpdate, bootstrapConfigToUpdate)
		if err != nil {
			return fmt.Errorf("error triggering in-place version update for machine %s: %w", machineToUpdate.Name, err)
		}

		return nil
	}

	logger.Info("Starting standalone in-place update")
	return c.runStandaloneAutopilotPlan(ctx, workloadClientset, scope)
}

func (c *K0sController) runStandaloneAutopilotPlan(ctx context.Context, clientset *kubernetes.Clientset, scope *controlplane) error {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	params := autopilot.PlanParameters{
		ID:          fmt.Sprintf("id-%s-%s", scope.kcp.Name, timestamp),
		Timestamp:   timestamp,
		Version:     scope.kcp.Spec.Version,
		DownloadURL: scope.kcp.Spec.K0sConfigSpec.DownloadURL,
		Target:      autopilot.ControllersTarget,
		Nodes:       scope.notUpToDateMachines.Names(),
	}

	err := autopilot.CreatePlan(ctx, clientset, &params)
	if err != nil {
		return fmt.Errorf("error creating autopilot plan: %w", err)
	}
	c.startUpdateMachineVersions(ctx, clientset, scope)

	return nil
}

func isK0smotronExtensionForInplaceUpdateDeployed(ctx context.Context, c client.Client) (bool, error) {
	var k0smotronInplaceVersionUpdateExtension runtimev1.ExtensionConfig
	err := c.Get(ctx, client.ObjectKey{Name: "inplace-version-update-extensionconfig"}, &k0smotronInplaceVersionUpdateExtension)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("error getting k0smotron inplace version update extension: %w", err)
	}

	return true, nil
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
		plan, err := autopilot.GetPlan(ctx, clientset)
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

func triggerCAPIInplaceVersionUpdate(ctx context.Context, c client.Client, desiredVersion string, desiredMachine *clusterv1.Machine, desiredInfraMachine *unstructured.Unstructured, desiredBootstrapConfig *bootstrapv2.K0sControllerConfig) error {
	if _, ok := desiredMachine.Annotations[clusterv1.UpdateInProgressAnnotation]; !ok {
		orig := desiredMachine.DeepCopy()
		desiredMachine.Spec.Version = desiredVersion
		if desiredMachine.Annotations == nil {
			desiredMachine.Annotations = map[string]string{}
		}
		desiredMachine.Annotations[clusterv1.UpdateInProgressAnnotation] = ""
		desiredMachine.Annotations[runtimev1.PendingHooksAnnotation] = runtimecatalog.HookName(runtimehooksv1.UpdateMachine)
		if err := c.Patch(ctx, desiredMachine, client.MergeFrom(orig)); err != nil {
			return fmt.Errorf("failed to trigger in-place update for Machine %s by setting the %s annotation: %w",
				klog.KObj(desiredMachine), clusterv1.UpdateInProgressAnnotation, err)
		}
	}

	if _, ok := desiredInfraMachine.GetAnnotations()[clusterv1.UpdateInProgressAnnotation]; !ok {
		origInfra := desiredInfraMachine.DeepCopy()
		infraMachineAnnotations := desiredInfraMachine.GetAnnotations()
		if infraMachineAnnotations == nil {
			infraMachineAnnotations = map[string]string{}
		}
		infraMachineAnnotations[clusterv1.UpdateInProgressAnnotation] = ""
		desiredInfraMachine.SetAnnotations(infraMachineAnnotations)
		if err := c.Patch(ctx, desiredInfraMachine, client.MergeFrom(origInfra)); err != nil {
			return fmt.Errorf("failed to trigger in-place update for InfrastructureMachine %s by setting the %s annotation: %w",
				klog.KObj(desiredInfraMachine), clusterv1.UpdateInProgressAnnotation, err)
		}
	}

	if _, ok := desiredBootstrapConfig.Annotations[clusterv1.UpdateInProgressAnnotation]; !ok {
		origBootstrap := desiredBootstrapConfig.DeepCopy()
		if desiredBootstrapConfig.Annotations == nil {
			desiredBootstrapConfig.Annotations = map[string]string{}
		}
		desiredBootstrapConfig.Annotations[clusterv1.UpdateInProgressAnnotation] = ""
		if err := c.Patch(ctx, desiredBootstrapConfig, client.MergeFrom(origBootstrap)); err != nil {
			return fmt.Errorf("failed to trigger in-place update for BootstrapConfig %s by setting the %s annotation: %w",
				klog.KObj(desiredBootstrapConfig), clusterv1.UpdateInProgressAnnotation, err)
		}
	}

	return nil
}
