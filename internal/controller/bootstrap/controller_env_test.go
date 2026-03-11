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
	bootstrapv1beta2 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta2"
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

func TestReconcileNoK0sConfig(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-no-config")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(cluster, ns)

	r := &Reconciler{
		Client: testEnv,
	}

	nonExistingK0sConfig := client.ObjectKey{
		Namespace: ns.Name,
		Name:      "non-existing-config",
	}
	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: nonExistingK0sConfig})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileReturnErrorWhenConfigOwnerIsNotFound(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-config-error-owner-not-found")
	require.NoError(t, err)

	k0sConfig := &bootstrapv1beta2.K0sConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
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
	require.NoError(t, testEnv.Create(ctx, k0sConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sConfig, ns)

	r := &Reconciler{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileReturnErrorWhenConfigOwnerRefIsMissing(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-config-error-owner-ref-missing")
	require.NoError(t, err)

	k0sConfig := &bootstrapv1beta2.K0sConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "config",
			Namespace:       ns.Name,
			OwnerReferences: []metav1.OwnerReference{},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sConfig, ns)

	r := &Reconciler{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileReturnErrorWhenClusterConfigBelongsIsNotFound(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-config-error-cluster-not-found")
	require.NoError(t, err)

	machineName := fmt.Sprintf("%s-%d", "machine-for-config", 0)
	machineForConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName,
			Namespace: ns.Name,
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: "non-existing-cluster",
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachine",
				Name:     "machine-for-config-infra",
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     "machine-for-config-bootstrap",
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sConfig",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, machineForConfig))
	k0sConfig := &bootstrapv1beta2.K0sConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
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
	require.NoError(t, testEnv.Create(ctx, k0sConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sConfig, machineForConfig, ns)

	r := &Reconciler{
		Client: testEnv,
	}

	// Cluster is not created yet.
	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileConfigPausedCluster(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-config-paused-cluster")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)

	// Cluster 'paused'.
	cluster.Spec.Paused = ptr.To(true)

	require.NoError(t, testEnv.Create(ctx, cluster))

	machineForConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-config", 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForConfig",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachine",
				Name:     "machine-for-config-infra",
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     "machine-for-config-bootstrap",
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sConfig",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, machineForConfig))

	k0sConfig := &bootstrapv1beta2.K0sConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForConfig.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sConfig, cluster, machineForConfig, ns)

	r := &Reconciler{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcilePausedK0sConfig(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-paused-config")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))

	machineForConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-config", 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForConfig",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachine",
				Name:     "machine-for-config-infra",
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     "machine-for-config-bootstrap",
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sConfig",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, machineForConfig))
	k0sConfig := &bootstrapv1beta2.K0sConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForConfig.Name,
					UID:        "1",
				},
			},
		},
	}

	// K0sConfig with 'paused' annotation.
	k0sConfig.Annotations = map[string]string{clusterv1.PausedAnnotation: "true"}

	require.NoError(t, testEnv.Create(ctx, k0sConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sConfig, cluster, machineForConfig, ns)

	r := &Reconciler{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileConfigBootstrapDataAlreadyCreated(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-config-bootstrap-data-already-created")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))

	machineForConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-config", 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForConfig",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachine",
				Name:     "machine-for-config-infra",
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     "machine-for-config-bootstrap",
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sConfig",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, machineForConfig))

	k0sConfig := &bootstrapv1beta2.K0sConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForConfig.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sConfig))

	// Bootstrap data is already crreated.
	k0sConfig.Status.Initialization.DataSecretCreated = ptr.To(true)
	require.NoError(t, testEnv.Status().Update(ctx, k0sConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sConfig, cluster, machineForConfig, ns)

	r := &Reconciler{
		Client:              testEnv,
		SecretCachingClient: testEnv,
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{}, result)
		// We assume that the bootstrap data is already created, so secret bootstrap data shouldn't be created again.
		assert.True(c, apierrors.IsNotFound(testEnv.Get(ctx, client.ObjectKeyFromObject(k0sConfig), &corev1.Secret{})))
	}, 10*time.Second, 100*time.Millisecond)
}

func TestReconcileConfigControlPlaneIsZero(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-config-control-plane-not-ready")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)
	// Cluster.Spec.ControlPlaneEndpoint is not set by infra provider
	cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{}
	require.NoError(t, testEnv.Create(ctx, cluster))

	machineForConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-config", 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForConfig",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachine",
				Name:     "machine-for-config-infra",
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     "machine-for-config-bootstrap",
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sConfig",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, machineForConfig))
	k0sConfig := &bootstrapv1beta2.K0sConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForConfig",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForConfig.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sConfig, cluster, machineForConfig, ns)

	r := &Reconciler{
		Client: testEnv,
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		// Cluster.Spec.ControlPlaneEndpoint is not set by infra provider
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{Requeue: true, RequeueAfter: time.Second * 30}, result)

		updatedK0sConfig := &bootstrapv1beta2.K0sConfig{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKeyFromObject(k0sConfig), updatedK0sConfig))
		assert.True(c, conditions.IsFalse(updatedK0sConfig, bootstrapv1beta2.DataSecretAvailableCondition))
		assert.Equal(c, bootstrapv1beta2.WaitingForControlPlaneInitializationReason, conditions.GetReason(updatedK0sConfig, bootstrapv1beta2.DataSecretAvailableCondition))
	}, 10*time.Second, 100*time.Millisecond)
}

func TestReconcileControlPlaneNotReady(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-config-control-plane-not-ready")
	require.NoError(t, err)

	cluster, kcp, _ := createClusterWithControlPlane(ns.GetName())
	require.NoError(t, testEnv.Create(ctx, cluster))
	require.NoError(t, testEnv.Create(ctx, kcp))

	machineForWorker := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-worker", 0),
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
				Name:     "machine-for-worker-infra",
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     "machine-for-worker-bootstrap",
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sConfig",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, machineForWorker))
	k0sConfig := &bootstrapv1beta2.K0sConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForWorker.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sConfig, cluster, machineForWorker, ns)

	r := &Reconciler{
		Client: testEnv,
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		// Cluster.Spec.ControlPlaneEndpoint is not initialize by infra provider
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5}, result)

		updatedK0sConfig := &bootstrapv1beta2.K0sConfig{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKeyFromObject(k0sConfig), updatedK0sConfig))
		assert.True(c, conditions.IsFalse(updatedK0sConfig, bootstrapv1beta2.DataSecretAvailableCondition))
		assert.Equal(c, bootstrapv1beta2.WaitingForControlPlaneInitializationReason, conditions.GetReason(updatedK0sConfig, bootstrapv1beta2.DataSecretAvailableCondition))

		// Cluster.Spec.ControlPlaneEndpoint is set but Cluster.Status.ControlPlaneReady is false
		cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
			Host: "http://test.host",
			Port: 9999,
		}
		assert.NoError(c, testEnv.Update(ctx, cluster))

		result, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{Requeue: true, RequeueAfter: time.Second * 5}, result)

		updatedK0sConfig = &bootstrapv1beta2.K0sConfig{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKeyFromObject(k0sConfig), updatedK0sConfig))
		assert.True(c, conditions.IsFalse(updatedK0sConfig, bootstrapv1beta2.DataSecretAvailableCondition))
		assert.Equal(c, bootstrapv1beta2.WaitingForControlPlaneInitializationReason, conditions.GetReason(updatedK0sConfig, bootstrapv1beta2.DataSecretAvailableCondition))
	}, 10*time.Second, 100*time.Millisecond)
}

func TestReconcileConfigGenerateBootstrapDataForControlPlane(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-config-generate-bootstrap-data")
	require.NoError(t, err)

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))
	require.NoError(t, testEnv.Create(ctx, kcp))

	cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
		Host: "localhost",
	}
	require.NoError(t, testEnv.Status().Update(ctx, cluster))

	machineForConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-controller",
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForConfig",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     "v1.30.0",
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Kind:     "GenericInfrastructureMachine",
				Name:     "machine-for-config-infra",
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     "machine-for-config-bootstrap",
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sConfig",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, machineForConfig))

	k0sConfig := &bootstrapv1beta2.K0sConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "bootstrap.cluster.x-k8s.io/v1beta2",
			Kind:       "K0sConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-controller",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForConfig.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sConfig, cluster, machineForConfig, ns)

	r := &Reconciler{
		Client:              testEnv,
		SecretCachingClient: secretCachingClient,
	}

	kubeconfigSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name(cluster.Name, secret.Kubeconfig),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{},
		},
		Data: map[string][]byte{
			secret.KubeconfigDataName: {},
		},
	}
	require.NoError(t, testEnv.Create(ctx, kubeconfigSecret))
	clusterCerts := secret.NewCertificatesForInitialControlPlane(&kubeadmbootstrapv1.ClusterConfiguration{})
	require.NoError(t, clusterCerts.Generate())
	caCert := clusterCerts.GetByPurpose(secret.ClusterCA)
	caCertSecret := caCert.AsSecret(
		client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name},
		*metav1.NewControllerRef(kcp, cpv1beta2.GroupVersion.WithKind("K0sControlPlane")),
	)
	require.NoError(t, testEnv.Create(ctx, caCertSecret))

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{}, result)

		bootstrapSecret := &corev1.Secret{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKey{Namespace: k0sConfig.Namespace, Name: k0sConfig.Name}, bootstrapSecret))

		updatedK0sConfig := &bootstrapv1beta2.K0sConfig{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKeyFromObject(k0sConfig), updatedK0sConfig))

		assert.True(c, ptr.Deref(updatedK0sConfig.Status.Initialization.DataSecretCreated, false))
		if assert.NotNil(c, updatedK0sConfig.Status.DataSecretName) {
			assert.Equal(c, *updatedK0sConfig.Status.DataSecretName, updatedK0sConfig.Name)
		}
		assert.True(c, conditions.IsTrue(updatedK0sConfig, bootstrapv1beta2.DataSecretAvailableCondition))
	}, 20*time.Second, 100*time.Millisecond)
}

func TestReconcileGenerateBootstrapDataForWorker(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-config-generate-bootstrap-data")
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

	machineForConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-worker", 0),
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
				Name:     "machine-for-infra",
				APIGroup: clusterv1.GroupVersionInfrastructure.Group,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Name:     "machine-for-bootstrap",
					APIGroup: clusterv1.GroupVersionBootstrap.Group,
					Kind:     "K0sConfig",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, machineForConfig))

	k0sConfig := &bootstrapv1beta2.K0sConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "bootstrap.cluster.x-k8s.io/v1beta2",
			Kind:       "K0sConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForConfig.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sConfig, cluster, machineForConfig, kcp, ns)

	workloadClient, _ := fakeremote.NewClusterClient(ctx, "", testEnv, types.NamespacedName{})
	r := &Reconciler{
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
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{}, result)

		bootstrapSecret := &corev1.Secret{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKey{Namespace: k0sConfig.Namespace, Name: k0sConfig.Name}, bootstrapSecret))

		updatedK0sConfig := &bootstrapv1beta2.K0sConfig{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKeyFromObject(k0sConfig), updatedK0sConfig))

		fmt.Println(updatedK0sConfig.Status)
		assert.NotNil(c, updatedK0sConfig.Status.Initialization.DataSecretCreated)
		assert.True(c, *updatedK0sConfig.Status.Initialization.DataSecretCreated)
		assert.NotNil(c, updatedK0sConfig.Status.DataSecretName)
		if updatedK0sConfig.Status.DataSecretName != nil {
			assert.Equal(c, *updatedK0sConfig.Status.DataSecretName, updatedK0sConfig.Name)
			assert.True(c, conditions.IsTrue(updatedK0sConfig, bootstrapv1.DataSecretAvailableCondition))

			// Verify the created secret has the correct labels
			secretObj := &corev1.Secret{}
			err = testEnv.Get(ctx, client.ObjectKey{Name: updatedK0sConfig.Name, Namespace: ns.Name}, secretObj)
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
