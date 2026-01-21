//go:build envtest

/*
Copyright 2024.

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
	"testing"
	"time"

	autopilot "github.com/k0sproject/k0s/pkg/apis/autopilot/v1beta2"
	cpv1beta1 "github.com/k0sproject/k0smotron/api/controlplane/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"
	"k8s.io/kubectl/pkg/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNewReplicaStatusComputer(t *testing.T) {
	t.Run("inplace strategy returns a plan status computer", func(t *testing.T) {
		ns, err := testEnv.CreateNamespace(ctx, "test-inplace-strategy-returns-plan-status-computer")
		require.NoError(t, err)

		cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
		require.NoError(t, testEnv.Create(ctx, cluster))

		kcp.Spec.UpdateStrategy = cpv1beta1.UpdateInPlace
		require.NoError(t, testEnv.Create(ctx, kcp))

		defer func(do ...client.Object) {
			require.NoError(t, testEnv.Cleanup(ctx, do...))
		}(cluster, kcp, ns)

		plan := &autopilot.Plan{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Plan",
				APIVersion: "autopilot.k0sproject.io/v1beta2",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-plan",
			},
			Spec: autopilot.PlanSpec{
				Commands: []autopilot.PlanCommand{
					{
						K0sUpdate: &autopilot.PlanCommandK0sUpdate{
							Version: "v1.31.0+k0s.0",
						},
					},
				},
			},
		}
		expectedPlanStatusComputer := &planStatus{
			plan: *plan,
		}

		frt := &fakeRoundTripper{plan: plan}
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

		controller := &K0sController{
			Client:                    testEnv,
			workloadClusterKubeClient: kubernetes.New(restClient),
		}
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			rc, err := controller.newReplicasStatusComputer(ctx, cluster, kcp)
			assert.NoError(t, err)
			ps, ok := rc.(*planStatus)
			assert.True(t, ok)
			assert.Equal(t, expectedPlanStatusComputer.plan.Name, ps.plan.Name)
		}, 10*time.Second, 100*time.Millisecond)

	})
	t.Run("inplace strategy returns a machine status computer if Plan resource is not found", func(t *testing.T) {
		ns, err := testEnv.CreateNamespace(ctx, "test-inplace-strategy-returns-machine-status-computer")
		require.NoError(t, err)

		cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
		require.NoError(t, testEnv.Create(ctx, cluster))

		kcp.Spec.UpdateStrategy = cpv1beta1.UpdateInPlace
		require.NoError(t, testEnv.Create(ctx, kcp))

		firstMachinesForKCP := &clusterv1.Machine{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Machine",
				APIVersion: "cluster.x-k8s.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "machine1",
				Namespace: ns.Name,
				Labels: map[string]string{
					clusterv1.ClusterNameLabel:         cluster.Name,
					clusterv1.MachineControlPlaneLabel: "",
				},
			},
			Spec: clusterv1.MachineSpec{
				ClusterName: cluster.Name,
				InfrastructureRef: clusterv1.ContractVersionedObjectReference{
					Kind:     "GenericInfrastructureMachineTemplate",
					Name:     gmt.GetName(),
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
		expectedMachine := firstMachinesForKCP.DeepCopy()
		require.NoError(t, testEnv.Create(ctx, firstMachinesForKCP))

		defer func(do ...client.Object) {
			require.NoError(t, testEnv.Cleanup(ctx, do...))
		}(cluster, kcp, firstMachinesForKCP, ns)

		expectedMachineStatusComputer := &machineStatus{
			machines: collections.Machines{
				expectedMachine.Name: expectedMachine,
			},
		}

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

		controller := &K0sController{
			Client:                    testEnv,
			workloadClusterKubeClient: kubernetes.New(restClient),
		}

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			rc, err := controller.newReplicasStatusComputer(ctx, cluster, kcp)
			assert.NoError(t, err)
			ms, ok := rc.(*machineStatus)
			assert.True(t, ok)
			assert.Len(t, ms.machines, 1)
			assert.Equal(t, expectedMachineStatusComputer.machines[expectedMachine.Name].Name, ms.machines[expectedMachine.Name].Name)
		}, 10*time.Second, 100*time.Millisecond)

	})
	t.Run("recreate strategy return a machine status computer", func(t *testing.T) {
		ns, err := testEnv.CreateNamespace(ctx, "test-recreate-strategy-returns-machine-status-computer")
		require.NoError(t, err)

		cluster, kcp, gmt := createClusterWithControlPlane(ns.Name)
		require.NoError(t, testEnv.Create(ctx, cluster))

		kcp.Spec.UpdateStrategy = cpv1beta1.UpdateRecreate
		require.NoError(t, testEnv.Create(ctx, kcp))

		firstMachinesForKCP := &clusterv1.Machine{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Machine",
				APIVersion: "cluster.x-k8s.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "machine1",
				Namespace: ns.Name,
				Labels: map[string]string{
					clusterv1.ClusterNameLabel:         cluster.Name,
					clusterv1.MachineControlPlaneLabel: "",
				},
			},
			Spec: clusterv1.MachineSpec{
				ClusterName: cluster.Name,
				InfrastructureRef: clusterv1.ContractVersionedObjectReference{
					Kind:     "GenericInfrastructureMachineTemplate",
					Name:     gmt.GetName(),
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
		expectedMachine := firstMachinesForKCP.DeepCopy()
		require.NoError(t, testEnv.Create(ctx, firstMachinesForKCP))

		defer func(do ...client.Object) {
			require.NoError(t, testEnv.Cleanup(ctx, do...))
		}(cluster, kcp, firstMachinesForKCP, ns)

		expectedMachineStatusComputer := &machineStatus{
			machines: collections.Machines{
				expectedMachine.Name: expectedMachine,
			},
		}
		controller := &K0sController{
			Client: testEnv,
		}
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			rc, err := controller.newReplicasStatusComputer(ctx, cluster, kcp)
			assert.NoError(t, err)
			ms, ok := rc.(*machineStatus)
			assert.True(t, ok)
			assert.Len(t, ms.machines, 1)
			assert.Equal(t, expectedMachineStatusComputer.machines[expectedMachine.Name].Name, ms.machines[expectedMachine.Name].Name)
		}, 10*time.Second, 100*time.Millisecond)
	})
}
