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

	bootstrapv1 "github.com/k0sproject/k0smotron/api/bootstrap/v1beta1"
	cpv1beta2 "github.com/k0sproject/k0smotron/api/controlplane/v1beta2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	kubeadmbootstrapv1 "sigs.k8s.io/cluster-api/api/bootstrap/kubeadm/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestReconcileNoK0sControllerConfig(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-no-controllerconfig")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(cluster, ns)

	r := &ControlPlaneController{
		Client: testEnv,
	}

	nonExistingK0sControllerConfig := client.ObjectKey{
		Namespace: ns.Name,
		Name:      "non-existing-config",
	}
	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: nonExistingK0sControllerConfig})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileReturnErrorWhenControllerConfigOwnerIsNotFound(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-controllerconfig-error-owner-not-found")
	require.NoError(t, err)

	k0sControllerConfig := &bootstrapv1.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller-config",
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
	require.NoError(t, testEnv.Create(ctx, k0sControllerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sControllerConfig, ns)

	r := &ControlPlaneController{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sControllerConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileReturnErrorWhenControllerConfigOwnerRefIsMissing(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-controllerconfig-error-owner-ref-missing")
	require.NoError(t, err)

	k0sControllerConfig := &bootstrapv1.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "controller-config",
			Namespace:       ns.Name,
			OwnerReferences: []metav1.OwnerReference{},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sControllerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sControllerConfig, ns)

	r := &ControlPlaneController{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sControllerConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileReturnErrorWhenClusterControllerConfigBelongsIsNotFound(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-controllerconfig-error-cluster-not-found")
	require.NoError(t, err)

	machineName := fmt.Sprintf("%s-%d", "machine-for-controller", 0)
	machineForControllerConfig := &clusterv1.Machine{
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
	require.NoError(t, testEnv.Create(ctx, machineForControllerConfig))
	k0sControllerConfig := &bootstrapv1.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller-config",
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
	require.NoError(t, testEnv.Create(ctx, k0sControllerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sControllerConfig, machineForControllerConfig, ns)

	r := &ControlPlaneController{
		Client: testEnv,
	}

	// Cluster is not created yet.
	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sControllerConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileControllerConfigPausedCluster(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-controllerconfig-paused-cluster")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)

	// Cluster 'paused'.
	cluster.Spec.Paused = ptr.To(true)

	require.NoError(t, testEnv.Create(ctx, cluster))

	machineForControllerConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-controlelr", 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForControllerConfig",
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
	require.NoError(t, testEnv.Create(ctx, machineForControllerConfig))

	k0sControllerConfig := &bootstrapv1.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller-config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForControllerConfig.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sControllerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sControllerConfig, cluster, machineForControllerConfig, ns)

	r := &ControlPlaneController{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sControllerConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcilePausedK0sControllerConfig(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-paused-controllerconfig")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))

	machineForControllerConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-controller", 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForControllerConfig",
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
	require.NoError(t, testEnv.Create(ctx, machineForControllerConfig))
	k0sControllerConfig := &bootstrapv1.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller-config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForControllerConfig.Name,
					UID:        "1",
				},
			},
		},
	}

	// K0sControlPlane with 'paused' annotation.
	k0sControllerConfig.Annotations = map[string]string{clusterv1.PausedAnnotation: "true"}

	require.NoError(t, testEnv.Create(ctx, k0sControllerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sControllerConfig, cluster, machineForControllerConfig, ns)

	r := &ControlPlaneController{
		Client: testEnv,
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sControllerConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
}

func TestReconcileControllerBootstrapDataAlreadyCreated(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-controllerconfig-bootstrap-data-already-created")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))

	machineForControllerConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-controller", 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForControllerConfig",
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
	require.NoError(t, testEnv.Create(ctx, machineForControllerConfig))

	k0sControllerConfig := &bootstrapv1.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller-config",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForControllerConfig.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sControllerConfig))

	// Bootstrap data is already crreated.
	k0sControllerConfig.Status.Ready = true
	require.NoError(t, testEnv.Status().Update(ctx, k0sControllerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sControllerConfig, cluster, machineForControllerConfig, ns)

	r := &ControlPlaneController{
		Client: testEnv,
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sControllerConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{}, result)
		// We assume that the bootstrap data is already created, so secret bootstrap data shouldn't be created again.
		assert.True(c, apierrors.IsNotFound(testEnv.Get(ctx, client.ObjectKeyFromObject(k0sControllerConfig), &corev1.Secret{})))
	}, 10*time.Second, 100*time.Millisecond)
}

func TestReconcileControllerConfigControlPlaneIsZero(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-controllerconfig-control-plane-not-ready")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)
	// Cluster.Spec.ControlPlaneEndpoint is not set by infra provider
	cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{}
	require.NoError(t, testEnv.Create(ctx, cluster))

	machineForControllerConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", "machine-for-controller", 0),
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForControllerConfig",
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
	require.NoError(t, testEnv.Create(ctx, machineForControllerConfig))
	k0sControllerConfig := &bootstrapv1.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller-config",
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForControllerConfig",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForControllerConfig.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sControllerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sControllerConfig, cluster, machineForControllerConfig, ns)

	r := &ControlPlaneController{
		Client: testEnv,
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		// Cluster.Spec.ControlPlaneEndpoint is not set by infra provider
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sControllerConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{Requeue: true, RequeueAfter: time.Second * 30}, result)

		updatedK0sControllerConfig := &bootstrapv1.K0sControllerConfig{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKeyFromObject(k0sControllerConfig), updatedK0sControllerConfig))
		assert.True(c, conditions.IsFalse(updatedK0sControllerConfig, bootstrapv1.DataSecretAvailableCondition))
		assert.Equal(c, bootstrapv1.WaitingForControlPlaneInitializationReason, conditions.GetReason(updatedK0sControllerConfig, bootstrapv1.DataSecretAvailableCondition))
	}, 10*time.Second, 100*time.Millisecond)
}

func TestReconcileControllerConfigGenerateBootstrapData(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-reconcile-controllerconfig-generate-bootstrap-data")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))

	cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
		Host: "localhost",
	}
	require.NoError(t, testEnv.Status().Update(ctx, cluster))

	machineForControllerConfig := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-controller",
			Namespace: ns.Name,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             cluster.Name,
				clusterv1.MachineControlPlaneLabel:     "true",
				clusterv1.MachineControlPlaneNameLabel: "machineForControllerConfig",
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
	require.NoError(t, testEnv.Create(ctx, machineForControllerConfig))

	k0sControllerConfig := &bootstrapv1.K0sControllerConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "bootstrap.cluster.x-k8s.io/v1beta2",
			Kind:       "K0sControllerConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-controller",
			Namespace: ns.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       machineForControllerConfig.Name,
					UID:        "1",
				},
			},
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sControllerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sControllerConfig, cluster, machineForControllerConfig, ns)

	r := &ControlPlaneController{
		Client:              testEnv,
		SecretCachingClient: secretCachingClient,
	}

	kcp := &cpv1beta2.K0sControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-kcp",
			UID:  "1",
		},
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
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sControllerConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{}, result)

		bootstrapSecret := &corev1.Secret{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKey{Namespace: k0sControllerConfig.Namespace, Name: k0sControllerConfig.Name}, bootstrapSecret))

		updatedK0sControllerConfig := &bootstrapv1.K0sControllerConfig{}
		assert.NoError(c, testEnv.Get(ctx, client.ObjectKeyFromObject(k0sControllerConfig), updatedK0sControllerConfig))

		assert.True(c, updatedK0sControllerConfig.Status.Ready)
		assert.NotNil(c, updatedK0sControllerConfig.Status.DataSecretName)
		assert.Equal(c, *updatedK0sControllerConfig.Status.DataSecretName, updatedK0sControllerConfig.Name)
		assert.True(c, conditions.IsTrue(updatedK0sControllerConfig, bootstrapv1.DataSecretAvailableCondition))
	}, 20*time.Second, 100*time.Millisecond)
}
