package bootstrap

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	// Skip the control plane machines that don't have worker enabled
	if capiutil.IsControlPlaneMachine(machine) && machine.ObjectMeta.Labels["k0smotron.io/control-plane-worker-enabled"] != "true" {
		return ctrl.Result{}, nil
	}

	// Skip non-k0s machines
	if machine.Spec.Bootstrap.ConfigRef.Kind != "K0sControllerConfig" && machine.Spec.Bootstrap.ConfigRef.Kind != "K0sWorkerConfig" {
		return ctrl.Result{}, nil
	}

	if machine.Spec.ProviderID == nil || *machine.Spec.ProviderID == "" {
		return ctrl.Result{}, fmt.Errorf("waiting for providerID for the machine %s/%s", machine.Namespace, machine.Name)
	}

	cluster, err := capiutil.GetClusterByName(ctx, p.Client, machine.Namespace, machine.Spec.ClusterName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("can't get cluster %s/%s: %w", machine.Namespace, machine.Spec.ClusterName, err)
	}

	childClient, err := k0smoutil.GetKubeClient(context.Background(), p.Client, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("can't get kube client for cluster %s/%s: %w. may not be created yet", machine.Namespace, machine.Spec.ClusterName, err)
	}

	nodes, err := childClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", machineNameNodeLabel, machine.GetName()),
	})
	if err != nil || len(nodes.Items) == 0 {
		log.Info("waiting for node to be available for machine " + machine.Name)
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	}

	node := nodes.Items[0]
	if node.Spec.ProviderID == "" {
		node.Spec.ProviderID = *machine.Spec.ProviderID
		err = retry.OnError(retry.DefaultBackoff, func(err error) bool {
			return true
		}, func() error {
			_, upErr := childClient.CoreV1().Nodes().Update(context.Background(), &node, metav1.UpdateOptions{})
			return upErr
		})

		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update node '%s' with providerID: %w", node.Name, err)
		}
	}

	return ctrl.Result{}, nil
}

func (p *ProviderIDController) SetupWithManager(mgr ctrl.Manager) error {
	apiResources, err := p.ClientSet.Discovery().ServerResourcesForGroupVersion(clusterv1.GroupVersion.String())
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if apiResources == nil {
		log.Log.Info("CAPI crds are not installed yet, skipping initializing providerID controller")
		return nil
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1.Machine{}).
		Complete(p)
}
