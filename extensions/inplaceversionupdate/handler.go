//go:build extension

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

package inplaceversionupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/k0sproject/k0smotron/v2/internal/autopilot"
	"github.com/k0sproject/k0smotron/v2/internal/controller/util"
	"gomodules.xyz/jsonpatch/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	bootstrapv1 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type InPlaceVersionUpdateHandler struct {
	client ctrlclient.Client
}

func NewInPlaceVersionUpdateHandler(client ctrlclient.Client) *InPlaceVersionUpdateHandler {
	return &InPlaceVersionUpdateHandler{
		client: client,
	}
}

func DoCanUpdateMachine(ctx context.Context, req *runtimehooksv1.CanUpdateMachineRequest, resp *runtimehooksv1.CanUpdateMachineResponse) {
	log := ctrl.LoggerFrom(ctx).WithValues("Machine", klog.KObj(&req.Desired.Machine))
	log.Info("CanUpdateMachine for version update handler is called")

	currentMachine := req.Current.Machine.DeepCopy()
	desiredMachine := req.Desired.Machine.DeepCopy()

	if currentMachine.Spec.Version != desiredMachine.Spec.Version {
		currentMachine.Spec.Version = desiredMachine.Spec.Version
	}

	if err := computeCanUpdateMachineResponse(req, resp, currentMachine); err != nil {
		log.Error(err, "Failed to compute CanUpdateMachineResponse")
		resp.Message = err.Error()
		resp.Status = runtimehooksv1.ResponseStatusFailure
		return
	}

	log.Info("CanUpdateMachine completed successfully", "response", resp)
	resp.Status = runtimehooksv1.ResponseStatusSuccess
}

func DoCanUpdateMachineSet(ctx context.Context, req *runtimehooksv1.CanUpdateMachineSetRequest, resp *runtimehooksv1.CanUpdateMachineSetResponse) {
	log := ctrl.LoggerFrom(ctx).WithValues("MachineSet", klog.KObj(&req.Desired.MachineSet))
	log.Info("CanUpdateMachineSet for version update handler is called")

	currentMachineSet := req.Current.MachineSet.DeepCopy()
	desiredMachineSet := req.Desired.MachineSet.DeepCopy()

	if currentMachineSet.Spec.Template.Spec.Version != desiredMachineSet.Spec.Template.Spec.Version {
		currentMachineSet.Spec.Template.Spec.Version = desiredMachineSet.Spec.Template.Spec.Version
	}

	if err := computeCanUpdateMachineSetResponse(req, resp, currentMachineSet); err != nil {
		log.Error(err, "Failed to compute CanUpdateMachineSetResponse")
		resp.Message = err.Error()
		resp.Status = runtimehooksv1.ResponseStatusFailure
		return
	}

	log.Info("CanUpdateMachineSet completed successfully", "response", resp)
	resp.Status = runtimehooksv1.ResponseStatusSuccess
}

func (ipuv *InPlaceVersionUpdateHandler) DoUpdateMachine(ctx context.Context, req *runtimehooksv1.UpdateMachineRequest, resp *runtimehooksv1.UpdateMachineResponse) {
	log := ctrl.LoggerFrom(ctx).WithValues("Machine", klog.KObj(&req.Desired.Machine))
	log.Info("UpdateMachine for version update handler is called")

	desiredMachine := req.Desired.Machine.DeepCopy()

	cluster := &clusterv1.Cluster{}
	if err := ipuv.client.Get(ctx, ctrlclient.ObjectKey{
		Name:      desiredMachine.Spec.ClusterName,
		Namespace: desiredMachine.Namespace,
	}, cluster); err != nil {
		log.Error(err, "Failed to get cluster")
		resp.Message = err.Error()
		resp.Status = runtimehooksv1.ResponseStatusFailure
		return
	}

	// Use clientset to interact with the workload cluster when creating the autopilot plan. This webhook server
	// doesn't run inside a controller-runtime Manager, so it has no clustercache.ClusterCache available; passing nil
	// makes the helper build the rest.Config directly from the workload cluster's kubeconfig secret instead.
	clientset, err := util.GetWorkloadClusterClientset(ctx, ipuv.client, nil, cluster)
	if err != nil {
		log.Error(err, "Failed to get workload cluster client")
		resp.Message = err.Error()
		resp.Status = runtimehooksv1.ResponseStatusFailure
		return
	}

	isControlPlaneMachine := false
	if _, ok := desiredMachine.Labels[clusterv1.MachineControlPlaneLabel]; ok {
		isControlPlaneMachine = true
	}

	plan, err := autopilot.GetPlan(ctx, clientset)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating autopilot plan")

			if err := createAutopilotPlanForMachine(ctx, ipuv.client, clientset, desiredMachine, isControlPlaneMachine); err != nil {
				log.Error(err, "Failed to create autopilot plan")
				resp.Message = err.Error()
				resp.Status = runtimehooksv1.ResponseStatusFailure
				return
			}
			log.Info("Autopilot plan created, waiting for completion")
			resp.Status = runtimehooksv1.ResponseStatusSuccess
			resp.Message = "Extension is updating Machine"
			resp.RetryAfterSeconds = 15
			return
		}

		log.Error(err, "Failed to get existing autopilot plan")
		resp.Message = err.Error()
		resp.Status = runtimehooksv1.ResponseStatusFailure
		return
	}

	isCompleted, err := autopilot.IsPlanCompleted(plan)
	if err != nil {
		if errors.Is(err, autopilot.ErrUnexpectedPlanState) {
			log.Error(err, "Autopilot plan is in an unexpected state. Check workload cluster logs for more details.")
			resp.Message = fmt.Sprintf("Autopilot plan is in an unexpected state. Check workload cluster logs for more details: %v", err)
			resp.Status = runtimehooksv1.ResponseStatusFailure
			return
		}

		log.Error(err, "Failed to check if autopilot plan is completed")
		resp.Message = err.Error()
		resp.Status = runtimehooksv1.ResponseStatusFailure
		return
	}
	if !isCompleted {
		log.Info("Autopilot plan not completed yet, retrying later")
		resp.Status = runtimehooksv1.ResponseStatusSuccess
		resp.Message = "Extension is updating Machine"
		resp.RetryAfterSeconds = 15
		return
	}

	currentPlanTargetNodes, err := autopilot.GetPlanTargetNodes(plan)
	if err != nil {
		log.Error(err, "Failed to get autopilot plan target")
		resp.Message = err.Error()
		resp.Status = runtimehooksv1.ResponseStatusFailure
		return
	}

	targetVersion, err := autopilot.GetPlanTargetVersion(plan)
	if err != nil {
		log.Error(err, "Failed to get autopilot plan target version")
		resp.Message = err.Error()
		resp.Status = runtimehooksv1.ResponseStatusFailure
		return
	}

	// Proceed with the removal of the plan and creation of a new one if any of the following conditions is met:
	// - The current machine is not in the list of target nodes of the completed plan
	// - The target version of the completed plan does not match the desired version of the current machine
	if !slices.Contains(currentPlanTargetNodes, desiredMachine.Name) || targetVersion != desiredMachine.Spec.Version {
		err := autopilot.DeletePlan(ctx, clientset)
		if err != nil {
			log.Error(err, "Failed to delete old autopilot plan")
			resp.Message = err.Error()
			resp.Status = runtimehooksv1.ResponseStatusFailure
			return
		}

		log.Info("Creating autopilot plan")

		if err := createAutopilotPlanForMachine(ctx, ipuv.client, clientset, desiredMachine, isControlPlaneMachine); err != nil {
			log.Error(err, "Failed to create autopilot plan")
			resp.Message = err.Error()
			resp.Status = runtimehooksv1.ResponseStatusFailure
			return
		}
		log.Info("Autopilot plan created, waiting for completion")
		resp.Status = runtimehooksv1.ResponseStatusSuccess
		resp.Message = "Extension is updating Machine"
		resp.RetryAfterSeconds = 15
		return

	}

	log.Info("Autopilot plan completed, Machine updated successfully")
	resp.Status = runtimehooksv1.ResponseStatusSuccess
	resp.Message = "Extension completed updating Machine"
	resp.RetryAfterSeconds = 0
}

func computeCanUpdateMachineResponse(req *runtimehooksv1.CanUpdateMachineRequest, resp *runtimehooksv1.CanUpdateMachineResponse, currentMachine *clusterv1.Machine) error {
	marshalledCurrentMachine, err := json.Marshal(req.Current.Machine)
	if err != nil {
		return err
	}

	var machinePatch []byte
	if isMachineBootstrappedWithK0s(currentMachine) {
		// This extension can only update machines bootstrapped with k0s. Otherwise, we
		// emit an empty patch, indicating that this webhook cannot handle the update.
		machinePatch, err = createJSONPatch(marshalledCurrentMachine, currentMachine)
		if err != nil {
			return err
		}
	}
	resp.MachinePatch = runtimehooksv1.Patch{
		PatchType: runtimehooksv1.JSONPatchType,
		Patch:     machinePatch,
	}
	resp.BootstrapConfigPatch = runtimehooksv1.Patch{
		PatchType: runtimehooksv1.JSONPatchType,
		Patch:     nil,
	}
	resp.InfrastructureMachinePatch = runtimehooksv1.Patch{
		PatchType: runtimehooksv1.JSONPatchType,
		Patch:     nil,
	}
	return nil
}

func computeCanUpdateMachineSetResponse(req *runtimehooksv1.CanUpdateMachineSetRequest, resp *runtimehooksv1.CanUpdateMachineSetResponse, currentMachineSet *clusterv1.MachineSet) error {
	marshalledCurrentMachineSet, err := json.Marshal(req.Current.MachineSet)
	if err != nil {
		return err
	}
	machineSetPatch, err := createJSONPatch(marshalledCurrentMachineSet, currentMachineSet)
	if err != nil {
		return err
	}

	resp.MachineSetPatch = runtimehooksv1.Patch{
		PatchType: runtimehooksv1.JSONPatchType,
		Patch:     machineSetPatch,
	}

	return nil
}

// createJSONPatch creates a RFC 6902 JSON patch from the original and the modified object.
func createJSONPatch(marshalledOriginal []byte, modified runtime.Object) ([]byte, error) {
	// TODO: avoid producing patches for status (although they will be ignored by the KCP / MD controllers anyway)
	marshalledModified, err := json.Marshal(modified)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal modified object: %v", err)
	}

	patch, err := jsonpatch.CreatePatch(marshalledOriginal, marshalledModified)
	if err != nil {
		return nil, fmt.Errorf("failed to create patch: %v", err)
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal patch: %v", err)
	}

	return patchBytes, nil
}

func isMachineBootstrappedWithK0s(machine *clusterv1.Machine) bool {
	if machine.Spec.Bootstrap.ConfigRef.GroupKind().Group == bootstrapv1.GroupVersion.Group {
		if machine.Spec.Bootstrap.ConfigRef.Kind == "K0sControllerConfig" ||
			machine.Spec.Bootstrap.ConfigRef.Kind == "K0sWorkerConfig" {
			return true
		}
	}

	return false
}
