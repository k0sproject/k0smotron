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
	"fmt"
	"strings"

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
	// Nodes is the list of node names that are targeted by the autopilot plan.
	Nodes []string
}

// CreatePlan creates a new autopilot plan in the cluster with the specified version and machines.
func CreatePlan(ctx context.Context, clientset *kubernetes.Clientset, params *PlanParameters) error {
	amd64DownloadURL := `https://get.k0sproject.io/` + params.Version + `/k0s-` + params.Version + `-amd64`
	arm64DownloadURL := `https://get.k0sproject.io/` + params.Version + `/k0s-` + params.Version + `-arm64`
	armDownloadURL := `https://get.k0sproject.io/` + params.Version + `/k0s-` + params.Version + `-arm`
	if params.DownloadURL == "" {
		// Use the default download URL if the user has not specified a custom one.
		params.DownloadURL = util.DefaultK0sDownloadURL
	}
	if params.DownloadURL != util.DefaultK0sDownloadURL {
		amd64DownloadURL = params.DownloadURL
		arm64DownloadURL = params.DownloadURL
		armDownloadURL = params.DownloadURL
	}

	plan := []byte(`
	{
		"apiVersion": "autopilot.k0sproject.io/v1beta2",
		"kind": "Plan",
		"metadata": {
		  "name": "autopilot"
		},
		"spec": {
			"id": "` + params.ID + `",
			"timestamp": "` + params.Timestamp + `",
			"commands": [{
				"k0supdate": {
					"version": "` + params.Version + `",
					"platforms": {
						"linux-amd64": {
							"url": "` + amd64DownloadURL + `"
						},
						"linux-arm64": {
							"url": "` + arm64DownloadURL + `"
						},
						"linux-arm": {
							"url": "` + armDownloadURL + `"
						}
					},
					"targets": {
						"` + string(params.Target) + `": {
							"discovery": {
							    "static": {
									"nodes": ["` + strings.Join(params.Nodes, `","`) + `"]
								}
							}
						}
					}
				}
			}]
		}
	}`)

	return clientset.RESTClient().Post().
		AbsPath("/apis/autopilot.k0sproject.io/v1beta2/plans").
		Body(plan).
		Do(ctx).
		Error()
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

// GetPlanTargetNodes retrieves the list of nodes targeted by the autopilot plan.
func GetPlanTargetNodes(plan *unstructured.Unstructured) ([]string, error) {
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
