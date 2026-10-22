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
	"fmt"
	"slices"
	"strings"
	"time"

	bootstrapv1 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	autopilot "github.com/k0sproject/k0smotron/v2/internal/autopilot"
	"github.com/k0sproject/k0smotron/v2/internal/controller/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/controllers/external"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// createAutopilotPlanForMachine creates an autopilot plan for the given desired machine and applies it to the cluster. Although the autopilot plan can be applied to multiple nodes,
// this function only targets one machine at a time. This is in line with the design of the in-place update CAPI extension, which focuses on updating individual machines rather
// than multiple machines simultaneously.
func createAutopilotPlanForMachine(ctx context.Context, c client.Client, clientset *kubernetes.Clientset, desiredMachine *clusterv1.Machine, isControlPlane bool) error {
	log := ctrl.LoggerFrom(ctx)

	downloadURL, err := getDownloadURLFromBootstrapConfig(ctx, c, desiredMachine.Spec.Bootstrap.ConfigRef, isControlPlane, desiredMachine.Namespace)
	if err != nil {
		return fmt.Errorf("error getting download URL from bootstrap config: %w", err)
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	// machine version may lack the k0s suffix, the download URL and autopilot require it
	version := util.NormalizeK0sVersion(desiredMachine.Spec.Version)

	planParams := &autopilot.PlanParameters{
		// compose autopilot plan id by using machine name, timestamp and desired version to ensure uniqueness
		// in case of multiple updates on the same machine
		ID:          fmt.Sprintf("id-%s-%s-%s", desiredMachine.Name, version, timestamp),
		Timestamp:   timestamp,
		Version:     version,
		DownloadURL: downloadURL,
	}

	if isControlPlane {
		// Control plane nodes are signaled through ControlNode objects, which are named after the host.
		planParams.Target = autopilot.ControllersTarget
		planParams.Nodes = []string{desiredMachine.Name}
	} else {
		// A worker node is not necessarily named after its Machine (see K0sWorkerConfig.spec.useSystemHostname),
		// so the plan selects the node through the machine-name label k0smotron sets on every worker node.
		nodeName, err := resolveWorkerNode(ctx, clientset, desiredMachine.Name)
		if err != nil {
			return fmt.Errorf("error resolving worker node for machine %s: %w", desiredMachine.Name, err)
		}
		log.Info("Targeting worker node of Machine in autopilot plan", "Node", nodeName)

		planParams.Target = autopilot.WorkersTarget
		planParams.NodeSelector = workerNodeSelector(desiredMachine.Name)
	}

	err = autopilot.CreatePlan(ctx, clientset, planParams)
	if err != nil {
		return fmt.Errorf("error creating autopilot plan: %w", err)
	}

	return nil
}

// workerNodeSelector returns the label selector matching the worker node bootstrapped for the given Machine.
func workerNodeSelector(machineName string) string {
	return fmt.Sprintf("%s=%s", bootstrapv1.MachineNameNodeLabel, machineName)
}

// resolveWorkerNode returns the name of the worker node bootstrapped for the given Machine. Autopilot
// completes a plan whose selector matches no node without updating anything, so the node is resolved up front
// to fail loudly instead.
func resolveWorkerNode(ctx context.Context, clientset kubernetes.Interface, machineName string) (string, error) {
	selector := workerNodeSelector(machineName)

	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return "", fmt.Errorf("error listing nodes with selector %q: %w", selector, err)
	}

	switch len(nodes.Items) {
	case 0:
		return "", fmt.Errorf("no node with label selector %q found in the workload cluster", selector)
	case 1:
		return nodes.Items[0].Name, nil
	default:
		names := make([]string, 0, len(nodes.Items))
		for _, node := range nodes.Items {
			names = append(names, node.Name)
		}
		return "", fmt.Errorf("expected a single node with label selector %q in the workload cluster, found %d: %s", selector, len(names), strings.Join(names, ", "))
	}
}

// planTargetsMachine reports whether the autopilot plan targets the given Machine. Control plane machines are
// targeted statically by name. Worker machines are targeted through the machine-name node label, but plans
// created by previous versions of this extension targeted them statically by Machine name, which still counts.
func planTargetsMachine(plan *unstructured.Unstructured, machine *clusterv1.Machine, isControlPlane bool) (bool, error) {
	targetNodes, err := autopilot.GetPlanTargetNodes(plan)
	if err != nil {
		return false, err
	}
	if slices.Contains(targetNodes, machine.Name) {
		return true, nil
	}
	if isControlPlane {
		return false, nil
	}

	selector, err := autopilot.GetPlanTargetSelector(plan)
	if err != nil {
		return false, err
	}

	return selector == workerNodeSelector(machine.Name), nil
}

func getDownloadURLFromBootstrapConfig(ctx context.Context, client client.Client, configRef clusterv1.ContractVersionedObjectReference, isControlPlane bool, namespace string) (string, error) {
	downloadURL := ""

	uBootstrapConfig, err := external.GetObjectFromContractVersionedRef(ctx, client, configRef, namespace)
	if err != nil {
		return "", fmt.Errorf("error fetching bootstrap config: %w", err)
	}

	if isControlPlane {
		k0sControllerConfig := &bootstrapv1.K0sControllerConfig{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(uBootstrapConfig.Object, k0sControllerConfig)
		if err != nil {
			return "", fmt.Errorf("failed to convert controller bootstrap config for machine object: %w", err)
		}
		downloadURL = k0sControllerConfig.Spec.K0sConfigSpec.DownloadURL
	} else {
		k0sWorkerConfig := &bootstrapv1.K0sWorkerConfig{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(uBootstrapConfig.Object, k0sWorkerConfig)
		if err != nil {
			return "", fmt.Errorf("failed to convert worker bootstrap config for machine object: %w", err)
		}
		downloadURL = k0sWorkerConfig.Spec.DownloadURL
	}

	return downloadURL, nil
}
