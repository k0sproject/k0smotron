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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var ErrPooledMachineNotFound = fmt.Errorf("free pooled machine not found")

type RemoteMachineController struct {
	client.Client
	Scheme     *runtime.Scheme
	ClientSet  *kubernetes.Clientset
	RESTConfig *rest.Config
}

type RemoteMachineMode int

const (
	RemoteMachineFinalizer = "remotemachine.k0smotron.io/finalizer"

	ModeController RemoteMachineMode = iota
	ModeWorker
	ModeNonK0s
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=remotemachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=remotemachines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=pooledremotemachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=pooledremotemachines/status,verbs=get;list;watch;create;update;patch;delete
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

	mode := ModeNonK0s
	if machine.Spec.Bootstrap.ConfigRef != nil {
		switch machine.Spec.Bootstrap.ConfigRef.Kind {
		case "K0sWorkerConfig":
			mode = ModeWorker
		case "K0sControllerConfig":
			mode = ModeController
		default:
			mode = ModeNonK0s
		}
	}

	log = log.WithValues("machine", machine.Name)

	if rm.ObjectMeta.DeletionTimestamp.IsZero() {
		defer func() {
			// Always update the RemoteMachine status with the phase the state machine is in
			if err := r.Status().Update(ctx, rm); err != nil {
				log.Error(err, "Failed to update RemoteMachine status")
			}
		}()

		if rm.Spec.Pool != "" {
			err := r.reservePooledMachine(ctx, rm)
			if err != nil {
				log.Error(err, "Error reserving PooledMachine")
				return ctrl.Result{Requeue: true}, err
			}
		} else {
			if rm.Spec.Address == "" || rm.Spec.SSHKeyRef.Name == "" {
				rm.Status.FailureReason = "MissingFields"
				rm.Status.FailureMessage = "If pool is empty, following fields are required: address, sshKeyRef"
				rm.Status.Ready = false
				if err := r.Status().Update(ctx, rm); err != nil {
					log.Error(err, "Failed to update RemoteMachine status")
				}
				return ctrl.Result{Requeue: true}, nil
			}
		}

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

	if !rm.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(rm, RemoteMachineFinalizer) {
			if err := p.Cleanup(ctx, mode); err != nil {
				p.log.Error(err, "Failed to cleanup RemoteMachine")
			}
			if rm.Spec.Pool != "" {
				// Return the machine back to pool
				if err := r.returnMachineToPool(ctx, rm); err != nil {
					return ctrl.Result{}, err
				}
			}
			controllerutil.RemoveFinalizer(rm, RemoteMachineFinalizer)
			if err := r.Update(ctx, rm); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(rm, RemoteMachineFinalizer) {
		controllerutil.AddFinalizer(rm, RemoteMachineFinalizer)
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

func (r *RemoteMachineController) reservePooledMachine(ctx context.Context, rm *infrastructure.RemoteMachine) error {
	pooledMachineList := &infrastructure.PooledRemoteMachineList{}
	if err := r.Client.List(ctx, pooledMachineList, client.InNamespace(rm.Namespace)); err != nil {
		return fmt.Errorf("failed to list pooled machines: %w", err)
	}

	var (
		firstFreePooledMachine *infrastructure.PooledRemoteMachine
		foundPooledMachine     *infrastructure.PooledRemoteMachine
	)
	for _, pm := range pooledMachineList.Items {
		if pm.Spec.Pool == rm.Spec.Pool {
			if pm.Status.Reserved && pm.Status.MachineRef.Name == rm.GetName() {
				foundPooledMachine = &pm
				break
			}

			if !pm.Status.Reserved {
				firstFreePooledMachine = &pm
			}
		}
	}

	if foundPooledMachine == nil && firstFreePooledMachine == nil {
		return ErrPooledMachineNotFound
	}

	if foundPooledMachine == nil && firstFreePooledMachine != nil {
		foundPooledMachine = firstFreePooledMachine
		foundPooledMachine.Status.Reserved = true
		foundPooledMachine.Status.MachineRef = infrastructure.RemoteMachineRef{
			Name:      rm.GetName(),
			Namespace: rm.GetNamespace(),
		}

		err := r.Status().Update(ctx, foundPooledMachine)
		if err != nil {
			return fmt.Errorf("failed to update pooled machine status: %w", err)
		}
	}

	rm.Spec.Address = foundPooledMachine.Spec.Machine.Address
	rm.Spec.Port = foundPooledMachine.Spec.Machine.Port
	rm.Spec.User = foundPooledMachine.Spec.Machine.User
	rm.Spec.SSHKeyRef = foundPooledMachine.Spec.Machine.SSHKeyRef

	return nil
}

func (r *RemoteMachineController) returnMachineToPool(ctx context.Context, rm *infrastructure.RemoteMachine) error {
	if rm.Spec.Pool == "" {
		return nil
	}

	pool := rm.Spec.Pool
	pooledMachines := &infrastructure.PooledRemoteMachineList{}
	err := r.List(ctx, pooledMachines, &client.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pooled machines: %w", err)
	}
	if len(pooledMachines.Items) == 0 {
		return fmt.Errorf("no pooled machines found for pool %s", pool)
	}

	for _, pooledMachine := range pooledMachines.Items {
		if pooledMachine.Status.Reserved &&
			pooledMachine.Status.MachineRef.Name == rm.Name &&
			pooledMachine.Status.MachineRef.Namespace == rm.Namespace {

			pooledMachine.Status.Reserved = false
			pooledMachine.Status.MachineRef = infrastructure.RemoteMachineRef{}
			if err := r.Status().Update(ctx, &pooledMachine); err != nil {
				return fmt.Errorf("failed to update pooled machine: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("no pooled machine found for remote machine %s/%s", rm.Namespace, rm.Name)
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
