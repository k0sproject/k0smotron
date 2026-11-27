//go:build !envtest

/*
Copyright 2026.

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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	km "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
)

// fakeAPIServer is a minimal API server that counts the requests it receives. If a pod is given, it serves it
// as the running controller pod of the k0smotron cluster's StatefulSet and records the pods that were exec'd
// into, failing the exec itself. Any other request gets a 404.
type fakeAPIServer struct {
	*httptest.Server

	mu       sync.Mutex
	requests int
	execs    []string
}

func newFakeAPIServer(t *testing.T, kmc *km.Cluster, pod *v1.Pod) *fakeAPIServer {
	t.Helper()

	stsName := km.GetStatefulSetName(kmc.Name)
	sts := appsv1.StatefulSet{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
		ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: kmc.Namespace},
		Spec:       appsv1.StatefulSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": stsName}}},
	}

	s := &fakeAPIServer{}
	mux := http.NewServeMux()
	if pod != nil {
		pods := v1.PodList{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"},
			Items:    []v1.Pod{*pod},
		}
		mux.HandleFunc(fmt.Sprintf("GET /apis/apps/v1/namespaces/%s/statefulsets/%s", kmc.Namespace, stsName), func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sts)
		})
		mux.HandleFunc(fmt.Sprintf("GET /api/v1/namespaces/%s/pods", kmc.Namespace), func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(pods)
		})
		mux.HandleFunc(fmt.Sprintf("POST /api/v1/namespaces/%s/pods/{pod}/exec", kmc.Namespace), func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			s.execs = append(s.execs, r.PathValue("pod"))
			s.mu.Unlock()
			http.Error(w, "exec is not supported", http.StatusInternalServerError)
		})
	}

	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests++
		s.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *fakeAPIServer) recorded() (requests int, execs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests, append([]string(nil), s.execs...)
}

func newRemoteHostKubeconfigSecret(name, namespace, server string) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data: map[string][]byte{"value": fmt.Appendf(nil, `apiVersion: v1
kind: Config
clusters:
- cluster: {server: %s}
  name: host
contexts:
- context: {cluster: host, user: admin}
  name: host
current-context: host
users:
- name: admin
  user: {}
`, server)},
	}
}

func newTestJoinTokenRequestReconciler(t *testing.T, managementServer string, objs ...client.Object) *JoinTokenRequestReconciler {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(scheme))
	require.NoError(t, km.AddToScheme(scheme))

	restConfig := &rest.Config{Host: managementServer}
	clientSet, err := kubernetes.NewForConfig(restConfig)
	require.NoError(t, err)

	return &JoinTokenRequestReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objs...).
			WithStatusSubresource(&km.JoinTokenRequest{}).
			Build(),
		Scheme:     scheme,
		ClientSet:  clientSet,
		RESTConfig: restConfig,
	}
}

func newTestCluster(remoteHost *km.RemoteHostClusterSpec) *km.Cluster {
	return &km.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default", UID: "test-uid"},
		Spec:       km.ClusterSpec{RemoteHostCluster: remoteHost},
	}
}

func newTestControllerPod(name string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Status:     v1.PodStatus{Phase: v1.PodRunning},
	}
}

var testRemoteHostCluster = &km.RemoteHostClusterSpec{
	KubeconfigRef: &km.KubeconfigRef{Name: "host-kubeconfig", Namespace: "default", Key: "value"},
}

func TestJoinTokenRequestReconciler_hostingClusterClients(t *testing.T) {
	t.Run("control plane in the management cluster", func(t *testing.T) {
		r := newTestJoinTokenRequestReconciler(t, "https://management.example.com:6443")

		clientSet, restConfig, err := r.hostingClusterClients(context.Background(), newTestCluster(nil))
		require.NoError(t, err)
		assert.Same(t, r.ClientSet, clientSet)
		assert.Same(t, r.RESTConfig, restConfig)
	})

	t.Run("control plane in a remote host cluster", func(t *testing.T) {
		r := newTestJoinTokenRequestReconciler(t, "https://management.example.com:6443",
			newRemoteHostKubeconfigSecret("host-kubeconfig", "default", "https://host.example.com:6443"))

		clientSet, restConfig, err := r.hostingClusterClients(context.Background(), newTestCluster(testRemoteHostCluster))
		require.NoError(t, err)
		assert.NotSame(t, r.ClientSet, clientSet)
		assert.Equal(t, "https://host.example.com:6443", restConfig.Host)
	})

	t.Run("missing remote host cluster kubeconfig", func(t *testing.T) {
		r := newTestJoinTokenRequestReconciler(t, "https://management.example.com:6443")

		_, _, err := r.hostingClusterClients(context.Background(), newTestCluster(testRemoteHostCluster))
		require.ErrorContains(t, err, "failed to get client from the remote host cluster kubeconfig")
	})
}

// TestJoinTokenRequestReconcile_RemoteHostCluster checks that the token is created and invalidated in the
// controller pod running in the remote host cluster, and that the management cluster is not queried for it.
func TestJoinTokenRequestReconcile_RemoteHostCluster(t *testing.T) {
	newJTR := func() *km.JoinTokenRequest {
		return &km.JoinTokenRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "token",
				Namespace:  "default",
				Finalizers: []string{joinTokenRequestFinalizer},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: km.GroupVersion.String(),
					Kind:       "Cluster",
					Name:       "test",
					UID:        "test-uid",
					Controller: new(true),
				}},
			},
			Spec: km.JoinTokenRequestSpec{ClusterName: "test", Role: "worker", Expiry: "1h"},
		}
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "token", Namespace: "default"}}

	t.Run("token creation", func(t *testing.T) {
		kmc := newTestCluster(testRemoteHostCluster)
		management := newFakeAPIServer(t, kmc, nil)
		host := newFakeAPIServer(t, kmc, newTestControllerPod("host-pod"))
		r := newTestJoinTokenRequestReconciler(t, management.URL, kmc, newJTR(),
			newRemoteHostKubeconfigSecret("host-kubeconfig", "default", host.URL))

		_, err := r.Reconcile(context.Background(), req)
		require.ErrorContains(t, err, "failed to exec command")

		_, execs := host.recorded()
		assert.Equal(t, []string{"host-pod"}, execs)
		requests, _ := management.recorded()
		assert.Zero(t, requests)

		var jtr km.JoinTokenRequest
		require.NoError(t, r.Get(context.Background(), req.NamespacedName, &jtr))
		cond := conditions.Get(&jtr, km.JoinTokenRequestSecretCondition)
		require.NotNil(t, cond)
		assert.True(t, strings.HasPrefix(cond.Message, "Failed getting token"), cond.Message)
	})

	t.Run("token invalidation on deletion", func(t *testing.T) {
		kmc := newTestCluster(testRemoteHostCluster)
		management := newFakeAPIServer(t, kmc, nil)
		host := newFakeAPIServer(t, kmc, newTestControllerPod("host-pod"))
		jtr := newJTR()
		jtr.DeletionTimestamp = new(metav1.Now())
		jtr.Status.TokenID = "abcdef"
		r := newTestJoinTokenRequestReconciler(t, management.URL, kmc, jtr,
			newRemoteHostKubeconfigSecret("host-kubeconfig", "default", host.URL))

		_, err := r.Reconcile(context.Background(), req)
		require.ErrorContains(t, err, "failed to exec command")

		_, execs := host.recorded()
		assert.Equal(t, []string{"host-pod"}, execs)
		requests, _ := management.recorded()
		assert.Zero(t, requests)
	})

	t.Run("deletion after the cluster is gone", func(t *testing.T) {
		kmc := newTestCluster(testRemoteHostCluster)
		management := newFakeAPIServer(t, kmc, nil)
		jtr := newJTR()
		jtr.DeletionTimestamp = new(metav1.Now())
		jtr.Status.TokenID = "abcdef"
		r := newTestJoinTokenRequestReconciler(t, management.URL, jtr)

		_, err := r.Reconcile(context.Background(), req)
		require.NoError(t, err)

		requests, _ := management.recorded()
		assert.Zero(t, requests)
		// Removing the finalizer lets the JoinTokenRequest go away.
		err = r.Get(context.Background(), req.NamespacedName, &km.JoinTokenRequest{})
		assert.True(t, apierrors.IsNotFound(err), "expected the JoinTokenRequest to be deleted, got %v", err)
	})
}
