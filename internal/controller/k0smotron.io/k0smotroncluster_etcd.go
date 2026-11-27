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
	"bytes"
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"text/template"
	"time"

	batchv1 "k8s.io/api/batch/v1"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"

	kcontrollerutil "github.com/k0sproject/k0smotron/v2/internal/controller/util"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
)

var etcdEntrypointScriptTmpl *template.Template

func init() {
	etcdEntrypointScriptTmpl = template.Must(template.New("entrypoint.sh").Parse(etcdEntrypointScriptTemplate))
}

func (scope *kmcScope) reconcileEtcd(ctx context.Context, kmc *km.Cluster, certFingerprint string) (ctrl.Result, error) {
	if kmc.Spec.Storage.Type != km.StorageTypeEtcd {
		return ctrl.Result{}, nil
	}

	if err := scope.reconcileEtcdSvc(ctx, kmc); err != nil {
		return ctrl.Result{}, fmt.Errorf("error reconciling etcd service: %w", err)
	}

	// We can continue reconciling etcd if we need to requeue for statefulset, but if there is an error we should return immediately
	result, err := scope.reconcileEtcdStatefulSet(ctx, kmc, certFingerprint)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error reconciling etcd statefulset: %w", err)
	}
	if kmc.Spec.Storage.Etcd.DefragJob.Enabled {
		if err := scope.reconcileEtcdDefragJob(ctx, kmc); err != nil {
			return ctrl.Result{}, fmt.Errorf("error reconciling etcd defrag job: %w", err)
		}
	}

	return result, nil
}

func (scope *kmcScope) reconcileEtcdSvc(ctx context.Context, kmc *km.Cluster) error {
	// Service selector is immutable after creation. Add app.kubernetes.io/component only to metadata labels.
	selectorLabels := kcontrollerutil.LabelsForEtcdK0smotronCluster(kmc)
	metadataLabels := make(map[string]string, len(selectorLabels)+1)
	maps.Copy(metadataLabels, selectorLabels)
	metadataLabels[kcontrollerutil.ComponentLabel] = kcontrollerutil.ComponentEtcd

	svc := v1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetEtcdServiceName(),
			Namespace:   kmc.Namespace,
			Labels:      metadataLabels,
			Annotations: kcontrollerutil.AnnotationsForK0smotronCluster(kmc),
		},
		Spec: v1.ServiceSpec{
			Type:                     v1.ServiceTypeClusterIP,
			ClusterIP:                v1.ClusterIPNone,
			Selector:                 selectorLabels,
			PublishNotReadyAddresses: true,
			Ports: []v1.ServicePort{
				{
					Name:       "client",
					Port:       2379,
					TargetPort: intstr.FromInt32(2379),
				},
				{
					Name:       "peer",
					Port:       2380,
					TargetPort: intstr.FromInt32(2380),
				},
			},
		},
	}

	_ = kcontrollerutil.SetExternalOwnerReference(kmc, &svc, scope.client.Scheme(), scope.externalOwner)

	return scope.reconcileResource(ctx, kmc, &svc)
}

func (scope *kmcScope) reconcileEtcdDefragJob(ctx context.Context, kmc *km.Cluster) error {
	// Add app.kubernetes.io/component to metadata labels; keep legacy component=etcd from LabelsForEtcdK0smotronCluster.
	selectorLabels := kcontrollerutil.LabelsForEtcdK0smotronCluster(kmc)
	metadataLabels := make(map[string]string, len(selectorLabels)+1)
	maps.Copy(metadataLabels, selectorLabels)
	metadataLabels[kcontrollerutil.ComponentLabel] = kcontrollerutil.ComponentEtcd

	cronJob := batchv1.CronJob{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "CronJob",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetEtcdDefragJobName(),
			Namespace:   kmc.Namespace,
			Labels:      metadataLabels,
			Annotations: kcontrollerutil.AnnotationsForK0smotronCluster(kmc),
		},
		Spec: batchv1.CronJobSpec{
			Schedule:          kmc.Spec.Storage.Etcd.DefragJob.Schedule,
			ConcurrencyPolicy: batchv1.ForbidConcurrent,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: metadataLabels,
						},
						Spec: v1.PodSpec{
							RestartPolicy: v1.RestartPolicyOnFailure,
							Containers: []v1.Container{
								{
									Name:            "etcd-defrag",
									Image:           kmc.Spec.Storage.Etcd.DefragJob.Image,
									ImagePullPolicy: v1.PullIfNotPresent,
									Args: []string{
										fmt.Sprintf("--endpoints=https://%s:2379", kmc.GetEtcdServiceName()),
										"--cacert=/var/lib/k0s/pki/etcd/ca.crt",
										"--cert=/var/lib/k0s/pki/etcd/client.crt",
										"--key=/var/lib/k0s/pki/etcd/client.key",
										"--cluster",
										"--defrag-rule",
										kmc.Spec.Storage.Etcd.DefragJob.Rule,
									},
									VolumeMounts: []v1.VolumeMount{
										{Name: "certs", MountPath: "/var/lib/k0s/pki/etcd/"},
									},
								},
							},
							Volumes: []v1.Volume{{
								Name: "certs",
								VolumeSource: v1.VolumeSource{
									Projected: &v1.ProjectedVolumeSource{
										Sources: []v1.VolumeProjection{
											{
												Secret: &v1.SecretProjection{
													LocalObjectReference: v1.LocalObjectReference{Name: secret.Name(kmc.Name, secret.EtcdCA)},
													Items: []v1.KeyToPath{
														{Key: "tls.crt", Path: "ca.crt"},
														{Key: "tls.key", Path: "ca.key"},
													},
												},
											}, {
												Secret: &v1.SecretProjection{
													LocalObjectReference: v1.LocalObjectReference{Name: secret.Name(kmc.Name, "etcd-server")},
													Items: []v1.KeyToPath{
														{Key: "tls.crt", Path: "client.crt"},
														{Key: "tls.key", Path: "client.key"},
													},
												},
											},
										},
									},
								},
							}},
						},
					},
				},
			},
		},
	}

	_ = kcontrollerutil.SetExternalOwnerReference(kmc, &cronJob, scope.client.Scheme(), scope.externalOwner)

	return scope.reconcileResource(ctx, kmc, &cronJob)
}

func (scope *kmcScope) reconcileEtcdStatefulSet(ctx context.Context, kmc *km.Cluster, certFingerprint string) (ctrl.Result, error) {
	foundStatefulSet, err := scope.clienSet.AppsV1().StatefulSets(kmc.Namespace).Get(ctx, kmc.GetEtcdStatefulSetName(), metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		foundStatefulSet = nil
	}
	joinConfigMap, err := scope.clienSet.CoreV1().ConfigMaps(kmc.Namespace).Get(ctx, kmc.GetEtcdJoinConfigMapName(), metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		joinConfigMap = nil
	}
	wasBootstrapping := joinConfigMap != nil && joinConfigMap.Data[isEtcdBootstrappingKey] == "true"

	desiredReplicas := calculateDesiredEtcdReplicas(kmc)

	replicasToApply := desiredReplicas
	if foundStatefulSet != nil {
		// We calculate the number of replicas to apply when the etcd cluster already exists. In that case, we need to take into account
		// the state of the existing etcd cluster for deciding the number of replicas to apply.
		currentReplicas := *foundStatefulSet.Spec.Replicas
		readyReplicas := foundStatefulSet.Status.ReadyReplicas
		replicasToApply = calculateReplicasToApply(desiredReplicas, currentReplicas, readyReplicas, wasBootstrapping)
	}

	isBootstrapping := calculateIsEtcdBootstrappingKey(foundStatefulSet, joinConfigMap)

	joinConfigMap = generateEtcdJoinConfigMap(kmc, isBootstrapping)
	_ = kcontrollerutil.SetExternalOwnerReference(kmc, joinConfigMap, scope.client.Scheme(), scope.externalOwner)
	if err := scope.reconcileResource(ctx, kmc, joinConfigMap); err != nil {
		return ctrl.Result{}, fmt.Errorf("error reconciling etcd join configmap: %w", err)
	}

	effectiveFingerprint := effectiveCertFingerprint(foundStatefulSet, certFingerprint, scope.currentReconcileState.certificatesRenewed)

	statefulSet := generateEtcdStatefulSet(kmc, foundStatefulSet, replicasToApply, effectiveFingerprint)
	_ = kcontrollerutil.SetExternalOwnerReference(kmc, &statefulSet, scope.client.Scheme(), scope.externalOwner)
	err = scope.reconcileResource(ctx, kmc, &statefulSet)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error reconciling etcd statefulset: %w", err)
	}

	if replicasToApply < desiredReplicas {
		// Members are added one at a time so requeue until the desired number of etcd replicas is applied.
		scope.currentReconcileState.etcdScaleUpPending = true
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}
	return ctrl.Result{}, nil
}

const (
	etcdJoinScriptKey      = "join.sh"
	isEtcdBootstrappingKey = "bootstrap"
	etcdJoinMountPath      = "/etcd-join"
)

func calculateIsEtcdBootstrappingKey(sts *apps.StatefulSet, joinConfigMap *v1.ConfigMap) bool {
	if sts == nil || sts.GetName() == "" {
		// No StatefulSet, no members: there is no cluster to join.
		return true
	}
	if joinConfigMap == nil {
		// StatefulSet created by a version without this ConfigMap: the cluster already exists.
		return false
	}
	if joinConfigMap.Data[isEtcdBootstrappingKey] == "false" {
		// The cluster has formed once. Even if every member goes down, a member without data must
		// join it, not bootstrap a new one.
		return false
	}
	// If there is any replica ready, we consider the etcd to be created.
	return sts.Status.ReadyReplicas == 0
}

func generateEtcdJoinConfigMap(kmc *km.Cluster, isEtcdBootstrapping bool) *v1.ConfigMap {
	return &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetEtcdJoinConfigMapName(),
			Namespace:   kmc.Namespace,
			Labels:      kcontrollerutil.LabelsForK0smotronComponent(kmc, kcontrollerutil.ComponentEtcd),
			Annotations: kcontrollerutil.AnnotationsForK0smotronCluster(kmc),
		},
		Data: map[string]string{
			etcdJoinScriptKey:      etcdJoinScript,
			isEtcdBootstrappingKey: strconv.FormatBool(isEtcdBootstrapping),
		},
	}
}

func generateEtcdStatefulSet(kmc *km.Cluster, existingSts *apps.StatefulSet, replicas int32, certFingerprint string) apps.StatefulSet {
	// StatefulSet selector is immutable after creation. Add app.kubernetes.io/component only to metadata and template labels.
	selectorLabels := kcontrollerutil.LabelsForEtcdK0smotronCluster(kmc)
	labels := make(map[string]string, len(selectorLabels)+1)
	maps.Copy(labels, selectorLabels)
	labels[kcontrollerutil.ComponentLabel] = kcontrollerutil.ComponentEtcd

	size := kmc.Spec.Storage.Etcd.Persistence.Size

	if size.IsZero() {
		size = resource.MustParse("1Gi")
	}
	pvc := v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "etcd-data",
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
			Resources: v1.VolumeResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceStorage: size,
				},
			},
		},
	}
	if kmc.Spec.Storage.Etcd.Persistence.StorageClass != "" {
		pvc.Spec.StorageClassName = &kmc.Spec.Storage.Etcd.Persistence.StorageClass
	}

	var etcdEntrypointScriptBuf bytes.Buffer
	_ = etcdEntrypointScriptTmpl.Execute(&etcdEntrypointScriptBuf, struct {
		Args []string
	}{
		Args: kmc.Spec.Storage.Etcd.Args,
	})

	statefulSet := apps.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        kmc.GetEtcdStatefulSetName(),
			Namespace:   kmc.Namespace,
			Labels:      labels,
			Annotations: kcontrollerutil.AnnotationsForK0smotronCluster(kmc),
		},
		Spec: apps.StatefulSetSpec{
			ServiceName: kmc.GetEtcdServiceName(),
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Replicas:            &replicas,
			PodManagementPolicy: apps.ParallelPodManagement,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: certificateAnnotations(certFingerprint),
				},
				Spec: v1.PodSpec{
					AutomountServiceAccountToken: new(false),
					Affinity: &v1.Affinity{PodAntiAffinity: &v1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []v1.WeightedPodAffinityTerm{
							{
								Weight: 100,
								PodAffinityTerm: v1.PodAffinityTerm{
									TopologyKey: "topology.kubernetes.io/zone",
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: kcontrollerutil.LabelsForEtcdK0smotronCluster(kmc),
									},
								},
							},
							{
								Weight: 50,
								PodAffinityTerm: v1.PodAffinityTerm{
									TopologyKey: "kubernetes.io/hostname",
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: kcontrollerutil.LabelsForEtcdK0smotronCluster(kmc),
									},
								},
							},
						},
					}},
					Volumes: []v1.Volume{
						{
							Name: "certs",
							VolumeSource: v1.VolumeSource{
								Projected: &v1.ProjectedVolumeSource{
									Sources: []v1.VolumeProjection{
										{
											Secret: &v1.SecretProjection{
												LocalObjectReference: v1.LocalObjectReference{Name: secret.Name(kmc.Name, secret.EtcdCA)},
												Items: []v1.KeyToPath{
													{Key: "tls.crt", Path: "ca.crt"},
													{Key: "tls.key", Path: "ca.key"},
												},
											},
										}, {
											Secret: &v1.SecretProjection{
												LocalObjectReference: v1.LocalObjectReference{Name: secret.Name(kmc.Name, "etcd-server")},
												Items: []v1.KeyToPath{
													{Key: "tls.crt", Path: "server.crt"},
													{Key: "tls.key", Path: "server.key"},
												},
											},
										}, {
											Secret: &v1.SecretProjection{
												LocalObjectReference: v1.LocalObjectReference{Name: secret.Name(kmc.Name, "etcd-peer")},
												Items: []v1.KeyToPath{
													{Key: "tls.crt", Path: "peer.crt"},
													{Key: "tls.key", Path: "peer.key"},
												},
											},
										},
									},
								},
							},
						},
						{
							Name: "join",
							VolumeSource: v1.VolumeSource{
								ConfigMap: &v1.ConfigMapVolumeSource{
									LocalObjectReference: v1.LocalObjectReference{Name: kmc.GetEtcdJoinConfigMapName()},
								},
							},
						},
					},
					SecurityContext: &v1.PodSecurityContext{
						FSGroup: new(int64(1001)),
					},
					InitContainers: generateEtcdInitContainers(kmc, existingSts),
					Containers: []v1.Container{{
						Name:            "etcd",
						Image:           kmc.Spec.Storage.Etcd.Image,
						ImagePullPolicy: v1.PullIfNotPresent,
						Command:         []string{"/bin/bash"},
						Args:            []string{"-c", etcdEntrypointScriptBuf.String()},
						Env: []v1.EnvVar{
							{Name: "SVC_NAME", Value: kmc.GetEtcdServiceName()},
							{Name: "ETCDCTL_ENDPOINTS", Value: fmt.Sprintf("https://%s:2379", kmc.GetEtcdServiceName())},
							{Name: "ETCDCTL_CACERT", Value: "/var/lib/k0s/pki/etcd/ca.crt"},
							{Name: "ETCDCTL_CERT", Value: "/var/lib/k0s/pki/etcd/server.crt"},
							{Name: "ETCDCTL_KEY", Value: "/var/lib/k0s/pki/etcd/server.key"},
							{Name: "ETCD_INITIAL_CLUSTER", Value: initialCluster(kmc, replicas)},
						},
						Resources: kmc.Spec.Storage.Etcd.Resources,
						ReadinessProbe: &v1.Probe{
							ProbeHandler: v1.ProbeHandler{
								Exec: &v1.ExecAction{
									// Probe this member only. Going through the Service would report the health of
									// whichever member answers.
									Command: []string{"sh", "-c", "ETCDCTL_ENDPOINTS=https://${HOSTNAME}.${SVC_NAME}:2379 etcdctl endpoint health"},
								},
							},
						},
						Ports: []v1.ContainerPort{
							{
								Name:          "client",
								Protocol:      v1.ProtocolTCP,
								ContainerPort: 2379,
							},
							{
								Name:          "peer",
								Protocol:      v1.ProtocolTCP,
								ContainerPort: 2380,
							},
						},
						VolumeMounts: []v1.VolumeMount{
							{Name: "certs", MountPath: "/var/lib/k0s/pki/etcd/"},
							{Name: "etcd-data", MountPath: "/var/lib/k0s/etcd"},
						},
					}},
				},
			},
			VolumeClaimTemplates: []v1.PersistentVolumeClaim{pvc},
		},
	}

	if kmc.Spec.ServiceAccount != "" {
		statefulSet.Spec.Template.Spec.ServiceAccountName = kmc.Spec.ServiceAccount
	}

	if kmc.Spec.TopologySpreadConstraints != nil {
		statefulSet.Spec.Template.Spec.TopologySpreadConstraints = kmc.Spec.TopologySpreadConstraints
	}

	if kmc.Spec.Storage.Etcd.AutoDeletePVCs {
		statefulSet.Spec.PersistentVolumeClaimRetentionPolicy = &apps.StatefulSetPersistentVolumeClaimRetentionPolicy{
			WhenDeleted: apps.DeletePersistentVolumeClaimRetentionPolicyType,
		}
	}

	return statefulSet
}

func initialCluster(kmc *km.Cluster, replicas int32) string {
	var members []string
	stsName := kmc.GetEtcdStatefulSetName()
	svcName := kmc.GetEtcdServiceName()
	for i := range replicas {
		members = append(members, fmt.Sprintf("%s-%d=https://%s-%d.%s:2380", stsName, i, stsName, i, svcName))
	}
	return strings.Join(members, ",")
}

func generateEtcdInitContainers(kmc *km.Cluster, existingSts *apps.StatefulSet) []v1.Container {
	checkImage := kmc.Spec.GetImage()
	if existingSts != nil {
		for _, c := range existingSts.Spec.Template.Spec.InitContainers {
			if c.Name == "dns-check" {
				checkImage = c.Image
				break
			}
		}
	}
	return []v1.Container{
		{
			// Wait for the pods dns name is resolvable, since it takes some time after the pod is created
			// and etcd tries to connect to the other members using the dns names
			Name:            "dns-check",
			Image:           checkImage,
			ImagePullPolicy: v1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-c"},
			Args:            []string{"getent ahostsv4 ${HOSTNAME}.${SVC_NAME}." + kmc.Namespace + ".svc"},
			Env: []v1.EnvVar{
				{Name: "SVC_NAME", Value: kmc.GetEtcdServiceName()},
			},
		},
		{
			Name:            "init",
			Image:           kmc.Spec.Storage.Etcd.Image,
			ImagePullPolicy: v1.PullIfNotPresent,
			Command:         []string{"/bin/bash", etcdJoinMountPath + "/" + etcdJoinScriptKey},
			Env: []v1.EnvVar{
				{Name: "SVC_NAME", Value: kmc.GetEtcdServiceName()},
				{Name: "ETCDCTL_API", Value: "3"},
				{Name: "ETCDCTL_CACERT", Value: "/var/lib/k0s/pki/etcd/ca.crt"},
				{Name: "ETCDCTL_CERT", Value: "/var/lib/k0s/pki/etcd/server.crt"},
				{Name: "ETCDCTL_KEY", Value: "/var/lib/k0s/pki/etcd/server.key"},
			},
			VolumeMounts: []v1.VolumeMount{
				{Name: "certs", MountPath: "/var/lib/k0s/pki/etcd/"},
				{Name: "etcd-data", MountPath: "/var/lib/k0s/etcd"},
				{Name: "join", MountPath: etcdJoinMountPath, ReadOnly: true},
			},
		},
	}
}

func calculateDesiredEtcdReplicas(kmc *km.Cluster) int32 {
	desiredReplicas := int32(1)
	if kmc.Spec.Replicas > 1 {
		desiredReplicas = kmc.Spec.Replicas
		if kmc.Spec.Replicas%2 == 0 {
			desiredReplicas = kmc.Spec.Replicas + 1
		}
	}

	return desiredReplicas
}

func calculateReplicasToApply(desired, current, ready int32, isEtcdBootstrapping bool) int32 {
	if isEtcdBootstrapping {
		// Do not scale while etcd is bootstrapping. Adding new members during a bootstrapping process
		// will differ from the initial cluster formation calculating a new Cluster ID for the added member, which
		// will create a second cluster instead of joining the existing one.
		return current
	}

	if ready < current {
		// Do not scale up if not all current members are ready
		return current
	}

	if desired > current {
		// Scale up etcd by one replica at a time
		return current + 1
	}

	// We do not scale etcd when:
	// - The desired replicas are less than the current replicas (scale down is not allowed)
	// - The desired replicas are equal to the current replicas (no scaling needed)
	return current
}

const etcdEntrypointScriptTemplate = `

export ETCD_INITIAL_CLUSTER_STATE="new"
if [[ -f /var/lib/k0s/etcd/existing ]]; then
  export ETCD_INITIAL_CLUSTER_STATE="existing"
fi

etcd --name ${HOSTNAME} \
  --listen-peer-urls=https://0.0.0.0:2380 \
  --listen-client-urls=https://0.0.0.0:2379 \
  --advertise-client-urls=https://${HOSTNAME}.${SVC_NAME}:2379 \
  --initial-advertise-peer-urls=https://${HOSTNAME}.${SVC_NAME}:2380 \
  --client-cert-auth=true \
  --tls-min-version=TLS1.2 \
  --trusted-ca-file=/var/lib/k0s/pki/etcd/ca.crt \
  --cert-file=/var/lib/k0s/pki/etcd/server.crt \
  --key-file=/var/lib/k0s/pki/etcd/server.key \
  --peer-trusted-ca-file=/var/lib/k0s/pki/etcd/ca.crt \
  --peer-key-file=/var/lib/k0s/pki/etcd/peer.key \
  --peer-cert-file=/var/lib/k0s/pki/etcd/peer.crt \
  --peer-client-cert-auth=true \
  --enable-pprof=false \
  --auto-compaction-mode=periodic \
  --auto-compaction-retention=5m \
  --snapshot-count=10000 \
{{- range $arg := .Args }}
  {{ $arg }} \
{{- end }}
  --data-dir=/var/lib/k0s/etcd
`

// etcdJoinScript runs in the init container of every etcd member. It's porpuse is to ensure that a new etcd member
// joins the existing cluster correctly. It skips joining in the following cases:
// 1. The cluster is being bootstrapped (mount path contains the bootstrap key with value "true")
// 2. The member has data (snapshot db exists)
// 3. The member has already been added to the cluster (sentinel file exists)
const etcdJoinScript = `#!/bin/bash

set -eu

if [[ "$(cat ` + etcdJoinMountPath + "/" + isEtcdBootstrappingKey + `)" == "true" ]]; then
    echo "Cluster is being bootstrapped. Skipping join"
    exit 0
fi

if [[ -f /var/lib/k0s/etcd/member/snap/db ]]; then
    echo "Snapshot db exists, the member has data. Skipping join"
    exit 0
fi

if [[ -f /var/lib/k0s/etcd/existing ]]; then
    # Sentinel file exists, indicating the member has already been added to the cluster.
    echo "Member already added to the cluster. Skipping join"
    exit 0
fi

# Increase the chances of successfully joining the cluster by setting bigger timeouts for etcdctl commands.
export ETCDCTL_DIAL_TIMEOUT=5s
export ETCDCTL_COMMAND_TIMEOUT=10s

export ETCDCTL_ENDPOINTS="https://${SVC_NAME}:2379"

PEER_URL="https://${HOSTNAME}.${SVC_NAME}:2380"

# At this point, the cluster already exists, so never 'exit 0' without joining. If the members
# cannot be listed (e.g. no quorum), fail and let kubelet retry the init container.
if ! members=$(etcdctl member list -w simple); then
    echo "Failed to list members, will retry"
    exit 1
fi

# Match by peer URL: a member that was added but never started is listed without a name.
MEMBER_ID=$(echo "${members}" | awk -F', ' -v url="${PEER_URL}" '$4 == url { print $1 }')
if [[ -n "${MEMBER_ID}" ]]; then
    echo "Member ${HOSTNAME} (${MEMBER_ID}) is already registered, removing it"
    etcdctl member remove "${MEMBER_ID}"
fi

echo "Adding member ${HOSTNAME} to the cluster"
etcdctl member add "${HOSTNAME}" --peer-urls "${PEER_URL}"
touch /var/lib/k0s/etcd/existing
`
