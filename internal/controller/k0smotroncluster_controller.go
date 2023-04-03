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
	"reflect"
	"time"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	km "github.com/k0sproject/k0smotron/api/v1beta1"
)

const (
	defaultK0SVersion       = "v1.26.2-k0s.1"
	defaultAPIPort          = 30443
	defaultKonnectivityPort = 30132
)

// K0smotronClusterReconciler reconciles a K0smotronCluster object
type K0smotronClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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

	logger.Info("Reconciling deployment")
	if err := r.reconcileDeployment(ctx, req, kmc); err != nil {
		r.updateStatus(ctx, kmc, "Failed reconciling deployment")
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
	var foundCM v1.ConfigMap
	err := r.Get(ctx, client.ObjectKey{
		Namespace: kmc.Namespace,
		Name:      kmc.GetConfigMapName(),
	}, &foundCM)
	if err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "Unable to get Configmap. Aborting reconciliation")
			return err
		}
		cm := r.generateCM(&kmc)
		return r.Create(ctx, &cm)
	}

	expectedCM := r.generateCM(&kmc)
	if reflect.DeepEqual(foundCM.Data, expectedCM.Data) {
		return nil
	}

	logger.Info("Reconciling configmap")
	return r.Update(ctx, &expectedCM)
}

func (r *K0smotronClusterReconciler) reconcileServices(ctx context.Context, req ctrl.Request, kmc km.K0smotronCluster) error {
	logger := log.FromContext(ctx)
	// Depending on ingress configuration create nodePort service.
	logger.Info("Reconciling services")
	var foundSvc v1.Service
	err := r.Get(ctx, client.ObjectKey{
		Namespace: kmc.Namespace,
		Name:      kmc.GetNodePortName(),
	}, &foundSvc)
	if err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "Unable to get service. Aborting reconciliation")
			return err
		}
		svc := r.generateService(&kmc)
		return r.Create(ctx, &svc)
	}

	expectedSvc := r.generateService(&kmc)
	if reflect.DeepEqual(foundSvc.Spec, expectedSvc.Spec) {
		return nil
	}

	logger.Info("Reconciling service")
	return r.Update(ctx, &expectedSvc)
}

func (r *K0smotronClusterReconciler) reconcileDeployment(ctx context.Context, req ctrl.Request, kmc km.K0smotronCluster) error {
	logger := log.FromContext(ctx)
	// TODO Name cannot be hardcoded
	logger.Info("Reconciling %s/%s deployment")
	var foundDep apps.Deployment
	err := r.Get(ctx, client.ObjectKey{
		Namespace: kmc.Namespace,
		Name:      kmc.GetDeploymentName(),
	}, &foundDep)
	if err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "Unable to get Configmap. Aborting reconciliation")
			return err
		}
		dep := r.generateDeployment(&kmc)
		err = r.Create(ctx, &dep)
		if err != nil {
			return err
		}
	}
	dep := r.generateDeployment(&kmc)
	// deploymentConfigs are quite smart so it's safe to issue updates even if there aren't any changes
	err = r.Update(ctx, &dep)
	if err != nil {
		return err
	}

	return nil
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

	ctrl.SetControllerReference(kmc, &dep, r.Scheme)
	return dep
}
