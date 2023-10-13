/*


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

package infrastructure

import (
	"context"
	"fmt"

	infrastructure "github.com/k0sproject/k0smotron/api/infrastructure/v1beta1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type RemoteMachineController struct {
	client.Client
	Scheme     *runtime.Scheme
	ClientSet  *kubernetes.Clientset
	RESTConfig *rest.Config
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=remotemachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=remotemachines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=exp.cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *RemoteMachineController) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := log.FromContext(ctx).WithValues("remotemachine", req.NamespacedName)
	log.Info("Reconciling RemoteMachine")

	rm := &infrastructure.RemoteMachine{}
	if err := r.Get(ctx, req.NamespacedName, rm); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("RemoteMachine not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get RemoteMachine")
		return ctrl.Result{}, err
	}

	// Fetch the Machine that ows RemoteMachine
	machine, err := capiutil.GetOwnerMachine(ctx, r.Client, rm.ObjectMeta)
	if err != nil {
		return ctrl.Result{Requeue: true}, err
	}
	if machine == nil {
		log.Info("Waiting for Machine Controller to set OwnerRef on RemoterMachine")
		return ctrl.Result{Requeue: true}, nil
	}

	log = log.WithValues("machine", machine.Name)

	defer func() {
		// Always update the RemoteMachine status with the phase the state machine is in
		if err := r.Status().Update(ctx, rm); err != nil {
			log.Error(err, "Failed to update RemoteMachine status")
		}
	}()

	// Fetch the Cluster
	cluster, err := capiutil.GetClusterFromMetadata(ctx, r.Client, machine.ObjectMeta)
	if err != nil {
		log.Info("RemoteMachine owner Machine is missing cluster label or cluster does not exist")
		return ctrl.Result{Requeue: true}, err
	}
	if cluster == nil {
		log.Info(fmt.Sprintf("Cluster association broken for RemoteMachine %s/%s", rm.Namespace, rm.Name))
		return ctrl.Result{Requeue: true}, nil
	}

	// Bail out early if surrounding objects are not ready
	if cluster.Spec.Paused || annotations.IsPaused(cluster, rm) {
		log.Info("Cluster is paused, skipping RemoteMachine reconciliation")
	}

	if !cluster.Status.InfrastructureReady {
		log.Info("Cluster infrastructure is not ready yet")
		return ctrl.Result{Requeue: true}, nil
	}

	if rm.Spec.ProviderID != "" {
		log.Info("RemoteMachine already has ProviderID, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	if machine.Spec.Bootstrap.DataSecretName == nil {
		log.Info("Waiting for Bootstrap Controller to set bootstrap data")
		return ctrl.Result{Requeue: true}, nil
	}

	// Fetch the bootstrap data
	bootstrapData, err := r.getBootstrapData(ctx, machine)
	if err != nil {
		log.Error(err, "Failed to get bootstrap data")
		return ctrl.Result{}, err
	}

	// Get the ssh key
	sshKey, err := r.getSSHKey(ctx, rm)
	if err != nil {
		log.Error(err, "Failed to get ssh key")
		return ctrl.Result{Requeue: true}, err
	}

	p := &Provisioner{
		bootstrapData: bootstrapData,
		sshKey:        sshKey,
		machine:       rm,
		log:           log,
	}

	defer func() {
		log.Info("Reconcile complete")
		if err != nil {
			rm.Status.FailureReason = "ProvisionFailed"
			rm.Status.FailureMessage = err.Error()
			rm.Status.Ready = false
		} else {
			rm.Status.FailureReason = ""
			rm.Status.FailureMessage = ""
			rm.Status.Ready = true
		}
		log.Info(fmt.Sprintf("Updating RemoteMachine status: %+v", rm.Status))
		// Always update the RemoteMachine status with the phase the state machine is in
		if err := r.Status().Update(ctx, rm); err != nil {
			log.Error(err, "Failed to update RemoteMachine status")
		}
	}()

	err = p.Provision(ctx)
	if err != nil {
		log.Error(err, "Failed to provision RemoteMachine")
		return ctrl.Result{}, err
	}

	rm.Spec.ProviderID = fmt.Sprintf("remote-machine://%s:%d", rm.Spec.Address, rm.Spec.Port)

	if err := r.Update(ctx, rm); err != nil {
		log.Error(err, "Failed to update RemoteMachine")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *RemoteMachineController) getSSHKey(ctx context.Context, rm *infrastructure.RemoteMachine) ([]byte, error) {
	secret := &v1.Secret{}
	key := client.ObjectKey{
		Namespace: rm.Namespace,
		Name:      rm.Spec.SSHKeyRef.Name,
	}

	if err := r.Client.Get(ctx, key, secret); err != nil {
		return nil, err
	}

	return secret.Data["value"], nil

}

func (r *RemoteMachineController) getBootstrapData(ctx context.Context, machine *clusterv1.Machine) ([]byte, error) {
	secret := &v1.Secret{}
	key := client.ObjectKey{
		Namespace: machine.Namespace,
		Name:      *machine.Spec.Bootstrap.DataSecretName,
	}

	if err := r.Client.Get(ctx, key, secret); err != nil {
		return nil, err
	}

	return secret.Data["value"], nil
}

func (r *RemoteMachineController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructure.RemoteMachine{}).
		Complete(r)
}
