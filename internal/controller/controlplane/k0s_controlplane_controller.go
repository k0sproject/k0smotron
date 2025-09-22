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

package controlplane

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	autopilot "github.com/k0sproject/k0s/pkg/apis/autopilot/v1beta2"
	"github.com/k0sproject/k0smotron/internal/controller/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	kubeadmbootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/certs"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/failuredomains"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	kutil "github.com/k0sproject/k0smotron/internal/util"
)

const (
	defaultK0sSuffix  = "k0s.0"
	defaultK0sVersion = "v1.27.9+k0s.0"
)

var (
	ErrNotReady               = fmt.Errorf("waiting for the state")
	ErrNewMachinesNotReady    = fmt.Errorf("waiting for new machines: %w", ErrNotReady)
	FRPTokenNameTemplate      = "%s-frp-token"
	FRPConfigMapNameTemplate  = "%s-frps-config"
	FRPDeploymentNameTemplate = "%s-frps"
	FRPServiceNameTemplate    = "%s-frps"
)

type K0sController struct {
	client.Client
	SecretCachingClient client.Client
	ClientSet           *kubernetes.Clientset
	RESTConfig          *rest.Config
	// workloadClusterKubeClient is used during testing to inject a fake client
	workloadClusterKubeClient *kubernetes.Clientset
}

// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=k0scontrolplanes/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=k0scontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch;update;patch

func (c *K0sController) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := log.FromContext(ctx).WithValues("controlplane", req.NamespacedName)
	kcp := &cpv1beta1.K0sControlPlane{}

	defer func() {
		version := ""
		if kcp != nil {
			version = kcp.Spec.Version
		}
		log.Info("Reconciliation finished", "result", res, "error", err, "status.version", version)
	}()
	if err := c.Get(ctx, req.NamespacedName, kcp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get K0sControlPlane")
		return ctrl.Result{}, err
	}

	if finalizerAdded, err := util.EnsureFinalizer(ctx, c.Client, kcp, cpv1beta1.K0sControlPlaneFinalizer); err != nil || finalizerAdded {
		return ctrl.Result{}, err
	}

	kcpPatchHelper, err := patch.NewHelper(kcp, c.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciling K0sControlPlane", "version", kcp.Spec.Version)

	if kcp.Spec.Version == "" {
		kcp.Spec.Version = defaultK0sVersion
	}

	if !strings.Contains(kcp.Spec.Version, "+k0s.") {
		kcp.Spec.Version = fmt.Sprintf("%s+%s", kcp.Spec.Version, defaultK0sSuffix)
	}

	cluster, err := capiutil.GetOwnerCluster(ctx, c.Client, kcp.ObjectMeta)
	if err != nil {
		log.Error(err, "Failed to get owner cluster")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info("Waiting for Cluster Controller to set OwnerRef on K0sControlPlane")
		return ctrl.Result{}, nil
	}

	clusterPatchHelper, err := patch.NewHelper(cluster, c.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	if annotations.IsPaused(cluster, kcp) {
		log.Info("Reconciliation is paused for this object or owning cluster")
		return ctrl.Result{}, nil
	}

	// Always patch the object to update the status
	defer func() {
		log.Info("Updating status")
		existingStatus := kcp.Status.DeepCopy()

		// When controlplane is being deleted, we don't update the status to avoid requests workload API
		// because it is terminating so machines probably are terminating too.
		// TODO: maybe updateStatus method should be refactored to at least report unavailable machines,
		// which not requires to call workload API.
		var derr error
		if kcp.DeletionTimestamp.IsZero() {
			// Separate var for status update errors to avoid shadowing err
			derr = c.updateStatus(ctx, kcp, cluster)
			if derr != nil {
				if !errors.Is(derr, errUpgradeNotCompleted) {
					log.Error(derr, "Failed to update status")
					return
				}

				if res.IsZero() {
					res = ctrl.Result{RequeueAfter: 10 * time.Second}
				}
			}

			if errors.Is(err, ErrNotReady) || reflect.DeepEqual(existingStatus, kcp.Status) {
				return
			}
		}

		derr = kcpPatchHelper.Patch(ctx, kcp)
		if derr != nil {
			log.Error(derr, "Failed to patch status")
			res = ctrl.Result{}
			err = derr
			return
		}
		log.Info("Status updated successfully")

		if kcp.Status.Ready {
			if perr := clusterPatchHelper.Patch(ctx, cluster); perr != nil {
				err = fmt.Errorf("failed to patch cluster: %w", perr)
			}
		}

		// Requeue the reconciliation if the status is not ready
		if !kcp.Status.Ready {
			log.Info("Requeuing reconciliation in 20sec since the control plane is not ready")
			res = ctrl.Result{RequeueAfter: 20 * time.Second, Requeue: true}
		}

	}()

	if !kcp.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("Reconcile K0sControlPlane deletion")
		return c.reconcileDelete(ctx, cluster, kcp)
	}

	log = log.WithValues("cluster", cluster.Name)

	if err := c.ensureCertificates(ctx, cluster, kcp); err != nil {
		log.Error(err, "Failed to ensure certificates")
		return ctrl.Result{}, err
	}

	if err := c.reconcileTunneling(ctx, cluster, kcp); err != nil {
		log.Error(err, "Failed to reconcile tunneling")
		return ctrl.Result{}, err
	}

	if err := c.reconcileConfig(ctx, cluster, kcp); err != nil {
		log.Error(err, "Failed to reconcile config")
		return ctrl.Result{}, err
	}

	err = c.reconcile(ctx, cluster, kcp)
	if err != nil {
		if errors.Is(err, ErrNotReady) {
			return ctrl.Result{RequeueAfter: 10 * time.Second, Requeue: true}, nil
		}
		return res, err
	}

	return res, err

}

func (c *K0sController) reconcileKubeconfig(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) error {
	logger := log.FromContext(ctx, "cluster", cluster.Name, "kcp", kcp.Name)

	if cluster.Spec.ControlPlaneEndpoint.IsZero() {
		return fmt.Errorf("control plane endpoint is not set: %w", ErrNotReady)
	}

	kubeconfigSecrets := []*corev1.Secret{}

	// Always rotate certificates if needed.
	defer func() {
		for _, kc := range kubeconfigSecrets {
			needsRotation, err := kubeconfig.NeedsClientCertRotation(kc, certs.ClientCertificateRenewalDuration)
			if err != nil {
				logger.Error(err, "Failed to check if certificate needs rotation.")
				return
			}

			if needsRotation {
				logger.Info("Rotating kubeconfig secret", "Secret", kc.GetName())
				if err := c.regenerateKubeconfigSecret(ctx, kc, cluster.Name); err != nil {
					logger.Error(err, "Failed to regenerate kubeconfig")
					return
				}
			}
		}
	}()

	workloadClusterKubeconfigSecret, err := secret.GetFromNamespacedName(ctx, c.SecretCachingClient, capiutil.ObjectKey(cluster), secret.Kubeconfig)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return kubeconfig.CreateSecret(ctx, c.SecretCachingClient, cluster)
		}

		return err
	}
	kubeconfigSecrets = append(kubeconfigSecrets, workloadClusterKubeconfigSecret)

	if kcp.Spec.K0sConfigSpec.Tunneling.Enabled {
		clusterKey := client.ObjectKey{
			Name:      cluster.GetName(),
			Namespace: cluster.GetNamespace(),
		}

		if kcp.Spec.K0sConfigSpec.Tunneling.Mode == "proxy" {

			secretName := secret.Name(cluster.Name+"-proxied", secret.Kubeconfig)

			proxiedKubeconfig := &corev1.Secret{}
			err := c.SecretCachingClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: secretName}, proxiedKubeconfig)
			if err != nil {
				if apierrors.IsNotFound(err) {
					kc, err := c.generateKubeconfig(ctx, clusterKey, fmt.Sprintf("https://%s", cluster.Spec.ControlPlaneEndpoint.String()))
					if err != nil {
						return err
					}

					for cn := range kc.Clusters {
						kc.Clusters[cn].ProxyURL = fmt.Sprintf("http://%s:%d", kcp.Spec.K0sConfigSpec.Tunneling.ServerAddress, kcp.Spec.K0sConfigSpec.Tunneling.TunnelingNodePort)
					}

					err = c.createKubeconfigSecret(ctx, kc, cluster, secretName)
					if err != nil {
						return err
					}
				}
				return err
			}
			kubeconfigSecrets = append(kubeconfigSecrets, proxiedKubeconfig)

		} else {
			secretName := secret.Name(cluster.Name+"-tunneled", secret.Kubeconfig)

			tunneledKubeconfig := &corev1.Secret{}
			err := c.SecretCachingClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: secretName}, tunneledKubeconfig)
			if err != nil {
				if apierrors.IsNotFound(err) {
					kc, err := c.generateKubeconfig(ctx, clusterKey, fmt.Sprintf("https://%s:%d", kcp.Spec.K0sConfigSpec.Tunneling.ServerAddress, kcp.Spec.K0sConfigSpec.Tunneling.TunnelingNodePort))
					if err != nil {
						return err
					}

					err = c.createKubeconfigSecret(ctx, kc, cluster, secretName)
					if err != nil {
						return err
					}
				}
				return err
			}
			kubeconfigSecrets = append(kubeconfigSecrets, tunneledKubeconfig)
		}
	}

	return nil
}

func (c *K0sController) reconcile(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) error {
	var err error
	kcp.Spec.K0sConfigSpec.K0s, err = enrichK0sConfigWithClusterData(cluster, kcp.Spec.K0sConfigSpec.K0s)
	if err != nil {
		return err
	}

	err = c.reconcileKubeconfig(ctx, cluster, kcp)
	if err != nil {
		return fmt.Errorf("error reconciling kubeconfig secret: %w", err)
	}

	err = c.reconcileUnhealthyMachines(ctx, cluster, kcp)
	if err != nil {
		return err
	}

	err = c.reconcileMachines(ctx, cluster, kcp)
	if err != nil {
		return err
	}

	return nil
}

func (c *K0sController) reconcileMachines(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) error {
	logger := log.FromContext(ctx, "cluster", cluster.Name, "kcp", kcp.Name)

	allMachines, err := collections.GetFilteredMachinesForCluster(ctx, c, cluster, collections.ControlPlaneMachines(cluster.Name))
	if err != nil {
		return fmt.Errorf("error collecting machines: %w", err)
	}
	activeMachines := allMachines.Filter(collections.ActiveMachines)
	deletedMachines := allMachines.Filter(collections.HasDeletionTimestamp)

	if deletedMachines.Len() > 0 {
		var errs []error
		for _, m := range deletedMachines.SortedByCreationTimestamp() {
			err := c.deleteK0sNodeResources(ctx, cluster, kcp, m)
			if err != nil {
				errs = append(errs, fmt.Errorf("error deleting k0s node resources: %w", err))
			}
		}

		if len(errs) > 0 {
			return kerrors.NewAggregate(errs)
		}
	}

	infraMachines, err := c.getInfraMachines(ctx, activeMachines)
	if err != nil {
		return fmt.Errorf("error getting infra machines: %w", err)
	}

	bootstrapConfigs, err := c.getBootstrapConfigs(ctx, activeMachines)
	if err != nil {
		return fmt.Errorf("error getting bootstrap configs: %w", err)
	}

	currentVersion, err := minVersion(activeMachines)
	if err != nil {
		return fmt.Errorf("error getting current cluster version from machines: %w", err)
	}
	log.Log.Info("Got current cluster version", "version", currentVersion)

	machineNamesToDelete := make(map[string]bool)
	desiredMachines := make(collections.Machines, len(allMachines))

	var clusterIsUpdating bool
	var infraMachineMissing bool
	for _, m := range activeMachines.SortedByCreationTimestamp() {
		if m.Spec.Version == nil || (!versionMatches(m, kcp.Spec.Version)) {
			clusterIsUpdating = true
			if kcp.Spec.UpdateStrategy == cpv1beta1.UpdateInPlace {
				desiredMachines.Insert(m)
			} else {
				machineNamesToDelete[m.Name] = true
			}
		} else if !matchesTemplateClonedFrom(infraMachines, kcp, m) || c.hasControllerConfigChanged(bootstrapConfigs, kcp, m) {
			if _, found := infraMachines[m.Name]; !found {
				infraMachineMissing = true
			}
			machineNamesToDelete[m.Name] = true
		} else {
			desiredMachines.Insert(m)
		}
	}

	// if it is necessary to reduce the number of replicas even counting the replicas to be eliminated
	// because they are outdated, we choose the oldest among the valid ones.
	if activeMachines.Len() > int(kcp.Spec.Replicas)+len(machineNamesToDelete) && len(desiredMachines) > 0 {
		machineNamesToDelete[desiredMachines.Oldest().Name] = true
	}
	log.Log.Info("Collected machines", "count", activeMachines.Len(), "desired", kcp.Spec.Replicas, "updating", clusterIsUpdating, "deleting", len(machineNamesToDelete), "desiredMachines", desiredMachines.Names())

	go func() {
		err = c.deleteOldControlNodes(ctx, cluster)
		if err != nil {
			logger.Error(err, "Error deleting old control nodes")
		}
	}()

	if clusterIsUpdating {
		log.Log.Info("Cluster is updating", "currentVersion", currentVersion, "newVersion", kcp.Spec.Version, "strategy", kcp.Spec.UpdateStrategy)
		if kcp.Spec.UpdateStrategy == cpv1beta1.UpdateRecreate {
			// If the cluster is running in single mode, we can't use the Recreate strategy
			if kcp.Spec.K0sConfigSpec.Args != nil {
				for _, arg := range kcp.Spec.K0sConfigSpec.Args {
					if arg == "--single" {
						return fmt.Errorf("UpdateRecreate strategy is not allowed when the cluster is running in single mode")
					}
				}
			}
		} else {
			kubeClient, err := c.getKubeClient(ctx, cluster)
			if err != nil {
				return fmt.Errorf("error getting cluster client set for machine update: %w", err)
			}

			err = c.createAutopilotPlan(ctx, kcp, cluster, kubeClient)
			if err != nil {
				return fmt.Errorf("error creating autopilot plan: %w", err)
			}
		}
	}

	if infraMachineMissing || (len(machineNamesToDelete)+len(desiredMachines) > int(kcp.Spec.Replicas)) {
		m := activeMachines.Newest().Name
		err := c.checkMachineIsReady(ctx, m, cluster)
		if err != nil {
			logger.Error(err, "Error checking machine left", "machine", m)
			return err
		}

		logger.Info("Found machines to delete", "count", len(machineNamesToDelete))

		// Remove the oldest machine abd wait for the machine to be deleted to avoid etcd issues
		machineToDelete := activeMachines.Filter(func(m *clusterv1.Machine) bool {
			return machineNamesToDelete[m.Name]
		}).Oldest()
		logger.Info("Found oldest machine to delete", "machine", machineToDelete.Name)
		if machineToDelete.Status.Phase == string(clusterv1.MachinePhaseDeleting) {
			logger.Info("Machine is being deleted, waiting for it to be deleted", "machine", machineToDelete.Name)
			return fmt.Errorf("waiting for previous machine to be deleted")
		}

		err = c.runMachineDeletionSequence(ctx, cluster, kcp, machineToDelete)
		if err != nil {
			return err
		}

		logger.Info("Deleted machine", "machine", machineToDelete.Name)
	}

	if len(desiredMachines) < int(kcp.Spec.Replicas) {

		name := names.SimpleNameGenerator.GenerateName(fmt.Sprintf("%s-", kcp.Name))
		log.Log.Info("desire machine", "name", name)

		for _, mn := range deletedMachines.Names() {
			if name == mn {
				logger.Info("machine is being deleted, requeue", "machine", mn)
				return ErrNotReady
			}
		}
		// If it is not the first machine to create, wait for the previous machine to be created to avoid etcd issues
		// if cluster if updating. Some providers don't publish failure domains immediately, so wait for the first
		// machine to be ready It's not slowing down the process overall, as we wait to the first machine anyway to
		// create join tokens.
		if activeMachines.Len() >= 1 {
			err := c.checkMachineIsReady(ctx, activeMachines.Newest().Name, cluster)
			if err != nil {
				return err
			}
		}

		infraMachine, err := c.createMachineFromTemplate(ctx, name, cluster, kcp)
		if err != nil {
			return fmt.Errorf("error creating machine from template: %w", err)
		}

		infraRef := corev1.ObjectReference{
			APIVersion: infraMachine.GetAPIVersion(),
			Kind:       infraMachine.GetKind(),
			Name:       infraMachine.GetName(),
			Namespace:  kcp.Namespace,
		}

		selectedFailureDomain := failuredomains.PickFewest(ctx, cluster.Status.FailureDomains.FilterControlPlane(), activeMachines, deletedMachines)
		machine, err := c.createMachine(ctx, name, cluster, kcp, infraRef, selectedFailureDomain)
		if err != nil {
			return fmt.Errorf("error creating machine: %w", err)
		}
		activeMachines[machine.Name] = machine
		desiredMachines.Insert(machine)

		err = c.createBootstrapConfig(ctx, name, cluster, kcp, activeMachines[name], cluster.Name)
		if err != nil {
			return fmt.Errorf("error creating bootstrap config: %w", err)
		}
	}

	if len(desiredMachines) < int(kcp.Spec.Replicas) {
		return ErrNewMachinesNotReady
	}

	return nil
}

func (c *K0sController) runMachineDeletionSequence(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane, machine *clusterv1.Machine) error {
	err := c.deleteK0sNodeResources(ctx, cluster, kcp, machine)
	if err != nil {
		return fmt.Errorf("error deleting k0s node resources: %w", err)
	}

	if err := c.deleteMachine(ctx, machine.Name, kcp); err != nil {
		return fmt.Errorf("error deleting machine from template: %w", err)
	}

	return nil
}

func (c *K0sController) deleteK0sNodeResources(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane, machine *clusterv1.Machine) error {
	logger := log.FromContext(ctx)

	if kcp.Status.Ready {
		kubeClient, err := c.getKubeClient(ctx, cluster)
		if err != nil {
			return fmt.Errorf("error getting cluster client set for deletion: %w", err)
		}

		waitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		err = wait.PollUntilContextCancel(waitCtx, 10*time.Second, true, func(fctx context.Context) (bool, error) {
			if err := c.markChildControlNodeToLeave(fctx, machine.Name, kubeClient); err != nil {
				return false, fmt.Errorf("error marking controlnode to leave: %w", err)
			}

			ok, err := c.checkMachineLeft(fctx, machine.Name, kubeClient)
			if err != nil {
				logger.Error(err, "Error checking machine left", "machine", machine.Name)
			}
			return ok, err
		})
		if err != nil {
			return fmt.Errorf("error checking machine left: %w", err)
		}
	}

	if err := c.removePreTerminateHookAnnotationFromMachine(ctx, machine); err != nil {
		return fmt.Errorf("failed to remove pre-terminate hook from control plane Machine '%s': %w", machine.Name, err)
	}

	return nil
}

func (c *K0sController) createBootstrapConfig(ctx context.Context, name string, _ *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane, machine *clusterv1.Machine, clusterName string) error {

	k0sConfigSpec := kcp.Spec.K0sConfigSpec.DeepCopy()
	k0sConfigSpec.Args = uniqueArgs(k0sConfigSpec.Args)

	controllerConfig := bootstrapv1.K0sControllerConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "bootstrap.cluster.x-k8s.io/v1beta1",
			Kind:       "K0sControllerConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   kcp.Namespace,
			Labels:      controlPlaneCommonLabelsForCluster(kcp, clusterName),
			Annotations: kcp.Spec.MachineTemplate.ObjectMeta.Annotations,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         machine.APIVersion,
				Kind:               machine.Kind,
				Name:               machine.GetName(),
				UID:                machine.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}},
		},
		Spec: bootstrapv1.K0sControllerConfigSpec{
			Version:       kcp.Spec.Version,
			K0sConfigSpec: k0sConfigSpec,
		},
	}

	if err := c.Client.Patch(ctx, &controllerConfig, client.Apply, &client.PatchOptions{
		FieldManager: "k0smotron",
	}); err != nil {
		return fmt.Errorf("error patching K0sControllerConfig: %w", err)
	}

	return nil
}

func (c *K0sController) checkMachineIsReady(ctx context.Context, machineName string, cluster *clusterv1.Cluster) error {
	kubeClient, err := c.getKubeClient(ctx, cluster)
	if err != nil {
		return fmt.Errorf("error getting cluster client set for machine update: %w", err)
	}
	var cn autopilot.ControlNode
	err = kubeClient.RESTClient().Get().AbsPath("/apis/autopilot.k0sproject.io/v1beta2/controlnodes/" + machineName).Do(ctx).Into(&cn)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ErrNewMachinesNotReady
		}
		return fmt.Errorf("error getting controlnode: %w", err)
	}

	joinedAt := cn.CreationTimestamp.Time

	// Check if the node has joined properly more than a minute ago
	// This allows a small "cool down" period between new nodes joining and old ones leaving
	if time.Since(joinedAt) < time.Minute {
		return ErrNewMachinesNotReady
	}

	return nil
}

func (c *K0sController) ensureCertificates(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) error {
	certificates := secret.NewCertificatesForInitialControlPlane(&kubeadmbootstrapv1.ClusterConfiguration{
		CertificatesDir: "/var/lib/k0s/pki",
	})
	return certificates.LookupOrGenerateCached(ctx, c.SecretCachingClient, c.Client, capiutil.ObjectKey(cluster), *metav1.NewControllerRef(kcp, cpv1beta1.GroupVersion.WithKind("K0sControlPlane")))
}

func (c *K0sController) reconcileConfig(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) error {
	log := log.FromContext(ctx)
	if kcp.Spec.K0sConfigSpec.K0s != nil {
		nllbEnabled, found, err := unstructured.NestedBool(kcp.Spec.K0sConfigSpec.K0s.Object, "spec", "network", "nodeLocalLoadBalancing", "enabled")
		if err != nil {
			return fmt.Errorf("error getting nodeLocalLoadBalancing: %v", err)
		}
		// Set the external address if NLLB is not enabled
		// Otherwise, just add the external address to the SANs to allow the clients to connect using LB address
		if !(found && nllbEnabled) {
			err = unstructured.SetNestedField(kcp.Spec.K0sConfigSpec.K0s.Object, cluster.Spec.ControlPlaneEndpoint.Host, "spec", "api", "externalAddress")
			if err != nil {
				return fmt.Errorf("error setting control plane endpoint: %v", err)
			}
		} else {
			sans := []string{cluster.Spec.ControlPlaneEndpoint.Host}
			existingSANs, sansFound, err := unstructured.NestedStringSlice(kcp.Spec.K0sConfigSpec.K0s.Object, "spec", "api", "sans")
			if err == nil && sansFound {
				sans = util.AddToExistingSans(existingSANs, sans)
			}
			err = unstructured.SetNestedStringSlice(kcp.Spec.K0sConfigSpec.K0s.Object, sans, "spec", "api", "sans")
			if err != nil {
				return fmt.Errorf("error setting sans: %v", err)
			}
		}

		if kcp.Spec.K0sConfigSpec.Tunneling.ServerAddress != "" {
			sans, _, err := unstructured.NestedStringSlice(kcp.Spec.K0sConfigSpec.K0s.Object, "spec", "api", "sans")
			if err != nil {
				return fmt.Errorf("error getting sans from config: %v", err)
			}
			sans = util.AddToExistingSans(sans, []string{kcp.Spec.K0sConfigSpec.Tunneling.ServerAddress})
			err = unstructured.SetNestedStringSlice(kcp.Spec.K0sConfigSpec.K0s.Object, sans, "spec", "api", "sans")
			if err != nil {
				return fmt.Errorf("error setting sans to the config: %v", err)
			}
		}

		// Reconcile the dynamic config
		dErr := kutil.ReconcileDynamicConfig(ctx, cluster, c.Client, *kcp.Spec.K0sConfigSpec.K0s.DeepCopy())
		if dErr != nil {
			// Don't return error from dynamic config reconciliation, as it may not be created yet
			log.Error(fmt.Errorf("failed to reconcile dynamic config, kubeconfig may not be available yet: %w", dErr), "Failed to reconcile dynamic config")
		}
	}

	return nil
}

func (c *K0sController) reconcileTunneling(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) error {
	if !kcp.Spec.K0sConfigSpec.Tunneling.Enabled {
		return nil
	}

	if kcp.Spec.K0sConfigSpec.Tunneling.ServerAddress == "" {
		ip, err := c.detectNodeIP(ctx, kcp)
		if err != nil {
			return fmt.Errorf("error detecting node IP: %w", err)
		}
		kcp.Spec.K0sConfigSpec.Tunneling.ServerAddress = ip
	}

	frpToken, err := c.createFRPToken(ctx, cluster, kcp)
	if err != nil {
		return fmt.Errorf("error creating FRP token secret: %w", err)
	}

	var frpsConfig string
	if kcp.Spec.K0sConfigSpec.Tunneling.Mode == "proxy" {
		frpsConfig = `
[common]
bind_port = 7000
tcpmux_httpconnect_port = 6443
authentication_method = token
token = ` + frpToken + `
`
	} else {
		frpsConfig = `
[common]
bind_port = 7000
authentication_method = token
token = ` + frpToken + `
`
	}

	frpsCMName := fmt.Sprintf(FRPConfigMapNameTemplate, kcp.GetName())
	cm := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      frpsCMName,
			Namespace: kcp.GetNamespace(),
		},
		Data: map[string]string{
			"frps.ini": frpsConfig,
		},
	}

	_ = ctrl.SetControllerReference(kcp, &cm, c.Client.Scheme())
	err = c.Client.Patch(ctx, &cm, client.Apply, &client.PatchOptions{FieldManager: "k0s-bootstrap"})
	if err != nil {
		return fmt.Errorf("error creating ConfigMap: %w", err)
	}

	frpsDeployment := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(FRPDeploymentNameTemplate, kcp.GetName()),
			Namespace: kcp.GetNamespace(),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"k0smotron_cluster": kcp.GetName(),
					"app":               "frps",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"k0smotron_cluster": kcp.GetName(),
						"app":               "frps",
					},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{{
						Name: frpsCMName,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: frpsCMName,
								},
								Items: []corev1.KeyToPath{{
									Key:  "frps.ini",
									Path: "frps.ini",
								}},
							},
						},
					}},
					Containers: []corev1.Container{{
						Name:            "frps",
						Image:           "snowdreamtech/frps:0.51.3",
						ImagePullPolicy: corev1.PullIfNotPresent,
						Ports: []corev1.ContainerPort{
							{
								Name:          "api",
								Protocol:      corev1.ProtocolTCP,
								ContainerPort: 7000,
							},
							{
								Name:          "tunnel",
								Protocol:      corev1.ProtocolTCP,
								ContainerPort: 6443,
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      frpsCMName,
							MountPath: "/etc/frp/frps.ini",
							SubPath:   "frps.ini",
						}},
					}},
				}},
		},
	}
	_ = ctrl.SetControllerReference(kcp, &frpsDeployment, c.Client.Scheme())
	err = c.Client.Patch(ctx, &frpsDeployment, client.Apply, &client.PatchOptions{FieldManager: "k0s-bootstrap"})
	if err != nil {
		return fmt.Errorf("error creating Deployment: %w", err)
	}

	frpsService := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(FRPServiceNameTemplate, kcp.GetName()),
			Namespace: kcp.GetNamespace(),
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"k0smotron_cluster": kcp.GetName(),
				"app":               "frps",
			},
			Ports: []corev1.ServicePort{{
				Name:       "api",
				Protocol:   corev1.ProtocolTCP,
				Port:       7000,
				TargetPort: intstr.FromInt(7000),
				NodePort:   kcp.Spec.K0sConfigSpec.Tunneling.ServerNodePort,
			}, {
				Name:       "tunnel",
				Protocol:   corev1.ProtocolTCP,
				Port:       6443,
				TargetPort: intstr.FromInt(6443),
				NodePort:   kcp.Spec.K0sConfigSpec.Tunneling.TunnelingNodePort,
			}},
			Type: corev1.ServiceTypeNodePort,
		},
	}
	_ = ctrl.SetControllerReference(kcp, &frpsService, c.Client.Scheme())
	err = c.Client.Patch(ctx, &frpsService, client.Apply, &client.PatchOptions{FieldManager: "k0s-bootstrap"})
	if err != nil {
		return fmt.Errorf("error creating Service: %w", err)
	}

	return nil
}

func (c *K0sController) reconcileDelete(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	allMachines, err := collections.GetFilteredMachinesForCluster(ctx, c, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get machines: %w", err)
	}

	cpMachines := allMachines.Filter(collections.ControlPlaneMachines(cluster.Name))

	if len(cpMachines) == 0 {
		// No machines left, we can finally delete the K0sControlPlane by removing the finalizer.
		controllerutil.RemoveFinalizer(kcp, cpv1beta1.K0sControlPlaneFinalizer)
		return ctrl.Result{}, nil
	}

	// Wait for removing worker machines first to avoid possible issues removing worker nodes without a controlplane running.
	if allMachines.Len() != cpMachines.Len() {
		logger.Info("Waiting for worker nodes to be deleted first")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	var errs []error
	for _, m := range cpMachines {
		if !m.DeletionTimestamp.IsZero() {
			// Machine is already being deleted.
			continue
		}

		if err := c.removePreTerminateHookAnnotationFromMachine(ctx, m); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove pre-terminate hook from control plane Machine '%s': %w", m.Name, err))
			continue
		}

		err := c.Delete(ctx, m)
		if err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("failed to delete control plane Machine '%s': %w", m.Name, err))
		}
	}

	// Requeue to wait for the machines and their dependencies to be deleted.
	return ctrl.Result{RequeueAfter: 10 * time.Second}, kerrors.NewAggregate(errs)
}

func (c *K0sController) removePreTerminateHookAnnotationFromMachine(ctx context.Context, machine *clusterv1.Machine) error {
	if _, exists := machine.Annotations[cpv1beta1.K0ControlPlanePreTerminateHookCleanupAnnotation]; !exists {
		// Nothing to do, the annotation is not set (anymore) on the Machine
		return nil
	}

	log := log.FromContext(ctx)
	log.Info("Removing pre-terminate hook from control plane Machine")

	machineOriginal := machine.DeepCopy()
	delete(machine.Annotations, cpv1beta1.K0ControlPlanePreTerminateHookCleanupAnnotation)
	if err := c.Client.Patch(ctx, machine, client.MergeFrom(machineOriginal)); err != nil {
		return fmt.Errorf("failed to remove pre-terminate hook from control plane Machine: %w", err)
	}

	return nil
}

func (c *K0sController) detectNodeIP(ctx context.Context, _ *cpv1beta1.K0sControlPlane) (string, error) {
	nodes, err := c.ClientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	return util.FindNodeAddress(nodes), nil
}

func (c *K0sController) createFRPToken(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) (string, error) {
	secretName := fmt.Sprintf(FRPTokenNameTemplate, cluster.Name)

	var existingSecret corev1.Secret
	err := c.SecretCachingClient.Get(ctx, client.ObjectKey{Name: secretName, Namespace: cluster.Namespace}, &existingSecret)
	if err == nil {
		return string(existingSecret.Data["value"]), nil
	} else if !apierrors.IsNotFound(err) {
		return "", err
	}

	frpToken := uuid.New().String()
	frpSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
		},
		Data: map[string][]byte{
			"value": []byte(frpToken),
		},
		Type: clusterv1.ClusterSecretType,
	}

	_ = ctrl.SetControllerReference(kcp, frpSecret, c.Client.Scheme())

	return frpToken, c.Client.Patch(ctx, frpSecret, client.Apply, &client.PatchOptions{
		FieldManager: "k0smotron",
	})
}

// SetupWithManager sets up the controller with the Manager.
func (c *K0sController) SetupWithManager(mgr ctrl.Manager) error {
	// Check if the cluster.x-k8s.io API is available and if not, don't try to watch for Machine objects
	return ctrl.NewControllerManagedBy(mgr).
		For(&cpv1beta1.K0sControlPlane{}).
		Owns(&clusterv1.Machine{}).
		Complete(c)
}
