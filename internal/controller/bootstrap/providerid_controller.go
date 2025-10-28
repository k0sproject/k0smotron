package bootstrap

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"net"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	k0smoutil "github.com/k0sproject/k0smotron/internal/controller/util"
)

type ProviderIDController struct {
	client.Client
	Scheme    *runtime.Scheme
	ClientSet *kubernetes.Clientset
}

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

	pid, err := getProviderIDFromMachine(machine)
	if err != nil {
		return ctrl.Result{}, err
	}
	if pid == "" {
		log.Info("waiting for providerID for the machine " + machine.Name)
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}

	cluster, err := capiutil.GetClusterByName(ctx, p.Client, machine.Namespace, machine.Spec.ClusterName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("can't get cluster %s/%s: %w", machine.Namespace, machine.Spec.ClusterName, err)
	}

	childClient, err := k0smoutil.GetKubeClient(context.Background(), p.Client, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("can't get kube client for cluster %s/%s: %w. may not be created yet", machine.Namespace, machine.Spec.ClusterName, err)
	}

	nodes, err := childClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list nodes in cluster %s/%s: %w", cluster.Namespace, cluster.Name, err)
	}

	var node *corev1.Node
	for _, n := range nodes.Items {
		if n.Spec.ProviderID == *machine.Spec.ProviderID {
			// ProviderID is already set on the node
			return ctrl.Result{}, nil
		}

		// If node name matches machine name, we have found our node
		if n.Name == machine.GetName() {
			node = &n
			break
		}

		// Check k0smotron.io/machine-name node label
		if val, ok := n.Labels[machineNameNodeLabel]; ok && val == machine.GetName() {
			node = &n
			break
		}

		// Check node addresses against machine addresses
		for _, addr := range machine.Status.Addresses {
			for _, nodeAddr := range n.Status.Addresses {
				if addr.Address == nodeAddr.Address && !net.ParseIP(nodeAddr.Address).IsLoopback() {
					node = &n
					break
				}
			}
		}
	}

	if node == nil {
		log.Info("waiting for node to be available for machine " + machine.Name)
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	}

	if node.Spec.ProviderID == "" {
		node.Spec.ProviderID = pid
		node.Labels[machineNameNodeLabel] = machine.GetName()
		err = retry.OnError(retry.DefaultBackoff, func(err error) bool {
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

// getProviderIDFromMachine returns spec.providerID working with both CAPI v1beta1 (*string)
// and v1beta2 (string) via unstructured conversion.
func getProviderIDFromMachine(machine *clusterv1.Machine) (string, error) {
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(machine)
	if err != nil {
		return "", err
	}
	s, found, err := unstructured.NestedString(obj, "spec", "providerID")
	if err != nil || !found {
		return "", err
	}
	return s, nil
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
