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

package k0smotron

import (
	"context"
	"errors"
	"fmt"

	km "github.com/k0sproject/k0smotron/api/k0smotron/v1beta1"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var ErrEmptyKineDataSourceURL = errors.New("kineDataSourceURL can't be empty if replicas > 1")

// findStatefulSetPod returns a first running pod from a StatefulSet
func (r *ClusterReconciler) findStatefulSetPod(ctx context.Context, statefulSet string, namespace string) (*v1.Pod, error) {
	dep, err := r.ClientSet.AppsV1().StatefulSets(namespace).Get(ctx, statefulSet, metav1.GetOptions{})
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
		return nil, fmt.Errorf("did not find matching pods for statefulSet %s", statefulSet)
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
		return nil, fmt.Errorf("did not find running pods for statefulSet %s", statefulSet)
	}
	return runningPod, nil
}

func (r *ClusterReconciler) generateStatefulSet(kmc *km.Cluster) (apps.StatefulSet, error) {
	k0sVersion := kmc.Spec.K0sVersion
	if k0sVersion == "" {
		k0sVersion = defaultK0SVersion
	}

	if kmc.Spec.Replicas > 1 && kmc.Spec.KineDataSourceURL == "" {
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
					Containers: []v1.Container{{
						Name:            "controller",
						Image:           fmt.Sprintf("%s:%s", kmc.Spec.K0sImage, k0sVersion),
						ImagePullPolicy: v1.PullIfNotPresent,
						Args:            []string{"k0s", "controller", "--config", "/etc/k0s/k0s.yaml"},
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
				}},
		}}

	// Mount certificates if they are provided
	if kmc.Spec.CertificateRefs != nil && len(kmc.Spec.CertificateRefs) > 0 {
		r.mountSecrets(kmc, &statefulSet)
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

// mountSecrets mounts the certificates as secrets to the controller and creates
// an init container that copies the certificates to the correct location
func (r *ClusterReconciler) mountSecrets(kmc *km.Cluster, sfs *apps.StatefulSet) {
	projectedSecrets := []v1.VolumeProjection{}

	for _, cert := range kmc.Spec.CertificateRefs {
		switch cert.Type {
		case "ca":
			projectedSecrets = append(projectedSecrets, v1.VolumeProjection{
				Secret: &v1.SecretProjection{
					LocalObjectReference: v1.LocalObjectReference{Name: cert.Name},
					Items: []v1.KeyToPath{
						{
							Key:  "tls.crt",
							Path: "ca.crt",
						},
						{
							Key:  "tls.key",
							Path: "ca.key",
						},
					},
				},
			})

		case "sa":
			projectedSecrets = append(projectedSecrets, v1.VolumeProjection{
				Secret: &v1.SecretProjection{
					LocalObjectReference: v1.LocalObjectReference{Name: cert.Name},
					Items: []v1.KeyToPath{
						{
							Key:  "tls.crt",
							Path: "sa.pub",
						},
						{
							Key:  "tls.key",
							Path: "sa.key",
						},
					},
				},
			})
		case "proxy":
			projectedSecrets = append(projectedSecrets, v1.VolumeProjection{
				Secret: &v1.SecretProjection{
					LocalObjectReference: v1.LocalObjectReference{Name: cert.Name},
					Items: []v1.KeyToPath{
						{
							Key:  "tls.crt",
							Path: "front-proxy-ca.crt",
						},
						{
							Key:  "tls.key",
							Path: "front-proxy-ca.key",
						},
					},
				},
			})

		}
	}
	sfs.Spec.Template.Spec.Volumes = append(sfs.Spec.Template.Spec.Volumes, v1.Volume{
		Name: "certs",
		VolumeSource: v1.VolumeSource{
			Projected: &v1.ProjectedVolumeSource{
				Sources: projectedSecrets,
			},
		},
	})

	// We need to copy the certs from the projected volume to the /var/lib/k0s/pki directory
	// Otherwise k0s will trip over the permissions and RO mounts
	sfs.Spec.Template.Spec.InitContainers = append(sfs.Spec.Template.Spec.InitContainers, v1.Container{
		Name:  "certs-init",
		Image: "busybox",
		Command: []string{
			"sh",
			"-c",
			"mkdir -p /var/lib/k0s/pki && cp /certs-init/*.* /var/lib/k0s/pki/",
		},
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      "certs",
				MountPath: "/certs-init",
			},
			{
				Name:      kmc.GetVolumeName(),
				MountPath: "/var/lib/k0s",
			},
		},
	})
}

// func (r *ClusterReconciler) mountSecrets(kmc *km.Cluster, sfs *apps.StatefulSet) {
// 	certPermission := int32(0644)
// 	keyPermission := int32(0640)
// 	volumes := []v1.Volume{}
// 	volumeMounts := []v1.VolumeMount{}
// 	for _, cert := range kmc.Spec.CertificateRefs {
// 		switch cert.Type {
// 		case "ca":
// 			volumes = append(volumes, v1.Volume{
// 				Name: "ca-cert",
// 				VolumeSource: v1.VolumeSource{
// 					Secret: &v1.SecretVolumeSource{
// 						SecretName: cert.Name,
// 						Items: []v1.KeyToPath{
// 							{
// 								Key:  "tls.crt",
// 								Path: "ca.crt",
// 								Mode: &certPermission,
// 							},
// 						},
// 					},
// 				},
// 			})
// 			volumeMounts = append(volumeMounts, v1.VolumeMount{
// 				Name:      "ca-cert",
// 				MountPath: "/var/lib/k0s/pki/ca.crt",
// 				SubPath:   "ca.crt",
// 			})

// 			volumes = append(volumes, v1.Volume{
// 				Name: "ca-key",
// 				VolumeSource: v1.VolumeSource{
// 					Secret: &v1.SecretVolumeSource{
// 						SecretName: cert.Name,
// 						Items: []v1.KeyToPath{
// 							{
// 								Key:  "tls.key",
// 								Path: "ca.key",
// 								Mode: &keyPermission,
// 							},
// 						},
// 					},
// 				},
// 			})
// 			volumeMounts = append(volumeMounts, v1.VolumeMount{
// 				Name:      "ca-key",
// 				MountPath: "/var/lib/k0s/pki/ca.key",
// 				SubPath:   "ca.key",
// 			})

// 		case "sa":
// 			volumes = append(volumes, v1.Volume{
// 				Name: "sa-pub",
// 				VolumeSource: v1.VolumeSource{
// 					Secret: &v1.SecretVolumeSource{
// 						SecretName: cert.Name,
// 						Items: []v1.KeyToPath{
// 							{
// 								Key:  "tls.crt",
// 								Path: "sa.pub",
// 								Mode: &certPermission,
// 							},
// 						},
// 					},
// 				},
// 			})
// 			volumeMounts = append(volumeMounts, v1.VolumeMount{
// 				Name:      "sa-pub",
// 				MountPath: "/var/lib/k0s/pki/sa.pub",
// 				SubPath:   "sa.pub",
// 			})

// 			volumes = append(volumes, v1.Volume{
// 				Name: "sa-key",
// 				VolumeSource: v1.VolumeSource{
// 					Secret: &v1.SecretVolumeSource{
// 						SecretName: cert.Name,
// 						Items: []v1.KeyToPath{
// 							{
// 								Key:  "tls.key",
// 								Path: "sa.key",
// 								Mode: &keyPermission,
// 							},
// 						},
// 					},
// 				},
// 			})
// 			volumeMounts = append(volumeMounts, v1.VolumeMount{
// 				Name:      "sa-key",
// 				MountPath: "/var/lib/k0s/pki/sa.key",
// 				SubPath:   "sa.key",
// 			})
// 		case "proxy":
// 			volumes = append(volumes, v1.Volume{
// 				Name: "proxy-cert",
// 				VolumeSource: v1.VolumeSource{
// 					Secret: &v1.SecretVolumeSource{
// 						SecretName: cert.Name,
// 						Items: []v1.KeyToPath{
// 							{
// 								Key:  "tls.crt",
// 								Path: "front-proxy-ca.crt",
// 								Mode: &certPermission,
// 							},
// 						},
// 					},
// 				},
// 			})
// 			volumeMounts = append(volumeMounts, v1.VolumeMount{
// 				Name:      "proxy-cert",
// 				MountPath: "/var/lib/k0s/pki/front-proxy-ca.crt",
// 				SubPath:   "front-proxy-ca.crt",
// 			})

// 			volumes = append(volumes, v1.Volume{
// 				Name: "proxy-key",
// 				VolumeSource: v1.VolumeSource{
// 					Secret: &v1.SecretVolumeSource{
// 						SecretName: cert.Name,
// 						Items: []v1.KeyToPath{
// 							{
// 								Key:  "tls.key",
// 								Path: "front-proxy-ca.key",
// 								Mode: &keyPermission,
// 							},
// 						},
// 					},
// 				},
// 			})
// 			volumeMounts = append(volumeMounts, v1.VolumeMount{
// 				Name:      "proxy-key",
// 				MountPath: "/var/lib/k0s/pki/front-proxy-ca.key",
// 				SubPath:   "front-proxy-ca.key",
// 			})
// 		}
// 	}
// 	sfs.Spec.Template.Spec.Volumes = append(sfs.Spec.Template.Spec.Volumes, volumes...)
// 	sfs.Spec.Template.Spec.Containers[0].VolumeMounts = append(sfs.Spec.Template.Spec.Containers[0].VolumeMounts, volumeMounts...)
// }

func (r *ClusterReconciler) reconcileStatefulSet(ctx context.Context, kmc km.Cluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling statefulset")
	statefulSet, err := r.generateStatefulSet(&kmc)
	if err != nil {
		return fmt.Errorf("failed to generate statefulset: %w", err)
	}
	return r.Client.Patch(ctx, &statefulSet, client.Apply, patchOpts...)
}
