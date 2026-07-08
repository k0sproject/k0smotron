/*
Copyright 2025.

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
	"fmt"
	"strings"
	"time"

	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	"github.com/k0sproject/k0smotron/v2/internal/controller/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/cluster-api/util/collections"
)

func createAutopilotPlan(ctx context.Context, clientset *kubernetes.Clientset, kcp *cpv1beta2.K0sControlPlane, machines collections.Machines) error {
	amd64DownloadURL := `https://get.k0sproject.io/` + kcp.Spec.Version + `/k0s-` + kcp.Spec.Version + `-amd64`
	arm64DownloadURL := `https://get.k0sproject.io/` + kcp.Spec.Version + `/k0s-` + kcp.Spec.Version + `-arm64`
	armDownloadURL := `https://get.k0sproject.io/` + kcp.Spec.Version + `/k0s-` + kcp.Spec.Version + `-arm`
	if kcp.Spec.K0sConfigSpec.DownloadURL == "" {
		// Use the default download URL if the user has not specified a custom one.
		kcp.Spec.K0sConfigSpec.DownloadURL = util.DefaultK0sDownloadURL
	}
	if kcp.Spec.K0sConfigSpec.DownloadURL != util.DefaultK0sDownloadURL {
		amd64DownloadURL = kcp.Spec.K0sConfigSpec.DownloadURL
		arm64DownloadURL = kcp.Spec.K0sConfigSpec.DownloadURL
		armDownloadURL = kcp.Spec.K0sConfigSpec.DownloadURL
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	plan := []byte(`
	{
		"apiVersion": "autopilot.k0sproject.io/v1beta2",
		"kind": "Plan",
		"metadata": {
		  "name": "autopilot"
		},
		"spec": {
			"id": "id-` + kcp.Name + `-` + timestamp + `",
			"timestamp": "` + timestamp + `",
			"commands": [{
				"k0supdate": {
					"version": "` + kcp.Spec.Version + `",
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
						"controllers": {
							"discovery": {
							    "static": {
									"nodes": ["` + strings.Join(machines.Names(), `","`) + `"]
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

func getAutopilotPlan(ctx context.Context, clientset *kubernetes.Clientset) (unstructured.Unstructured, error) {
	var existingPlan unstructured.Unstructured
	err := clientset.RESTClient().Get().AbsPath("/apis/autopilot.k0sproject.io/v1beta2/plans/autopilot").Do(ctx).Into(&existingPlan)
	if err != nil {
		return existingPlan, fmt.Errorf("error getting autopilot plan: %w", err)
	}
	return existingPlan, err
}

func deleteAutopilotPlan(ctx context.Context, clientset *kubernetes.Clientset) error {
	return clientset.RESTClient().Delete().AbsPath("/apis/autopilot.k0sproject.io/v1beta2/plans/autopilot").Do(ctx).Error()
}

func isAutopilotPlanCompleted(plan unstructured.Unstructured, desiredVersion string) (bool, error) {
	state, found, err := unstructured.NestedString(plan.Object, "status", "state")
	if err != nil {
		return false, fmt.Errorf("error getting autopilot plan's state: %w", err)
	}
	if found {
		commands, found, err := unstructured.NestedSlice(plan.Object, "spec", "commands")
		if err != nil || !found || len(commands) == 0 {
			return false, fmt.Errorf("error getting current autopilot plan's commands: %w", err)
		}

		version, found, err := unstructured.NestedString(commands[0].(map[string]any), "k0supdate", "version")
		if err != nil || !found {
			return false, fmt.Errorf("error getting current autopilot plan's version: %w", err)
		}
		if state == "Schedulable" || state == "SchedulableWait" {
			// it is necessary to check if the current autopilot process corresponds to a previous update by comparing the current
			// version of the resource with the desired one. If that is the case, the state is not yet ready to proceed with a new plan.
			if version != desiredVersion {
				return false, fmt.Errorf("previous autopilot is not finished: %w", util.ErrNotReady)
			}

			return false, nil
		}

		if state == "Completed" {
			// If the state is completed, it is necessary to check if the current version of the resource corresponds to the desired one.
			// If that is the case, it is not necessary to proceed with a new plan.
			if version == desiredVersion {
				return true, nil
			}
		}
	}

	return true, nil
}
