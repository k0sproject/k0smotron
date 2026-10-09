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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	bootstrapapi "k8s.io/cluster-bootstrap/token/api"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/kubeadm/reconcilers/kubeadmconfig"
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
		res, err := r.reconcileBootstrapToken(context.Background(), &clusterv1.Cluster{}, config, newConfigOwner(t, machine))
		require.NoError(t, err)
		requireRequeueWithinJitter(t, ttl, res)
		require.WithinDuration(t, time.Now().Add(ttl), getTokenExpiration(t, wc), 5*time.Second)
	})

	t.Run("joined machine leaves the token alone", func(t *testing.T) {
		exp := time.Now().Add(time.Minute).Truncate(time.Second)
		wc := fake.NewClientBuilder().WithObjects(newTokenSecret(exp)).Build()
		r := &Controller{TokenTTL: ttl, workloadClusterClient: wc}
		res, err := r.reconcileBootstrapToken(context.Background(), &clusterv1.Cluster{}, config, newConfigOwner(t, joinedMachine))
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)
		require.True(t, getTokenExpiration(t, wc).Equal(exp))
	})

	t.Run("config without tracked token is skipped", func(t *testing.T) {
		r := &Controller{TokenTTL: ttl}
		res, err := r.reconcileBootstrapToken(context.Background(), &clusterv1.Cluster{}, &bootstrapv2.K0sWorkerConfig{}, newConfigOwner(t, machine))
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)
	})
}

// requireRequeueWithinJitter asserts that res requeues after the refresh interval plus up to the allowed jitter.
func requireRequeueWithinJitter(t require.TestingT, ttl time.Duration, res ctrl.Result) {
	base := ttl / 3
	require.GreaterOrEqual(t, res.RequeueAfter, base)
	require.LessOrEqual(t, res.RequeueAfter, base+time.Duration(float64(base)*tokenRefreshJitterFactor))
}
