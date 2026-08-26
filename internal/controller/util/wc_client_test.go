/*
Copyright 2024 k0smotron authors.

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

package util

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stretchr/testify/require"

	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
)

// Test_isTunneledRestConfigPossible pins which control planes are reached through a
// tunnel, since that decides which kubeconfig is used.
func Test_isTunneledRestConfigPossible(t *testing.T) {
	kcp := func(enabled bool, args ...string) *cpv1beta2.K0sControlPlane {
		cp := &cpv1beta2.K0sControlPlane{}
		cp.Spec.K0sConfigSpec.Tunneling.Enabled = enabled
		cp.Spec.K0sConfigSpec.Args = args

		return cp
	}

	for _, tc := range []struct {
		name string
		cp   *cpv1beta2.K0sControlPlane
		want bool
	}{
		{
			name: "a hosted control plane does not tunnel",
			cp:   nil,
		},
		{
			name: "tunneling off",
			cp:   kcp(false, "--enable-worker"),
		},
		{
			name: "tunneling on without a worker cannot tunnel yet",
			cp:   kcp(true),
		},
		{
			name: "tunneling on with a worker",
			cp:   kcp(true, "--enable-worker"),
			want: true,
		},
		{
			name: "the worker flag among others",
			cp:   kcp(true, "--single", "--enable-worker", "--debug"),
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isTunneledRestConfigPossible(tc.cp))
		})
	}
}

// TestRestConfigFromKubeconfigIsBounded covers every request having a deadline. The
// discovery a fresh client does on first use ignores the caller's context.
func TestRestConfigFromKubeconfigIsBounded(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-kubeconfig", Namespace: "default"},
		Data: map[string][]byte{secret.KubeconfigDataName: []byte(`apiVersion: v1
kind: Config
clusters:
- cluster: {server: https://192.0.2.1:6443}
  name: workload
contexts:
- context: {cluster: workload, user: admin}
  name: workload
current-context: workload
users:
- name: admin
  user: {}
`)},
	}).Build()

	got, err := fromKubeconfigSecretToRestConfig(context.Background(), c,
		client.ObjectKey{Namespace: "default", Name: "test-kubeconfig"})
	require.NoError(t, err)
	require.Equal(t, 10*time.Second, got.Timeout,
		"a kubeconfig carries no timeout, so one has to be set here")
}

// TestGetControllerRuntimeClientRejectsNoConfig covers the tunneling switch having no
// default, so an unset mode reports instead of dereferencing nothing.
func TestGetControllerRuntimeClientRejectsNoConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	kcp := &cpv1beta2.K0sControlPlane{}
	kcp.Spec.K0sConfigSpec.Tunneling.Enabled = true
	kcp.Spec.K0sConfigSpec.Args = []string{"--enable-worker"}
	// Mode is deliberately unset, which the switch does not handle.

	cl, err := GetControllerRuntimeClient(context.Background(),
		fake.NewClientBuilder().WithScheme(scheme).Build(), nil, kcp,
		client.ObjectKey{Namespace: "default", Name: "test"})

	require.Error(t, err)
	require.Nil(t, cl)
}

// kubeconfigSecret writes a kubeconfig secret with whatever cluster and user stanzas
// the caller needs.
func kubeconfigSecret(name, clusterFields, userFields string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Data: map[string][]byte{secret.KubeconfigDataName: []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
%s
  name: workload
contexts:
- context: {cluster: workload, user: admin}
  name: workload
current-context: workload
users:
- name: admin
  user:
%s
`, clusterFields, userFields))},
	}
}

// TestGetControllerRuntimeClientKeepsAuth covers the client carrying the credentials
// the kubeconfig names. Handing client.New a hand built transport drops all of them.
func TestGetControllerRuntimeClientKeepsAuth(t *testing.T) {
	seen := make(chan string, 8)

	// TLS, since client-go carries a bearer token only over a secure connection.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case seen <- r.Header.Get("Authorization"):
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"APIVersions","versions":["v1"]}`))
	}))
	t.Cleanup(srv.Close)

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	hub := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		kubeconfigSecret("test-kubeconfig",
			"    server: "+srv.URL+"\n    insecure-skip-tls-verify: true", "    token: sekret")).Build()

	cl, err := GetControllerRuntimeClient(context.Background(), hub, nil, nil,
		client.ObjectKey{Namespace: "default", Name: "test"})
	require.NoError(t, err)

	// The read itself does not have to succeed, only carry the token.
	_ = cl.Get(context.Background(), client.ObjectKey{Name: "kube-system"}, &corev1.Namespace{})

	select {
	case got := <-seen:
		require.Equal(t, "Bearer sekret", got, "a token kubeconfig has to reach the workload cluster")
	default:
		t.Fatal("the client never talked to the server")
	}
}

// TestGetControllerRuntimeClientKeepsProxy covers proxy mode tunneling, where the
// proxy is the only route to the workload cluster.
func TestGetControllerRuntimeClientKeepsProxy(t *testing.T) {
	proxied := make(chan string, 8)

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case proxied <- r.URL.String():
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"APIVersions","versions":["v1"]}`))
	}))
	t.Cleanup(proxy.Close)

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	// A server address nothing listens on, so only the proxy can answer.
	hub := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		kubeconfigSecret("test-kubeconfig",
			"    server: http://192.0.2.1:6443\n    proxy-url: "+proxy.URL, "    token: sekret")).Build()

	cl, err := GetControllerRuntimeClient(context.Background(), hub, nil, nil,
		client.ObjectKey{Namespace: "default", Name: "test"})
	require.NoError(t, err)

	_ = cl.Get(context.Background(), client.ObjectKey{Name: "kube-system"}, &corev1.Namespace{})

	select {
	case got := <-proxied:
		require.Contains(t, got, "192.0.2.1", "the request has to go out through the proxy")
	default:
		t.Fatal("the proxy the kubeconfig names was bypassed")
	}
}
