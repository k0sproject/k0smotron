//go:build extension

package inplaceversionupdate

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/transport"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// errPreviousAutopilotPlanInProgress is returned when there is an existing autopilot plan
// from previous update that is still in progress.
var errAutopilotPlanInProgress = fmt.Errorf("autopilot plan from previous update is still in progress")

func getExistingAutopilotPlanFromMachine(ctx context.Context, clientset *kubernetes.Clientset, machine *clusterv1.Machine, isControlPlane bool) (*unstructured.Unstructured, error) {
	var existingPlan unstructured.Unstructured
	err := clientset.RESTClient().Get().AbsPath("/apis/autopilot.k0sproject.io/v1beta2/plans/autopilot").Do(ctx).Into(&existingPlan)
	if err != nil {
		return nil, err
	}

	commands, found, err := unstructured.NestedSlice(existingPlan.Object, "spec", "commands")
	if err != nil {
		return nil, fmt.Errorf("error getting autopilot plan commands: %w", err)
	}
	if !found || len(commands) == 0 {
		return nil, fmt.Errorf("autopilot plan commands not found")
	}

	cmd0, ok := commands[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected type for command[0]")
	}

	targets, found, err := unstructured.NestedMap(
		cmd0,
		"k0supdate", "targets", "controllers", "discovery", "static",
	)
	if err != nil {
		return nil, fmt.Errorf("error getting autopilot plan's targets: %w", err)
	}
	if !found {
		targets, found, err = unstructured.NestedMap(
			cmd0,
			"k0supdate", "targets", "workers", "discovery", "static",
		)
		if err != nil {
			return nil, fmt.Errorf("error getting autopilot plan's targets: %w", err)
		}
		if !found {
			return nil, fmt.Errorf("autopilot plan targets not found")
		}
	}

	nodes, ok := targets["nodes"].([]any)
	if !ok {
		return nil, fmt.Errorf("error parsing autopilot plan's target nodes")
	}

	if len(nodes) != 1 {
		return nil, fmt.Errorf("unexpected number of target nodes in autopilot plan: %d", len(nodes))
	}

	if nodes[0].(string) != machine.Name {
		// Autopilot plan is related to a previous update for another machine.
		isCompleted, err := isAutopilotPlanCompleted(&existingPlan)
		if err != nil {
			return nil, fmt.Errorf("error checking if autopilot plan is completed: %w", err)
		}
		// If the plan is already completed, we can delete it to allow new updates.
		// Otherwise, we cannot proceed with the update for this machine yet.
		if isCompleted {
			err := clientset.RESTClient().Delete().AbsPath("/apis/autopilot.k0sproject.io/v1beta2/plans/autopilot").Do(ctx).Error()
			if err != nil {
				if !apierrors.IsNotFound(err) {
					return nil, fmt.Errorf("error deleting completed autopilot plan targeting other machine: %w", err)
				}
			}
			return nil, apierrors.NewNotFound(schema.GroupResource{Group: "autopilot.k0sproject.io", Resource: "plans"}, "autopilot")
		} else {
			return nil, fmt.Errorf("autopilot plan for machine '%s' found: %w", machine.Name, errAutopilotPlanInProgress)
		}
	}

	return &existingPlan, nil
}

func isAutopilotPlanCompleted(autopilotPlan *unstructured.Unstructured) (bool, error) {
	state, found, err := unstructured.NestedString(autopilotPlan.Object, "status", "state")
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
		return false, nil
	}
}

func createAutopilotPlanForMachine(ctx context.Context, clientset *kubernetes.Clientset, desiredMachine *clusterv1.Machine, isControlPlane bool) error {
	// TODO: use DownloadURL from K0sControllerConfig/K0sWorkerConfig when available
	amd64DownloadURL := `https://get.k0sproject.io/` + desiredMachine.Spec.Version + `/k0s-` + desiredMachine.Spec.Version + `-amd64`
	arm64DownloadURL := `https://get.k0sproject.io/` + desiredMachine.Spec.Version + `/k0s-` + desiredMachine.Spec.Version + `-arm64`
	armDownloadURL := `https://get.k0sproject.io/` + desiredMachine.Spec.Version + `/k0s-` + desiredMachine.Spec.Version + `-arm`

	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	// compose autopilot plan id by using machine name, timestamp and desired version to ensure uniqueness
	// in case of multiple updates on the same machine
	planID := fmt.Sprintf("id-%s-%s-%s", desiredMachine.Name, desiredMachine.Spec.Version, timestamp)

	target := "controllers"
	if !isControlPlane {
		target = "workers"
	}

	ap := []byte(`
	{
		"apiVersion": "autopilot.k0sproject.io/v1beta2",
		"kind": "Plan",
		"metadata": {
		  "name": "autopilot"
		},
		"spec": {
			"id": "` + planID + `",
			"timestamp": "` + timestamp + `",
			"commands": [{
				"k0supdate": {
					"version": "` + desiredMachine.Spec.Version + `",
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
						"` + target + `": {
							"discovery": {
							    "static": {
									"nodes": ["` + desiredMachine.Name + `"]
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
		Body(ap).
		Do(ctx).
		Error()
}

func getWorkloadClusterClient(ctx context.Context, client client.Client, cluster client.ObjectKey) (*kubernetes.Clientset, error) {
	data, err := kubeconfig.FromSecret(ctx, client, cluster)
	if err != nil {
		return nil, fmt.Errorf("error fetching %s kubeconfig from secret: %w", cluster.Name, err)
	}
	config, err := clientcmd.NewClientConfigFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("error generating %s clientconfig: %w", cluster.Name, err)
	}
	restConfig, err := config.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("error generating %s restconfig:  %w", cluster.Name, err)
	}

	tCfg, err := restConfig.TransportConfig()
	if err != nil {
		return nil, fmt.Errorf("error generating %s transport config: %w", cluster.Name, err)
	}
	tlsCfg, err := transport.TLSConfigFor(tCfg)
	if err != nil {
		return nil, fmt.Errorf("error generating %s tls config: %w", cluster.Name, err)
	}

	// Disable keep-alive to avoid hanging connections
	cl := http.DefaultClient
	cl.Transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: -1,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       5 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsCfg,
	}

	return kubernetes.NewForConfigAndClient(restConfig, cl)
}
