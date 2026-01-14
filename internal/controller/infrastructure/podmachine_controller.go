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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	capictrl "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1beta1 "github.com/k0sproject/k0smotron/api/infrastructure/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/finalizers"
	"sigs.k8s.io/cluster-api/util/patch"
)

const (
	// PodMachineFinalizer is the finalizer used by PodMachine controller
	PodMachineFinalizer = "podmachine.k0smotron.io/finalizer"
)

// PodMachineController reconciles a PodMachine object
type PodMachineController struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=podmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=podmachines/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=podmachines/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile handles PodMachine reconciliation requests
func (r *PodMachineController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("podmachine", req.NamespacedName)
	logger.Info("Reconciling PodMachine")

	podMachine := &infrastructurev1beta1.PodMachine{}
	if err := r.Get(ctx, req.NamespacedName, podMachine); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("PodMachine not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get PodMachine")
		return ctrl.Result{}, err
	}

	if finalizerAdded, err := finalizers.EnsureFinalizer(ctx, r.Client, podMachine, PodMachineFinalizer); err != nil || finalizerAdded {
		return ctrl.Result{}, err
	}

	// Fetch the Machine that owns PodMachine
	machine, err := capiutil.GetOwnerMachine(ctx, r.Client, podMachine.ObjectMeta)
	if err != nil {
		logger.Error(err, "Failed to get owner Machine")
		return ctrl.Result{}, err
	}
	if machine == nil {
		logger.Info("Machine Controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if !podMachine.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, podMachine)
	}

	// Handle normal reconciliation
	return r.reconcileNormal(ctx, podMachine, machine)
}

func (r *PodMachineController) reconcileDelete(ctx context.Context, podMachine *infrastructurev1beta1.PodMachine) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("podmachine", types.NamespacedName{Name: podMachine.Name, Namespace: podMachine.Namespace})

	if podMachine.Status.PodRef != nil {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podMachine.Status.PodRef.Name,
				Namespace: podMachine.Status.PodRef.Namespace,
			},
		}

		if err := r.Client.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
			logger.Error(err, "Failed to delete pod")
			return ctrl.Result{}, err
		}
	}

	if controllerutil.ContainsFinalizer(podMachine, PodMachineFinalizer) {
		controllerutil.RemoveFinalizer(podMachine, PodMachineFinalizer)
		if err := r.Update(ctx, podMachine); err != nil {
			return ctrl.Result{}, err
		}
	}

	logger.Info("Successfully deleted PodMachine")
	return ctrl.Result{}, nil
}

func (r *PodMachineController) reconcileNormal(ctx context.Context, podMachine *infrastructurev1beta1.PodMachine, machine *clusterv1.Machine) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("podmachine", types.NamespacedName{Name: podMachine.Name, Namespace: podMachine.Namespace})

	// Create or update the pod
	pod, err := r.createOrUpdatePod(ctx, podMachine, machine)
	if err != nil {
		logger.Error(err, "Failed to create/update pod")
		return ctrl.Result{}, err
	}

	podMachine.Spec.ProviderID = fmt.Sprintf("pod-machine://%s/%s", pod.Namespace, pod.Name)

	// Update status
	if err := r.updateStatus(ctx, podMachine, pod); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *PodMachineController) createOrUpdatePod(ctx context.Context, podMachine *infrastructurev1beta1.PodMachine, machine *clusterv1.Machine) (*corev1.Pod, error) {
	podName := podMachine.Name

	// Check if pod already exists
	existingPod := &corev1.Pod{}
	err := r.Get(ctx, client.ObjectKey{Name: podName, Namespace: podMachine.Namespace}, existingPod)

	if err != nil && errors.IsNotFound(err) {
		// Create cloud-init ConfigMap
		_, err := r.createCloudInitConfigMap(ctx, podMachine)
		if err != nil {
			return nil, fmt.Errorf("failed to create cloud-init ConfigMap: %w", err)
		}

		// Create new pod
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: podMachine.Namespace,
				Labels: map[string]string{
					"cluster.x-k8s.io/cluster-name": machine.Spec.ClusterName,
				},
				Annotations: map[string]string{
					"cluster.x-k8s.io/machine": machine.Name,
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         "infrastructure.cluster.x-k8s.io/v1beta1",
						Kind:               "PodMachine",
						Name:               podMachine.Name,
						UID:                podMachine.UID,
						Controller:         &[]bool{true}[0],
						BlockOwnerDeletion: &[]bool{true}[0],
					},
				},
			},
			Spec: podMachine.Spec.PodTemplate.Spec,
		}

		// Add cloud-init volume mounts
		r.addCloudInitVolumeMounts(pod, podMachine)

		return pod, r.Create(ctx, pod)
	} else if err != nil {
		return nil, err
	}

	// Pod exists - ensure cloud-init ConfigMap exists
	_, err = r.createCloudInitConfigMap(ctx, podMachine)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure cloud-init ConfigMap exists: %w", err)
	}

	// Check if pod has cloud-init volume mounts, if not, add them
	hasCloudInitVolumes := false
	for _, volume := range existingPod.Spec.Volumes {
		if volume.Name == "cloud-init-meta-data" || volume.Name == "cloud-init-user-data" {
			hasCloudInitVolumes = true
			break
		}
	}

	if !hasCloudInitVolumes {
		r.addCloudInitVolumeMounts(existingPod, podMachine)
		if err := r.Update(ctx, existingPod); err != nil {
			return nil, fmt.Errorf("failed to update pod with cloud-init volumes: %w", err)
		}
	}

	return existingPod, nil
}

func (r *PodMachineController) createCloudInitConfigMap(ctx context.Context, podMachine *infrastructurev1beta1.PodMachine) (*corev1.ConfigMap, error) {
	configMapName := podMachine.Name

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: podMachine.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "infrastructure.cluster.x-k8s.io/v1beta1",
					Kind:               "PodMachine",
					Name:               podMachine.Name,
					UID:                podMachine.UID,
					Controller:         &[]bool{true}[0],
					BlockOwnerDeletion: &[]bool{true}[0],
				},
			},
		},
		Data: map[string]string{
			"meta-data": fmt.Sprintf("hostname: %s", podMachine.Name),
		},
	}

	existingConfigMap := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: podMachine.Namespace}, existingConfigMap)
	if err != nil && errors.IsNotFound(err) {
		return configMap, r.Create(ctx, configMap)
	} else if err != nil {
		return nil, err
	}

	existingConfigMap.Data = configMap.Data
	return existingConfigMap, r.Update(ctx, existingConfigMap)
}

func (r *PodMachineController) addCloudInitVolumeMounts(pod *corev1.Pod, podMachine *infrastructurev1beta1.PodMachine) {
	configMapName := podMachine.Name
	secretName := podMachine.Name

	// Add volumes for cloud-init data
	volumes := []corev1.Volume{
		{
			Name: "cloud-init-meta-data",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  "meta-data",
							Path: "meta-data",
						},
					},
				},
			},
		},
		{
			Name: "cloud-init-user-data",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
					Items: []corev1.KeyToPath{
						{
							Key:  "value",
							Path: "user-data",
						},
					},
				},
			},
		},
	}

	// Add volumes to pod spec
	pod.Spec.Volumes = append(pod.Spec.Volumes, volumes...)

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "cloud-init-meta-data",
			MountPath: "/var/lib/cloud/seed/nocloud/meta-data",
			SubPath:   "meta-data",
			ReadOnly:  true,
		},
		{
			Name:      "cloud-init-user-data",
			MountPath: "/var/lib/cloud/seed/nocloud/user-data",
			SubPath:   "user-data",
			ReadOnly:  true,
		},
	}
	// Add volume mounts to all containers
	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].VolumeMounts = append(pod.Spec.Containers[i].VolumeMounts, volumeMounts...)
	}
}

func (r *PodMachineController) updateStatus(ctx context.Context, podMachine *infrastructurev1beta1.PodMachine, pod *corev1.Pod) error {
	patchHelper, err := patch.NewHelper(podMachine, r.Client)
	if err != nil {
		return err
	}

	// Update pod reference
	podMachine.Status.PodRef = &corev1.ObjectReference{
		APIVersion: pod.APIVersion,
		Kind:       pod.Kind,
		Name:       pod.Name,
		Namespace:  pod.Namespace,
		UID:        pod.UID,
	}

	// Update addresses
	addresses := []clusterv1.MachineAddress{}
	if pod.Status.PodIP != "" {
		addresses = append(addresses, clusterv1.MachineAddress{
			Type:    clusterv1.MachineInternalIP,
			Address: pod.Status.PodIP,
		})
	}
	podMachine.Status.Addresses = addresses

	// Update ready status
	ready := false
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			ready = true
			break
		}
	}
	podMachine.Status.Ready = ready

	// Note: Conditions would be set here in a full implementation
	// For now, we just update the basic status

	return patchHelper.Patch(ctx, podMachine)
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodMachineController) SetupWithManager(mgr ctrl.Manager, opts capictrl.Options) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(opts).
		For(&infrastructurev1beta1.PodMachine{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
