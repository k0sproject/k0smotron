//go:build envtest

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
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	bootstrapv2 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	cpv1beta2 "github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestReconcileMachinesScaleUp(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-machine-scale-up")
	require.NoError(t, err)

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))
	require.NoError(t, testEnv.Create(ctx, gmt))

	desiredReplicas := 5
	kcp.Spec.Replicas = int32(desiredReplicas)
	require.NoError(t, testEnv.Create(ctx, kcp))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(kcp, gmt, cluster, ns)

	kcpOwnerRef := *metav1.NewControllerRef(kcp, cpv1beta2.GroupVersion.WithKind("K0sControlPlane"))

	k0sConfigAnnotationValue, err := generateK0sConfigAnnotationValueForMachine(kcp, fmt.Sprintf("%s-%d", kcp.Name, 0))
	require.NoError(t, err)

	firstMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: kcp.GetName(),
			},
			Annotations: map[string]string{
				cpv1beta2.MachineK0sConfigAnnotation: k0sConfigAnnotationValue,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachineTemplate",
				Name:     gmt.GetName(),
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     fmt.Sprintf("%s-%d", kcp.Name, 0),
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sControllerConfig",
				},
			},
		},
	}
	firstMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	require.NoError(t, testEnv.Create(ctx, firstMachineRelatedToControlPlane))
	firstMachineControllerConfig := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 0),
			Namespace: ns.Name,
			Labels:    controlPlaneCommonLabelsForCluster(kcp, cluster.Name),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "cluster.x-k8s.io/v1beta1",
				Kind:               "Machine",
				Name:               firstMachineRelatedToControlPlane.GetName(),
				UID:                firstMachineRelatedToControlPlane.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}},
		},
	}
	require.NoError(t, testEnv.Create(ctx, firstMachineControllerConfig))

	k0sConfigAnnotationValue, err = generateK0sConfigAnnotationValueForMachine(kcp, fmt.Sprintf("%s-%d", kcp.Name, 1))
	require.NoError(t, err)

	secondMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 1),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: kcp.GetName(),
			},
			Annotations: map[string]string{
				cpv1beta2.MachineK0sConfigAnnotation: k0sConfigAnnotationValue,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachineTemplate",
				Name:     gmt.GetName(),
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     fmt.Sprintf("%s-%d", kcp.Name, 1),
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sControllerConfig",
				},
			},
		},
	}
	secondMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	require.NoError(t, testEnv.Create(ctx, secondMachineRelatedToControlPlane))
	secondMachineControllerConfig := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 1),
			Namespace: ns.Name,
			Labels:    controlPlaneCommonLabelsForCluster(kcp, cluster.Name),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "cluster.x-k8s.io/v1beta1",
				Kind:               "Machine",
				Name:               secondMachineRelatedToControlPlane.GetName(),
				UID:                secondMachineRelatedToControlPlane.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}},
		},
	}
	require.NoError(t, testEnv.Create(ctx, secondMachineControllerConfig))

	machineNotRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "external-machine",
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachine",
				Name:     "machine-for-controller-infra",
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     "machine-for-controller-bootstrap",
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sControllerConfig",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, machineNotRelatedToControlPlane))

	frt := &fakeRoundTripper{}
	fakeClient := &restfake.RESTClient{
		Client: restfake.CreateHTTPClient(frt.run),
	}

	restClient, _ := rest.RESTClientFor(&rest.Config{
		ContentConfig: rest.ContentConfig{
			NegotiatedSerializer: scheme.Codecs,
			GroupVersion:         &metav1.SchemeGroupVersion,
		},
	})
	restClient.Client = fakeClient.Client

	clientSet, err := kubernetes.NewForConfig(testEnv.Config)
	require.NoError(t, err)

	r := &K0sController{
		Client:                    testEnv,
		ClientSet:                 clientSet,
		workloadClusterKubeClient: kubernetes.New(restClient),
	}

	require.Eventually(t, func() bool {
		controlplane, err := r.retrieveControlPlaneState(ctx, cluster, kcp)
		require.NoError(t, err)

		res, err := r.reconcileMachines(ctx, controlplane)
		return err == nil && res.IsZero()
	}, 5*time.Second, 100*time.Millisecond)

	machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
	require.NoError(t, err)
	require.Len(t, machines, desiredReplicas)
	for _, m := range machines {
		expectedLabels := map[string]string{
			clusterv1.ClusterNameLabel:             cluster.GetName(),
			clusterv1.MachineControlPlaneLabel:     "true",
			clusterv1.MachineControlPlaneNameLabel: kcp.GetName(),
		}
		require.Equal(t, expectedLabels, m.Labels)
		require.True(t, metav1.IsControlledBy(m, kcp))
		require.Equal(t, kcp.Spec.Version, m.Spec.Version)
	}
}

func TestReconcileMachinesScaleDown(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-machines-scale-down")
	require.NoError(t, err)

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))
	require.NoError(t, testEnv.Create(ctx, gmt))

	desiredReplicas := 1
	kcp.Spec.Replicas = int32(desiredReplicas)
	require.NoError(t, testEnv.Create(ctx, kcp))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(kcp, gmt, cluster, ns)

	kcpOwnerRef := *metav1.NewControllerRef(kcp, cpv1beta2.GroupVersion.WithKind("K0sControlPlane"))

	frt := &fakeRoundTripper{}
	fakeClient := &restfake.RESTClient{
		Client: restfake.CreateHTTPClient(frt.run),
	}

	restClient, _ := rest.RESTClientFor(&rest.Config{
		ContentConfig: rest.ContentConfig{
			NegotiatedSerializer: scheme.Codecs,
			GroupVersion:         &metav1.SchemeGroupVersion,
		},
	})
	restClient.Client = fakeClient.Client

	k0sConfigAnnotationValue, err := generateK0sConfigAnnotationValueForMachine(kcp, fmt.Sprintf("%s-%d", kcp.Name, 0))
	require.NoError(t, err)

	firstMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: kcp.GetName(),
			},
			Annotations: map[string]string{
				cpv1beta2.MachineK0sConfigAnnotation: k0sConfigAnnotationValue,
			},
			OwnerReferences: []metav1.OwnerReference{kcpOwnerRef},
			Finalizers:      []string{clusterv1.MachineFinalizer},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachineTemplate",
				Name:     gmt.GetName(),
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     fmt.Sprintf("%s-%d", kcp.Name, 0),
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sControllerConfig",
				},
			},
		},
	}
	firstMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	require.NoError(t, testEnv.Create(ctx, firstMachineRelatedToControlPlane))
	firstControllerConfig := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 0),
			Namespace: ns.Name,
			Labels:    controlPlaneCommonLabelsForCluster(kcp, cluster.Name),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "cluster.x-k8s.io/v1beta1",
				Kind:               "Machine",
				Name:               firstMachineRelatedToControlPlane.GetName(),
				UID:                firstMachineRelatedToControlPlane.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}},
		},
	}
	require.NoError(t, testEnv.Create(ctx, firstControllerConfig))

	k0sConfigAnnotationValue, err = generateK0sConfigAnnotationValueForMachine(kcp, fmt.Sprintf("%s-%d", kcp.Name, 1))
	require.NoError(t, err)

	secondMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 1),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: kcp.GetName(),
			},
			Annotations: map[string]string{
				cpv1beta2.MachineK0sConfigAnnotation: k0sConfigAnnotationValue,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachineTemplate",
				Name:     gmt.GetName(),
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     fmt.Sprintf("%s-%d", kcp.Name, 1),
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sControllerConfig",
				},
			},
		},
	}
	secondMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	require.NoError(t, testEnv.Create(ctx, secondMachineRelatedToControlPlane))
	secondControllerConfig := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 1),
			Namespace: ns.Name,
			Labels:    controlPlaneCommonLabelsForCluster(kcp, cluster.Name),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "cluster.x-k8s.io/v1beta1",
				Kind:               "Machine",
				Name:               secondMachineRelatedToControlPlane.GetName(),
				UID:                secondMachineRelatedToControlPlane.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}},
		},
	}
	require.NoError(t, testEnv.Create(ctx, secondControllerConfig))

	k0sConfigAnnotationValue, err = generateK0sConfigAnnotationValueForMachine(kcp, fmt.Sprintf("%s-%d", kcp.Name, 2))
	require.NoError(t, err)

	thirdMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 2),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: kcp.GetName(),
			},
			Annotations: map[string]string{
				cpv1beta2.MachineK0sConfigAnnotation: k0sConfigAnnotationValue,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachineTemplate",
				Name:     gmt.GetName(),
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     fmt.Sprintf("%s-%d", kcp.Name, 2),
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sControllerConfig",
				},
			},
		},
	}
	thirdMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	require.NoError(t, testEnv.Create(ctx, thirdMachineRelatedToControlPlane))
	thirdControllerConfig := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 2),
			Namespace: ns.Name,
			Labels:    controlPlaneCommonLabelsForCluster(kcp, cluster.Name),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "cluster.x-k8s.io/v1beta1",
				Kind:               "Machine",
				Name:               thirdMachineRelatedToControlPlane.GetName(),
				UID:                thirdMachineRelatedToControlPlane.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}},
		},
	}
	require.NoError(t, testEnv.Create(ctx, thirdControllerConfig))

	machineNotRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "external-machine",
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachine",
				Name:     gmt.GetName(),
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     "external-machine-bootstrap",
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sControllerConfig",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, machineNotRelatedToControlPlane))
	notRelatedControllerConfig := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "external-machine-bootstrap",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "cluster.x-k8s.io/v1beta1",
				Kind:               "Machine",
				Name:               machineNotRelatedToControlPlane.GetName(),
				UID:                machineNotRelatedToControlPlane.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}},
		},
	}
	require.NoError(t, testEnv.Create(ctx, notRelatedControllerConfig))

	clientSet, err := kubernetes.NewForConfig(testEnv.Config)
	require.NoError(t, err)

	r := &K0sController{
		Client:                    testEnv,
		ClientSet:                 clientSet,
		workloadClusterKubeClient: kubernetes.New(restClient),
	}

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		controlplane, err := r.retrieveControlPlaneState(ctx, cluster, kcp)
		require.NoError(t, err)

		_, err = r.reconcileMachines(ctx, controlplane)
		assert.NoError(c, err)

		machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
		assert.NoError(c, err)
		assert.Len(c, machines, desiredReplicas)

		for _, m := range machines {
			expectedLabels := map[string]string{
				clusterv1.ClusterNameLabel:             cluster.GetName(),
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: kcp.GetName(),
			}
			assert.Equal(c, expectedLabels, m.Labels)
			assert.True(c, metav1.IsControlledBy(m, kcp))
			assert.Equal(c, kcp.Spec.Version, m.Spec.Version)
		}
	}, 10*time.Second, 100*time.Millisecond)
}

func TestReconcileMachinesSyncOldMachines(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-machines-sync-old-machines")
	require.NoError(t, err)

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))
	require.NoError(t, testEnv.Create(ctx, gmt))

	desiredReplicas := 3
	kcp.Spec.Replicas = int32(desiredReplicas)
	require.NoError(t, testEnv.Create(ctx, kcp))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(kcp, gmt, cluster, ns)

	kcpOwnerRef := *metav1.NewControllerRef(kcp, cpv1beta2.GroupVersion.WithKind("K0sControlPlane"))

	frt := &fakeRoundTripper{}
	fakeClient := &restfake.RESTClient{
		Client: restfake.CreateHTTPClient(frt.run),
	}

	restClient, _ := rest.RESTClientFor(&rest.Config{
		ContentConfig: rest.ContentConfig{
			NegotiatedSerializer: scheme.Codecs,
			GroupVersion:         &metav1.SchemeGroupVersion,
		},
	})
	restClient.Client = fakeClient.Client

	clientSet, err := kubernetes.NewForConfig(testEnv.Config)
	require.NoError(t, err)

	r := &K0sController{
		Client:                    testEnv,
		workloadClusterKubeClient: kubernetes.New(restClient),
		ClientSet:                 clientSet,
	}

	k0sConfigAnnotationValue, err := generateK0sConfigAnnotationValueForMachine(kcp, fmt.Sprintf("%s-%d", kcp.Name, 0))
	require.NoError(t, err)

	firstMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: kcp.GetName(),
			},
			Annotations: map[string]string{
				cpv1beta2.MachineK0sConfigAnnotation: k0sConfigAnnotationValue,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.29.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachineTemplate",
				Name:     gmt.GetName(),
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     fmt.Sprintf("%s-%d", kcp.Name, 0),
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sControllerConfig",
				},
			},
		},
	}
	firstMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	require.NoError(t, testEnv.Create(ctx, firstMachineRelatedToControlPlane))
	firstControllerConfig := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 0),
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "cluster.x-k8s.io/v1beta1",
				Kind:               "Machine",
				Name:               firstMachineRelatedToControlPlane.GetName(),
				UID:                firstMachineRelatedToControlPlane.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}},
		},
	}
	require.NoError(t, testEnv.Create(ctx, firstControllerConfig))

	k0sConfigAnnotationValue, err = generateK0sConfigAnnotationValueForMachine(kcp, fmt.Sprintf("%s-%d", kcp.Name, 1))
	require.NoError(t, err)

	secondMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 1),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: kcp.GetName(),
			},
			Annotations: map[string]string{
				cpv1beta2.MachineK0sConfigAnnotation: k0sConfigAnnotationValue,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachineTemplate",
				Name:     gmt.GetName(),
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     fmt.Sprintf("%s-%d", kcp.Name, 1),
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sControllerConfig",
				},
			},
		},
	}
	secondMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	require.NoError(t, testEnv.Create(ctx, secondMachineRelatedToControlPlane))
	secondControllerConfig := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 1),
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "cluster.x-k8s.io/v1beta1",
				Kind:               "Machine",
				Name:               secondMachineRelatedToControlPlane.GetName(),
				UID:                secondMachineRelatedToControlPlane.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}},
		},
	}
	require.NoError(t, testEnv.Create(ctx, secondControllerConfig))

	k0sConfigAnnotationValue, err = generateK0sConfigAnnotationValueForMachine(kcp, fmt.Sprintf("%s-%d", kcp.Name, 2))
	require.NoError(t, err)

	thirdMachineRelatedToControlPlane := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 2),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: kcp.GetName(),
			},
			Annotations: map[string]string{
				cpv1beta2.MachineK0sConfigAnnotation: k0sConfigAnnotationValue,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.29.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachineTemplate",
				Name:     gmt.GetName(),
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     fmt.Sprintf("%s-%d", kcp.Name, 2),
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sControllerConfig",
				},
			},
		},
	}
	thirdMachineRelatedToControlPlane.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	require.NoError(t, testEnv.Create(ctx, thirdMachineRelatedToControlPlane))
	thirdControllerConfig := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, 2),
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "cluster.x-k8s.io/v1beta1",
				Kind:               "Machine",
				Name:               thirdMachineRelatedToControlPlane.GetName(),
				UID:                thirdMachineRelatedToControlPlane.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}},
		},
	}
	require.NoError(t, testEnv.Create(ctx, thirdControllerConfig))

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		controlplane, err := r.retrieveControlPlaneState(ctx, cluster, kcp)
		require.NoError(t, err)

		_, err = r.reconcileMachines(ctx, controlplane)
		assert.NoError(c, err)

		machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
		assert.NoError(c, err)
		assert.Len(c, machines, desiredReplicas)

		for _, m := range machines {
			expectedLabels := map[string]string{
				clusterv1.ClusterNameLabel:             cluster.GetName(),
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: kcp.GetName(),
			}
			assert.Equal(c, expectedLabels, m.Labels)
			assert.True(c, metav1.IsControlledBy(m, kcp))
			assert.Equal(c, kcp.Spec.Version, m.Spec.Version)
		}
	}, 5*time.Second, 100*time.Millisecond)
}

// TestReconcileMachinesNoOpWhenAtDesiredState verifies that reconcileMachines is idempotent:
// when the number of up-to-date machines already equals Spec.Replicas, it returns a zero
// result without creating or deleting any machine.
func TestReconcileMachinesNoOpWhenAtDesiredState(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-machines-noop")
	require.NoError(t, err)

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	kcp.Spec.Replicas = 3
	require.NoError(t, testEnv.Create(ctx, cluster))
	require.NoError(t, testEnv.Create(ctx, gmt))
	require.NoError(t, testEnv.Create(ctx, kcp))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(kcp, gmt, cluster, ns)

	m0, cfg0 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 0), ns.Name, cluster, kcp, gmt, kcp.Spec.Version)
	m1, cfg1 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 1), ns.Name, cluster, kcp, gmt, kcp.Spec.Version)
	m2, cfg2 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 2), ns.Name, cluster, kcp, gmt, kcp.Spec.Version)
	defer func() {
		require.NoError(t, testEnv.Cleanup(ctx, m0, cfg0, m1, cfg1, m2, cfg2))
	}()

	r := buildTestController(t, nil)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		controlplane, err := r.retrieveControlPlaneState(ctx, cluster, kcp)
		require.NoError(t, err)

		res, err := r.reconcileMachines(ctx, controlplane)
		assert.NoError(c, err)
		assert.True(c, res.IsZero(), "expected zero result, got %v", res)

		machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
		assert.NoError(c, err)
		assert.Len(c, machines, 3)
	}, 10*time.Second, 100*time.Millisecond)
}

// TestReconcileMachinesScaleDownProtectsLastReplica verifies that when the only existing
// machine is outdated (version mismatch), the controller scales UP to create a replacement
// rather than deleting the existing machine, which would leave the control plane with zero nodes.
func TestReconcileMachinesScaleDownProtectsLastReplica(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-machines-protect-last-replica")
	require.NoError(t, err)

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	kcp.Spec.Replicas = 1
	require.NoError(t, testEnv.Create(ctx, cluster))
	require.NoError(t, testEnv.Create(ctx, gmt))
	require.NoError(t, testEnv.Create(ctx, kcp))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(kcp, gmt, cluster, ns)

	// Single machine with old version: scaleDown would leave 0 machines, so it must be blocked.
	m0, cfg0 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 0), ns.Name, cluster, kcp, gmt, "v1.29.0")
	defer func() {
		require.NoError(t, testEnv.Cleanup(ctx, m0, cfg0))
	}()

	r := buildTestController(t, nil)

	// The controller must scale up (add a spare) instead of deleting the only existing machine.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		controlplane, err := r.retrieveControlPlaneState(ctx, cluster, kcp)
		require.NoError(t, err)

		_, err = r.reconcileMachines(ctx, controlplane)
		assert.NoError(c, err)

		machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
		assert.NoError(c, err)
		// A second machine should have been created; the outdated one must not have been deleted.
		assert.Len(c, machines, 2)
	}, 10*time.Second, 100*time.Millisecond)
}

// TestReconcileMachinesDeleteFirstStrategy verifies the UpdateRecreateDeleteFirst strategy:
// when all desired replicas are present but one is outdated, the controller deletes the
// outdated machine first (scale down) before creating a replacement (scale up).
func TestReconcileMachinesDeleteFirstStrategy(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-machines-delete-first")
	require.NoError(t, err)

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	kcp.Spec.Replicas = 3
	kcp.Spec.UpdateStrategy = cpv1beta2.UpdateRecreateDeleteFirst
	require.NoError(t, testEnv.Create(ctx, cluster))
	require.NoError(t, testEnv.Create(ctx, gmt))
	require.NoError(t, testEnv.Create(ctx, kcp))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(kcp, gmt, cluster, ns)

	m0, cfg0 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 0), ns.Name, cluster, kcp, gmt, kcp.Spec.Version)
	m1, cfg1 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 1), ns.Name, cluster, kcp, gmt, kcp.Spec.Version)
	// m2 is outdated and must be deleted before a new machine is created.
	m2, cfg2 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 2), ns.Name, cluster, kcp, gmt, "v1.29.0")
	defer func() {
		require.NoError(t, testEnv.Cleanup(ctx, m0, cfg0, m1, cfg1, m2, cfg2))
	}()

	r := buildTestController(t, nil)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		controlplane, err := r.retrieveControlPlaneState(ctx, cluster, kcp)
		require.NoError(t, err)

		_, err = r.reconcileMachines(ctx, controlplane)
		assert.NoError(c, err)

		machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
		assert.NoError(c, err)
		// The outdated machine is deleted first; no new machine must appear yet.
		assert.Len(c, machines, 2)
		for _, m := range machines {
			assert.Equal(c, kcp.Spec.Version, m.Spec.Version)
		}
	}, 10*time.Second, 100*time.Millisecond)
}

// TestReconcileMachinesPreflightBlocksWhenLatestMachineNotReady verifies that
// reconcileMachines requeues and does not create new machines when the most recently
// created machine has not yet appeared as a controlnode in the workload cluster.
func TestReconcileMachinesPreflightBlocksWhenLatestMachineNotReady(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-machines-preflight-not-ready")
	require.NoError(t, err)

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	kcp.Spec.Replicas = 3
	require.NoError(t, testEnv.Create(ctx, cluster))
	require.NoError(t, testEnv.Create(ctx, gmt))
	require.NoError(t, testEnv.Create(ctx, kcp))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(kcp, gmt, cluster, ns)

	m0, cfg0 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 0), ns.Name, cluster, kcp, gmt, kcp.Spec.Version)
	m1, cfg1 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 1), ns.Name, cluster, kcp, gmt, kcp.Spec.Version)
	defer func() {
		require.NoError(t, testEnv.Cleanup(ctx, m0, cfg0, m1, cfg1))
	}()

	// Workload cluster returns 404 for all controlnode lookups → latest machine is "not ready".
	r := buildTestController(t, (&fakeRoundTripperControlNodeNotFound{}).run)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		controlplane, err := r.retrieveControlPlaneState(ctx, cluster, kcp)
		require.NoError(t, err)

		res, err := r.reconcileMachines(ctx, controlplane)
		assert.NoError(c, err)
		// Preflight must block progress: result must request a requeue.
		assert.False(c, res.IsZero(), "expected requeue result, got zero")

		machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
		assert.NoError(c, err)
		// No new machine should have been created.
		assert.Len(c, machines, 2)
	}, 10*time.Second, 100*time.Millisecond)
}

// TestReconcileMachinesInPlaceUpdateTriggered verifies that when UpdateStrategy is InPlace
// and there are machines with an outdated version, preflightChecks triggers the autopilot
// in-place update plan and returns a requeue result without creating or deleting machines.
func TestReconcileMachinesInPlaceUpdateTriggered(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-machines-inplace-update")
	require.NoError(t, err)

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	kcp.Spec.Replicas = 3
	kcp.Spec.UpdateStrategy = cpv1beta2.UpdateInPlace
	require.NoError(t, testEnv.Create(ctx, cluster))
	require.NoError(t, testEnv.Create(ctx, gmt))
	require.NoError(t, testEnv.Create(ctx, kcp))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(kcp, gmt, cluster, ns)

	m0, cfg0 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 0), ns.Name, cluster, kcp, gmt, "v1.29.0")
	m1, cfg1 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 1), ns.Name, cluster, kcp, gmt, kcp.Spec.Version)
	m2, cfg2 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 2), ns.Name, cluster, kcp, gmt, "v1.29.0")
	defer func() {
		require.NoError(t, testEnv.Cleanup(ctx, m0, cfg0, m1, cfg1, m2, cfg2))
	}()

	// Workload cluster accepts autopilot plan POSTs so that createAutopilotPlan succeeds.
	r := buildTestController(t, (&fakeRoundTripperWithAutopilotPost{}).run)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		controlplane, err := r.retrieveControlPlaneState(ctx, cluster, kcp)
		require.NoError(t, err)

		res, err := r.reconcileMachines(ctx, controlplane)
		assert.NoError(c, err)
		// InPlace update path must return a requeue so the controller polls until autopilot finishes.
		assert.False(c, res.IsZero(), "expected requeue result, got zero")

		machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
		assert.NoError(c, err)
		// No machine must be created or deleted during an in-place update.
		assert.Len(c, machines, 3)
	}, 10*time.Second, 100*time.Millisecond)
}

// TestReconcileMachinesRequeuesWhileNotUpToDate verifies the full 3->2->3->2 rolling-replace cycle:
//
//  1. Initial state: 3 machines (2 outdated and 1 up-to-date) → scale down an outdated machine (3->2) and requeue since not up-to-date yet.
//  2. State: 2 machines (1 outdated and 1 up-to-date) → scale up a new machine (2->3) and requeue since not all are up-to-date yet.
//  3. State: 3 machines (1 outdated and 2 up-to-date) → scale down the remaining outdated machine (3->2); now all machines are up-to-date so no requeue.
func TestReconcileMachinesRequeuesWhileNotUpToDate(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-machines-requeue-not-up-to-date")
	require.NoError(t, err)

	cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
	kcp.Spec.Replicas = 2
	require.NoError(t, testEnv.Create(ctx, cluster))
	require.NoError(t, testEnv.Create(ctx, gmt))
	require.NoError(t, testEnv.Create(ctx, kcp))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(kcp, gmt, cluster, ns)

	m0, cfg0 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 0), ns.Name, cluster, kcp, gmt, "v1.29.0") // outdated
	m1, cfg1 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 1), ns.Name, cluster, kcp, gmt, "v1.29.0") // outdated
	m2, cfg2 := createControlPlaneMachine(t, fmt.Sprintf("%s-%d", kcp.Name, 2), ns.Name, cluster, kcp, gmt, kcp.Spec.Version)
	defer func() {
		require.NoError(t, testEnv.Cleanup(ctx, m0, cfg0, m1, cfg1, m2, cfg2))
	}()

	r := buildTestController(t, nil)

	// Phase 1 – scale down a not up-to-date machine and return a requeue result since not all machines are up-to-date yet.
	controlplane, err := r.retrieveControlPlaneState(ctx, cluster, kcp)
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := r.reconcileMachines(ctx, controlplane)
		assert.NoError(c, err)

		machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
		assert.NoError(c, err)
		assert.Len(c, machines, 2)
		assert.False(c, res.IsZero(), "expected requeue result while not up-to-date, got zero")
	}, 10*time.Second, 100*time.Millisecond)

	controlplane, err = r.retrieveControlPlaneState(ctx, cluster, kcp)
	require.NoError(t, err)

	// Phase 2 – scale up a new desired machine and return a requeue result since not all machines are up-to-date yet.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := r.reconcileMachines(ctx, controlplane)
		assert.NoError(c, err)

		machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
		assert.NoError(c, err)
		assert.Len(c, machines, 3)
		assert.False(c, res.IsZero(), "expected requeue result while not up-to-date, got zero")
	}, 10*time.Second, 100*time.Millisecond)

	controlplane, err = r.retrieveControlPlaneState(ctx, cluster, kcp)
	require.NoError(t, err)

	// Phase 3 – scale down the pending outdated machine; now all machines are up-to-date so reconcileMachines returns zero result.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := r.reconcileMachines(ctx, controlplane)
		assert.NoError(c, err)

		machines, err := collections.GetFilteredMachinesForCluster(ctx, testEnv, cluster, collections.ControlPlaneMachines(cluster.Name), collections.ActiveMachines)
		assert.NoError(c, err)
		assert.Len(c, machines, 2)
		assert.True(c, res.IsZero(), "expected zero result once converged, got %v", res)
		for _, m := range machines {
			assert.Equal(c, kcp.Spec.Version, m.Spec.Version)
		}
	}, 10*time.Hour, 100*time.Millisecond)
}

// fakeRoundTripperControlNodeNotFound returns 404 for controlnode GET requests so that
// isLatestMachineReady returns false, simulating a machine that has not yet joined.
type fakeRoundTripperControlNodeNotFound struct{}

func (f *fakeRoundTripperControlNodeNotFound) run(req *http.Request) (*http.Response, error) {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	if req.Method == "GET" && strings.HasPrefix(req.URL.Path, "/apis/autopilot.k0sproject.io/v1beta2/controlnodes/") {
		return &http.Response{StatusCode: http.StatusNotFound, Header: header, Body: nil}, nil
	}
	return (&fakeRoundTripper{}).run(req)
}

// fakeRoundTripperWithAutopilotPost delegates to fakeRoundTripper for all requests except
// POST to the autopilot plans endpoint, which it accepts so that createAutopilotPlan succeeds.
type fakeRoundTripperWithAutopilotPost struct{}

func (f *fakeRoundTripperWithAutopilotPost) run(req *http.Request) (*http.Response, error) {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	if req.Method == "POST" && strings.HasPrefix(req.URL.Path, "/apis/autopilot.k0sproject.io/v1beta2/plans") {
		body := `{"apiVersion":"autopilot.k0sproject.io/v1beta2","kind":"Plan","metadata":{"name":"autopilot"}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       io.NopCloser(bytes.NewReader([]byte(body))),
		}, nil
	}
	return (&fakeRoundTripper{}).run(req)
}

// buildTestController creates a K0sController wired to testEnv with a custom workload-cluster
// round-tripper function. Pass nil to use the default fakeRoundTripper behaviour.
func buildTestController(t *testing.T, roundTripFunc func(*http.Request) (*http.Response, error)) *K0sController {
	t.Helper()
	if roundTripFunc == nil {
		roundTripFunc = (&fakeRoundTripper{}).run
	}
	fakeClient := &restfake.RESTClient{
		Client: restfake.CreateHTTPClient(roundTripFunc),
	}
	restClient, _ := rest.RESTClientFor(&rest.Config{
		ContentConfig: rest.ContentConfig{
			NegotiatedSerializer: scheme.Codecs,
			GroupVersion:         &metav1.SchemeGroupVersion,
		},
	})
	restClient.Client = fakeClient.Client
	clientSet, err := kubernetes.NewForConfig(testEnv.Config)
	require.NoError(t, err)
	return &K0sController{
		Client:                    testEnv,
		ClientSet:                 clientSet,
		workloadClusterKubeClient: kubernetes.New(restClient),
	}
}

// createControlPlaneMachine creates a Machine + K0sControllerConfig pair owned by kcp and
// returns both objects. version is e.g. "v1.30.0".
func createControlPlaneMachine(t *testing.T, name, namespace string, cluster *clusterv1.Cluster, kcp *cpv1beta2.K0sControlPlane, gmt interface{ GetName() string }, version string) (*clusterv1.Machine, *bootstrapv2.K0sControllerConfig) {
	t.Helper()
	kcpOwnerRef := *metav1.NewControllerRef(kcp, cpv1beta2.GroupVersion.WithKind("K0sControlPlane"))

	k0sConfigAnnotationValue, err := generateK0sConfigAnnotationValueForMachine(kcp, name)
	require.NoError(t, err)

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: kcp.GetName(),
			},
			Annotations: map[string]string{
				cpv1beta2.MachineK0sConfigAnnotation: k0sConfigAnnotationValue,
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     version,
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachineTemplate",
				Name:     gmt.GetName(),
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     name,
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sControllerConfig",
				},
			},
		},
	}
	machine.SetOwnerReferences([]metav1.OwnerReference{kcpOwnerRef})
	require.NoError(t, testEnv.Create(ctx, machine))

	config := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    controlPlaneCommonLabelsForCluster(kcp, cluster.Name),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "cluster.x-k8s.io/v1beta1",
				Kind:               "Machine",
				Name:               machine.GetName(),
				UID:                machine.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			}},
		},
	}
	require.NoError(t, testEnv.Create(ctx, config))
	return machine, config
}
