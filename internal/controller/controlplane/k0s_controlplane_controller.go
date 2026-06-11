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

	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/google/uuid"
	autopilot "github.com/k0sproject/k0s/pkg/apis/autopilot/v1beta2"
	"github.com/k0sproject/k0smotron/internal/controller/util"
	"github.com/k0sproject/version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	kubeadmbootstrapv1 "sigs.k8s.io/cluster-api/api/bootstrap/kubeadm/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/controllers/clustercache"
	"sigs.k8s.io/cluster-api/controllers/external"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/certs"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bootstrapv2 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	cpv1beta2 "github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	kutil "github.com/k0sproject/k0smotron/internal/util"
)

const (
	defaultK0sSuffix  = "k0s.0"
	defaultK0sVersion = "v1.27.9+k0s.0"
)

var (
	// ErrNewMachinesNotReady is used to indicate that the new machines are not ready yet.
	ErrNewMachinesNotReady = fmt.Errorf("waiting for new machines: %w", util.ErrNotReady)
	// FRPTokenNameTemplate is the template for the name of the secret that contains the FRP token.
	FRPTokenNameTemplate = "%s-frp-token"
	// FRPConfigMapNameTemplate is the template for the name of the ConfigMap that contains the FRP configuration.
	FRPConfigMapNameTemplate = "%s-frps-config"
	// FRPDeploymentNameTemplate is the template for the name of the Deployment that runs the FRP server.
	FRPDeploymentNameTemplate = "%s-frps"
	// FRPServiceNameTemplate is the template for the name of the Service that exposes the FRP server.
	FRPServiceNameTemplate = "%s-frps"
	minVersionForETCDName  = version.MustParse("v1.31.1")
)

type machineState struct {
	isVersionUpToDate   bool
	isInfraUpToDate     bool
	isBootstrapUpToDate bool
	controllerConfig    *bootstrapv2.K0sControllerConfig
	infraMachine        *unstructured.Unstructured
}

type controlplane struct {
	cluster                   *clusterv1.Cluster
	kcp                       *cpv1beta2.K0sControlPlane
	activeMachines            collections.Machines
	deletedMachines           collections.Machines
	upToDateMachines          collections.Machines
	notUpToDateMachines       collections.Machines
	controllerConfigs         map[string]*bootstrapv2.K0sControllerConfig
	infraMachines             map[string]*unstructured.Unstructured
	hasMachinesWithOldVersion bool
}

// K0sController is responsible for reconciling K0sControlPlane objects.
type K0sController struct {
	client.Client
	SecretCachingClient client.Client
	ClusterCache        clustercache.ClusterCache
	ClientSet           *kubernetes.Clientset
	RESTConfig          *rest.Config
	// workloadClusterKubeClient is used during testing to inject a fake client
	workloadClusterKubeClient *kubernetes.Clientset
}

// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=k0scontrolplanes/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=k0scontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch;update;patch;create;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0scontrollerconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=k0scontrollerconfigs/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

// Reconcile reconciles a K0sControlPlane object.
func (c *K0sController) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := log.FromContext(ctx).WithValues("controlplane", req.NamespacedName)
	kcp := &cpv1beta2.K0sControlPlane{}

	if err := c.Get(ctx, req.NamespacedName, kcp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get K0sControlPlane")
		return ctrl.Result{}, err
	}

	if finalizerAdded, err := util.EnsureFinalizer(ctx, c.Client, kcp, cpv1beta2.K0sControlPlaneFinalizer); err != nil || finalizerAdded {
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

	controlplane, err := c.retrieveControlPlaneState(ctx, cluster, kcp)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting machines state: %w", err)
	}

	// Always patch the object to update the status
	defer func() {
		log.Info("Updating status")

		// When controlplane is being deleted, we don't update the status to avoid requests workload API
		// because it is terminating so machines probably are terminating too.
		// TODO: maybe updateStatus method should be refactored to at least report unavailable machines,
		// which not requires to call workload API.
		var derr error
		if kcp.DeletionTimestamp.IsZero() {
			// Separate var for status update errors to avoid shadowing err
			derr = c.updateStatus(ctx, controlplane)
			if derr != nil {
				log.Error(derr, "Failed to calculate status")
			}
		}

		derr = kcpPatchHelper.Patch(ctx, controlplane.kcp)
		if derr != nil {
			log.Error(derr, "Failed to patch status")
			res = ctrl.Result{}
			err = derr
			return
		}
		log.Info("Status updated successfully")

		if ptr.Deref(controlplane.kcp.Status.Initialization.ControlPlaneInitialized, false) {
			if perr := clusterPatchHelper.Patch(ctx, controlplane.cluster); perr != nil {
				err = fmt.Errorf("failed to patch cluster: %w", perr)
			}
		}

		if needsRequeue(controlplane.kcp) {
			if res.IsZero() {
				res = ctrl.Result{RequeueAfter: 20 * time.Second, Requeue: true}
			}
		}
	}()

	if !controlplane.kcp.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("Reconcile K0sControlPlane deletion")
		return c.reconcileDelete(ctx, controlplane)
	}

	log = log.WithValues("cluster", cluster.Name)

	if err := c.ensureCertificates(ctx, controlplane); err != nil {
		log.Error(err, "Failed to ensure certificates")
		return ctrl.Result{}, err
	}

	if err := c.reconcileTunneling(ctx, controlplane); err != nil {
		log.Error(err, "Failed to reconcile tunneling")
		return ctrl.Result{}, err
	}

	if err := c.reconcileConfig(ctx, controlplane); err != nil {
		log.Error(err, "Failed to reconcile config")
		return ctrl.Result{}, err
	}

	err = c.reconcileKubeconfig(ctx, controlplane)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error reconciling kubeconfig secret: %w", err)
	}

	return c.reconcileMachines(ctx, controlplane)
}

func (c *K0sController) reconcileKubeconfig(ctx context.Context, controlplane *controlplane) error {
	logger := log.FromContext(ctx, "cluster", controlplane.cluster.Name, "kcp", controlplane.kcp.Name)

	if controlplane.cluster.Spec.ControlPlaneEndpoint.IsZero() {
		return fmt.Errorf("control plane endpoint is not set: %w", util.ErrNotReady)
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
				if err := c.regenerateKubeconfigSecret(ctx, kc, controlplane.cluster.Name); err != nil {
					logger.Error(err, "Failed to regenerate kubeconfig")
					return
				}
			}
		}
	}()

	clusterKey := client.ObjectKey{
		Name:      controlplane.cluster.GetName(),
		Namespace: controlplane.cluster.GetNamespace(),
	}

	workloadClusterKubeconfigSecret, err := secret.GetFromNamespacedName(ctx, c.SecretCachingClient, capiutil.ObjectKey(controlplane.cluster), secret.Kubeconfig)
	if err != nil {
		if apierrors.IsNotFound(err) {
			kc, err := c.generateKubeconfig(ctx, clusterKey, fmt.Sprintf("https://%s", controlplane.cluster.Spec.ControlPlaneEndpoint.String()))
			if err != nil {
				return err
			}

			workloadClusterKubeconfigSecret, err = c.createKubeconfigSecret(ctx, kc, controlplane.cluster, secret.Name(controlplane.cluster.Name, secret.Kubeconfig), controlplane.kcp.Spec.KubeconfigSecretMetadata)
		} else {
			return err
		}
	}
	kubeconfigSecrets = append(kubeconfigSecrets, workloadClusterKubeconfigSecret)

	if controlplane.kcp.Spec.K0sConfigSpec.Tunneling.Enabled {

		if controlplane.kcp.Spec.K0sConfigSpec.Tunneling.Mode == "proxy" {

			secretName := secret.Name(controlplane.cluster.Name+"-proxied", secret.Kubeconfig)

			proxiedKubeconfig := &corev1.Secret{}
			err := c.SecretCachingClient.Get(ctx, client.ObjectKey{Namespace: controlplane.cluster.Namespace, Name: secretName}, proxiedKubeconfig)
			if err != nil {
				if apierrors.IsNotFound(err) {
					kc, err := c.generateKubeconfig(ctx, clusterKey, fmt.Sprintf("https://%s", controlplane.cluster.Spec.ControlPlaneEndpoint.String()))
					if err != nil {
						return err
					}

					for cn := range kc.Clusters {
						kc.Clusters[cn].ProxyURL = fmt.Sprintf("http://%s:%d", controlplane.kcp.Spec.K0sConfigSpec.Tunneling.ServerAddress, controlplane.kcp.Spec.K0sConfigSpec.Tunneling.TunnelingNodePort)
					}

					proxiedKubeconfig, err = c.createKubeconfigSecret(ctx, kc, controlplane.cluster, secretName, controlplane.kcp.Spec.KubeconfigSecretMetadata)
					if err != nil {
						return err
					}
				} else {
					return err
				}
			}
			kubeconfigSecrets = append(kubeconfigSecrets, proxiedKubeconfig)

		} else {
			secretName := secret.Name(controlplane.cluster.Name+"-tunneled", secret.Kubeconfig)

			tunneledKubeconfig := &corev1.Secret{}
			err := c.SecretCachingClient.Get(ctx, client.ObjectKey{Namespace: controlplane.cluster.Namespace, Name: secretName}, tunneledKubeconfig)
			if err != nil {
				if apierrors.IsNotFound(err) {
					kc, err := c.generateKubeconfig(ctx, clusterKey, fmt.Sprintf("https://%s:%d", controlplane.kcp.Spec.K0sConfigSpec.Tunneling.ServerAddress, controlplane.kcp.Spec.K0sConfigSpec.Tunneling.TunnelingNodePort))
					if err != nil {
						return err
					}

					tunneledKubeconfig, err = c.createKubeconfigSecret(ctx, kc, controlplane.cluster, secretName, controlplane.kcp.Spec.KubeconfigSecretMetadata)
					if err != nil {
						return err
					}
				} else {
					return err
				}
			}
			kubeconfigSecrets = append(kubeconfigSecrets, tunneledKubeconfig)
		}
	}

	return nil
}

func (c *K0sController) createBootstrapConfig(ctx context.Context, name string, k0sConfigSpec *bootstrapv2.K0sConfigSpec, kcp *cpv1beta2.K0sControlPlane, clusterName string) error {

	controllerConfig := bootstrapv2.K0sControllerConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "bootstrap.cluster.x-k8s.io/v1beta2",
			Kind:       "K0sControllerConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   kcp.Namespace,
			Labels:      controlPlaneCommonLabelsForCluster(kcp, clusterName),
			Annotations: kcp.Spec.MachineTemplate.ObjectMeta.Annotations,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: cpv1beta2.GroupVersion.String(),
				Kind:       "K0sControlPlane",
				Name:       kcp.GetName(),
				UID:        kcp.GetUID(),
			}},
		},
		Spec: bootstrapv2.K0sControllerConfigSpec{
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
	kubeClient, err := c.getWorkloadClusterClientset(ctx, cluster)
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

func (c *K0sController) ensureCertificates(ctx context.Context, controlplane *controlplane) error {
	certificates := secret.NewCertificatesForInitialControlPlane(&kubeadmbootstrapv1.ClusterConfiguration{
		CertificatesDir: "/var/lib/k0s/pki",
	})
	return certificates.LookupOrGenerateCached(ctx, c.SecretCachingClient, c.Client, capiutil.ObjectKey(controlplane.cluster), *metav1.NewControllerRef(controlplane.kcp, cpv1beta2.GroupVersion.WithKind("K0sControlPlane")))
}

func (c *K0sController) reconcileConfig(ctx context.Context, controlplane *controlplane) error {
	logger := log.FromContext(ctx)

	if controlplane.kcp.Spec.K0sConfigSpec.K0s != nil {
		nllbEnabled, found, err := unstructured.NestedBool(controlplane.kcp.Spec.K0sConfigSpec.K0s.Object, "spec", "network", "nodeLocalLoadBalancing", "enabled")
		if err != nil {
			return fmt.Errorf("error getting nodeLocalLoadBalancing: %v", err)
		}
		// Set the external address if NLLB is not enabled
		// Otherwise, just add the external address to the SANs to allow the clients to connect using LB address
		if !(found && nllbEnabled) {
			err = unstructured.SetNestedField(controlplane.kcp.Spec.K0sConfigSpec.K0s.Object, controlplane.cluster.Spec.ControlPlaneEndpoint.Host, "spec", "api", "externalAddress")
			if err != nil {
				return fmt.Errorf("error setting control plane endpoint: %v", err)
			}
		} else if controlplane.cluster.Spec.ControlPlaneEndpoint.Host != "" {
			sans := []string{controlplane.cluster.Spec.ControlPlaneEndpoint.Host}
			existingSANs, sansFound, err := unstructured.NestedStringSlice(controlplane.kcp.Spec.K0sConfigSpec.K0s.Object, "spec", "api", "sans")
			if err == nil && sansFound {
				sans = util.AddToExistingSans(existingSANs, sans)
			}
			err = unstructured.SetNestedStringSlice(controlplane.kcp.Spec.K0sConfigSpec.K0s.Object, sans, "spec", "api", "sans")
			if err != nil {
				return fmt.Errorf("error setting sans: %v", err)
			}
		}

		if controlplane.kcp.Spec.K0sConfigSpec.Tunneling.ServerAddress != "" {
			sans, _, err := unstructured.NestedStringSlice(controlplane.kcp.Spec.K0sConfigSpec.K0s.Object, "spec", "api", "sans")
			if err != nil {
				return fmt.Errorf("error getting sans from config: %v", err)
			}
			sans = util.AddToExistingSans(sans, []string{controlplane.kcp.Spec.K0sConfigSpec.Tunneling.ServerAddress})
			err = unstructured.SetNestedStringSlice(controlplane.kcp.Spec.K0sConfigSpec.K0s.Object, sans, "spec", "api", "sans")
			if err != nil {
				return fmt.Errorf("error setting sans to the config: %v", err)
			}
		}

		controlplane.kcp.Spec.K0sConfigSpec.K0s, err = enrichK0sConfigWithClusterData(controlplane.cluster, controlplane.kcp.Spec.K0sConfigSpec.K0s)
		if err != nil {
			return err
		}

		workloadClient, err := util.GetControllerRuntimeClient(ctx, c.Client, c.ClusterCache, controlplane.kcp, client.ObjectKeyFromObject(controlplane.cluster))
		if err != nil {
			if errors.Is(err, util.ErrNotReady) {
				return nil
			}

			return fmt.Errorf("error getting workload cluster client: %w", err)
		}

		err = kutil.ReconcileDynamicConfig(ctx, workloadClient, *controlplane.kcp.Spec.K0sConfigSpec.K0s.DeepCopy())
		if err != nil {
			logger.Error(err, "Failed to reconcile dynamic config, will retry")
		}
	}

	return nil
}

func (c *K0sController) reconcileTunneling(ctx context.Context, controlplane *controlplane) error {
	if !controlplane.kcp.Spec.K0sConfigSpec.Tunneling.Enabled {
		return nil
	}

	if controlplane.kcp.Spec.K0sConfigSpec.Tunneling.ServerAddress == "" {
		ip, err := util.FindNodeAddress(ctx, c.Client)
		if err != nil {
			return fmt.Errorf("error detecting node IP: %w", err)
		}
		controlplane.kcp.Spec.K0sConfigSpec.Tunneling.ServerAddress = ip
	}

	frpToken, err := c.createFRPToken(ctx, controlplane.cluster, controlplane.kcp)
	if err != nil {
		return fmt.Errorf("error creating FRP token secret: %w", err)
	}

	var frpsConfig string
	if controlplane.kcp.Spec.K0sConfigSpec.Tunneling.Mode == "proxy" {
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

	frpsCMName := fmt.Sprintf(FRPConfigMapNameTemplate, controlplane.kcp.GetName())
	cm := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      frpsCMName,
			Namespace: controlplane.kcp.GetNamespace(),
			Labels: map[string]string{
				util.ComponentLabel: util.ComponentTunneling,
			},
		},
		Data: map[string]string{
			"frps.ini": frpsConfig,
		},
	}

	_ = ctrl.SetControllerReference(controlplane.kcp, &cm, c.Client.Scheme())
	err = c.Client.Patch(ctx, &cm, client.Apply, &client.PatchOptions{FieldManager: "k0s-bootstrap"})
	if err != nil {
		return fmt.Errorf("error creating ConfigMap: %w", err)
	}

	// Deployment selector is immutable after creation. Add app.kubernetes.io/component only to metadata and template labels.
	frpsSelectorLabels := map[string]string{
		"k0smotron_cluster": controlplane.kcp.GetName(),
		"app":               "frps",
	}
	frpsDeployment := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(FRPDeploymentNameTemplate, controlplane.kcp.GetName()),
			Namespace: controlplane.kcp.GetNamespace(),
			Labels: map[string]string{
				"k0smotron_cluster": controlplane.kcp.GetName(),
				"app":               "frps",
				util.ComponentLabel: util.ComponentTunneling,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: frpsSelectorLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"k0smotron_cluster": controlplane.kcp.GetName(),
						"app":               "frps",
						util.ComponentLabel: util.ComponentTunneling,
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
	_ = ctrl.SetControllerReference(controlplane.kcp, &frpsDeployment, c.Client.Scheme())
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
			Name:      fmt.Sprintf(FRPServiceNameTemplate, controlplane.kcp.GetName()),
			Namespace: controlplane.kcp.GetNamespace(),
			Labels: map[string]string{
				"k0smotron_cluster": controlplane.kcp.GetName(),
				"app":               "frps",
				util.ComponentLabel: util.ComponentTunneling,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: frpsSelectorLabels,
			Ports: []corev1.ServicePort{{
				Name:       "api",
				Protocol:   corev1.ProtocolTCP,
				Port:       7000,
				TargetPort: intstr.FromInt(7000),
				NodePort:   controlplane.kcp.Spec.K0sConfigSpec.Tunneling.ServerNodePort,
			}, {
				Name:       "tunnel",
				Protocol:   corev1.ProtocolTCP,
				Port:       6443,
				TargetPort: intstr.FromInt(6443),
				NodePort:   controlplane.kcp.Spec.K0sConfigSpec.Tunneling.TunnelingNodePort,
			}},
			Type: corev1.ServiceTypeNodePort,
		},
	}
	_ = ctrl.SetControllerReference(controlplane.kcp, &frpsService, c.Client.Scheme())
	err = c.Client.Patch(ctx, &frpsService, client.Apply, &client.PatchOptions{FieldManager: "k0s-bootstrap"})
	if err != nil {
		return fmt.Errorf("error creating Service: %w", err)
	}

	return nil
}

func (c *K0sController) reconcileDelete(ctx context.Context, controlplane *controlplane) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	allMachines, err := collections.GetFilteredMachinesForCluster(ctx, c, controlplane.cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get machines: %w", err)
	}

	cpMachines := allMachines.Filter(collections.ControlPlaneMachines(controlplane.cluster.Name))

	if len(cpMachines) == 0 {
		// No machines left, we can finally delete the K0sControlPlane by removing the finalizer.
		controllerutil.RemoveFinalizer(controlplane.kcp, cpv1beta2.K0sControlPlaneFinalizer)
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

		err := c.Delete(ctx, m)
		if err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("failed to delete control plane Machine '%s': %w", m.Name, err))
			continue
		}

		if err := c.removePreTerminateHookAnnotationFromMachine(ctx, m); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove pre-terminate hook from control plane Machine '%s': %w", m.Name, err))
		}
	}

	// Requeue to wait for the machines and their dependencies to be deleted.
	return ctrl.Result{RequeueAfter: 10 * time.Second}, kerrors.NewAggregate(errs)
}

func (c *K0sController) createFRPToken(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta2.K0sControlPlane) (string, error) {
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

func (c *K0sController) retrieveControlPlaneState(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta2.K0sControlPlane) (*controlplane, error) {
	machines, err := collections.GetFilteredMachinesForCluster(ctx, c, cluster, collections.ControlPlaneMachines(cluster.Name))
	if err != nil {
		return nil, fmt.Errorf("failed to filter machines for control plane: %w", err)
	}

	if machines == nil {
		return nil, fmt.Errorf("machines collection is nil")
	}

	deletedMachines := machines.Filter(collections.HasDeletionTimestamp)
	activeMachines := machines.Filter(collections.ActiveMachines)

	var (
		upToDateMachines          = collections.New()
		controllerConfigs         = make(map[string]*bootstrapv2.K0sControllerConfig)
		infraMachines             = make(map[string]*unstructured.Unstructured)
		hasMachinesWithOldVersion bool
	)
	for _, machine := range activeMachines {
		machineState, err := c.calculateMachineState(ctx, kcp, machine)
		if err != nil {
			return nil, fmt.Errorf("error calculating machine state for machine %s: %w", machine.Name, err)
		}
		if !machineState.isVersionUpToDate {
			hasMachinesWithOldVersion = true
		}

		if machineState.isVersionUpToDate && machineState.isInfraUpToDate && machineState.isBootstrapUpToDate {
			upToDateMachines.Insert(machine)
		}

		if machineState.controllerConfig != nil {
			controllerConfigs[machine.Name] = machineState.controllerConfig
		}

		if machineState.infraMachine != nil {
			infraMachines[machine.Name] = machineState.infraMachine
		}
	}

	scope := &controlplane{
		cluster:                   cluster,
		kcp:                       kcp,
		deletedMachines:           deletedMachines,
		activeMachines:            activeMachines,
		upToDateMachines:          upToDateMachines,
		notUpToDateMachines:       activeMachines.Difference(upToDateMachines),
		hasMachinesWithOldVersion: hasMachinesWithOldVersion,
		controllerConfigs:         controllerConfigs,
		infraMachines:             infraMachines,
	}

	return scope, nil
}

func (c *K0sController) calculateMachineState(ctx context.Context, kcp *cpv1beta2.K0sControlPlane, m *clusterv1.Machine) (machineState, error) {
	logger := log.FromContext(ctx, "machine", m.Name)

	uInfraMachine, err := external.GetObjectFromContractVersionedRef(ctx, c.Client, m.Spec.InfrastructureRef, m.Namespace)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return machineState{}, fmt.Errorf("failed to retrieve infra machine for machine object %s: %w", m.Name, err)
		}
		logger.Info("Infrastructure machine not found")
	}

	uBootstrapConfig, err := external.GetObjectFromContractVersionedRef(ctx, c.Client, m.Spec.Bootstrap.ConfigRef, m.Namespace)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return machineState{}, fmt.Errorf("failed to retrieve controller config for machine object %s: %w", m.Name, err)
		}
		logger.Info("Bootstrap config not found")
	}
	var bootstrapConfig *bootstrapv2.K0sControllerConfig
	if uBootstrapConfig != nil {
		bootstrapConfig = &bootstrapv2.K0sControllerConfig{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(uBootstrapConfig.Object, bootstrapConfig)
		if err != nil {
			return machineState{}, fmt.Errorf("failed to convert bootstrap config for machine object %s: %w", m.Name, err)
		}
	}

	ms := machineState{
		isVersionUpToDate:   versionMatches(m, kcp.Spec.Version),
		isInfraUpToDate:     isInfraMachineUpToDate(uInfraMachine, kcp, m),
		isBootstrapUpToDate: isBootstrapConfigUpToDate(bootstrapConfig, kcp, m),
		controllerConfig:    bootstrapConfig,
		infraMachine:        uInfraMachine,
	}
	return ms, nil
}

// SetupWithManager sets up the controller with the Manager.
func (c *K0sController) SetupWithManager(mgr ctrl.Manager, opts controller.Options) error {
	// Check if the cluster.x-k8s.io API is available and if not, don't try to watch for Machine objects
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(opts).
		For(&cpv1beta2.K0sControlPlane{}).
		Owns(&clusterv1.Machine{}).
		Complete(c)
}
