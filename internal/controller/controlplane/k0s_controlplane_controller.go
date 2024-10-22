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
	"strings"
	"time"

	"github.com/google/uuid"
	autopilot "github.com/k0sproject/k0s/pkg/apis/autopilot/v1beta2"
	"github.com/k0sproject/k0smotron/internal/controller/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	kubeadmbootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
)

const (
	defaultK0sSuffix  = "k0s.0"
	defaultK0sVersion = "v1.27.9+k0s.0"
)

var ErrNewMachinesNotReady = fmt.Errorf("waiting for new machines")

type K0sController struct {
	client.Client
	Scheme     *runtime.Scheme
	ClientSet  *kubernetes.Clientset
	RESTConfig *rest.Config
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

	if !kcp.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("K0sControlPlane is being deleted, no action needed")
		return ctrl.Result{}, nil
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

	// Always patch the object to update the status
	defer func() {
		log.Info("Updating status")
		// Separate var for status update errors to avoid shadowing err
		derr := c.updateStatus(ctx, kcp, cluster)
		if derr != nil {
			log.Error(derr, "Failed to update status")
			return
		}

		// // Patch the status with server-side apply
		// kcp.ObjectMeta.ManagedFields = nil // Remove managed fields when doing server-side apply
		// derr = c.Status().Patch(ctx, kcp, client.Apply, client.FieldOwner(fieldOwner))
		derr = c.Status().Patch(ctx, kcp, client.Merge)
		if derr != nil {
			log.Error(derr, "Failed to patch status")
			res = ctrl.Result{}
			err = derr
			return
		}
		log.Info("Status updated successfully")

		// Requeue the reconciliation if the status is not ready
		if !kcp.Status.Ready {
			log.Info("Requeuing reconciliation in 20sec since the control plane is not ready")
			res = ctrl.Result{RequeueAfter: 20 * time.Second, Requeue: true}
		}
	}()

	log = log.WithValues("cluster", cluster.Name)

	if annotations.IsPaused(cluster, kcp) {
		log.Info("Reconciliation is paused for this object or owning cluster")
		return ctrl.Result{}, nil
	}

	if err := c.ensureCertificates(ctx, cluster, kcp); err != nil {
		log.Error(err, "Failed to ensure certificates")
		return ctrl.Result{}, err
	}

	if err := c.reconcileTunneling(ctx, cluster, kcp); err != nil {
		log.Error(err, "Failed to reconcile tunneling")
		return ctrl.Result{}, err
	}

	_, err = c.reconcile(ctx, cluster, kcp)
	if err != nil {
		if errors.Is(err, ErrNewMachinesNotReady) {
			return ctrl.Result{RequeueAfter: 10, Requeue: true}, nil
		}
		return res, err
	}

	return res, err

}

func (c *K0sController) reconcileKubeconfig(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) error {
	if cluster.Spec.ControlPlaneEndpoint.IsZero() {
		return errors.New("control plane endpoint is not set")
	}

	secretName := secret.Name(cluster.Name, secret.Kubeconfig)
	err := c.Client.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: secretName}, &corev1.Secret{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return kubeconfig.CreateSecret(ctx, c.Client, cluster)
		}
		return err
	}

	if kcp.Spec.K0sConfigSpec.Tunneling.Enabled {
		if kcp.Spec.K0sConfigSpec.Tunneling.Mode == "proxy" {
			secretName := secret.Name(cluster.Name+"-proxied", secret.Kubeconfig)
			err := c.Client.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: secretName}, &corev1.Secret{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					kc, err := c.generateKubeconfig(ctx, cluster, fmt.Sprintf("https://%s", cluster.Spec.ControlPlaneEndpoint.String()))
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
		} else {
			secretName := secret.Name(cluster.Name+"-tunneled", secret.Kubeconfig)
			err := c.Client.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: secretName}, &corev1.Secret{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					kc, err := c.generateKubeconfig(ctx, cluster, fmt.Sprintf("https://%s:%d", kcp.Spec.K0sConfigSpec.Tunneling.ServerAddress, kcp.Spec.K0sConfigSpec.Tunneling.TunnelingNodePort))
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
		}
	}

	return nil
}

func (c *K0sController) reconcile(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) (int32, error) {
	var err error
	kcp.Spec.K0sConfigSpec.K0s, err = enrichK0sConfigWithClusterData(cluster, kcp.Spec.K0sConfigSpec.K0s)
	if err != nil {
		return kcp.Status.Replicas, err
	}

	replicasToReport, err := c.reconcileMachines(ctx, cluster, kcp)
	if err != nil {
		return replicasToReport, err
	}

	err = c.reconcileKubeconfig(ctx, cluster, kcp)
	if err != nil {
		return replicasToReport, fmt.Errorf("error reconciling kubeconfig secret: %w", err)
	}

	return replicasToReport, nil
}

func (c *K0sController) reconcileMachines(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) (int32, error) {

	logger := log.FromContext(ctx, "cluster", cluster.Name, "kcp", kcp.Name)

	replicasToReport := kcp.Spec.Replicas

	machines, err := collections.GetFilteredMachinesForCluster(ctx, c, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
	if err != nil {
		return replicasToReport, fmt.Errorf("error collecting machines: %w", err)
	}
	if machines == nil {
		return replicasToReport, fmt.Errorf("machines collection is nil")
	}
	logger.Info("Collected machines", "count", machines.Len())

	currentReplicas := machines.Len()
	desiredReplicas := kcp.Spec.Replicas
	machinesToDelete := 0
	if currentReplicas > int(desiredReplicas) {
		machinesToDelete = currentReplicas - int(desiredReplicas)
		replicasToReport = kcp.Status.Replicas
	}

	currentVersion, err := minVersion(machines)
	if err != nil {
		return replicasToReport, fmt.Errorf("error getting current cluster version from machines: %w", err)
	}
	log.Log.Info("Got current cluster version", "version", currentVersion)

	var clusterIsUpdating bool
	var oldMachines int
	for _, m := range machines {
		if m.Spec.Version == nil || !versionMatches(m, kcp.Spec.Version) {
			oldMachines++
		}
	}

	if oldMachines > 0 {
		log.Log.Info("Cluster is updating", "currentVersion", currentVersion, "newVersion", kcp.Spec.Version, "strategy", kcp.Spec.UpdateStrategy)
		clusterIsUpdating = true
		if kcp.Spec.UpdateStrategy == cpv1beta1.UpdateRecreate {

			// If the cluster is running in single mode, we can't use the Recreate strategy
			if kcp.Spec.K0sConfigSpec.Args != nil {
				for _, arg := range kcp.Spec.K0sConfigSpec.Args {
					if arg == "--single" {
						return replicasToReport, fmt.Errorf("UpdateRecreate strategy is not allowed when the cluster is running in single mode")
					}
				}
			}

			desiredReplicas += kcp.Spec.Replicas
			machinesToDelete = int(kcp.Spec.Replicas)
			replicasToReport = desiredReplicas
			log.Log.Info("Calculated new replicas", "desiredReplicas", desiredReplicas, "machinesToDelete", machinesToDelete, "replicasToReport", replicasToReport, "currentReplicas", currentReplicas)
		} else {
			kubeClient, err := c.getKubeClient(ctx, cluster)
			if err != nil {
				return replicasToReport, fmt.Errorf("error getting cluster client set for machine update: %w", err)
			}

			err = c.createAutopilotPlan(ctx, kcp, cluster, kubeClient)
			if err != nil {
				return replicasToReport, fmt.Errorf("error creating autopilot plan: %w", err)
			}
		}
	}

	machineNames := make(map[string]bool)
	for _, m := range machines.Names() {
		machineNames[m] = true
	}

	if len(machineNames) < int(desiredReplicas) {
		for i := len(machineNames); i < int(desiredReplicas); i++ {
			name := machineName(kcp.Name, i)
			machineNames[name] = false
			if len(machineNames) == int(desiredReplicas) {
				break
			}
		}
	}

	for name, exists := range machineNames {
		if !exists || kcp.Spec.UpdateStrategy == cpv1beta1.UpdateInPlace {
			// Wait for the previous machine to be created to avoid etcd issues
			if clusterIsUpdating {
				err := c.checkMachineIsReady(ctx, machines.Newest().Name, cluster)
				if err != nil {
					return int32(machines.Len()), err
				}
			}

			machineFromTemplate, err := c.createMachineFromTemplate(ctx, name, cluster, kcp)
			if err != nil {
				return replicasToReport, fmt.Errorf("error creating machine from template: %w", err)
			}

			infraRef := corev1.ObjectReference{
				APIVersion: machineFromTemplate.GetAPIVersion(),
				Kind:       machineFromTemplate.GetKind(),
				Name:       machineFromTemplate.GetName(),
				Namespace:  kcp.Namespace,
			}

			machine, err := c.createMachine(ctx, name, cluster, kcp, infraRef)
			if err != nil {
				return replicasToReport, fmt.Errorf("error creating machine: %w", err)
			}
			machines[machine.Name] = machine
		}

		err = c.createBootstrapConfig(ctx, name, cluster, kcp, machines[name])
		if err != nil {
			return replicasToReport, fmt.Errorf("error creating bootstrap config: %w", err)
		}
	}

	for _, m := range machines {
		if m.Spec.Version != nil && *m.Spec.Version != kcp.Spec.Version {
			logger.Info("Machine version is different from K0sControlPlane version", "machine", m.Name, "machineVersion", *m.Spec.Version, "kcpVersion", kcp.Spec.Version)
			continue
		}

		if machinesToDelete > 0 {
			err := c.checkMachineIsReady(ctx, m.Name, cluster)
			if err != nil {
				return int32(machines.Len()), err
			}
		}
	}

	if machinesToDelete > 0 {
		logger.Info("Found machines to delete", "count", machinesToDelete)
		kubeClient, err := c.getKubeClient(ctx, cluster)
		if err != nil {
			return replicasToReport, fmt.Errorf("error getting cluster client set for deletion: %w", err)
		}

		// Remove the last machine and report the new number of replicas to status
		// On the next reconcile, the next machine will be removed
		// Wait for the previous machine to be deleted to avoid etcd issues
		machine := machines.Oldest()
		logger.Info("Found oldest machine to delete", "machine", machine.Name)
		if machine.Status.Phase == string(clusterv1.MachinePhaseDeleting) {
			logger.Info("Machine is being deleted, waiting for it to be deleted", "machine", machine.Name)
			return kcp.Status.Replicas, fmt.Errorf("waiting for previous machine to be deleted")
		}

		replicasToReport--
		name := machine.Name
		if err := c.markChildControlNodeToLeave(ctx, name, kubeClient); err != nil {
			return replicasToReport, fmt.Errorf("error marking controlnode to leave: %w", err)
		}

		if err := c.deleteBootstrapConfig(ctx, name, kcp); err != nil {
			return replicasToReport, fmt.Errorf("error deleting machine from template: %w", err)
		}

		if err := c.deleteMachineFromTemplate(ctx, name, cluster, kcp); err != nil {
			return replicasToReport, fmt.Errorf("error deleting machine from template: %w", err)
		}

		if err := c.deleteMachine(ctx, name, kcp); err != nil {
			return replicasToReport, fmt.Errorf("error deleting machine from template: %w", err)
		}

		logger.Info("Deleted machine", "machine", name, "replicasToReport", replicasToReport)
		return replicasToReport, nil
	}

	return replicasToReport, nil
}

func (c *K0sController) createBootstrapConfig(ctx context.Context, name string, _ *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane, machine *clusterv1.Machine) error {
	controllerConfig := bootstrapv1.K0sControllerConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "bootstrap.cluster.x-k8s.io/v1beta1",
			Kind:       "K0sControllerConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: kcp.Namespace,
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
			K0sConfigSpec: &kcp.Spec.K0sConfigSpec,
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

func (c *K0sController) deleteBootstrapConfig(ctx context.Context, name string, kcp *cpv1beta1.K0sControlPlane) error {
	controllerConfig := bootstrapv1.K0sControllerConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "bootstrap.cluster.x-k8s.io/v1beta1",
			Kind:       "K0sControllerConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: kcp.Namespace,
		},
	}

	err := c.Client.Delete(ctx, &controllerConfig)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("error deleting K0sControllerConfig: %w", err)
	}
	return nil
}

func (c *K0sController) ensureCertificates(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) error {
	certificates := secret.NewCertificatesForInitialControlPlane(&kubeadmbootstrapv1.ClusterConfiguration{
		CertificatesDir: "/var/lib/k0s/pki",
	})
	return certificates.LookupOrGenerate(ctx, c.Client, capiutil.ObjectKey(cluster), *metav1.NewControllerRef(kcp, cpv1beta1.GroupVersion.WithKind("K0sControlPlane")))
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

	frpsCMName := kcp.GetName() + "-frps-config"
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

	_ = ctrl.SetControllerReference(kcp, &cm, c.Scheme)
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
			Name:      kcp.GetName() + "-frps",
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
	_ = ctrl.SetControllerReference(kcp, &frpsDeployment, c.Scheme)
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
			Name:      kcp.GetName() + "-frps",
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
	_ = ctrl.SetControllerReference(kcp, &frpsService, c.Scheme)
	err = c.Client.Patch(ctx, &frpsService, client.Apply, &client.PatchOptions{FieldManager: "k0s-bootstrap"})
	if err != nil {
		return fmt.Errorf("error creating Service: %w", err)
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
	secretName := cluster.Name + "-frp-token"

	var existingSecret corev1.Secret
	err := c.Client.Get(ctx, client.ObjectKey{Name: secretName, Namespace: cluster.Namespace}, &existingSecret)
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

	_ = ctrl.SetControllerReference(kcp, frpSecret, c.Scheme)

	return frpToken, c.Client.Patch(ctx, frpSecret, client.Apply, &client.PatchOptions{
		FieldManager: "k0smotron",
	})
}

func machineName(base string, i int) string {
	return fmt.Sprintf("%s-%d", base, i)
}

// SetupWithManager sets up the controller with the Manager.
func (c *K0sController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cpv1beta1.K0sControlPlane{}).
		Owns(&clusterv1.Machine{}).
		Complete(c)
}
