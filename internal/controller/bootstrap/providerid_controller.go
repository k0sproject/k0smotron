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
package bootstrap

import (
	"context"
	"fmt"
	"net"
	"time"

	corev1 "k8s.io/api/core/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/controllers/clustercache"
	capiutil "sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	k0smoutil "github.com/k0sproject/k0smotron/v2/internal/controller/util"
)

// nodeListPageSize bounds one node list request. The client carries a request
// timeout, which a whole large cluster in a single response can outlast.
const nodeListPageSize = 500

// ProviderIDController is responsible for reconciling the ProviderID field of the Machine resource.
type ProviderIDController struct {
	client.Client
	Scheme       *runtime.Scheme
	ClientSet    *kubernetes.Clientset
	ClusterCache clustercache.ClusterCache
}

// Reconcile reconciles the ProviderID field of the Machine resource and
// ensures it is set on the corresponding Node in the workload cluster.
func (p *ProviderIDController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("providerID", req.NamespacedName)
	log.Info("Reconciling machine's ProviderID")

	machine := &clusterv1.Machine{}
	if err := p.Get(ctx, req.NamespacedName, machine); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("machine not found")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get machine")
		return ctrl.Result{}, err
	}

	// Skip non-k0smotron managed machines
	if machine.Spec.Bootstrap.ConfigRef.Kind != "K0sControllerConfig" && machine.Spec.Bootstrap.ConfigRef.Kind != "K0sWorkerConfig" &&
		machine.Spec.InfrastructureRef.Kind != "RemoteMachine" {
		return ctrl.Result{}, nil
	}

	// Skip the control plane machines that don't have worker enabled
	if machine.Spec.Bootstrap.ConfigRef.Kind == "K0sControllerConfig" && machine.ObjectMeta.Labels["k0smotron.io/control-plane-worker-enabled"] != "true" {
		return ctrl.Result{}, nil
	}

	if machine.Spec.ProviderID == "" {
		log.Info("waiting for providerID for the machine " + machine.Name)
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}

	cluster, err := capiutil.GetClusterByName(ctx, p.Client, machine.Namespace, machine.Spec.ClusterName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("can't get cluster %s/%s: %w", machine.Namespace, machine.Spec.ClusterName, err)
	}

	childClient, err := k0smoutil.GetWorkloadClusterClientset(ctx, p.Client, p.ClusterCache, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("can't get kube client for cluster %s/%s: %w. may not be created yet", machine.Namespace, machine.Spec.ClusterName, err)
	}

	node, providerIDSet, err := nodeForMachine(ctx, childClient.CoreV1().Nodes().List, machine)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list nodes in cluster %s/%s: %w", cluster.Namespace, cluster.Name, err)
	}
	if providerIDSet {
		return ctrl.Result{}, nil
	}

	if node == nil {
		log.Info("waiting for node to be available for machine " + machine.Name)
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	}

	if node.Spec.ProviderID == "" {
		node.Spec.ProviderID = machine.Spec.ProviderID
		node.Labels[machineNameNodeLabel] = machine.GetName()
		err = retry.OnError(retry.DefaultBackoff, func(_ error) bool {
			return true
		}, func() error {
			_, upErr := childClient.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
			return upErr
		})

		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update node '%s' with providerID: %w", node.Name, err)
		}
	}

	return ctrl.Result{}, nil
}

// listNodes is the node list call, which a test can stand in for.
type listNodes func(ctx context.Context, opts metav1.ListOptions) (*corev1.NodeList, error)

// nodeForMachine finds the node the machine's providerID belongs on, a page at a
// time. The bool reports the providerID already being on a node.
func nodeForMachine(ctx context.Context, list listNodes, machine *clusterv1.Machine) (*corev1.Node, bool, error) {
	opts := metav1.ListOptions{Limit: nodeListPageSize}

	for {
		nodes, err := list(ctx, opts)
		if err != nil {
			return nil, false, err
		}

		for i := range nodes.Items {
			n := &nodes.Items[i]

			if n.Spec.ProviderID == machine.Spec.ProviderID {
				// ProviderID is already set on the node
				return nil, true, nil
			}

			// If node name matches machine name, we have found our node
			if n.Name == machine.GetName() {
				return n, false, nil
			}

			// Check k0smotron.io/machine-name node label
			if val, ok := n.Labels[machineNameNodeLabel]; ok && val == machine.GetName() {
				return n, false, nil
			}

			// Check node addresses against machine addresses
			for _, addr := range machine.Status.Addresses {
				for _, nodeAddr := range n.Status.Addresses {
					if addr.Address == nodeAddr.Address && !net.ParseIP(nodeAddr.Address).IsLoopback() {
						return n, false, nil
					}
				}
			}
		}

		if nodes.Continue == "" {
			return nil, false, nil
		}

		opts.Continue = nodes.Continue
	}
}

// SetupWithManager sets up the controller with the Manager.
func (p *ProviderIDController) SetupWithManager(mgr ctrl.Manager, opts controller.Options) error {
	apiResources, err := p.ClientSet.Discovery().ServerResourcesForGroupVersion(clusterv1.GroupVersion.String())
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if apiResources == nil {
		log.Log.Info("CAPI crds are not installed yet, skipping initializing providerID controller")
		return nil
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(opts).
		For(&clusterv1.Machine{}).
		Complete(p)
}
