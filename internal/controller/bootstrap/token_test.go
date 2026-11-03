//go:build !envtest

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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	bootstrapapi "k8s.io/cluster-bootstrap/token/api"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	bootstrapv2 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
)

const testTokenID = "abcdef"

func newTokenSecret(expiration time.Time) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "bootstrap-token-" + testTokenID, Namespace: metav1.NamespaceSystem},
		Type:       corev1.SecretTypeBootstrapToken,
		Data: map[string][]byte{
			bootstrapapi.BootstrapTokenIDKey:         []byte(testTokenID),
			bootstrapapi.BootstrapTokenExpirationKey: []byte(expiration.UTC().Format(time.RFC3339)),
		},
	}
}

func getTokenExpiration(t *testing.T, c client.Client) time.Time {
	t.Helper()
	s, err := getTokenSecret(context.Background(), c, testTokenID)
	require.NoError(t, err)
	exp, err := time.Parse(time.RFC3339, string(s.Data[bootstrapapi.BootstrapTokenExpirationKey]))
	require.NoError(t, err)
	return exp
}

func TestRefreshBootstrapToken(t *testing.T) {
	ttl := 15 * time.Minute

	t.Run("fresh token is not refreshed", func(t *testing.T) {
		exp := time.Now().Add(ttl).Truncate(time.Second)
		c := fake.NewClientBuilder().WithObjects(newTokenSecret(exp)).Build()
		refreshed, err := refreshBootstrapTokenIfNeeded(context.Background(), c, testTokenID, ttl)
		require.NoError(t, err)
		require.False(t, refreshed)
		require.True(t, getTokenExpiration(t, c).Equal(exp))
	})

	t.Run("token close to expire is refreshed", func(t *testing.T) {
		c := fake.NewClientBuilder().WithObjects(newTokenSecret(time.Now().Add(ttl / 2))).Build()
		refreshed, err := refreshBootstrapTokenIfNeeded(context.Background(), c, testTokenID, ttl)
		require.NoError(t, err)
		require.True(t, refreshed)
		require.WithinDuration(t, time.Now().Add(ttl), getTokenExpiration(t, c), 5*time.Second)
	})

	t.Run("missing token returns not found", func(t *testing.T) {
		c := fake.NewClientBuilder().Build()
		refreshed, err := refreshBootstrapTokenIfNeeded(context.Background(), c, testTokenID, ttl)
		require.Error(t, err)
		require.False(t, refreshed)
		require.Error(t, err)
	})
}

func newConfigOwner(t *testing.T, obj client.Object) *bsutil.ConfigOwner {
	t.Helper()
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	require.NoError(t, err)
	return &bsutil.ConfigOwner{Unstructured: &unstructured.Unstructured{Object: m}}
}

func TestWorkerReconcileBootstrapToken(t *testing.T) {
	ttl := 15 * time.Minute
	config := &bootstrapv2.K0sWorkerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "worker",
			Annotations: map[string]string{bootstrapTokenIDAnnotation: testTokenID},
		},
	}
	machine := &clusterv1.Machine{TypeMeta: metav1.TypeMeta{APIVersion: clusterv1.GroupVersion.String(), Kind: "Machine"}}
	joinedMachine := machine.DeepCopy()
	joinedMachine.Status.NodeRef = clusterv1.MachineNodeReference{Name: "node"}

	t.Run("not joined machine refreshes the token", func(t *testing.T) {
		wc := fake.NewClientBuilder().WithObjects(newTokenSecret(time.Now().Add(time.Minute))).Build()
		r := &Controller{TokenTTL: ttl, workloadClusterClient: wc}
		res, _, err := r.reconcileBootstrapToken(context.Background(), &clusterv1.Cluster{}, config, newConfigOwner(t, machine))
		require.NoError(t, err)
		requireRequeueWithinJitter(t, ttl, res)
		require.WithinDuration(t, time.Now().Add(ttl), getTokenExpiration(t, wc), 5*time.Second)
	})

	t.Run("joined machine revokes the token", func(t *testing.T) {
		config := config.DeepCopy()
		wc := fake.NewClientBuilder().WithObjects(newTokenSecret(time.Now().Add(ttl))).Build()
		r := &Controller{TokenTTL: ttl, workloadClusterClient: wc}
		res, rotate, err := r.reconcileBootstrapToken(context.Background(), &clusterv1.Cluster{}, config, newConfigOwner(t, joinedMachine))
		require.NoError(t, err)
		require.False(t, rotate)
		require.Equal(t, ctrl.Result{}, res)
		requireTokenRevoked(t, wc, config)
	})

	t.Run("joined machine stops tracking a token that is already gone", func(t *testing.T) {
		config := config.DeepCopy()
		r := &Controller{TokenTTL: ttl, workloadClusterClient: fake.NewClientBuilder().Build()}
		res, rotate, err := r.reconcileBootstrapToken(context.Background(), &clusterv1.Cluster{}, config, newConfigOwner(t, joinedMachine))
		require.NoError(t, err)
		require.False(t, rotate)
		require.Equal(t, ctrl.Result{}, res)
		require.NotContains(t, config.GetAnnotations(), bootstrapTokenIDAnnotation)
	})

	pool := &clusterv1.MachinePool{
		TypeMeta: metav1.TypeMeta{APIVersion: clusterv1.GroupVersion.String(), Kind: "MachinePool"},
		Spec:     clusterv1.MachinePoolSpec{Replicas: new(int32(1))},
	}
	joinedPool := pool.DeepCopy()
	joinedPool.Status.NodeRefs = []corev1.ObjectReference{{Name: "node"}}

	for _, tc := range []struct {
		name       string
		owner      client.Object
		token      []client.Object
		wantRotate bool
		wantErr    bool
	}{
		{name: "joined pool keeps a token before half its lifetime", owner: joinedPool, token: []client.Object{newTokenSecret(time.Now().Add(ttl / 2).Add(time.Minute))}},
		{name: "joined pool rotates a token past half its lifetime", owner: joinedPool, token: []client.Object{newTokenSecret(time.Now().Add(ttl / 2).Add(-time.Minute))}, wantRotate: true},
		{name: "joined pool rotates a missing token", owner: joinedPool, wantRotate: true},
		{name: "pool still joining rotates a missing token", owner: pool, wantRotate: true},
		{name: "machine still joining reports a missing token", owner: machine, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Controller{TokenTTL: ttl, workloadClusterClient: fake.NewClientBuilder().WithObjects(tc.token...).Build()}
			res, rotate, err := r.reconcileBootstrapToken(t.Context(), &clusterv1.Cluster{}, config, newConfigOwner(t, tc.owner))
			require.Equal(t, tc.wantErr, err != nil)
			require.Equal(t, tc.wantRotate, rotate)
			if !rotate && !tc.wantErr {
				requireRequeueWithinJitter(t, ttl, res)
			}
		})
	}

	t.Run("config without tracked token is skipped", func(t *testing.T) {
		r := &Controller{TokenTTL: ttl}
		res, _, err := r.reconcileBootstrapToken(context.Background(), &clusterv1.Cluster{}, &bootstrapv2.K0sWorkerConfig{}, newConfigOwner(t, machine))
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)
	})
}

func TestControllerReconcileBootstrapToken(t *testing.T) {
	ttl := 15 * time.Minute
	config := &bootstrapv2.K0sControllerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "controller",
			Annotations: map[string]string{bootstrapTokenIDAnnotation: testTokenID},
		},
	}
	machine := &clusterv1.Machine{
		TypeMeta:   metav1.TypeMeta{APIVersion: clusterv1.GroupVersion.String(), Kind: "Machine"},
		ObjectMeta: metav1.ObjectMeta{Name: "controller-0"},
	}
	joinedMachine := machine.DeepCopy()
	joinedMachine.Status.NodeRef = clusterv1.MachineNodeReference{Name: "node"}

	controlNode := &unstructured.Unstructured{}
	controlNode.SetAPIVersion("autopilot.k0sproject.io/v1beta2")
	controlNode.SetKind("ControlNode")
	controlNode.SetName(machine.Name)

	newScope := func(config *bootstrapv2.K0sControllerConfig, owner client.Object) *ControllerScope {
		return &ControllerScope{Config: config, ConfigOwner: newConfigOwner(t, owner), Cluster: &clusterv1.Cluster{}}
	}

	t.Run("not joined controller refreshes the token", func(t *testing.T) {
		config := config.DeepCopy()
		wc := fake.NewClientBuilder().WithObjects(newTokenSecret(time.Now().Add(time.Minute))).Build()
		c := &ControlPlaneController{TokenTTL: ttl, workloadClusterClient: wc}
		res, err := c.reconcileBootstrapToken(t.Context(), newScope(config, machine))
		require.NoError(t, err)
		requireRequeueWithinJitter(t, ttl, res)
		require.WithinDuration(t, time.Now().Add(ttl), getTokenExpiration(t, wc), 5*time.Second)
		require.Equal(t, testTokenID, config.GetAnnotations()[bootstrapTokenIDAnnotation])
	})

	for _, tc := range []struct {
		name  string
		owner client.Object
		objs  []client.Object
	}{
		{name: "controller with a node revokes the token", owner: joinedMachine},
		{name: "controller without a worker revokes the token once its ControlNode exists", owner: machine, objs: []client.Object{controlNode}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := config.DeepCopy()
			wc := fake.NewClientBuilder().WithObjects(append(tc.objs, newTokenSecret(time.Now().Add(ttl)))...).Build()
			c := &ControlPlaneController{TokenTTL: ttl, workloadClusterClient: wc}
			res, err := c.reconcileBootstrapToken(t.Context(), newScope(config, tc.owner))
			require.NoError(t, err)
			require.Equal(t, ctrl.Result{}, res)
			requireTokenRevoked(t, wc, config)
		})
	}
}

// requireTokenRevoked asserts that the test token is deleted from the workload cluster and no longer tracked by config.
func requireTokenRevoked(t *testing.T, c client.Client, config client.Object) {
	t.Helper()
	_, err := getTokenSecret(context.Background(), c, testTokenID)
	require.True(t, apierrors.IsNotFound(err), "expected the bootstrap token to be deleted, got %v", err)
	require.NotContains(t, config.GetAnnotations(), bootstrapTokenIDAnnotation)
}

// requireRequeueWithinJitter asserts that res requeues after the refresh interval plus up to the allowed jitter.
func requireRequeueWithinJitter(t require.TestingT, ttl time.Duration, res ctrl.Result) {
	base := ttl / 3
	require.GreaterOrEqual(t, res.RequeueAfter, base)
	require.LessOrEqual(t, res.RequeueAfter, base+time.Duration(float64(base)*tokenRefreshJitterFactor))
}
