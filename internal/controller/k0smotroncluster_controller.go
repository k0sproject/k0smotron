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

package controller

import (
	"context"
	"fmt"
	"time"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	km "github.com/k0sproject/k0smotron/api/v1beta1"
	"github.com/k0sproject/k0smotron/internal/exec"
)

const (
	defaultK0SVersion       = "v1.26.2-k0s.1"
	defaultAPIPort          = 30443
	defaultKonnectivityPort = 30132
)

var patchOpts []client.PatchOption = []client.PatchOption{
	client.FieldOwner("k0smotron-operator"),
	client.ForceOwnership,
}

// K0smotronClusterReconciler reconciles a K0smotronCluster object
type K0smotronClusterReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	ClientSet  *kubernetes.Clientset
	RESTConfig *rest.Config
}

//+kubebuilder:rbac:groups=k0smotron.io,resources=k0smotronclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=k0smotron.io,resources=k0smotronclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=k0smotron.io,resources=k0smotronclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the K0smotronCluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *K0smotronClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var kmc km.K0smotronCluster
	if err := r.Get(ctx, req.NamespacedName, &kmc); err != nil {
		logger.Error(err, "unable to fetch K0smotronCluster")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if kmc.Spec.APIPort == 0 {
		kmc.Spec.APIPort = defaultAPIPort
	}
	if kmc.Spec.KonnectivityPort == 0 {
		kmc.Spec.APIPort = defaultKonnectivityPort
	}

	logger.Info("Reconciling")

	if err := r.reconcileCM(ctx, req, kmc); err != nil {
		r.updateStatus(ctx, kmc, "Failed reconciling configmap")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	logger.Info("Reconciling services")
	if err := r.reconcileServices(ctx, req, kmc); err != nil {
		r.updateStatus(ctx, kmc, "Failed reconciling services")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	logger.Info("Reconciling PVC")
	if err := r.reconcilePVC(ctx, req, kmc); err != nil {
		r.updateStatus(ctx, kmc, "Failed reconciling PVC")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	logger.Info("Reconciling deployment")
	if err := r.reconcileDeployment(ctx, req, kmc); err != nil {
		r.updateStatus(ctx, kmc, "Failed reconciling deployment")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	if err := r.reconcileKubeConfigSecret(ctx, kmc); err != nil {
		r.updateStatus(ctx, kmc, "Failed reconciling secret")
		return ctrl.Result{Requeue: true, RequeueAfter: time.Minute}, err
	}

	r.updateStatus(ctx, kmc, "Reconciliation successful")
	return ctrl.Result{}, nil
}

func (r *K0smotronClusterReconciler) updateStatus(ctx context.Context, kmc km.K0smotronCluster, status string) {
	logger := log.FromContext(ctx)
	kmc.Status.ReconciliationStatus = status
	if err := r.Status().Update(ctx, &kmc); err != nil {
		logger.Error(err, fmt.Sprintf("Unable to update status: %s", status))
	}
}

func (r *K0smotronClusterReconciler) reconcileCM(ctx context.Context, req ctrl.Request, kmc km.K0smotronCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling configmap")

	cm := r.generateCM(&kmc)

	return r.Client.Patch(ctx, &cm, client.Apply, patchOpts...)
}

func (r *K0smotronClusterReconciler) reconcileServices(ctx context.Context, req ctrl.Request, kmc km.K0smotronCluster) error {
	logger := log.FromContext(ctx)
	// Depending on ingress configuration create nodePort service.
	logger.Info("Reconciling services")
	svc := r.generateService(&kmc)
	return r.Client.Patch(ctx, &svc, client.Apply, patchOpts...)
}

func (r *K0smotronClusterReconciler) reconcileDeployment(ctx context.Context, req ctrl.Request, kmc km.K0smotronCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling deployment")
	deploy := r.generateDeployment(&kmc)

	return r.Client.Patch(ctx, &deploy, client.Apply, patchOpts...)
}

// SetupWithManager sets up the controller with the Manager.
func (r *K0smotronClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&km.K0smotronCluster{}).
		Complete(r)
}

func (r *K0smotronClusterReconciler) generateService(kmc *km.K0smotronCluster) v1.Service {
	// TODO Ports cannot be hardcoded
	// TODO Allow multiple service types
	svc := v1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kmc.GetNodePortName(),
			Namespace: kmc.Namespace,
			Labels:    map[string]string{"app": "k0smotron"},
		},
		Spec: v1.ServiceSpec{
			Type:     v1.ServiceTypeNodePort,
			Selector: map[string]string{"app": "k0smotron"},
			Ports: []v1.ServicePort{
				{
					Port:       int32(kmc.Spec.APIPort),
					TargetPort: intstr.FromInt(kmc.Spec.APIPort),
					Name:       "api",
					NodePort:   int32(kmc.Spec.APIPort),
				},
				{
					Port:       int32(kmc.Spec.KonnectivityPort),
					TargetPort: intstr.FromInt(kmc.Spec.KonnectivityPort),
					Name:       "konnectivity",
					NodePort:   int32(kmc.Spec.KonnectivityPort),
				},
			},
		},
	}

	_ = ctrl.SetControllerReference(kmc, &svc, r.Scheme)

	return svc
}

func (r *K0smotronClusterReconciler) generateCM(kmc *km.K0smotronCluster) v1.ConfigMap {
	// TODO port cannot be hardcoded
	// TODO externalAddress cannot be hardcoded
	// TODO k0s.yaml should probably be a
	// github.com/k0sproject/k0s/pkg/apis/k0s.k0sproject.io/v1beta1.ClusterConfig
	// and then unmarshalled into json to make modification of fields reliable
	cm := v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kmc.GetConfigMapName(),
			Namespace: kmc.Namespace,
		},
		Data: map[string]string{
			"k0s.yaml": fmt.Sprintf(`apiVersion: k0s.k0sproject.io/v1beta1
kind: ClusterConfig
metadata:
  name: k0s
spec:
  api:
    port: %d
    externalAddress: 172.17.0.3
  konnectivity:
    agentPort: %d`, kmc.Spec.APIPort, kmc.Spec.KonnectivityPort), // TODO: do it as a template or something like this
		},
	}

	_ = ctrl.SetControllerReference(kmc, &cm, r.Scheme)
	return cm
}

func (r *K0smotronClusterReconciler) generateDeployment(kmc *km.K0smotronCluster) apps.Deployment {
	k0sVersion := kmc.Spec.K0sVersion
	if k0sVersion == "" {
		k0sVersion = defaultK0SVersion
	}

	dep := apps.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kmc.GetDeploymentName(),
			Namespace: kmc.Namespace,
			Labels:    map[string]string{"app": "k0smotron"},
		},
		Spec: apps.DeploymentSpec{
			Strategy: apps.DeploymentStrategy{Type: apps.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "k0smotron"},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "k0smotron"},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{
						Name:            "controller",
						Image:           fmt.Sprintf("%s:%s", kmc.Spec.K0sImage, k0sVersion),
						ImagePullPolicy: v1.PullIfNotPresent,
						Args:            []string{"k0s", "controller", "--config", "/etc/k0s/k0s.yaml"},
						Ports: []v1.ContainerPort{
							{
								Name:          "api",
								Protocol:      v1.ProtocolTCP,
								ContainerPort: int32(kmc.Spec.APIPort),
							},
							{
								Name:          "konnectivity",
								Protocol:      v1.ProtocolTCP,
								ContainerPort: int32(kmc.Spec.KonnectivityPort),
							},
						},
						VolumeMounts: []v1.VolumeMount{{
							Name:      "k0s-config",
							MountPath: "/etc/k0s",
							ReadOnly:  true,
						}},
						ReadinessProbe: &v1.Probe{ProbeHandler: v1.ProbeHandler{Exec: &v1.ExecAction{Command: []string{"k0s", "status"}}}},
						LivenessProbe:  &v1.Probe{ProbeHandler: v1.ProbeHandler{Exec: &v1.ExecAction{Command: []string{"k0s", "status"}}}},
					}},
					Volumes: []v1.Volume{{
						Name: "k0s-config",
						VolumeSource: v1.VolumeSource{
							// TODO LocalObjectReference can't be hardcoded
							ConfigMap: &v1.ConfigMapVolumeSource{
								LocalObjectReference: v1.LocalObjectReference{Name: kmc.GetConfigMapName()},
								Items: []v1.KeyToPath{{
									Key:  "k0s.yaml",
									Path: "k0s.yaml",
								}},
							}}}},
				}}}}

	switch kmc.Spec.Persistence.Type {
	case "emptyDir":
		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, v1.Volume{
			Name: kmc.GetVolumeName(),
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		})
		dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
			Name:      kmc.GetVolumeName(),
			MountPath: "/var/lib/k0s",
		})
	case "hostPath":
		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, v1.Volume{
			Name: kmc.GetVolumeName(),
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: kmc.Spec.Persistence.HostPath,
				},
			},
		})
		dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
			Name:      kmc.GetVolumeName(),
			MountPath: "/var/lib/k0s",
		})
	case "pvc":
		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, v1.Volume{
			Name: kmc.GetVolumeName(),
			VolumeSource: v1.VolumeSource{
				PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
					ClaimName: kmc.GetVolumeName(),
				},
			},
		})
		dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
			Name:      kmc.GetVolumeName(),
			MountPath: "/var/lib/k0s",
		})
	}

	ctrl.SetControllerReference(kmc, &dep, r.Scheme)
	return dep
}

func (r *K0smotronClusterReconciler) reconcileKubeConfigSecret(ctx context.Context, kmc km.K0smotronCluster) error {
	logger := log.FromContext(ctx)
	pod, err := r.findDeploymentPod(ctx, kmc.GetDeploymentName(), kmc.Namespace)

	if err != nil {
		return err
	}

	output, err := exec.PodExecCmdOutput(ctx, r.ClientSet, r.RESTConfig, pod.Name, kmc.Namespace, "k0s kubeconfig admin")
	if err != nil {
		return err
	}

	logger.Info("Kubeconfig generated, creating the secret")

	secret := v1.Secret{
		// The dynamic r.Client needs TypeMeta to be set
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kmc.GetAdminConfigSecretName(),
			Namespace: kmc.Namespace,
			Labels:    map[string]string{"app": "k0smotron"},
		},
		StringData: map[string]string{"kubeconfig": output},
	}

	if err = ctrl.SetControllerReference(&kmc, &secret, r.Scheme); err != nil {
		return err
	}

	return r.Client.Patch(ctx, &secret, client.Apply, patchOpts...)
}

// FindDeploymentPod returns a first running pod from a deployment
func (r *K0smotronClusterReconciler) findDeploymentPod(ctx context.Context, deploymentName string, namespace string) (*v1.Pod, error) {
	dep, err := r.ClientSet.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	selector := metav1.FormatLabelSelector(dep.Spec.Selector)
	pods, err := r.ClientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) < 1 {
		return nil, fmt.Errorf("did not find matching pods for deployment %s", deploymentName)
	}
	// Find a running pod
	var runningPod *v1.Pod
	for _, p := range pods.Items {
		if p.Status.Phase == v1.PodRunning {
			runningPod = &p
			break
		}
	}
	if runningPod == nil {
		return nil, fmt.Errorf("did not find running pods for deployment %s", deploymentName)
	}
	return runningPod, nil
}

func (r *K0smotronClusterReconciler) reconcilePVC(ctx context.Context, req ctrl.Request, kmc km.K0smotronCluster) error {

	if kmc.Spec.Persistence.Type != "pvc" {
		return nil
	}

	pvc := r.generatePVC(&kmc, kmc.GetVolumeName())

	return r.Client.Patch(ctx, &pvc, client.Apply, patchOpts...)
}

func (r *K0smotronClusterReconciler) generatePVC(kmc *km.K0smotronCluster, name string) v1.PersistentVolumeClaim {
	pvc := v1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: kmc.Namespace,
			Labels:    map[string]string{"app": "k0smotron"},
		},
		Spec: kmc.Spec.Persistence.PersistentVolumeClaim.Spec,
	}

	ctrl.SetControllerReference(kmc, &pvc, r.Scheme)

	return pvc
}
