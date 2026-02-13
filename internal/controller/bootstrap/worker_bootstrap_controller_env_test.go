//go:build envtest

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

package bootstrap

import (
	"fmt"
	"testing"
	"time"

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
	cpv1beta2 "github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	kubeadmbootstrapv1 "sigs.k8s.io/cluster-api/api/bootstrap/kubeadm/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	fakeremote "sigs.k8s.io/cluster-api/controllers/remote/fake"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestReconcileNoK0sWorkerConfig(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-no-workerconfig")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(cluster, ns)

	r := &Controller{
		Client: testEnv,
	}

	nonExistingK0sWorkerConfig := client.ObjectKey{
		Namespace: ns.Name,
		Name:      "non-existing-config",
	}
	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: nonExistingK0sWorkerConfig})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileReturnErrorWhenWorkerConfigOwnerRefIsMissing(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-workerconfig-error-owner-ref-missing")
	require.NoError(t, err)

	k0sWorkerConfig := &bootstrapv1.K0sWorkerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "worker-config",
			Namespace:       ns.Name,
			OwnerReferences: []metav1.OwnerReference{},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sWorkerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sWorkerConfig, ns)

	r := &Controller{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sWorkerConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileReturnErrorWhenWorkerConfigOwnerIsNotFound(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-workerconfig-error-owner-not-found")
	require.NoError(t, err)

	k0sWorkerConfig := &bootstrapv1.K0sWorkerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       "non-existing-machine",
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sWorkerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sWorkerConfig, ns)

	r := &Controller{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sWorkerConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileReturnErrorWhenClusterIsNotFound(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-workerconfig-error-not-found")
	require.NoError(t, err)

	machineName := fmt.Sprintf("%s-%d", "machine-for-worker", 0)
	machineForWorkerConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName,
			Namespace: ns.Name,
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: "non-existing-cluster",
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
	require.NoError(t, testEnv.Create(ctx, machineForWorkerConfig))
	k0sWorkerConfig := &bootstrapv1.K0sWorkerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineName,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sWorkerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sWorkerConfig, machineForWorkerConfig, ns)

	r := &Controller{
		Client: testEnv,
	}

	// Cluster is not created yet.
	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sWorkerConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcilePausedCluster(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-workerconfig-paused-cluster")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)

	// Cluster 'paused'.
	cluster.Spec.Paused = ptr.To(true)

	require.NoError(t, testEnv.Create(ctx, cluster))

	machineForWorkerConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-worker", 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForWorkerConfig",
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
	require.NoError(t, testEnv.Create(ctx, machineForWorkerConfig))

	k0sWorkerConfig := &bootstrapv1.K0sWorkerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForWorkerConfig.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sWorkerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sWorkerConfig, cluster, machineForWorkerConfig, ns)

	r := &Controller{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sWorkerConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcilePausedK0sWorkerConfig(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-paused-workerconfig")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))

	machineForWorkerConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-worker", 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForWorkerConfig",
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
	require.NoError(t, testEnv.Create(ctx, machineForWorkerConfig))
	k0sWorkerConfig := &bootstrapv1.K0sWorkerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForWorkerConfig.Name,
					UID:        "1",
				},
			},
		},
	}

	// K0sControlPlane with 'paused' annotation.
	k0sWorkerConfig.Annotations = map[string]string{clusterv1.PausedAnnotation: "true"}

	require.NoError(t, testEnv.Create(ctx, k0sWorkerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sWorkerConfig, cluster, machineForWorkerConfig, ns)

	r := &Controller{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sWorkerConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileBootstrapDataAlreadyCreated(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-workerconfig-bootstrap-data-already-created")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))

	machineForWorkerConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-worker", 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForWorkerConfig",
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
	require.NoError(t, testEnv.Create(ctx, machineForWorkerConfig))

	k0sWorkerConfig := &bootstrapv1.K0sWorkerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForWorkerConfig.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sWorkerConfig))

	// Bootstrap data is already crreated.
	k0sWorkerConfig.Status.Initialization.DataSecretCreated = ptr.To(true)
	require.NoError(t, testEnv.Status().Update(ctx, k0sWorkerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sWorkerConfig, cluster, machineForWorkerConfig, ns)

	r := &Controller{
		Client: testEnv,
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sWorkerConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{}, result)
		// We assume that the bootstrap data is already created, so secret bootstrap data shouldn't be created again.
		assert.True(c, apierrors.IsNotFound(testEnv.Get(ctx, client.ObjectKeyFromObject(k0sWorkerConfig), &corev1.Secret{})))
	}, 10*time.Second, 100*time.Millisecond)

}

func TestReconcileControlPlaneNotReady(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-workerconfig-control-plane-not-ready")
	require.NoError(t, err)

	cluster, kcp, _ := createClusterWithControlPlane(ns.GetName())
	require.NoError(t, testEnv.Create(ctx, cluster))
	require.NoError(t, testEnv.Create(ctx, kcp))

	machineForWorkerConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-worker", 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForWorkerConfig",
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
	require.NoError(t, testEnv.Create(ctx, machineForWorkerConfig))
	k0sWorkerConfig := &bootstrapv1.K0sWorkerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForWorkerConfig.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sWorkerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sWorkerConfig, cluster, machineForWorkerConfig, ns)

	r := &Controller{
		Client: testEnv,
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		// Cluster.Spec.ControlPlaneEndpoint is not initialize by infra provider
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sWorkerConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5}, result)

		updatedK0sWorkerConfig := &bootstrapv1.K0sWorkerConfig{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKeyFromObject(k0sWorkerConfig), updatedK0sWorkerConfig))
		assert.True(c, conditions.IsFalse(updatedK0sWorkerConfig, bootstrapv1.DataSecretAvailableCondition))
		assert.Equal(c, bootstrapv1.WaitingForControlPlaneInitializationReason, conditions.GetReason(updatedK0sWorkerConfig, bootstrapv1.DataSecretAvailableCondition))

		// Cluster.Spec.ControlPlaneEndpoint is set but Cluster.Status.ControlPlaneReady is false
		cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
			Host: "http://test.host",
			Port: 9999,
		}
		assert.NoError(c, testEnv.Update(ctx, cluster))

		result, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sWorkerConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5}, result)

		updatedK0sWorkerConfig = &bootstrapv1.K0sWorkerConfig{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKeyFromObject(k0sWorkerConfig), updatedK0sWorkerConfig))
		assert.True(c, conditions.IsFalse(updatedK0sWorkerConfig, bootstrapv1.DataSecretAvailableCondition))
		assert.Equal(c, bootstrapv1.WaitingForControlPlaneInitializationReason, conditions.GetReason(updatedK0sWorkerConfig, bootstrapv1.DataSecretAvailableCondition))
	}, 10*time.Second, 100*time.Millisecond)
}

func TestReconcileGenerateBootstrapData(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-workerconfig-generate-bootstrap-data")
	require.NoError(t, err)

	cluster, kcp, _ := createClusterWithControlPlane(ns.GetName())
	require.NoError(t, testEnv.Create(ctx, cluster))
	require.NoError(t, testEnv.Create(ctx, kcp))

	conditions.Set(cluster, metav1.Condition{
		Type:   string(clusterv1.ClusterControlPlaneInitializedCondition),
		Status: metav1.ConditionTrue,
		Reason: "ControlPlaneReady",
	})
	require.NoError(t, testEnv.Status().Update(ctx, cluster))

	machineForWorkerConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-worker", 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForWorkerConfig",
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
	require.NoError(t, testEnv.Create(ctx, machineForWorkerConfig))

	k0sWorkerConfig := &bootstrapv1.K0sWorkerConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "bootstrap.cluster.x-k8s.io/v1beta2",
			Kind:       "K0sWorkerConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForWorkerConfig.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sWorkerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sWorkerConfig, cluster, machineForWorkerConfig, kcp, ns)

	workloadClient, _ := fakeremote.NewClusterClient(ctx, "", testEnv, types.NamespacedName{})
	r := &Controller{
		Client:                testEnv,
		workloadClusterClient: workloadClient,
		SecretCachingClient:   testEnv,
	}

	clusterCerts := secret.NewCertificatesForInitialControlPlane(&kubeadmbootstrapv1.ClusterConfiguration{})
	require.NoError(t, clusterCerts.Generate())
	caCert := clusterCerts.GetByPurpose(secret.ClusterCA)
	caCertSecret := caCert.AsSecret(
		client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name},
		*metav1.NewControllerRef(kcp, cpv1beta2.GroupVersion.WithKind("K0sControlPlane")),
	)
	require.NoError(t, testEnv.Create(ctx, caCertSecret))

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sWorkerConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{}, result)

		bootstrapSecret := &corev1.Secret{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKey{Namespace: k0sWorkerConfig.Namespace, Name: k0sWorkerConfig.Name}, bootstrapSecret))

		updatedK0sWorkerConfig := &bootstrapv1.K0sWorkerConfig{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKeyFromObject(k0sWorkerConfig), updatedK0sWorkerConfig))

		fmt.Println(updatedK0sWorkerConfig.Status)
		assert.NotNil(c, updatedK0sWorkerConfig.Status.Initialization.DataSecretCreated)
		assert.True(c, *updatedK0sWorkerConfig.Status.Initialization.DataSecretCreated)
		assert.NotNil(c, updatedK0sWorkerConfig.Status.DataSecretName)
		if updatedK0sWorkerConfig.Status.DataSecretName != nil {
			assert.Equal(c, *updatedK0sWorkerConfig.Status.DataSecretName, updatedK0sWorkerConfig.Name)
			assert.True(c, conditions.IsTrue(updatedK0sWorkerConfig, bootstrapv1.DataSecretAvailableCondition))

			// Verify the created secret has the correct labels
			secretObj := &corev1.Secret{}
			err = testEnv.Get(ctx, client.ObjectKey{Name: k0sWorkerConfig.Name, Namespace: ns.Name}, secretObj)
			assert.NoError(c, err, "bootstrap secret should have been created")
			assert.NotNil(c, secretObj)
			assert.Equal(c, cluster.Name, secretObj.Labels[clusterv1.ClusterNameLabel])
			assert.NotEmpty(c, secretObj.Data["value"])
		}
	}, 20*time.Second, 100*time.Millisecond)
}

func createClusterWithControlPlane(namespace string) (*clusterv1.Cluster, *cpv1beta2.K0sControlPlane, *unstructured.Unstructured) {
	kcpName := fmt.Sprintf("kcp-foo-%s", util.RandomString(6))

	cluster := newCluster(namespace)
	cluster.Spec = clusterv1.ClusterSpec{
		ControlPlaneRef: clusterv1.ContractVersionedObjectReference{
			Kind:     "K0sControlPlane",
			Name:     kcpName,
			APIGroup: cpv1beta2.GroupVersion.Group,
		},
		ControlPlaneEndpoint: clusterv1.APIEndpoint{
			Host: "test.endpoint",
			Port: 6443,
		},
	}

	kcp := &cpv1beta2.K0sControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cpv1beta2.GroupVersion.String(),
			Kind:       "K0sControlPlane",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kcpName,
			Namespace: namespace,
			UID:       "1",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Cluster",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       kcpName,
					UID:        "1",
				},
			},
			Finalizers: []string{cpv1beta2.K0sControlPlaneFinalizer},
		},
		Spec: cpv1beta2.K0sControlPlaneSpec{
			MachineTemplate: &cpv1beta2.K0sControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					Kind:       "GenericInfrastructureMachineTemplate",
					Namespace:  namespace,
					Name:       "infra-foo",
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
				},
			},
			UpdateStrategy: cpv1beta2.UpdateRecreate,
			Replicas:       int32(1),
			Version:        "v1.30.0",
		},
	}

	genericMachineTemplate := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "GenericInfrastructureMachineTemplate",
			"apiVersion": "infrastructure.cluster.x-k8s.io/v1beta1",
			"metadata": map[string]interface{}{
				"name":      "infra-foo",
				"namespace": namespace,
				"annotations": map[string]interface{}{
					clusterv1.TemplateClonedFromNameAnnotation:      kcp.Spec.MachineTemplate.InfrastructureRef.Name,
					clusterv1.TemplateClonedFromGroupKindAnnotation: kcp.Spec.MachineTemplate.InfrastructureRef.GroupVersionKind().GroupKind().String(),
				},
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"hello": "world",
					},
				},
			},
		},
	}
	return cluster, kcp, genericMachineTemplate
}
