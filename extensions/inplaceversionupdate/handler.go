//go:build extension

package inplaceversionupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	bootstrapv1beta1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	infrastructurev1beta1 "github.com/k0sproject/k0smotron/api/infrastructure/v1beta1"
	"gomodules.xyz/jsonpatch/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/klog/v2"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type InPlaceVersionUpdateHandler struct {
	decoder runtime.Decoder
	client  ctrlclient.Client
}

func NewInPlaceVersionUpdateHandler(client ctrlclient.Client) *InPlaceVersionUpdateHandler {
	scheme := runtime.NewScheme()
	_ = infrastructurev1beta1.AddToScheme(scheme)
	_ = bootstrapv1beta1.AddToScheme(scheme)
	_ = cpv1beta1.AddToScheme(scheme)
	return &InPlaceVersionUpdateHandler{
		client: client,
		decoder: serializer.NewCodecFactory(scheme).UniversalDecoder(
			infrastructurev1beta1.GroupVersion,
			bootstrapv1beta1.GroupVersion,
		),
	}
}

func DoCanUpdateMachine(ctx context.Context, req *runtimehooksv1.CanUpdateMachineRequest, resp *runtimehooksv1.CanUpdateMachineResponse) {
	log := ctrl.LoggerFrom(ctx).WithValues("Machine", klog.KObj(&req.Desired.Machine))
	log.Info("CanUpdateMachine for version update handler is called<<<")

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

	c, err := getWorkloadClusterClient(ctx, ipuv.client, ctrlclient.ObjectKey{
		Name:      desiredMachine.Spec.ClusterName,
		Namespace: desiredMachine.Namespace,
	})
	if err != nil {
		log.Error(err, "Failed to get workload cluster client")
		resp.Message = err.Error()
		resp.Status = runtimehooksv1.ResponseStatusFailure
		return
	}

	isControlPlane := false
	if _, ok := desiredMachine.Labels[clusterv1.MachineControlPlaneLabel]; ok {
		isControlPlane = true
	}

	autopilotPlan, err := getExistingAutopilotPlanFromMachine(ctx, c, desiredMachine, isControlPlane)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Creating autopilot plan for machine")

			isControlPlane := false
			if _, ok := desiredMachine.Labels[clusterv1.MachineControlPlaneLabel]; ok {
				isControlPlane = true
			}
			if err := createAutopilotPlanForMachine(ctx, c, desiredMachine, isControlPlane); err != nil {
				log.Error(err, "Failed to create autopilot plan for machine")
				resp.Message = err.Error()
				resp.Status = runtimehooksv1.ResponseStatusFailure
				return
			}
			log.Info("Autopilot plan created for machine, waiting for completion")
			resp.Status = runtimehooksv1.ResponseStatusSuccess
			resp.Message = "Extension is updating Machine"
			resp.RetryAfterSeconds = 15
			return
		}

		if errors.Is(err, errAutopilotPlanInProgress) {
			log.Info("Previous autopilot plan is still in progress, retrying later")
			resp.Status = runtimehooksv1.ResponseStatusSuccess
			resp.Message = "Extension is updating Machine"
			resp.RetryAfterSeconds = 15
			return
		}

		log.Error(err, "Failed to get existing autopilot plan from machine")
		resp.Message = err.Error()
		resp.Status = runtimehooksv1.ResponseStatusFailure
		return
	}

	isCompleted, err := isAutopilotPlanCompleted(autopilotPlan)
	if err != nil {
		log.Error(err, "Failed to check if autopilot plan is completed")
		resp.Message = err.Error()
		resp.Status = runtimehooksv1.ResponseStatusFailure
		return
	}
	if isCompleted {
		log.Info("Autopilot plan completed, proceeding with Machine update")
		resp.Status = runtimehooksv1.ResponseStatusSuccess
		resp.Message = "Extension completed updating Machine"
		resp.RetryAfterSeconds = 0
		return
	}

	log.Info("Autopilot plan not completed yet, retrying later")
	resp.Status = runtimehooksv1.ResponseStatusSuccess
	resp.Message = "Extension is updating Machine"
	resp.RetryAfterSeconds = 15
}

func computeCanUpdateMachineResponse(req *runtimehooksv1.CanUpdateMachineRequest, resp *runtimehooksv1.CanUpdateMachineResponse, currentMachine *clusterv1.Machine) error {
	marshalledCurrentMachine, err := json.Marshal(req.Current.Machine)
	if err != nil {
		return err
	}
	machinePatch, err := createJSONPatch(marshalledCurrentMachine, currentMachine)
	if err != nil {
		return err
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
