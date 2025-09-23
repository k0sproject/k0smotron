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
	"fmt"
	"reflect"
	"regexp"
	"strings"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta1"
	"github.com/k0sproject/k0smotron/internal/controller/util"
	kcontrollerutil "github.com/k0sproject/k0smotron/internal/controller/util"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var entrypointDefaultMode = int32(0744)

const (
	clusterLabel          = "k0smotron.io/cluster"
	statefulSetAnnotation = "k0smotron.io/statefulset-hash"
)

var versionRegex = regexp.MustCompile(`v\d+.\d+.\d+-k0s.\d+`)

// findStatefulSetPod returns a first running pod from a StatefulSet
func findStatefulSetPod(ctx context.Context, statefulSet string, namespace string, clientSet *kubernetes.Clientset) (*v1.Pod, error) {
	return util.FindStatefulSetPod(ctx, clientSet, statefulSet, namespace)
}

func (scope *kmcScope) generateStatefulSet(kmc *km.Cluster) (apps.StatefulSet, error) {

	labels := util.LabelsForK0smotronControlPlane(kmc)
	annotations := util.AnnotationsForK0smotronCluster(kmc)

	statefulSet := apps.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kmc.GetStatefulSetName(),
			Namespace: kmc.Namespace,
			Labels:    labels,
		},
		Spec: apps.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Replicas: &kmc.Spec.Replicas,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: v1.PodSpec{
					AutomountServiceAccountToken: ptr.To(false),
					Affinity: &v1.Affinity{PodAntiAffinity: &v1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []v1.WeightedPodAffinityTerm{
							{
								Weight: 100,
								PodAffinityTerm: v1.PodAffinityTerm{
									TopologyKey: "topology.kubernetes.io/zone",
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: util.DefaultK0smotronClusterLabels(kmc),
									},
								},
							},
							{
								Weight: 50,
								PodAffinityTerm: v1.PodAffinityTerm{
									TopologyKey: "kubernetes.io/hostname",
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: util.DefaultK0smotronClusterLabels(kmc),
									},
								},
							},
						},
					}},
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
						Image:           kmc.Spec.GetImage(),
						ImagePullPolicy: v1.PullIfNotPresent,
						Args:            []string{"/bin/sh", "-c", "/k0smotron-entrypoint.sh"},
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
						Resources: kmc.Spec.Resources,
						VolumeMounts: []v1.VolumeMount{{
							Name:      kmc.GetEntrypointConfigMapName(),
							MountPath: "/k0smotron-entrypoint.sh",
							SubPath:   "k0smotron-entrypoint.sh",
						}},
					}},
				}},
		}}

	if kmc.Spec.ServiceAccount != "" {
		statefulSet.Spec.Template.Spec.ServiceAccountName = kmc.Spec.ServiceAccount
	}

	if kmc.Spec.Monitoring.Enabled {
		if kmc.Spec.Persistence.Type == "" {
			kmc.Spec.Persistence.Type = "emptyDir"
		}
		addMonitoringStack(kmc, &statefulSet)
	}

	if kmc.Spec.KineDataSourceSecretName != "" {
		statefulSet.Spec.Template.Spec.Containers[0].EnvFrom = append(statefulSet.Spec.Template.Spec.Containers[0].EnvFrom, v1.EnvFromSource{
			SecretRef: &v1.SecretEnvSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: kmc.Spec.KineDataSourceSecretName,
				},
			},
		})
	}
	// Mount certificates if they are provided
	if len(kmc.Spec.CertificateRefs) > 0 {
		mountSecrets(kmc, &statefulSet)
	}

	if kmc.Spec.TopologySpreadConstraints != nil {
		statefulSet.Spec.Template.Spec.TopologySpreadConstraints = kmc.Spec.TopologySpreadConstraints
	}

	switch kmc.Spec.Persistence.Type {
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
		if kmc.Spec.Persistence.PersistentVolumeClaim == nil {
			return apps.StatefulSet{}, fmt.Errorf("persistence type is pvc but no pvc is defined")
		}
		if kmc.Spec.Persistence.PersistentVolumeClaim.Name == "" {
			kmc.Spec.Persistence.PersistentVolumeClaim.Name = kmc.GetVolumeName()
		}

		if kmc.Spec.Persistence.AutoDeletePVCs {
			statefulSet.Spec.PersistentVolumeClaimRetentionPolicy = &apps.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenDeleted: apps.DeletePersistentVolumeClaimRetentionPolicyType,
			}
		}
		statefulSet.Spec.VolumeClaimTemplates = append(statefulSet.Spec.VolumeClaimTemplates, v1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:        kmc.Spec.Persistence.PersistentVolumeClaim.Name,
				Namespace:   kmc.Spec.Persistence.PersistentVolumeClaim.Namespace,
				Labels:      kmc.Spec.Persistence.PersistentVolumeClaim.Labels,
				Annotations: kmc.Spec.Persistence.PersistentVolumeClaim.Annotations,
				Finalizers:  kmc.Spec.Persistence.PersistentVolumeClaim.Finalizers,
			},
			Spec: kmc.Spec.Persistence.PersistentVolumeClaim.Spec,
		})

		statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
			Name:      kmc.Spec.Persistence.PersistentVolumeClaim.Name,
			MountPath: "/var/lib/k0s",
		})
	case "emptyDir":
		fallthrough
	default:
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
	}

	for _, manifest := range kmc.Spec.Manifests {
		statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, manifest)

		statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
			Name:      manifest.Name,
			MountPath: fmt.Sprintf("/var/lib/k0s/manifests/%s", manifest.Name),
			ReadOnly:  true,
		})
	}

	for _, file := range kmc.Spec.Mounts {
		volumeName := strings.Replace(file.Path[1:], "/", "-", -1)
		statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, v1.Volume{Name: volumeName, VolumeSource: file.VolumeSource})

		if file.VolumeSource.ConfigMap != nil || file.VolumeSource.Secret != nil {
			file.ReadOnly = true
		}

		statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
			Name:      volumeName,
			MountPath: file.Path,
			ReadOnly:  file.ReadOnly,
		})
	}

	// Create k0s telemetry config in the configmap and mount it to the controller pod
	// If user disables k0s telemetry this will have not effect.
	cm := &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("kmc-%s-telemetry-config", kmc.Name),
			Namespace: kmc.Namespace,
		},
		Data: map[string]string{
			"configmap.yaml": `
apiVersion: v1
kind: ConfigMap
metadata:
  name: k0s-telemetry
  namespace: kube-system
data:
  provider: "k0smotron"
`,
		},
	}
	_ = kcontrollerutil.SetExternalOwnerReference(kmc, cm, scope.client.Scheme(), scope.externalOwner)

	if err := scope.client.Patch(context.Background(), cm, client.Apply, patchOpts...); err != nil {
		return apps.StatefulSet{}, err
	}
	statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, v1.Volume{
		Name: cm.Name,
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{Name: cm.Name},
			},
		},
	})

	statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
		Name:      cm.Name,
		MountPath: "/var/lib/k0s/manifests/k0s-telemetry",
		ReadOnly:  true,
	})

	_ = kcontrollerutil.SetExternalOwnerReference(kmc, &statefulSet, scope.client.Scheme(), scope.externalOwner)

	// We calculate the hash of the statefulset template and store it in the annotations
	// This is used to detect changes in the statefulset template and trigger a rollout
	// We exclude some fields below from the hash calculation to avoid unnecessary rollouts
	annotationHash := controller.ComputeHash(&statefulSet.Spec.Template, statefulSet.Status.CollisionCount)
	statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe = &v1.Probe{
		InitialDelaySeconds: 60,
		PeriodSeconds:       10,
		FailureThreshold:    15,
		ProbeHandler:        v1.ProbeHandler{Exec: &v1.ExecAction{Command: []string{"k0s", "status"}}},
	}
	statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe = &v1.Probe{
		InitialDelaySeconds: 90,
		FailureThreshold:    10,
		PeriodSeconds:       10,
		ProbeHandler:        v1.ProbeHandler{Exec: &v1.ExecAction{Command: []string{"k0s", "status"}}},
	}

	statefulSet.Annotations = map[string]string{
		statefulSetAnnotation: annotationHash,
	}
	for k, v := range annotations {
		statefulSet.Annotations[k] = v
	}

	return statefulSet, nil
}

// mountSecrets mounts the certificates as secrets to the controller and creates
// an init container that copies the certificates to the correct location
func mountSecrets(kmc *km.Cluster, sfs *apps.StatefulSet) {
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
		case "apiserver-etcd-client":
			projectedSecrets = append(projectedSecrets, v1.VolumeProjection{
				Secret: &v1.SecretProjection{
					LocalObjectReference: v1.LocalObjectReference{Name: cert.Name},
					Items: []v1.KeyToPath{
						{
							Key:  "tls.crt",
							Path: "apiserver-etcd-client.crt",
						},
						{
							Key:  "tls.key",
							Path: "apiserver-etcd-client.key",
						},
					},
				},
			})
		case "etcd":
			projectedSecrets = append(projectedSecrets, v1.VolumeProjection{
				Secret: &v1.SecretProjection{
					LocalObjectReference: v1.LocalObjectReference{Name: cert.Name},
					Items: []v1.KeyToPath{
						{
							Key:  "tls.crt",
							Path: "etcd-ca.crt",
						},
						{
							Key:  "tls.key",
							Path: "etcd-ca.key",
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

	k0sDataVolumeName := kmc.GetVolumeName()
	if kmc.Spec.Persistence.PersistentVolumeClaim != nil && kmc.Spec.Persistence.PersistentVolumeClaim.Name != "" {
		k0sDataVolumeName = kmc.Spec.Persistence.PersistentVolumeClaim.Name
	}

	// We need to copy the certs from the projected volume to the /var/lib/k0s/pki directory
	// Otherwise k0s will trip over the permissions and RO mounts
	sfs.Spec.Template.Spec.InitContainers = append(sfs.Spec.Template.Spec.InitContainers, v1.Container{
		Name:  "certs-init",
		Image: kmc.Spec.GetImage(),
		Command: []string{
			"sh",
			"-c",
			"mkdir -p /var/lib/k0s/pki && rm -rf /var/lib/k0s/pki/server.* && cp /certs-init/*.* /var/lib/k0s/pki/",
		},
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      "certs",
				MountPath: "/certs-init",
			},
			{
				Name:      k0sDataVolumeName,
				MountPath: "/var/lib/k0s",
			},
		},
	})
}

func addMonitoringStack(kmc *km.Cluster, statefulSet *apps.StatefulSet) {
	nginxConfCMName := kmc.GetMonitoringNginxConfigMapName()
	statefulSet.Spec.Template.Spec.Containers = append(statefulSet.Spec.Template.Spec.Containers, v1.Container{
		Name:            "monitoring-agent",
		Image:           kmc.Spec.Monitoring.PrometheusImage,
		ImagePullPolicy: v1.PullIfNotPresent,
		Command:         []string{"prometheus", "--config.file=/prometheus/prometheus.yml"},
		Args:            []string{"--storage.tsdb.retention.size=200MB"},
		Ports: []v1.ContainerPort{{
			Name:          "prometheus",
			Protocol:      v1.ProtocolTCP,
			ContainerPort: int32(9090),
		}},
		VolumeMounts: []v1.VolumeMount{{
			Name:      kmc.GetVolumeName(),
			MountPath: "/var/lib/k0s",
		}, {
			Name:      kmc.GetMonitoringConfigMapName(),
			MountPath: "/prometheus/prometheus.yml",
			SubPath:   "prometheus.yml",
		}},
	}, v1.Container{
		Name:            "monitoring-proxy",
		Image:           kmc.Spec.Monitoring.ProxyImage,
		ImagePullPolicy: v1.PullIfNotPresent,
		Ports: []v1.ContainerPort{{
			Name:          "nginx",
			Protocol:      v1.ProtocolTCP,
			ContainerPort: int32(8090),
		}},
		VolumeMounts: []v1.VolumeMount{{
			Name:      nginxConfCMName,
			MountPath: "/etc/nginx/nginx.conf",
			SubPath:   "nginx.conf",
		}},
	})

	monitoringAnnotations := map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/port":   "8090",
		"prometheus.io/path":   "/metrics",
	}
	if statefulSet.Spec.Template.Annotations == nil {
		statefulSet.Spec.Template.Annotations = make(map[string]string)
	}
	for k, v := range monitoringAnnotations {
		statefulSet.Spec.Template.Annotations[k] = v
	}

	statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, v1.Volume{
		Name: kmc.GetMonitoringConfigMapName(),
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: kmc.GetMonitoringConfigMapName(),
				},
				DefaultMode: &entrypointDefaultMode,
				Items: []v1.KeyToPath{{
					Key:  "prometheus.yml",
					Path: "prometheus.yml",
				}},
			},
		},
	}, v1.Volume{
		Name: nginxConfCMName,
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: kmc.GetMonitoringConfigMapName(),
				},
				DefaultMode: &entrypointDefaultMode,
				Items: []v1.KeyToPath{{
					Key:  "nginx.conf",
					Path: "nginx.conf",
				}},
			},
		},
	})
}

func (scope *kmcScope) reconcileStatefulSet(ctx context.Context, kmc *km.Cluster) error {
	statefulSet, err := scope.generateStatefulSet(kmc)
	if err != nil {
		return fmt.Errorf("failed to generate statefulset: %w", err)
	}

	selector, err := metav1.LabelSelectorAsSelector(statefulSet.Spec.Selector)
	if err != nil {
		return fmt.Errorf("error retrieving StatefulSet labels: %w", err)
	}
	kmc.Status.Selector = selector.String()

	foundStatefulSet, err := scope.clienSet.AppsV1().StatefulSets(statefulSet.Namespace).Get(ctx, statefulSet.Name, metav1.GetOptions{})
	if err != nil && apierrors.IsNotFound(err) {
		kmc.Status.Replicas = 0
		return scope.client.Patch(ctx, &statefulSet, client.Apply, patchOpts...)
	} else if err == nil {
		detectAndSetCurrentClusterVersion(foundStatefulSet, kmc)

		if !isStatefulSetsEqual(&statefulSet, foundStatefulSet) {
			return scope.client.Patch(ctx, &statefulSet, client.Apply, patchOpts...)
		}

		kmc.Status.Replicas = foundStatefulSet.Status.Replicas
		if foundStatefulSet.Status.ReadyReplicas == kmc.Spec.Replicas {
			kmc.Status.Ready = true
		}
	}

	if !isStatefulSetsEqual(&statefulSet, foundStatefulSet) {
		return scope.client.Patch(ctx, &statefulSet, client.Apply, patchOpts...)
	}

	if foundStatefulSet.Status.ReadyReplicas == 0 {
		return fmt.Errorf("%w: no replicas ready yet for statefulset '%s' (%d/%d)", ErrNotReady, foundStatefulSet.GetName(), foundStatefulSet.Status.ReadyReplicas, kmc.Spec.Replicas)
	}

	kmc.Status.Ready = true
	return nil
}

// If the version is empty from the spec, we try to detect it from the statefulset image.
func detectAndSetCurrentClusterVersion(foundStatefulSet *apps.StatefulSet, kmc *km.Cluster) {
	if kmc.Spec.Version == "" {
		imageParts := strings.Split(foundStatefulSet.Spec.Template.Spec.Containers[0].Image, ":")
		if len(imageParts) > 1 && versionRegex.Match([]byte(imageParts[1])) {
			kmc.Spec.Version = imageParts[1]
		}
	}
}

func isStatefulSetsEqual(newSts, oldSts *apps.StatefulSet) bool {
	return *newSts.Spec.Replicas == *oldSts.Spec.Replicas &&
		newSts.Annotations[statefulSetAnnotation] == oldSts.Annotations[statefulSetAnnotation] &&
		reflect.DeepEqual(newSts.Spec.Selector, oldSts.Spec.Selector) &&
		equality.Semantic.DeepDerivative(newSts.Spec.VolumeClaimTemplates, oldSts.Spec.VolumeClaimTemplates)
}
