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

package k0smotronio

import (
	"context"
	"errors"
	"fmt"
	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	"github.com/k0sproject/k0smotron/internal/controller/util"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var ErrEmptyKineDataSourceURL = errors.New("kineDataSourceURL can't be empty if replicas > 1")

var entrypointDefaultMode = int32(0744)

// findStatefulSetPod returns a first running pod from a StatefulSet
func (r *ClusterReconciler) findStatefulSetPod(ctx context.Context, statefulSet string, namespace string) (*v1.Pod, error) {
	return util.FindStatefulSetPod(ctx, r.ClientSet, statefulSet, namespace)
}

func (r *ClusterReconciler) generateStatefulSet(kmc *km.Cluster) (apps.StatefulSet, error) {
	k0sVersion := kmc.Spec.K0sVersion
	if k0sVersion == "" {
		k0sVersion = defaultK0SVersion
	}

	if kmc.Spec.Replicas > 1 && (kmc.Spec.KineDataSourceURL == "" && kmc.Spec.KineDataSourceSecretRef == nil) {
		return apps.StatefulSet{}, ErrEmptyKineDataSourceURL
	}

	statefulSet := apps.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kmc.GetStatefulSetName(),
			Namespace: kmc.Namespace,
			Labels:    map[string]string{"app": "k0smotron"},
		},
		Spec: apps.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "k0smotron"},
			},
			Replicas: &kmc.Spec.Replicas,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "k0smotron"},
				},
				Spec: v1.PodSpec{
					Volumes: []v1.Volume{{
						Name: kmc.GetEntrypointConfigMapName(),
						VolumeSource: v1.VolumeSource{
							ConfigMap: &v1.ConfigMapVolumeSource{
								LocalObjectReference: v1.LocalObjectReference{
									Name: kmc.GetEntrypointConfigMapName(),
								},
								DefaultMode: &entrypointDefaultMode,
								Items: []v1.KeyToPath{{
									Key:  "k0smotron-entrypoint.sh",
									Path: "k0smotron-entrypoint.sh",
								}},
							},
						},
					}},
					Containers: []v1.Container{{
						Name:            "controller",
						Image:           fmt.Sprintf("%s:%s", kmc.Spec.K0sImage, k0sVersion),
						ImagePullPolicy: v1.PullIfNotPresent,
						Args:            []string{"/k0smotron-entrypoint.sh"},
						Ports: []v1.ContainerPort{
							{
								Name:          "api",
								Protocol:      v1.ProtocolTCP,
								ContainerPort: int32(kmc.Spec.Service.APIPort),
							},
							{
								Name:          "konnectivity",
								Protocol:      v1.ProtocolTCP,
								ContainerPort: int32(kmc.Spec.Service.KonnectivityPort),
							},
						},
						EnvFrom: []v1.EnvFromSource{{
							ConfigMapRef: &v1.ConfigMapEnvSource{
								LocalObjectReference: v1.LocalObjectReference{
									Name: kmc.GetConfigMapName(),
								},
							},
						}},
						ReadinessProbe: &v1.Probe{
							InitialDelaySeconds: 5,
							ProbeHandler:        v1.ProbeHandler{Exec: &v1.ExecAction{Command: []string{"k0s", "status"}}},
						},
						LivenessProbe: &v1.Probe{
							InitialDelaySeconds: 10,
							ProbeHandler:        v1.ProbeHandler{Exec: &v1.ExecAction{Command: []string{"k0s", "status"}}},
						},
						VolumeMounts: []v1.VolumeMount{{
							Name:      kmc.GetEntrypointConfigMapName(),
							MountPath: "/k0smotron-entrypoint.sh",
							SubPath:   "k0smotron-entrypoint.sh",
						}},
					}},
				}},
		}}

	if kmc.Spec.KineDataSourceSecretRef != nil {
		statefulSet.Spec.Template.Spec.Containers[0].EnvFrom = append(statefulSet.Spec.Template.Spec.Containers[0].EnvFrom, v1.EnvFromSource{
			SecretRef: &v1.SecretEnvSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: kmc.Spec.KineDataSourceSecretRef.Name,
				},
			},
		})
	}

	switch kmc.Spec.Persistence.Type {
	case "emptyDir":
		statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, v1.Volume{
			Name: kmc.GetVolumeName(),
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		})
		statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
			Name:      kmc.GetVolumeName(),
			MountPath: "/var/lib/k0s",
		})
	case "hostPath":
		statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, v1.Volume{
			Name: kmc.GetVolumeName(),
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: kmc.Spec.Persistence.HostPath,
				},
			},
		})
		statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
			Name:      kmc.GetVolumeName(),
			MountPath: "/var/lib/k0s",
		})
	case "pvc":
		statefulSet.Spec.VolumeClaimTemplates = append(statefulSet.Spec.VolumeClaimTemplates, kmc.Spec.Persistence.PersistentVolumeClaim)

		statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
			Name:      kmc.GetVolumeName(),
			MountPath: "/var/lib/k0s",
		})
	}

	err := ctrl.SetControllerReference(kmc, &statefulSet, r.Scheme)
	return statefulSet, err
}

func (r *ClusterReconciler) reconcileStatefulSet(ctx context.Context, kmc km.Cluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling statefulset")
	statefulSet, err := r.generateStatefulSet(&kmc)
	if err != nil {
		return fmt.Errorf("failed to generate statefulset: %w", err)
	}
	return r.Client.Patch(ctx, &statefulSet, client.Apply, patchOpts...)
}
