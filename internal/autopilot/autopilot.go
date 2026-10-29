/*
Copyright 2023.

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

package autopilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/k0sproject/k0smotron/v2/internal/controller/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
)

// Target represents the target of the autopilot plan, either control plane nodes or worker nodes.
type Target string

const (
	// ControllersTarget is the target for control plane nodes in the autopilot plan.
	ControllersTarget Target = "controllers"
	// WorkersTarget is the target for worker nodes in the autopilot plan.
	WorkersTarget Target = "workers"
)

var (
	// ErrUnexpectedPlanState is used to indicate that the autopilot plan is in an unexpected state.
	ErrUnexpectedPlanState = fmt.Errorf("unexpected autopilot plan state")
)

// GetPlan retrieves the existing autopilot plan from the cluster.
func GetPlan(ctx context.Context, clientset *kubernetes.Clientset) (*unstructured.Unstructured, error) {
	existingPlan := &unstructured.Unstructured{}
	err := clientset.RESTClient().Get().AbsPath("/apis/autopilot.k0sproject.io/v1beta2/plans/autopilot").Do(ctx).Into(existingPlan)
	if err != nil {
		return nil, fmt.Errorf("error getting autopilot plan: %w", err)
	}
	return existingPlan, nil
}

// DeletePlan deletes the existing autopilot plan from the cluster.
func DeletePlan(ctx context.Context, clientset *kubernetes.Clientset) error {
	return clientset.RESTClient().Delete().AbsPath("/apis/autopilot.k0sproject.io/v1beta2/plans/autopilot").Do(ctx).Error()
}

// PlanParameters holds the parameters required to create an autopilot plan.
type PlanParameters struct {
	// ID is the unique identifier for the autopilot plan.
	ID string
	// Timestamp is the timestamp when the plan is created.
	Timestamp string
	// Version is the desired version to which the nodes should be updated.
	Version string
	// DownloadURL is the URL from which the desired version can be downloaded.
	DownloadURL string
	// Target specifies whether the plan is for control plane nodes or worker nodes.
	Target Target
	// Nodes is the list of node names targeted by the autopilot plan through its static discovery.
	// It is mutually exclusive with NodeSelector.
	Nodes []string
	// NodeSelector is a label selector that targets nodes through autopilot's selector discovery.
	// It is mutually exclusive with Nodes.
	NodeSelector string
}

// CreatePlan creates a new autopilot plan in the cluster with the specified version and targets.
func CreatePlan(ctx context.Context, clientset *kubernetes.Clientset, params *PlanParameters) error {
	plan, err := buildPlan(params)
	if err != nil {
		return err
	}

	return clientset.RESTClient().Post().
		AbsPath("/apis/autopilot.k0sproject.io/v1beta2/plans").
		Body(plan).
		Do(ctx).
		Error()
}

// buildPlan renders the autopilot Plan manifest described by the given parameters.
func buildPlan(params *PlanParameters) ([]byte, error) {
	if params.Target == "" {
		return nil, errors.New("autopilot plan target is required")
	}

	discovery, err := targetDiscovery(params)
	if err != nil {
		return nil, err
	}

	amd64DownloadURL := `https://get.k0sproject.io/` + params.Version + `/k0s-` + params.Version + `-amd64`
	arm64DownloadURL := `https://get.k0sproject.io/` + params.Version + `/k0s-` + params.Version + `-arm64`
	armDownloadURL := `https://get.k0sproject.io/` + params.Version + `/k0s-` + params.Version + `-arm`
	// Use the default download URL if the user has not specified a custom one.
	if params.DownloadURL != "" && params.DownloadURL != util.DefaultK0sDownloadURL {
		amd64DownloadURL = params.DownloadURL
		arm64DownloadURL = params.DownloadURL
		armDownloadURL = params.DownloadURL
	}

	plan := map[string]any{
		"apiVersion": "autopilot.k0sproject.io/v1beta2",
		"kind":       "Plan",
		"metadata": map[string]any{
			"name": "autopilot",
		},
		"spec": map[string]any{
			"id":        params.ID,
			"timestamp": params.Timestamp,
			"commands": []any{
				map[string]any{
					"k0supdate": map[string]any{
						"version": params.Version,
						"platforms": map[string]any{
							"linux-amd64": map[string]any{"url": amd64DownloadURL},
							"linux-arm64": map[string]any{"url": arm64DownloadURL},
							"linux-arm":   map[string]any{"url": armDownloadURL},
						},
						"targets": map[string]any{
							string(params.Target): map[string]any{
								"discovery": discovery,
							},
						},
					},
				},
			},
		},
	}

	return json.Marshal(plan)
}

// targetDiscovery renders the discovery section of the plan target: static when node names are given,
// selector when a label selector is given.
func targetDiscovery(params *PlanParameters) (map[string]any, error) {
	switch {
	case len(params.Nodes) > 0 && params.NodeSelector != "":
		return nil, errors.New("autopilot plan target must use either static nodes or a node selector, not both")
	case params.NodeSelector != "":
		return map[string]any{
			"selector": map[string]any{
				"labels": params.NodeSelector,
			},
		}, nil
	case len(params.Nodes) > 0:
		return map[string]any{
			"static": map[string]any{
				"nodes": params.Nodes,
			},
		}, nil
	default:
		return nil, errors.New("autopilot plan target requires static nodes or a node selector")
	}
}

// IsPlanCompleted checks if the autopilot plan has completed successfully.
func IsPlanCompleted(plan *unstructured.Unstructured) (bool, error) {
	state, found, err := unstructured.NestedString(plan.Object, "status", "state")
	if err != nil {
		return false, fmt.Errorf("error getting autopilot plan's state: %w", err)
	}
	if !found {
		return false, fmt.Errorf("autopilot plan state not found")
	}

	switch state {
	case "Completed":
		return true, nil
	// Following processing states declared in:
	// https://docs.k0sproject.io/stable/autopilot-multicommand/?h=schedulablewait#processing-states
	case "NewPlan", "Schedulable", "SchedulableWait":
		return false, nil
	// Any other state is considered as not completed.
	default:
		return false, fmt.Errorf("%w: %s", ErrUnexpectedPlanState, state)
	}
}

// GetPlanTargetNodes retrieves the list of nodes targeted by the autopilot plan through static discovery.
// Targets that use selector discovery contribute no nodes; see GetPlanTargetSelector for those.
func GetPlanTargetNodes(plan *unstructured.Unstructured) ([]string, error) {
	targets, err := planTargets(plan)
	if err != nil {
		return nil, err
	}

	var allNodes []string

	for targetName, targetValue := range targets {
		targetMap, ok := targetValue.(map[string]any)
		if !ok {
			continue
		}

		nodes, found, err := unstructured.NestedStringSlice(targetMap, "discovery", "static", "nodes")
		if err != nil {
			return nil, fmt.Errorf("error reading nodes for target %s: %w", targetName, err)
		}
		if !found {
			continue
		}

		allNodes = append(allNodes, nodes...)
	}

	return allNodes, nil
}

// GetPlanTargetSelector retrieves the label selector used by the selector discovery of the given plan target.
// It returns an empty string when the plan has no such target or the target does not use selector discovery.
func GetPlanTargetSelector(plan *unstructured.Unstructured, target Target) (string, error) {
	targets, err := planTargets(plan)
	if err != nil {
		return "", err
	}

	selector, _, err := unstructured.NestedString(targets, string(target), "discovery", "selector", "labels")
	if err != nil {
		return "", fmt.Errorf("error reading selector for target %s: %w", target, err)
	}

	return selector, nil
}

// planTargets retrieves the k0supdate targets of the autopilot plan's first command.
func planTargets(plan *unstructured.Unstructured) (map[string]any, error) {
	commands, found, err := unstructured.NestedSlice(plan.Object, "spec", "commands")
	if err != nil {
		return nil, fmt.Errorf("error reading spec.commands: %w", err)
	}
	if !found || len(commands) == 0 {
		return nil, fmt.Errorf("no commands found in plan")
	}

	firstCommand, ok := commands[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected type for command")
	}

	targets, found, err := unstructured.NestedMap(firstCommand, "k0supdate", "targets")
	if err != nil {
		return nil, fmt.Errorf("error reading k0supdate.targets: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("no targets found in plan")
	}

	return targets, nil
}

// GetPlanTargetVersion retrieves the target version specified in the autopilot plan.
func GetPlanTargetVersion(plan *unstructured.Unstructured) (string, error) {
	commands, found, err := unstructured.NestedSlice(plan.Object, "spec", "commands")
	if err != nil || !found || len(commands) == 0 {
		return "", fmt.Errorf("error getting current autopilot plan's commands: %w", err)
	}

	version, found, err := unstructured.NestedString(commands[0].(map[string]any), "k0supdate", "version")
	if err != nil || !found {
		return "", fmt.Errorf("error getting current autopilot plan's version: %w", err)
	}

	return version, nil
}
