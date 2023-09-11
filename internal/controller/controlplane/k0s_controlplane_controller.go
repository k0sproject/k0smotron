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
	"github.com/google/uuid"
	"github.com/k0sproject/k0smotron/internal/controller/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	kubeadmbootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
)

const (
	defaultK0SVersion = "v1.27.2-k0s.0"
)

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
	log.Info("Reconciling K0sControlPlane")

	kcp := &cpv1beta1.K0sControlPlane{}
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

	if kcp.Spec.K0sVersion == "" {
		kcp.Spec.K0sVersion = defaultK0SVersion
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

	log = log.WithValues("cluster", cluster.Name)

	if annotations.IsPaused(cluster, kcp) {
		log.Info("Reconciliation is paused for this object")
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

	res, err = c.reconcile(ctx, cluster, kcp)
	if err != nil {
		return res, err
	}

	// TODO: We need to have bit more detailed status and conditions handling
	kcp.Status.Ready = true
	kcp.Status.ExternalManagedControlPlane = false
	kcp.Status.Inititalized = true
	kcp.Status.ControlPlaneReady = true
	kcp.Status.Replicas = kcp.Spec.Replicas
	err = c.Status().Update(ctx, kcp)

	return res, err

}

func (c *K0sController) reconcileKubeconfig(ctx context.Context, cluster *clusterv1.Cluster) error {
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

	return nil
}

func (c *K0sController) reconcile(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) (ctrl.Result, error) {
	err := c.reconcileKubeconfig(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error reconciling kubeconfig secret: %w", err)
	}

	err = c.reconcileMachines(ctx, cluster, kcp)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (c *K0sController) reconcileMachines(ctx context.Context, cluster *clusterv1.Cluster, kcp *cpv1beta1.K0sControlPlane) error {
	// TODO: Scale down machines if needed
	if kcp.Status.Replicas > kcp.Spec.Replicas {
		for i := kcp.Spec.Replicas; i < kcp.Status.Replicas; i++ {
			name := machineName(kcp.Name, int(i))

			if err := c.deleteBootstrapConfig(ctx, name, kcp); err != nil {
				return fmt.Errorf("error deleting machine from template: %w", err)
			}

			if err := c.deleteMachineFromTemplate(ctx, name, cluster, kcp); err != nil {
				return fmt.Errorf("error deleting machine from template: %w", err)
			}

			if err := c.deleteMachine(ctx, name, kcp); err != nil {
				return fmt.Errorf("error deleting machine from template: %w", err)
			}
		}
	}
	for i := 0; i < int(kcp.Spec.Replicas); i++ {
		name := machineName(kcp.Name, i)

		machineFromTemplate, err := c.createMachineFromTemplate(ctx, name, cluster, kcp)
		if err != nil {
			return fmt.Errorf("error creating machine from template: %w", err)
		}

		infraRef := corev1.ObjectReference{
			APIVersion: machineFromTemplate.GetAPIVersion(),
			Kind:       machineFromTemplate.GetKind(),
			Name:       machineFromTemplate.GetName(),
			Namespace:  kcp.Namespace,
		}

		machine, err := c.createMachine(ctx, name, cluster, kcp, infraRef)
		if err != nil {
			return fmt.Errorf("error creating machine: %w", err)
		}

		err = c.createBootstrapConfig(ctx, name, cluster, kcp, machine)
		if err != nil {
			return fmt.Errorf("error creating bootstrap config: %w", err)
		}
	}

	return nil
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
				BlockOwnerDeletion: pointer.Bool(true),
				Controller:         pointer.Bool(true),
			}},
		},
		Spec: kcp.Spec.K0sConfigSpec,
	}

	if err := c.Client.Patch(ctx, &controllerConfig, client.Apply, &client.PatchOptions{
		FieldManager: "k0smotron",
	}); err != nil {
		return fmt.Errorf("error patching K0sControllerConfig: %w", err)
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
	return c.Client.Delete(ctx, &controllerConfig)
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

	frpsConfig := `
[common]
bind_port = 7000
authentication_method = token
token = ` + frpToken + `
`
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
				NodePort:   31700,
			}, {
				Name:       "tunnel",
				Protocol:   corev1.ProtocolTCP,
				Port:       6443,
				TargetPort: intstr.FromInt(6443),
				NodePort:   31443,
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
	frpToken := uuid.New().String()
	frpSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + "-frp-token",
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
		Complete(c)
}
