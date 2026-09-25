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

	bootstrapv1 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta1"
	bootstrapv2 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubeadmbootstrapv1 "sigs.k8s.io/cluster-api/api/bootstrap/kubeadm/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
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
		Spec: bootstrapv1.K0sControllerConfigSpec{
			Version: machineForControllerConfig.Spec.Version,
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
	cluster.Spec.Paused = new(true)

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
		Spec: bootstrapv1.K0sControllerConfigSpec{
			Version: machineForControllerConfig.Spec.Version,
		},
	}
	require.NoError(t, testEnv.Create(ctx, k0sControllerConfig))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(k0sControllerConfig, cluster, machineForControllerConfig, ns)

	r := &ControlPlaneController{
		Client: testEnv,
	}

	requireCached(t, cluster, machineForControllerConfig, k0sControllerConfig)

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sControllerConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)

	requirePausedReported(t, k0sControllerConfig)
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
		Spec: bootstrapv1.K0sControllerConfigSpec{
			Version: machineForControllerConfig.Spec.Version,
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

	requireCached(t, cluster, machineForControllerConfig, k0sControllerConfig)

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sControllerConfig)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)

	requirePausedReported(t, k0sControllerConfig)
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
		Spec: bootstrapv1.K0sControllerConfigSpec{
			Version: machineForControllerConfig.Spec.Version,
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
		Client:              testEnv,
		SecretCachingClient: secretCachingClient,
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(k0sControllerConfig)})
		assert.NoError(c, err)
		assert.Equal(c, ctrl.Result{}, result)
		// We assume that the bootstrap data is already created, so secret bootstrap data shouldn't be created again.
		assert.True(c, apierrors.IsNotFound(testEnv.Get(ctx, client.ObjectKeyFromObject(k0sControllerConfig), &corev1.Secret{})))
	}, 10*time.Second, 100*time.Millisecond)

	// A settled config still records the generation it observed, which is what keeps
	// the staleness signal usable after the data secret exists. Read as v1beta2, since
	// the field is only on the stored version and converting down drops it.
	hub := &bootstrapv2.K0sControllerConfig{}
	key := client.ObjectKeyFromObject(k0sControllerConfig)
	caughtUp := func() bool {
		if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
			return false
		}

		// Uncached, since the status is patched and a cached read still answers from
		// before the patch.
		if err := testEnv.GetAPIReader().Get(ctx, key, hub); err != nil {
			return false
		}

		return hub.Status.ObservedGeneration == hub.Generation
	}

	require.Eventually(t, caughtUp, 10*time.Second, 100*time.Millisecond,
		"a settled config still has to record the generation it observed")
	require.NotZero(t, hub.Generation, "a zero generation would make the assertion above vacuous")

	// The deferred summary runs for a settled config now, and it is computed from the
	// DataSecretAvailable condition, which this config never carried. Re-asserting it
	// in that branch is what keeps ConfigReady from being written Unknown for good.
	ready := conditions.Get(hub, string(bootstrapv2.ConfigReadyCondition))
	require.NotNil(t, ready, "the settled branch has to report readiness")
	require.Equal(t, metav1.ConditionTrue, ready.Status,
		"a config whose data secret exists is ready, whatever conditions it was missing")

	before := hub.Generation
	hub.Spec.Version = "v1.31.0+k0s.0"
	require.NoError(t, testEnv.Update(ctx, hub))
	require.NoError(t, testEnv.GetAPIReader().Get(ctx, key, hub))
	require.Greater(t, hub.Generation, before, "the edit has to move the generation")

	require.Eventually(t, caughtUp, 10*time.Second, 100*time.Millisecond,
		"the field did not follow the generation on a config that is already done")
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
		Spec: bootstrapv1.K0sControllerConfigSpec{
			Version: machineForControllerConfig.Spec.Version,
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
		Spec: bootstrapv1.K0sControllerConfigSpec{
			Version: machineForControllerConfig.Spec.Version,
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
		if assert.NotNil(c, updatedK0sControllerConfig.Status.DataSecretName) {
			assert.Equal(c, *updatedK0sControllerConfig.Status.DataSecretName, updatedK0sControllerConfig.Name)
		}
		assert.True(c, conditions.IsTrue(updatedK0sControllerConfig, bootstrapv1.DataSecretAvailableCondition))
	}, 20*time.Second, 100*time.Millisecond)
}

func TestGenControlPlaneJoinFilesLeavesNoTokenWhenTheJoinHostIsUnknown(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-join-token-not-leaked")
	require.NoError(t, err)

	kcpName := fmt.Sprintf("kcp-join-token-%s", util.RandomString(6))
	cluster := newCluster(ns.Name)
	cluster.Spec.ControlPlaneRef = clusterv1.ContractVersionedObjectReference{
		Kind:     "K0sControlPlane",
		Name:     kcpName,
		APIGroup: cpv1beta2.GroupVersion.Group,
	}
	require.NoError(t, testEnv.Create(ctx, cluster))

	kcp := &cpv1beta2.K0sControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cpv1beta2.GroupVersion.String(),
			Kind:       "K0sControlPlane",
		},
		ObjectMeta: metav1.ObjectMeta{Name: kcpName, Namespace: ns.Name, UID: "1"},
		Spec: cpv1beta2.K0sControlPlaneSpec{
			MachineTemplate: &cpv1beta2.K0sControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					Kind:       "GenericInfrastructureMachineTemplate",
					Namespace:  ns.Name,
					Name:       "infra-join-token",
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
				},
			},
			Replicas: int32(1),
			Version:  "v1.30.0",
		},
	}
	require.NoError(t, testEnv.Create(ctx, kcp))

	// The workload cluster is this same test env, so the token secret would really be
	// created and is really observable.
	kubeconfigSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name(cluster.Name, secret.Kubeconfig),
			Namespace: cluster.Namespace,
			Labels:    map[string]string{clusterv1.ClusterNameLabel: cluster.Name},
		},
		Data: map[string][]byte{
			secret.KubeconfigDataName: kubeconfig.FromEnvTestConfig(testEnv.Config, cluster),
		},
	}
	require.NoError(t, testEnv.Create(ctx, kubeconfigSecret))

	clusterCerts := secret.NewCertificatesForInitialControlPlane(&kubeadmbootstrapv1.ClusterConfiguration{})
	require.NoError(t, clusterCerts.Generate())
	caCertSecret := clusterCerts.GetByPurpose(secret.ClusterCA).AsSecret(
		client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name},
		*metav1.NewControllerRef(kcp, cpv1beta2.GroupVersion.WithKind("K0sControlPlane")),
	)
	require.NoError(t, testEnv.Create(ctx, caCertSecret))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(caCertSecret, kubeconfigSecret, kcp, cluster, ns)

	before := countBootstrapTokens(t)

	c := &ControlPlaneController{
		Client:              testEnv,
		SecretCachingClient: secretCachingClient,
	}
	scope := &ControllerScope{
		Cluster: cluster,
		Config: &bootstrapv2.K0sControllerConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "joining-controller", Namespace: ns.Name},
			Spec: bootstrapv2.K0sControllerConfigSpec{
				Version: "v1.30.0+k0s.0",
				K0sConfigSpec: &bootstrapv2.K0sConfigSpec{
					K0s: &unstructured.Unstructured{Object: map[string]any{}},
				},
			},
		},
	}

	// No machine carries an address, so the join host cannot be resolved and the
	// generation fails after the point the credential used to be created.
	_, err = c.genControlPlaneJoinFiles(ctx, scope, nil)
	require.Error(t, err)

	require.Equal(t, before, countBootstrapTokens(t),
		"a failed join file generation left a usable join token behind in the workload cluster")
}

func countBootstrapTokens(t *testing.T) int {
	t.Helper()

	secrets := &corev1.SecretList{}
	require.NoError(t, testEnv.List(ctx, secrets, client.InNamespace("kube-system")))

	n := 0
	for _, s := range secrets.Items {
		if s.Type == corev1.SecretTypeBootstrapToken {
			n++
		}
	}

	return n
}

func TestGetK0sTokenLeavesNoTokenWhenTheClusterCAIsMissing(t *testing.T) {
	ns, err := testEnv.CreateNamespace(ctx, "test-worker-join-token-not-leaked")
	require.NoError(t, err)

	cluster := newCluster(ns.Name)
	require.NoError(t, testEnv.Create(ctx, cluster))

	defer func(do ...client.Object) {
		require.NoError(t, testEnv.Cleanup(ctx, do...))
	}(cluster, ns)

	before := countBootstrapTokens(t)

	r := &Controller{
		Client:                testEnv,
		SecretCachingClient:   secretCachingClient,
		workloadClusterClient: testEnv,
	}
	scope := &Scope{
		Cluster:             cluster,
		client:              testEnv,
		secretCachingClient: secretCachingClient,
		Config: &bootstrapv2.K0sWorkerConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "joining-worker", Namespace: ns.Name},
		},
	}

	// The cluster has no certificate secrets, so the token cannot be signed and the
	// lookup fails after the point the credential used to be created.
	_, err = r.getK0sToken(ctx, scope)
	require.Error(t, err)

	require.Equal(t, before, countBootstrapTokens(t),
		"a failed token generation left a usable join token behind in the workload cluster")
}
