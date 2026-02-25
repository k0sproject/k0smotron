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
	"maps"
	"time"

	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	infrastructure "github.com/k0sproject/k0smotron/api/infrastructure/v1beta1"
	"github.com/k0sproject/k0smotron/internal/provisioner"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/finalizers"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ErrPooledMachineNotFound is returned when a RemoteMachine references a pool
// but no free machine is found in the pool.
var ErrPooledMachineNotFound = fmt.Errorf("free pooled machine not found")

// Provisioner defines the interface for provisioning a remote machine.
type Provisioner interface {
	Provision(ctx context.Context) error
	Cleanup(ctx context.Context, mode RemoteMachineMode) error
}

// RemoteMachineController is responsible for reconciling the RemoteMachine resource.
type RemoteMachineController struct {
	client.Client
	SecretCachingClient client.Client
	Scheme              *runtime.Scheme
	ClientSet           *kubernetes.Clientset
	RESTConfig          *rest.Config
}

// RemoteMachineMode defines the mode of the RemoteMachine, which can be
// either a control plane node, a worker node, or a non-k0s machine.
type RemoteMachineMode int

const (
	// RemoteMachineFinalizer is the finalizer used for RemoteMachine resources to ensure proper cleanup.
	RemoteMachineFinalizer = "remotemachine.k0smotron.io/finalizer"
	// ModeController indicates that the RemoteMachine is a control plane node.
	ModeController RemoteMachineMode = iota
	// ModeWorker indicates that the RemoteMachine is a worker node.
	ModeWorker
	// ModeNonK0s indicates that the RemoteMachine is not a k0s node, and should be treated as a generic machine.
	ModeNonK0s
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=remotemachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=remotemachines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=pooledremotemachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=pooledremotemachines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=exp.cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="batch",resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile reconciles the RemoteMachine resource and ensures the remote machine is provisioned and in a ready state.
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

	if finalizerAdded, err := finalizers.EnsureFinalizer(ctx, r.Client, rm, RemoteMachineFinalizer); err != nil || finalizerAdded {
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
	switch machine.Spec.Bootstrap.ConfigRef.Kind {
	case "K0sWorkerConfig":
		mode = ModeWorker
	case "K0sControllerConfig":
		mode = ModeController
	default:
		mode = ModeNonK0s
	}

	log = log.WithValues("machine", machine.Name)

	rmPatchHelper, err := patch.NewHelper(rm, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	if rm.ObjectMeta.DeletionTimestamp.IsZero() {
		if rm.Spec.Pool != "" {
			err := r.reservePooledMachine(ctx, rm)
			if err != nil {
				log.Error(err, "Error reserving PooledMachine")
				return ctrl.Result{Requeue: true}, err
			}
		}

		if rm.Spec.ProvisionJob == nil {
			if rm.Spec.Address == "" || rm.Spec.SSHKeyRef.Name == "" {
				rm.Status.FailureReason = "MissingFields"
				rm.Status.FailureMessage = "If pool is empty, following fields are required: address, sshKeyRef"
				rm.Status.Ready = false
				if err := rmPatchHelper.Patch(ctx, rm); err != nil {
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
		if annotations.IsPaused(cluster, rm) {
			log.Info("Cluster is paused, skipping RemoteMachine reconciliation")
			return ctrl.Result{}, nil
		}

		if !conditions.IsTrue(cluster, clusterv1.ClusterInfrastructureReadyCondition) {
			log.Info("Cluster infrastructure is not ready yet")
			return ctrl.Result{Requeue: true}, nil
		}

		if rm.Spec.ProviderID != "" {
			if !rm.Status.Ready {
				rm.Status.Ready = true
				if err := rmPatchHelper.Patch(ctx, rm); err != nil {
					log.Error(err, "Failed to update RemoteMachine status")
					return ctrl.Result{}, err
				}
			}
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
		// If the bootstrap data secret is not found AND the machine is being deleted, don't requeue
		if !(apierrors.IsNotFound(err) && !machine.ObjectMeta.DeletionTimestamp.IsZero()) {
			log.Error(err, "Failed to get bootstrap data")
			return ctrl.Result{}, err
		}
	}

	cloudInit := &provisioner.InputProvisionData{}
	err = yaml.Unmarshal(bootstrapData, cloudInit)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to parse bootstrap data: %w", err)
	}

	var p Provisioner
	if rm.Spec.ProvisionJob != nil {
		p = &JobProvisioner{
			bootstrapData: bootstrapData,
			cloudInit:     cloudInit,
			remoteMachine: rm,
			machine:       machine,
			provisionJob:  rm.Spec.ProvisionJob,
			client:        r.Client,
			clientSet:     r.ClientSet,
			log:           log,
		}
	} else {
		// Get the ssh key
		sshKey, err := r.getSSHKey(ctx, rm)
		if err != nil {
			log.Error(err, "Failed to get ssh key")
			return ctrl.Result{Requeue: true}, err
		}

		p = &SSHProvisioner{
			cloudInit: cloudInit,
			sshKey:    sshKey,
			machine:   rm,
			log:       log,
		}
	}

	if !rm.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(rm, RemoteMachineFinalizer) {
			if err := p.Cleanup(ctx, mode); err != nil {
				log.Error(err, "Failed to cleanup RemoteMachine")
			}
			if rm.Spec.Pool != "" {
				// Return the machine back to pool
				if err := r.returnMachineToPool(ctx, rm); err != nil {
					return ctrl.Result{}, err
				}
			}
			controllerutil.RemoveFinalizer(rm, RemoteMachineFinalizer)
			if err := rmPatchHelper.Patch(ctx, rm); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if rm.Status.Ready {
		return ctrl.Result{}, nil
	}

	provCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Running a goroutine to monitor if the RemoteMachine gets deleted during provisioning. This way we can delete
	// proceed to cleanup immediately without waiting for the provisioning to timeout. For example in scenarios where
	// the bootstrap process hangs and the controller needs to be able to delete the RemoteMachine. Controller-runtime
	// only runs one Reconcile at a time per object, so we need to monitor deletion in a separate goroutine.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-provCtx.Done():
				return
			case <-ticker.C:
				updatedRemoteMachine := &infrastructure.RemoteMachine{}
				if err := r.Get(provCtx, client.ObjectKeyFromObject(rm), updatedRemoteMachine); err == nil &&
					!updatedRemoteMachine.DeletionTimestamp.IsZero() {
					log.Info("Cancelling Bootstrap because the underlying machine has been deleted")
					cancel()
					return
				}
			}
		}
	}()

	defer func() {
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
		if patchErr := rmPatchHelper.Patch(ctx, rm); patchErr != nil {
			log.Error(patchErr, "Failed to update RemoteMachine status")
		}
	}()

	err = p.Provision(provCtx)
	if err != nil {
		log.Error(err, "Failed to provision RemoteMachine")
		return ctrl.Result{}, err
	}

	rm.Spec.ProviderID = fmt.Sprintf("remote-machine://%s:%d", rm.Spec.Address, rm.Spec.Port)
	rm.Status.Addresses = []clusterv1.MachineAddress{
		{
			Type:    clusterv1.MachineExternalIP,
			Address: rm.Spec.Address,
		},
	}

	m := machine.DeepCopy()
	for l := range rm.Labels {
		if _, ok := m.Labels[l]; !ok {
			m.Labels[l] = rm.Labels[l]
		}
	}

	if len(m.Annotations) == 0 {
		m.Annotations = make(map[string]string)
	}
	for k := range rm.Annotations {
		if _, ok := m.Annotations[k]; !ok {
			m.Annotations[k] = rm.Annotations[k]
		}
	}

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return r.Patch(ctx, m, client.MergeFrom(machine))
	})
	if err != nil {
		log.Error(err, "Failed to update Machine")
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

	maps.Copy(rm.Labels, foundPooledMachine.Labels)
	maps.Copy(rm.Annotations, foundPooledMachine.Annotations)
	rm.Spec.Address = foundPooledMachine.Spec.Machine.Address
	rm.Spec.Port = foundPooledMachine.Spec.Machine.Port
	rm.Spec.User = foundPooledMachine.Spec.Machine.User
	rm.Spec.SSHKeyRef = foundPooledMachine.Spec.Machine.SSHKeyRef
	rm.Spec.UseSudo = foundPooledMachine.Spec.Machine.UseSudo
	rm.Spec.CommandsAsScript = foundPooledMachine.Spec.Machine.CommandsAsScript
	rm.Spec.WorkingDir = foundPooledMachine.Spec.Machine.WorkingDir
	rm.Spec.CustomCleanUpCommands = foundPooledMachine.Spec.Machine.CustomCleanUpCommands

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
		if !rm.DeletionTimestamp.IsZero() {
			return nil
		}
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
	log := log.FromContext(ctx).WithValues("remotemachine", rm.Name)
	log.Error(fmt.Errorf("no pooled machine found for remote machine"), "pooled machine not found", "namespace", rm.Namespace, "name", rm.Name)

	return nil
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
	if machine.Spec.Bootstrap.DataSecretName == nil {
		return nil, fmt.Errorf("wait for bootstap secret for the machine: %s", machine.Name)
	}
	secret := &v1.Secret{}
	key := client.ObjectKey{
		Namespace: machine.Namespace,
		Name:      *machine.Spec.Bootstrap.DataSecretName,
	}

	if err := r.SecretCachingClient.Get(ctx, key, secret); err != nil {
		return nil, err
	}

	return secret.Data["value"], nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RemoteMachineController) SetupWithManager(mgr ctrl.Manager, opts controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(opts).
		For(&infrastructure.RemoteMachine{}).
		Complete(r)
}
