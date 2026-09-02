//go:build !envtest

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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	"github.com/go-logr/logr"
	bootstrapv2 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
	kapi "github.com/k0sproject/k0smotron/v2/api/k0smotron.io/v1beta2"
	kutil "github.com/k0sproject/k0smotron/v2/internal/controller/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/controllers/clustercache"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func Test_machineStatusCompute(t *testing.T) {
	t.Run("test no machines", func(t *testing.T) {
		kcp := &cpv1beta2.K0sControlPlane{
			Spec: cpv1beta2.K0sControlPlaneSpec{
				Version:  "v1.31.0",
				Replicas: 3,
			},
		}

		scope := &controlplane{
			kcp:            kcp,
			activeMachines: collections.Machines{},
		}
		err := computeReplicas(scope)

		require.NoError(t, err)
		require.Zero(t, ptr.Deref(kcp.Status.Replicas, 0))
		require.Empty(t, kcp.Status.Version)
		require.True(t, *kcp.Status.ExternalManagedControlPlane)
	})

	t.Run("test all machines are ready and available", func(t *testing.T) {
		kcp := &cpv1beta2.K0sControlPlane{
			Spec: cpv1beta2.K0sControlPlaneSpec{
				Version:  "v1.31.0",
				Replicas: 2,
			},
		}
		activeMachines := collections.Machines{
			"machine1": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: "v1.31.0",
				},
				Status: clusterv1.MachineStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.MachineReadyCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineAvailableCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineUpToDateCondition,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			"machine2": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: "v1.30.0",
				},
				Status: clusterv1.MachineStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.MachineReadyCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineAvailableCondition,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
		}

		scope := &controlplane{
			kcp:            kcp,
			activeMachines: activeMachines,
			upToDateMachines: collections.Machines{
				"machine1": activeMachines["machine1"],
			},
		}
		err := computeReplicas(scope)

		require.NoError(t, err)
		require.Equal(t, int32(2), *kcp.Status.Replicas)
		require.Equal(t, int32(2), *kcp.Status.AvailableReplicas)
		require.Equal(t, int32(1), *kcp.Status.UpToDateReplicas)
		require.Equal(t, int32(2), *kcp.Status.ReadyReplicas)
		require.True(t, *kcp.Status.ExternalManagedControlPlane)
		require.Equal(t, "v1.30.0", kcp.Status.Version)
	})

	t.Run("test all ready and available are ready but not using suffix", func(t *testing.T) {
		kcp := &cpv1beta2.K0sControlPlane{
			Spec: cpv1beta2.K0sControlPlaneSpec{
				Version:  "v1.31.0+k0s.0",
				Replicas: 2,
			},
		}
		machines := collections.Machines{
			"machine1": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: "v1.31.0",
				},
				Status: clusterv1.MachineStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.MachineReadyCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineAvailableCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineUpToDateCondition,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			"machine2": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: "v1.30.0",
				},
				Status: clusterv1.MachineStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.MachineReadyCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineAvailableCondition,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
		}

		scope := &controlplane{
			kcp:            kcp,
			activeMachines: machines,
			upToDateMachines: collections.Machines{
				"machine1": machines["machine1"],
			},
		}
		err := computeReplicas(scope)

		require.NoError(t, err)
		require.Equal(t, int32(2), *kcp.Status.Replicas)
		require.Equal(t, int32(2), *kcp.Status.AvailableReplicas)
		require.Equal(t, int32(1), *kcp.Status.UpToDateReplicas)
		require.Equal(t, int32(2), *kcp.Status.ReadyReplicas)
		require.True(t, *kcp.Status.ExternalManagedControlPlane)
		require.Equal(t, "v1.30.0+k0s.0", kcp.Status.Version)
	})

	t.Run("test non existent machines are unavailable and external managed is true", func(t *testing.T) {
		kcp := &cpv1beta2.K0sControlPlane{
			Spec: cpv1beta2.K0sControlPlaneSpec{
				Version:  "v1.31.0",
				Replicas: 3,
				K0sConfigSpec: bootstrapv2.K0sConfigSpec{
					Args: []string{"--enable-worker"},
				},
			},
		}
		machines := collections.Machines{
			"machine1": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: "v1.31.0",
				},
				Status: clusterv1.MachineStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.MachineReadyCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineUpToDateCondition,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			"machine2": &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: "v1.30.0",
				},
				Status: clusterv1.MachineStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.MachineReadyCondition,
							Status: metav1.ConditionTrue,
						},
						{
							Type:   clusterv1.MachineAvailableCondition,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
		}

		scope := &controlplane{
			kcp:            kcp,
			activeMachines: machines,
			upToDateMachines: collections.Machines{
				"machine1": machines["machine1"],
			},
		}
		err := computeReplicas(scope)

		require.NoError(t, err)
		require.Equal(t, int32(2), *kcp.Status.Replicas)
		require.Equal(t, int32(1), *kcp.Status.AvailableReplicas)
		require.Equal(t, int32(1), *kcp.Status.UpToDateReplicas)
		require.Equal(t, int32(2), *kcp.Status.ReadyReplicas)
		require.Nil(t, kcp.Status.ExternalManagedControlPlane)
		require.Equal(t, "v1.30.0", kcp.Status.Version)
	})
}

func Test_versionMatches(t *testing.T) {
	type args struct {
		machine *clusterv1.Machine
		ver     string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "version matches, both without suffix",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: "v1.31.0",
					},
				},
				ver: "v1.31.0",
			},
			want: true,
		},
		{
			name: "version does not match",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: "v1.31.0",
					},
				},
				ver: "v1.30.0",
			},
			want: false,
		},
		{
			name: "semver version match, machine version is missing the suffix",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: "v1.31.0",
					},
				},
				ver: "v1.31.0+k0s.0",
			},
			want: true,
		},
		{
			name: "semver version match, kcp version is missing the suffix",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: "v1.31.0+k0s.0",
					},
				},
				ver: "v1.31.0",
			},
			want: true,
		},
		{
			name: "versions match, both with the suffix",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: "v1.31.0+k0s.0",
					},
				},
				ver: "v1.31.0+k0s.0",
			},
			want: true,
		},
		{
			name: "versions do not match, machine version is missing",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: "",
					},
				},
				ver: "v1.31.0+k0s.0",
			},
			want: false,
		},
		{
			name: "versions do not match, machine version is empty",
			args: args{
				machine: &clusterv1.Machine{
					Spec: clusterv1.MachineSpec{
						Version: "",
					},
				},
				ver: "v1.31.0+k0s.0",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionMatches(tt.args.machine, tt.args.ver); got != tt.want {
				t.Errorf("versionMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

// newWorkloadAPI serves just enough of an apiserver for a real client to reach
// it. nsStatus decides how a read of the kube system namespace answers.
func newWorkloadAPI(t *testing.T, nsStatus int) *httptest.Server {
	writeJSON := func(w http.ResponseWriter, code int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		assert.NoError(t, json.NewEncoder(w).Encode(body))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "APIVersions", "versions": []string{"v1"}})
	})
	mux.HandleFunc("/apis", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": []any{}})
	})
	mux.HandleFunc("/api/v1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"kind": "APIResourceList", "groupVersion": "v1",
			"resources": []any{map[string]any{
				"name": "namespaces", "singularName": "", "namespaced": false, "kind": "Namespace",
				"verbs": []string{"get"},
			}},
		})
	})
	mux.HandleFunc("/api/v1/namespaces/kube-system", func(w http.ResponseWriter, _ *http.Request) {
		if nsStatus != http.StatusOK {
			writeJSON(w, nsStatus, map[string]any{
				"kind": "Status", "apiVersion": "v1", "status": "Failure",
				"message": "etcdserver request timed out", "code": nsStatus,
			})

			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"kind": "Namespace", "apiVersion": "v1",
			"metadata": map[string]any{"name": "kube-system", "uid": "8f1c"},
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// newSwitchableWorkloadAPI serves the same thing as newWorkloadAPI, but lets a
// test turn the namespace read on and off so a recovery can be exercised.
func newSwitchableWorkloadAPI(t *testing.T) (*httptest.Server, *atomic.Bool) {
	healthy := &atomic.Bool{}

	writeJSON := func(w http.ResponseWriter, code int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		assert.NoError(t, json.NewEncoder(w).Encode(body))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "APIVersions", "versions": []string{"v1"}})
	})
	mux.HandleFunc("/apis", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": []any{}})
	})
	mux.HandleFunc("/api/v1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"kind": "APIResourceList", "groupVersion": "v1",
			"resources": []any{map[string]any{
				"name": "namespaces", "singularName": "", "namespaced": false, "kind": "Namespace",
				"verbs": []string{"get"},
			}},
		})
	})
	mux.HandleFunc("/api/v1/namespaces/kube-system", func(w http.ResponseWriter, _ *http.Request) {
		if !healthy.Load() {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"kind": "Status", "apiVersion": "v1", "status": "Failure",
				"message": "etcdserver request timed out", "code": http.StatusInternalServerError,
			})

			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"kind": "Namespace", "apiVersion": "v1",
			"metadata": map[string]any{"name": "kube-system", "uid": "8f1c"},
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, healthy
}

// availabilityFixture builds a control plane whose workload client is reached
// through a kubeconfig secret, which is the path taken with no cluster cache.
func availabilityFixture(t *testing.T, serverURL string, everUp bool) (*K0sController, *controlplane) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, clusterv1.AddToScheme(scheme))

	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}

	kcp := &cpv1beta2.K0sControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default", UID: "kcp-uid"}}
	if everUp {
		kcp.Status.Initialization.ControlPlaneInitialized = new(true)
		conditions.Set(kcp, metav1.Condition{
			Type:   string(cpv1beta2.ControlPlaneAvailableCondition),
			Status: metav1.ConditionTrue,
			Reason: cpv1beta2.ControlPlaneAvailableReason,
		})
	}

	builder := fake.NewClientBuilder().WithScheme(scheme)
	if serverURL != "" {
		kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster: {server: %s}
  name: workload
contexts:
- context: {cluster: workload, user: admin}
  name: workload
current-context: workload
users:
- name: admin
  user: {}
`, serverURL)
		builder = builder.WithObjects(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "test-kubeconfig", Namespace: "default"},
			Data:       map[string][]byte{secret.KubeconfigDataName: []byte(kubeconfig)},
		})
	}

	return &K0sController{Client: builder.Build()}, &controlplane{cluster: cluster, kcp: kcp}
}

func availableCondition(kcp *cpv1beta2.K0sControlPlane) *metav1.Condition {
	return conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
}

// TestComputeAvailabilityPing covers a control plane that answers the API root
// but cannot serve a read, which the cluster cache probe cannot see.
func TestComputeAvailabilityPing(t *testing.T) {
	t.Run("a reachable control plane is available", func(t *testing.T) {
		srv := newWorkloadAPI(t, http.StatusOK)
		c, scope := availabilityFixture(t, srv.URL, false)

		c.computeAvailability(context.Background(), scope, logr.Discard())

		got := availableCondition(scope.kcp)
		require.NotNil(t, got)
		require.Equal(t, metav1.ConditionTrue, got.Status)
		require.True(t, initialized(scope.kcp.Status.Initialization.ControlPlaneInitialized))
	})

	t.Run("a control plane that was up and stops answering is not available", func(t *testing.T) {
		srv := newWorkloadAPI(t, http.StatusInternalServerError)
		c, scope := availabilityFixture(t, srv.URL, true)

		// One lost read is not enough on purpose, so fail once to open the grace
		// period, age it out, then fail again.
		c.computeAvailability(context.Background(), scope, logr.Discard())
		ageAvailableCondition(t, &c.availabilityFailures, scope.kcp, availabilityGracePeriod+time.Second)
		c.computeAvailability(context.Background(), scope, logr.Discard())

		got := availableCondition(scope.kcp)
		require.NotNil(t, got)
		require.Equal(t, metav1.ConditionFalse, got.Status,
			"a control plane that cannot serve a read must not keep reporting available")
		require.Equal(t, cpv1beta2.ControlPlaneNotAvailableReason, got.Reason)
		require.NotEmpty(t, got.Message)
	})

	t.Run("a control plane that was never up is assumed unavailable", func(t *testing.T) {
		srv := newWorkloadAPI(t, http.StatusInternalServerError)
		c, scope := availabilityFixture(t, srv.URL, false)

		c.computeAvailability(context.Background(), scope, logr.Discard())

		got := availableCondition(scope.kcp)
		require.NotNil(t, got, "one that has never been reached is not available")
		require.Equal(t, metav1.ConditionUnknown, got.Status)
		require.Equal(t, cpv1beta2.ControlPlaneAvailableUnknownReason, got.Reason)
		require.NotEmpty(t, got.Message, "and it has to say why")
		require.False(t, initialized(scope.kcp.Status.Initialization.ControlPlaneInitialized),
			"and a failed read must not latch initialization")
	})

	// The cluster cache probe asks for the API root, so a control plane whose
	// storage is broken still looks healthy to it.
	t.Run("a healthy cache probe cannot excuse a failing read", func(t *testing.T) {
		srv := newWorkloadAPI(t, http.StatusInternalServerError)
		c, scope := availabilityFixture(t, "", true)
		// A cache that is probing happily, which is the whole point. Leaving
		// lastProbeSuccess zero would let a cache consultation creep back in unseen.
		c.ClusterCache = stubClusterCache{
			restConfig:       &rest.Config{Host: srv.URL},
			lastProbeSuccess: time.Now(),
		}

		c.computeAvailability(context.Background(), scope, logr.Discard())
		ageAvailableCondition(t, &c.availabilityFailures, scope.kcp, availabilityGracePeriod+time.Second)
		c.computeAvailability(context.Background(), scope, logr.Discard())

		got := availableCondition(scope.kcp)
		require.NotNil(t, got)
		require.Equal(t, metav1.ConditionFalse, got.Status,
			"the cluster cache seeing no failure must not keep a broken control plane available")
		require.Equal(t, cpv1beta2.ControlPlaneNotAvailableReason, got.Reason)
	})
}

// TestComputeAvailabilityClientBuild covers the cluster cache being unable to
// hand out a client, which happens both on startup and once a cluster is gone.
func TestComputeAvailabilityClientBuild(t *testing.T) {
	for _, tc := range []struct {
		name       string
		everUp     bool
		cache      clustercache.ClusterCache
		wantStatus *metav1.ConditionStatus
		wantReason string
	}{
		{
			name:       "not connected yet and never up is assumed unavailable",
			cache:      stubClusterCache{err: clustercache.ErrClusterNotConnected},
			wantStatus: ptr.To(metav1.ConditionUnknown),
			wantReason: cpv1beta2.ControlPlaneAvailableUnknownReason,
		},
		{
			name:       "repeated probe failures while never up is still assumed unavailable",
			cache:      stubClusterCache{err: clustercache.ErrClusterNotConnected, failures: 5},
			wantStatus: ptr.To(metav1.ConditionUnknown),
			wantReason: cpv1beta2.ControlPlaneAvailableUnknownReason,
		},
		{
			name:       "any other client error while never up is assumed unavailable",
			cache:      stubClusterCache{err: errors.New("kubeconfig is malformed")},
			wantStatus: ptr.To(metav1.ConditionUnknown),
			wantReason: cpv1beta2.ControlPlaneAvailableUnknownReason,
		},
		{
			name:       "not connected and no probe failure yet keeps the last state",
			everUp:     true,
			cache:      stubClusterCache{err: clustercache.ErrClusterNotConnected},
			wantStatus: ptr.To(metav1.ConditionTrue),
			wantReason: cpv1beta2.ControlPlaneAvailableReason,
		},
		{
			name:       "not connected after repeated probe failures is not available",
			everUp:     true,
			cache:      stubClusterCache{err: clustercache.ErrClusterNotConnected, failures: 5},
			wantStatus: ptr.To(metav1.ConditionFalse),
			wantReason: cpv1beta2.ControlPlaneNotAvailableReason,
		},
		{
			name:       "any other client error is reported without waiting for the cache",
			everUp:     true,
			cache:      stubClusterCache{err: errors.New("kubeconfig is malformed")},
			wantStatus: ptr.To(metav1.ConditionFalse),
			wantReason: cpv1beta2.ControlPlaneNotAvailableReason,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, scope := availabilityFixture(t, "", tc.everUp)
			c.ClusterCache = tc.cache

			// Twice, so the first opens the grace period and the second closes it.
			// A row that reports nothing has no condition to age.
			c.computeAvailability(context.Background(), scope, logr.Discard())
			if _, counted := c.availabilityFailures.Load(availabilityKey(scope.kcp)); counted {
				ageAvailableCondition(t, &c.availabilityFailures, scope.kcp, availabilityGracePeriod+time.Second)
			}
			c.computeAvailability(context.Background(), scope, logr.Discard())

			got := availableCondition(scope.kcp)
			require.NotNil(t, got)
			require.Equal(t, *tc.wantStatus, got.Status)
			require.Equal(t, tc.wantReason, got.Reason)
		})
	}
}

// stubClusterCache answers only what computeAvailability reaches, so any other
// call panics rather than passing silently.
type stubClusterCache struct {
	clustercache.ClusterCache

	failures         int
	lastProbeSuccess time.Time
	restConfig       *rest.Config
	workloadClient   client.Client
	err              error
}

func (s stubClusterCache) GetRESTConfig(_ context.Context, _ client.ObjectKey) (*rest.Config, error) {
	return s.restConfig, s.err
}

// GetClient is the cached one, which nothing here should be probing through.
func (s stubClusterCache) GetClient(_ context.Context, _ client.ObjectKey) (client.Client, error) {
	return nil, errors.New("a probe has to read through GetUncachedClient")
}

// GetUncachedClient is what the hosted flavour reaches for, unlike the K0s one.
func (s stubClusterCache) GetUncachedClient(_ context.Context, _ client.ObjectKey) (client.Client, error) {
	if s.workloadClient == nil && s.err == nil {
		// A pair the real cache never hands out, and the caller has no nil guard.
		return nil, errors.New("stubClusterCache needs a workloadClient or an err")
	}

	return s.workloadClient, s.err
}

func (s stubClusterCache) GetHealthCheckingState(_ context.Context, _ client.ObjectKey) clustercache.HealthCheckingState {
	return clustercache.HealthCheckingState{
		ConsecutiveFailures:  s.failures,
		LastProbeSuccessTime: s.lastProbeSuccess,
	}
}

// TestComputeAvailabilityRequeuesWhenUndecided covers the one path that leaves
// the condition alone, which otherwise parks a stale state with no retry.
func TestComputeAvailabilityRequeuesWhenUndecided(t *testing.T) {
	t.Run("a cache that has not connected yet has to be looked at again", func(t *testing.T) {
		c, scope := availabilityFixture(t, "", true)
		c.ClusterCache = stubClusterCache{err: clustercache.ErrClusterNotConnected}

		c.computeAvailability(context.Background(), scope, logr.Discard())

		require.True(t, conditions.IsTrue(scope.kcp, string(cpv1beta2.ControlPlaneAvailableCondition)),
			"the last known state is kept on this path")
		require.True(t, scope.availabilityUndecided)
		require.Equal(t, availabilityRetryInterval, availabilityRequeueAfter(scope),
			"a cluster that never connects sends no cache event, so this is the only retry")
	})

	t.Run("a settled control plane is still polled", func(t *testing.T) {
		srv := newWorkloadAPI(t, http.StatusOK)
		c, scope := availabilityFixture(t, srv.URL, true)

		c.computeAvailability(context.Background(), scope, logr.Discard())

		require.False(t, scope.availabilityUndecided)
		require.Equal(t, availabilityPollInterval, availabilityRequeueAfter(scope),
			"nothing else notices a control plane reached through a tunnel going away")
	})
}

// TestAvailabilityReasonValues pins the strings users and CAPI actually see,
// which comparing against the constants alone would not catch.
func TestAvailabilityReasonValues(t *testing.T) {
	require.Equal(t, "NotAvailable", cpv1beta2.ControlPlaneNotAvailableReason)
	require.Equal(t, clusterv1.NotAvailableReason, cpv1beta2.ControlPlaneNotAvailableReason,
		"the contract reason is what CAPI mirrors onto the Cluster")
	require.Equal(t, "ConnectionDown", cpv1beta2.ControlPlaneConnectionDownReason)
	require.Equal(t, "AvailableUnknown", cpv1beta2.ControlPlaneAvailableUnknownReason)
	require.Equal(t, clusterv1.AvailableUnknownReason, cpv1beta2.ControlPlaneAvailableUnknownReason,
		"CAPI uses this string for availability it cannot speak to")
	require.Equal(t, clusterv1.ConnectionDownReason, cpv1beta2.ControlPlaneConnectionDownReason,
		"CAPI uses this same string while a connection is down")
}

// tunnelKCP turns a control plane into one the management cluster reaches through
// a tunnel, which is the combination that bypasses the cluster cache entirely.
func tunnelKCP(kcp *cpv1beta2.K0sControlPlane, mode string) {
	kcp.Spec.K0sConfigSpec.Tunneling.Enabled = true
	kcp.Spec.K0sConfigSpec.Tunneling.Mode = mode
	kcp.Spec.K0sConfigSpec.Args = append(kcp.Spec.K0sConfigSpec.Args, "--enable-worker")
}

// writeKubeconfig adds a kubeconfig secret under an arbitrary name, so a test can
// provide the tunneled one instead of the regular one.
func writeKubeconfig(t *testing.T, c *K0sController, name, serverURL string) {
	t.Helper()

	require.NoError(t, c.Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Data: map[string][]byte{secret.KubeconfigDataName: []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster: {server: %s}
  name: workload
contexts:
- context: {cluster: workload, user: admin}
  name: workload
current-context: workload
users:
- name: admin
  user: {}
`, serverURL))},
	}))
}

// TestComputeAvailabilityTunneled covers a tunneled control plane alongside the
// regular one, since the cache probes an endpoint neither reads through.
func TestComputeAvailabilityTunneled(t *testing.T) {
	for _, mode := range []string{"tunnel", "proxy"} {
		t.Run(mode+" mode measures the tunnel, not the cache", func(t *testing.T) {
			srv := newWorkloadAPI(t, http.StatusOK)
			c, scope := availabilityFixture(t, "", false)
			tunnelKCP(scope.kcp, mode)

			suffix := "tunneled"
			if mode == "proxy" {
				suffix = "proxied"
			}
			writeKubeconfig(t, c, "test-"+suffix+"-kubeconfig", srv.URL)

			// A cache that reports nothing but failures must not colour the verdict,
			// and must not be reached for a rest config either.
			c.ClusterCache = stubClusterCache{failures: 5, err: errors.New("the cache must not be asked")}

			c.computeAvailability(context.Background(), scope, logr.Discard())

			require.True(t, conditions.IsTrue(scope.kcp, string(cpv1beta2.ControlPlaneAvailableCondition)),
				"the tunnel answered, so the control plane is available")
		})
	}

	t.Run("a tunneled kubeconfig that is not written yet opens the grace period", func(t *testing.T) {
		c, scope := availabilityFixture(t, "", true)
		tunnelKCP(scope.kcp, "tunnel")
		c.ClusterCache = stubClusterCache{failures: 5}

		c.computeAvailability(context.Background(), scope, logr.Discard())

		got := availableCondition(scope.kcp)
		require.NotNil(t, got)
		require.Equal(t, metav1.ConditionUnknown, got.Status,
			"the cluster cannot be read, and the cache cannot speak for a tunnel")
		// reconcileKubeconfig writes this secret, so the grace period is what keeps
		// a self healing gap from being reported as an outage.
		require.Equal(t, availabilityRetryInterval, availabilityRequeueAfter(scope))
	})

	t.Run("a regular control plane is unaffected", func(t *testing.T) {
		srv := newWorkloadAPI(t, http.StatusOK)
		c, scope := availabilityFixture(t, srv.URL, false)
		c.ClusterCache = stubClusterCache{restConfig: &rest.Config{Host: srv.URL}}

		c.computeAvailability(context.Background(), scope, logr.Discard())

		require.True(t, conditions.IsTrue(scope.kcp, string(cpv1beta2.ControlPlaneAvailableCondition)))
	})
}

// TestAvailabilityRequeueAfter covers the interval on its own, since it is what brings the
// reconcile back to notice a control plane that went away. Replicas are not its concern.
func TestAvailabilityRequeueAfter(t *testing.T) {
	newKCP := func(available bool) *cpv1beta2.K0sControlPlane {
		kcp := &cpv1beta2.K0sControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
		if available {
			conditions.Set(kcp, metav1.Condition{
				Type:   string(cpv1beta2.ControlPlaneAvailableCondition),
				Status: metav1.ConditionTrue,
				Reason: cpv1beta2.ControlPlaneAvailableReason,
			})
		}

		return kcp
	}

	t.Run("a healthy control plane is still polled", func(t *testing.T) {
		require.Equal(t, availabilityPollInterval, availabilityRequeueAfter(&controlplane{kcp: newKCP(true)}),
			"waiting for an event here is what left a tunneled control plane reporting stale state")
	})

	t.Run("an undecided one comes back sooner", func(t *testing.T) {
		require.Equal(t, availabilityRetryInterval,
			availabilityRequeueAfter(&controlplane{kcp: newKCP(true), availabilityUndecided: true}))
	})

	t.Run("an unavailable one comes back sooner", func(t *testing.T) {
		require.Equal(t, availabilityRetryInterval, availabilityRequeueAfter(&controlplane{kcp: newKCP(false)}))
	})

	t.Run("replicas catching up are left to the scale logic", func(t *testing.T) {
		kcp := newKCP(true)
		kcp.Spec.Replicas = 3

		require.Equal(t, availabilityPollInterval, availabilityRequeueAfter(&controlplane{kcp: kcp}),
			"reconcileMachines asks for its own shorter interval, which requeueResult prefers")
	})

	t.Run("a deleting control plane is left to its own requeue", func(t *testing.T) {
		kcp := newKCP(true)
		kcp.DeletionTimestamp = &metav1.Time{Time: time.Unix(1, 0)}

		require.Zero(t, availabilityRequeueAfter(&controlplane{kcp: kcp}),
			"its status fields were deliberately not refreshed")
	})
}

// ageAvailableCondition backdates the condition and the in memory anchor, since the
// anchor is what the decision reads and ageing only the condition proves nothing.
func ageAvailableCondition(t *testing.T, failures *sync.Map, kcp availabilityReporter, age time.Duration) {
	t.Helper()

	got := conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
	require.NotNil(t, got, "there is no condition to age")

	backdated := time.Now().Add(-age)
	got.LastTransitionTime = metav1.NewTime(backdated)
	conditions.Set(kcp, *got)

	key := availabilityKey(kcp)
	previous, ok := failures.Load(key)
	require.True(t, ok, "no failure has been counted yet, so there is no window to age")
	failures.Store(key, availabilityFailures{since: backdated, seen: previous.(availabilityFailures).seen})
}

// anchorOf mirrors how setUnavailableAfterGracePeriod picks the anchor on a first
// failure, so the table exercises the same rule the controller applies.
func anchorOf(existing *metav1.Condition, now, created time.Time) time.Time {
	if existing != nil && existing.Status == metav1.ConditionUnknown &&
		existing.LastTransitionTime.Time.After(created) {
		return existing.LastTransitionTime.Time
	}

	return now
}

// Test_availabilityCondition covers the whole decision without a clock, since the
// boundary is what decides whether a blip or an outage is reported.
func Test_availabilityCondition(t *testing.T) {
	const grace = time.Minute

	now := time.Unix(1_700_000_000, 0)
	created := now.Add(-24 * time.Hour)
	unknownAt := func(d time.Duration, reason string) *metav1.Condition {
		return &metav1.Condition{
			Type:               string(cpv1beta2.ControlPlaneAvailableCondition),
			Status:             metav1.ConditionUnknown,
			Reason:             reason,
			LastTransitionTime: metav1.NewTime(now.Add(-d)),
		}
	}

	for _, tc := range []struct {
		name       string
		existing   *metav1.Condition
		failures   int
		wantStatus metav1.ConditionStatus
		wantReason string
	}{
		{
			name:       "nothing reported yet opens the grace period",
			wantStatus: metav1.ConditionUnknown,
			wantReason: cpv1beta2.ControlPlaneConnectionDownReason,
		},
		{
			name: "an available control plane opens the grace period",
			existing: &metav1.Condition{
				Type:   string(cpv1beta2.ControlPlaneAvailableCondition),
				Status: metav1.ConditionTrue,
				Reason: cpv1beta2.ControlPlaneAvailableReason,
			},
			wantStatus: metav1.ConditionUnknown,
			wantReason: cpv1beta2.ControlPlaneConnectionDownReason,
		},
		{
			name:       "just inside the grace period holds",
			existing:   unknownAt(grace-time.Nanosecond, cpv1beta2.ControlPlaneConnectionDownReason),
			wantStatus: metav1.ConditionUnknown,
			wantReason: cpv1beta2.ControlPlaneConnectionDownReason,
		},
		{
			name:       "exactly at the grace period reports",
			existing:   unknownAt(grace, cpv1beta2.ControlPlaneConnectionDownReason),
			wantStatus: metav1.ConditionFalse,
			wantReason: cpv1beta2.ControlPlaneNotAvailableReason,
		},
		{
			name:       "one failure never reports, however old the anchor",
			existing:   unknownAt(100*grace, cpv1beta2.ControlPlaneConnectionDownReason),
			failures:   1,
			wantStatus: metav1.ConditionUnknown,
			wantReason: cpv1beta2.ControlPlaneConnectionDownReason,
		},
		{
			name:       "an outage watched across long gaps still reports",
			existing:   unknownAt(100*grace, cpv1beta2.ControlPlaneConnectionDownReason),
			failures:   availabilityFailureThreshold,
			wantStatus: metav1.ConditionFalse,
			wantReason: cpv1beta2.ControlPlaneNotAvailableReason,
		},
		{
			name:       "past the grace period reports",
			existing:   unknownAt(2*grace, cpv1beta2.ControlPlaneConnectionDownReason),
			wantStatus: metav1.ConditionFalse,
			wantReason: cpv1beta2.ControlPlaneNotAvailableReason,
		},
		{
			name:       "an unknown from somewhere else still anchors it",
			existing:   unknownAt(2*grace, "SomethingElse"),
			wantStatus: metav1.ConditionFalse,
			wantReason: cpv1beta2.ControlPlaneNotAvailableReason,
		},
		{
			name: "an already reported outage does not go back to unknown",
			existing: &metav1.Condition{
				Type:               string(cpv1beta2.ControlPlaneAvailableCondition),
				Status:             metav1.ConditionFalse,
				Reason:             cpv1beta2.ControlPlaneNotAvailableReason,
				Message:            "the workload cluster API could not be read: earlier cause",
				LastTransitionTime: metav1.NewTime(now),
			},
			wantStatus: metav1.ConditionFalse,
			wantReason: cpv1beta2.ControlPlaneNotAvailableReason,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failures := tc.failures
			if failures == 0 {
				failures = availabilityFailureThreshold
			}

			got := availabilityCondition(tc.existing, now, anchorOf(tc.existing, now, created), grace, failures, "etcdserver request timed out")

			require.Equal(t, tc.wantStatus, got.Status)
			require.Equal(t, tc.wantReason, got.Reason)
			require.NotEmpty(t, got.Message)
		})
	}

	t.Run("no grace period at all reports straight away", func(t *testing.T) {
		got := availabilityCondition(nil, now, now, 0, availabilityFailureThreshold, "boom")

		require.Equal(t, metav1.ConditionFalse, got.Status)
	})

	t.Run("the cause reaches the report but is not rewritten after", func(t *testing.T) {
		aged := unknownAt(2*grace, cpv1beta2.ControlPlaneConnectionDownReason)
		first := availabilityCondition(aged, now, anchorOf(aged, now, created), grace,
			availabilityFailureThreshold, "etcdserver request timed out")
		require.Equal(t, metav1.ConditionFalse, first.Status)
		require.Contains(t, first.Message, "etcdserver request timed out")

		second := availabilityCondition(&first, now, anchorOf(&first, now, created), grace,
			availabilityFailureThreshold, "dial tcp 10.0.0.9:6443 connect refused")

		require.Equal(t, first.Message, second.Message,
			"a rotating endpoint in the cause would patch status on every retry")
		require.Equal(t, first.Reason, second.Reason,
			"and so would a reason that alternates with the two failure paths")
	})
}

// TestComputeAvailabilityDoesNotFlipOnOneFailure is the reviewer's concern, pinned
// with a literal single call so it cannot go vacuous if the timings change.
func TestComputeAvailabilityDoesNotFlipOnOneFailure(t *testing.T) {
	srv := newWorkloadAPI(t, http.StatusInternalServerError)
	c, scope := availabilityFixture(t, srv.URL, true)

	c.computeAvailability(context.Background(), scope, logr.Discard())

	got := availableCondition(scope.kcp)
	require.NotNil(t, got)
	require.Equal(t, metav1.ConditionUnknown, got.Status,
		"one lost read is not an outage")
	require.Equal(t, cpv1beta2.ControlPlaneConnectionDownReason, got.Reason)
	require.Equal(t, availabilityRetryInterval, availabilityRequeueAfter(scope),
		"and it has to come back, or the grace period is never reached")
}

// TestComputeAvailabilityKeepsTheAnchor covers the timestamp the grace period is
// measured from. If it moved on every retry the outage would never be reported.
func TestComputeAvailabilityKeepsTheAnchor(t *testing.T) {
	srv := newWorkloadAPI(t, http.StatusInternalServerError)
	c, scope := availabilityFixture(t, srv.URL, true)

	c.computeAvailability(context.Background(), scope, logr.Discard())
	anchor := availableCondition(scope.kcp).LastTransitionTime

	for range 3 {
		c.computeAvailability(context.Background(), scope, logr.Discard())
	}

	require.Equal(t, anchor, availableCondition(scope.kcp).LastTransitionTime,
		"a retry must not restart the clock")
	require.Equal(t, metav1.ConditionUnknown, availableCondition(scope.kcp).Status)
}

// TestComputeAvailabilityReportsAfterTheGracePeriod covers the flip being decided
// from persisted state, which is what makes it survive a restart or a new leader.
func TestComputeAvailabilityReportsAfterTheGracePeriod(t *testing.T) {
	srv := newWorkloadAPI(t, http.StatusInternalServerError)
	c, scope := availabilityFixture(t, srv.URL, true)

	c.computeAvailability(context.Background(), scope, logr.Discard())
	ageAvailableCondition(t, &c.availabilityFailures, scope.kcp, availabilityGracePeriod+time.Second)

	c.computeAvailability(context.Background(), scope, logr.Discard())

	got := availableCondition(scope.kcp)
	require.Equal(t, metav1.ConditionFalse, got.Status)
	require.Equal(t, cpv1beta2.ControlPlaneNotAvailableReason, got.Reason)
}

// TestComputeAvailabilityRecoversOnOneGoodRead covers the other direction, since
// recovery is not something to be cautious about.
func TestComputeAvailabilityRecoversOnOneGoodRead(t *testing.T) {
	srv, healthy := newSwitchableWorkloadAPI(t)
	c, scope := availabilityFixture(t, srv.URL, true)

	c.computeAvailability(context.Background(), scope, logr.Discard())
	ageAvailableCondition(t, &c.availabilityFailures, scope.kcp, availabilityGracePeriod+time.Second)
	c.computeAvailability(context.Background(), scope, logr.Discard())
	require.Equal(t, metav1.ConditionFalse, availableCondition(scope.kcp).Status)

	healthy.Store(true)
	c.computeAvailability(context.Background(), scope, logr.Discard())

	require.True(t, conditions.IsTrue(scope.kcp, string(cpv1beta2.ControlPlaneAvailableCondition)),
		"one good read is enough to be available again")
	require.Equal(t, availabilityPollInterval, availabilityRequeueAfter(scope))
}

// TestClusterCacheConnectionDown covers the second opinion, which is only worth
// having when it describes the endpoint that actually failed.
func TestClusterCacheConnectionDown(t *testing.T) {
	scope := &controlplane{
		cluster: &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"}},
		kcp:     &cpv1beta2.K0sControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"}},
	}

	for _, tc := range []struct {
		name  string
		cache clustercache.ClusterCache
		want  bool
	}{
		{
			name: "no cache leaves the failure as the only evidence",
			want: true,
		},
		{
			name:  "a cluster the cache never reached is unknown, not healthy",
			cache: stubClusterCache{},
		},
		{
			name:  "a few failures before any success is normal on startup",
			cache: stubClusterCache{failures: clusterCacheFailureThreshold - 1},
		},
		{
			name:  "never reached and past the threshold is down",
			cache: stubClusterCache{failures: clusterCacheFailureThreshold},
			want:  true,
		},
		{
			name:  "a recent success outweighs a few failures",
			cache: stubClusterCache{failures: 3, lastProbeSuccess: time.Now().Add(-10 * time.Second)},
		},
		{
			name:  "no success for a long time is down",
			cache: stubClusterCache{lastProbeSuccess: time.Now().Add(-5 * time.Minute)},
			want:  true,
		},
		{
			// The cache disconnects after this many probe failures and keeps the last
			// success, so waiting the grace period out from it would only add delay.
			name: "the cache having given up outweighs a success inside the grace period",
			cache: stubClusterCache{
				failures:         clusterCacheFailureThreshold,
				lastProbeSuccess: time.Now().Add(-50 * time.Second),
			},
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &K0sController{}
			if tc.cache != nil {
				c.ClusterCache = tc.cache
			}

			require.Equal(t, tc.want, c.clusterCacheConnectionDown(context.Background(), scope))
		})
	}
}

// TestComputeAvailabilityTunneledDoesNotAskTheCache covers the tunneled path, where
// the cache is connected to a different endpoint and so cannot excuse the failure.
func TestComputeAvailabilityTunneledDoesNotAskTheCache(t *testing.T) {
	c, scope := availabilityFixture(t, "", true)
	tunnelKCP(scope.kcp, "tunnel")

	// A cache that looks perfectly healthy, about an endpoint nobody read through.
	c.ClusterCache = stubClusterCache{lastProbeSuccess: time.Now()}

	c.computeAvailability(context.Background(), scope, logr.Discard())

	got := availableCondition(scope.kcp)
	require.NotNil(t, got)
	require.Equal(t, metav1.ConditionUnknown, got.Status,
		"a healthy cache must not park a tunneled control plane as decided")
	require.Equal(t, cpv1beta2.ControlPlaneConnectionDownReason, got.Reason,
		"one reason for both paths, so it cannot alternate and patch status on each retry")
	require.False(t, scope.availabilityUndecided,
		"the cache has no say here, so this is a real failure and not an undecided one")
}

// TestRequeueResult covers the result never going back alongside an error, since
// controller-runtime drops it in that case and the requeue would be lost.
func TestRequeueResult(t *testing.T) {
	kcp := &cpv1beta2.K0sControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
	conditions.Set(kcp, metav1.Condition{
		Type:   string(cpv1beta2.ControlPlaneAvailableCondition),
		Status: metav1.ConditionTrue,
		Reason: cpv1beta2.ControlPlaneAvailableReason,
	})
	scope := &controlplane{kcp: kcp}

	t.Run("a settled control plane is polled", func(t *testing.T) {
		require.Equal(t, ctrl.Result{RequeueAfter: availabilityPollInterval},
			requeueResult(nil, ctrl.Result{}, scope))
	})

	t.Run("an error takes the result with it", func(t *testing.T) {
		require.Zero(t, requeueResult(errors.New("boom"), ctrl.Result{}, scope),
			"controller-runtime ignores a result handed back with an error")
	})

	t.Run("an error drops a result someone else set", func(t *testing.T) {
		require.Zero(t, requeueResult(errors.New("boom"), ctrl.Result{RequeueAfter: time.Hour}, scope),
			"handing back both is only logged and dropped, so return neither")
	})

	t.Run("a result someone else already set is left alone", func(t *testing.T) {
		already := ctrl.Result{RequeueAfter: time.Hour}

		require.Equal(t, already, requeueResult(nil, already, scope))
	})
}

// TestAvailabilityTimings pins the numbers with literals. Asserting them through the
// symbols would pass whatever they were changed to.
func TestAvailabilityTimings(t *testing.T) {
	require.Equal(t, 2*time.Minute, availabilityGracePeriod)
	require.Equal(t, time.Minute, availabilityPollInterval)
	require.Equal(t, 20*time.Second, availabilityRetryInterval)
	require.Equal(t, 2, availabilityFailureThreshold)

	// The cache probes five times at ten seconds with a five second timeout each.
	require.Equal(t, 5, clusterCacheFailureThreshold)
	require.Equal(t, 75*time.Second, clusterCacheConnectionGracePeriod)

	require.Greater(t, availabilityGracePeriod, clusterCacheConnectionGracePeriod,
		"reporting an outage before the cache gives up would contradict CAPI on the Cluster")
	require.Greater(t, availabilityGracePeriod, time.Duration(availabilityFailureThreshold)*availabilityRetryInterval,
		"the threshold has to be reachable well inside the grace period")
}

// TestAvailabilityFailuresAreConsecutive covers the count meaning failures in a row.
// A leaked count would let an old outage condemn a later one.
func TestAvailabilityFailuresAreConsecutive(t *testing.T) {
	srv, healthy := newSwitchableWorkloadAPI(t)
	c, scope := availabilityFixture(t, srv.URL, true)
	c.ClusterCache = stubClusterCache{restConfig: &rest.Config{Host: srv.URL}}

	key := availabilityKey(scope.kcp)

	c.computeAvailability(context.Background(), scope, logr.Discard())
	got, ok := c.availabilityFailures.Load(key)
	require.True(t, ok)
	require.Equal(t, 1, got.(availabilityFailures).seen, "one failed read is one failure")

	c.computeAvailability(context.Background(), scope, logr.Discard())
	got, _ = c.availabilityFailures.Load(key)
	require.Equal(t, 2, got.(availabilityFailures).seen, "and they add up while they keep failing")

	healthy.Store(true)
	c.computeAvailability(context.Background(), scope, logr.Discard())

	_, ok = c.availabilityFailures.Load(key)
	require.False(t, ok, "a good read has to forget them, or they are not in a row")
}

// TestHostedComputeAvailability covers the hosted flavour reporting the same way as
// the K0s one, since CAPI mirrors both onto the same Cluster condition.
func TestHostedComputeAvailability(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	newScope := func(everUp bool) (*K0smotronController, *clusterv1.Cluster, *cpv1beta2.K0smotronControlPlane) {
		cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
		kcp := &cpv1beta2.K0smotronControlPlane{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default", UID: "hosted-uid"},
		}
		if everUp {
			// The bring up guard reads this latch, as the other flavour does.
			kcp.Status.Initialization.ControlPlaneInitialized = new(true)
			conditions.Set(kcp, metav1.Condition{
				Type:   string(cpv1beta2.ControlPlaneAvailableCondition),
				Status: metav1.ConditionTrue,
				Reason: cpv1beta2.ControlPlaneAvailableReason,
			})
		}

		return &K0smotronController{}, cluster, kcp
	}

	t.Run("one failed read does not report an outage", func(t *testing.T) {
		c, cluster, kcp := newScope(true)
		c.ClusterCache = stubClusterCache{err: errors.New("connection refused")}

		c.computeAvailability(context.Background(), cluster, kcp)

		got := conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
		require.NotNil(t, got)
		require.Equal(t, metav1.ConditionUnknown, got.Status,
			"the hosted flavour used to report False on the very first failure")
		require.Equal(t, cpv1beta2.ControlPlaneConnectionDownReason, got.Reason)
	})

	t.Run("a read that keeps failing reports an outage", func(t *testing.T) {
		c, cluster, kcp := newScope(true)
		c.ClusterCache = stubClusterCache{err: errors.New("connection refused")}

		c.computeAvailability(context.Background(), cluster, kcp)
		ageAvailableCondition(t, &c.availabilityFailures, kcp, availabilityGracePeriod+time.Second)
		c.computeAvailability(context.Background(), cluster, kcp)

		got := conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
		require.Equal(t, metav1.ConditionFalse, got.Status)
		require.Equal(t, cpv1beta2.ControlPlaneNotAvailableReason, got.Reason)
		// The reported message is the only place the cause surfaces, so handing the
		// grace period an empty one leaves an outage nobody can diagnose.
		require.Contains(t, got.Message, "connection refused",
			"the cause has to reach the message users read")
	})

	t.Run("a failing read reports the same reasons as the other flavour", func(t *testing.T) {
		c, cluster, kcp := newScope(true)
		// A client that hands out no namespace, so the ping is what fails.
		c.ClusterCache = stubClusterCache{workloadClient: fake.NewClientBuilder().WithScheme(scheme).Build()}

		c.computeAvailability(context.Background(), cluster, kcp)
		got := conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
		require.Equal(t, cpv1beta2.ControlPlaneConnectionDownReason, got.Reason,
			"both flavours have to report the same reason for the same event")

		ageAvailableCondition(t, &c.availabilityFailures, kcp, availabilityGracePeriod+time.Second)
		c.computeAvailability(context.Background(), cluster, kcp)
		got = conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
		require.Equal(t, metav1.ConditionFalse, got.Status)
		require.Equal(t, cpv1beta2.ControlPlaneNotAvailableReason, got.Reason)
	})

	t.Run("the unknown message never carries the error", func(t *testing.T) {
		c, cluster, kcp := newScope(true)
		c.ClusterCache = stubClusterCache{err: errors.New("dial tcp 10.0.0.7:6443 connect refused")}

		c.computeAvailability(context.Background(), cluster, kcp)

		got := conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
		require.NotContains(t, got.Message, "10.0.0.7",
			"a rotating endpoint in the message rewrites status on every retry")
	})

	t.Run("a good read reports available and forgets the failures", func(t *testing.T) {
		c, cluster, kcp := newScope(true)

		// Fail first, so there is actually something to forget. Succeeding from a
		// clean slate would leave the map empty either way.
		c.ClusterCache = stubClusterCache{err: errors.New("boom")}
		c.computeAvailability(context.Background(), cluster, kcp)
		_, ok := c.availabilityFailures.Load(availabilityKey(kcp))
		require.True(t, ok, "the failure has to have been counted")

		c.ClusterCache = stubClusterCache{workloadClient: fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: "8f1c"}}).Build()}
		c.computeAvailability(context.Background(), cluster, kcp)

		require.True(t, conditions.IsTrue(kcp, string(cpv1beta2.ControlPlaneAvailableCondition)))
		_, ok = c.availabilityFailures.Load(availabilityKey(kcp))
		require.False(t, ok, "a good read has to forget them, or an old outage condemns a later one")
	})

	t.Run("each flavour counts its own failures", func(t *testing.T) {
		hosted, cluster, kcp := newScope(true)
		hosted.ClusterCache = stubClusterCache{err: errors.New("boom")}
		hosted.computeAvailability(context.Background(), cluster, kcp)

		k0s := &K0sController{}
		_, ok := k0s.availabilityFailures.Load(availabilityKey(kcp))
		require.False(t, ok,
			"one map keyed by namespace and name would collide across the two kinds")
	})
}

// TestAvailabilityAnchorIgnoresTheCrdDefault covers the clamp where the anchor is
// chosen, since the hosted CRD defaults this condition to Unknown at the epoch.
func TestAvailabilityAnchorIgnoresTheCrdDefault(t *testing.T) {
	newKCP := func() *cpv1beta2.K0smotronControlPlane {
		kcp := &cpv1beta2.K0smotronControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test", Namespace: "default", UID: "hosted-uid",
				CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Minute)),
			},
		}
		// Exactly what the CRD default puts there.
		conditions.Set(kcp, metav1.Condition{
			Type:               string(cpv1beta2.ControlPlaneAvailableCondition),
			Status:             metav1.ConditionUnknown,
			Reason:             "ControlPlaneDoesNotExist",
			Message:            "Waiting for cluster topology to be reconciled",
			LastTransitionTime: metav1.NewTime(time.Unix(0, 0)),
		})

		return kcp
	}

	failures := &sync.Map{}
	kcp := newKCP()

	// Twice, so the failure threshold is met and only the anchor can hold it back.
	setUnavailableAfterGracePeriod(failures, kcp, "connection refused")
	setUnavailableAfterGracePeriod(failures, kcp, "connection refused")

	got := conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
	require.Equal(t, metav1.ConditionUnknown, got.Status,
		"trusting the epoch reports an outage on the second reconcile of a new cluster")
}

// TestAvailabilityAnchorResumesAfterARestart covers the anchor being carried over from
// what was persisted, which is what stops a restart from restarting the grace period.
func TestAvailabilityAnchorResumesAfterARestart(t *testing.T) {
	kcp := &cpv1beta2.K0smotronControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test", Namespace: "default", UID: "hosted-uid",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
		},
	}

	// What the process that went away left behind, already old enough to report.
	anchor := metav1.NewTime(time.Now().Add(-(availabilityGracePeriod + time.Second)))
	conditions.Set(kcp, metav1.Condition{
		Type:               string(cpv1beta2.ControlPlaneAvailableCondition),
		Status:             metav1.ConditionUnknown,
		Reason:             cpv1beta2.ControlPlaneConnectionDownReason,
		Message:            "the workload cluster API has not been readable",
		LastTransitionTime: anchor,
	})

	// Empty, as a restart or a new leader leaves it, so the persisted anchor is the
	// only evidence that the outage started before this process was watching.
	failures := &sync.Map{}

	setUnavailableAfterGracePeriod(failures, kcp, "connection refused")

	got := conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
	require.Equal(t, metav1.ConditionUnknown, got.Status,
		"one failure watched here is not enough, whatever the anchor says")
	require.Contains(t, got.Message, anchor.Time.UTC().Format(time.RFC3339),
		"restarting the window here means a long outage is never reported")

	setUnavailableAfterGracePeriod(failures, kcp, "connection refused")

	got = conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
	require.Equal(t, metav1.ConditionFalse, got.Status,
		"the second failure meets the threshold and the carried anchor is old enough")
	require.Equal(t, cpv1beta2.ControlPlaneNotAvailableReason, got.Reason)
}

// TestAvailabilityAnchorIgnoresAnAvailableCondition covers only an Unknown anchor being
// carried over, since a long available one dates the outage to before it started.
func TestAvailabilityAnchorIgnoresAnAvailableCondition(t *testing.T) {
	kcp := &cpv1beta2.K0smotronControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test", Namespace: "default", UID: "hosted-uid",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
		},
	}

	// Available for an hour, which is how long conditions.Set has left this stamp
	// alone. It says when the control plane came up, not when reads started failing.
	conditions.Set(kcp, metav1.Condition{
		Type:               string(cpv1beta2.ControlPlaneAvailableCondition),
		Status:             metav1.ConditionTrue,
		Reason:             cpv1beta2.ControlPlaneAvailableReason,
		LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
	})

	failures := &sync.Map{}

	// Twice, so only the anchor can hold the report back.
	setUnavailableAfterGracePeriod(failures, kcp, "connection refused")
	setUnavailableAfterGracePeriod(failures, kcp, "connection refused")

	got := conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
	require.Equal(t, metav1.ConditionUnknown, got.Status,
		"inheriting an available stamp reports an outage on the second failed read")
}

// TestAvailabilityAnchorIgnoresABringUpCondition covers only an outage window being
// carried over, since bring up leaves an Unknown of its own behind to be mistaken for one.
func TestAvailabilityAnchorIgnoresABringUpCondition(t *testing.T) {
	kcp := &cpv1beta2.K0sControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test", Namespace: "default", UID: "test-uid",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-3 * time.Hour)),
		},
	}

	// Exactly what computeAvailability writes while the control plane has never been
	// reached, stamped long enough ago that inheriting it would report at once.
	conditions.Set(kcp, metav1.Condition{
		Type:               string(cpv1beta2.ControlPlaneAvailableCondition),
		Status:             metav1.ConditionUnknown,
		Reason:             cpv1beta2.ControlPlaneAvailableUnknownReason,
		Message:            "the workload cluster API has not been reached yet",
		LastTransitionTime: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
	})

	failures := &sync.Map{}

	// Twice, so the threshold is met and only the anchor can hold the report back.
	setUnavailableAfterGracePeriod(failures, kcp, "connection refused")
	setUnavailableAfterGracePeriod(failures, kcp, "connection refused")

	got := conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
	require.Equal(t, metav1.ConditionUnknown, got.Status,
		"a bring up condition is not an outage window, so the grace period still applies")
}

// TestAvailabilityCountsPerObject covers the key carrying the UID, so a control plane
// recreated under the same name does not inherit a dead one's count.
func TestAvailabilityCountsPerObject(t *testing.T) {
	failures := &sync.Map{}

	newKCP := func(uid string) *cpv1beta2.K0sControlPlane {
		return &cpv1beta2.K0sControlPlane{ObjectMeta: metav1.ObjectMeta{
			Name: "test", Namespace: "default", UID: types.UID(uid),
			CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
		}}
	}

	dead := newKCP("first")
	setUnavailableAfterGracePeriod(failures, dead, "boom")
	setUnavailableAfterGracePeriod(failures, dead, "boom")

	// Aged, or the inherited anchor is young enough that the grace period holds the
	// report back on its own and the key scheme is never what decides.
	ageAvailableCondition(t, failures, dead, availabilityGracePeriod+time.Second)

	// Same namespace and name, new object.
	fresh := newKCP("second")
	setUnavailableAfterGracePeriod(failures, fresh, "boom")

	got := conditions.Get(fresh, string(cpv1beta2.ControlPlaneAvailableCondition))
	require.Equal(t, metav1.ConditionUnknown, got.Status,
		"inheriting the count means one failed read reports an outage on a new object")
}

// TestHostedAvailabilityDuringBringUp covers the guard the K0s flavour has, since the
// hosted condition starts out Unknown and would report an outage on a new cluster.
func TestHostedAvailabilityDuringBringUp(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	// Never up, and no condition at all so anything written here is a report.
	newScope := func() (*clusterv1.Cluster, *cpv1beta2.K0smotronControlPlane) {
		return &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}},
			&cpv1beta2.K0smotronControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default", UID: "hosted-uid"},
			}
	}

	// Both failure paths consult the guard, so each one is driven here on its own.
	for _, tc := range []struct {
		name  string
		cache stubClusterCache
	}{
		{
			name:  "a client that cannot be built is assumed unavailable",
			cache: stubClusterCache{err: errors.New("connection refused")},
		},
		{
			// A client that hands out no namespace, so the ping is what fails.
			name:  "a ping that fails is assumed unavailable too",
			cache: stubClusterCache{workloadClient: fake.NewClientBuilder().WithScheme(scheme).Build()},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster, kcp := newScope()
			c := &K0smotronController{ClusterCache: tc.cache}

			for range availabilityFailureThreshold + 1 {
				c.computeAvailability(context.Background(), cluster, kcp)
			}

			got := conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition))
			require.NotNil(t, got, "one that has never been reached is not available")
			require.Equal(t, metav1.ConditionUnknown, got.Status)
			require.Equal(t, cpv1beta2.ControlPlaneAvailableUnknownReason, got.Reason)
			require.False(t, initialized(kcp.Status.Initialization.ControlPlaneInitialized),
				"nothing answered, so the latch must not have been set either")
		})
	}

	t.Run("a good read is what brings it out of bring up", func(t *testing.T) {
		cluster, kcp := newScope()
		c := &K0smotronController{ClusterCache: stubClusterCache{
			workloadClient: fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: "8f1c"},
				}).Build(),
		}}

		c.computeAvailability(context.Background(), cluster, kcp)

		require.True(t, conditions.IsTrue(kcp, string(cpv1beta2.ControlPlaneAvailableCondition)))
		// Latched here and nowhere else, and every failure path is gated on it, so
		// without this the whole hosted outage report is dead code.
		require.True(t, initialized(kcp.Status.Initialization.ControlPlaneInitialized),
			"a good read has to latch initialization, or no outage is ever reported")
	})
}

// TestHostedComputeStatusAlwaysComputesAvailability covers availability being reached
// when computeStatus bails early, which is when a wedged one would look available.
func TestHostedComputeStatusAlwaysComputesAvailability(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, cpv1beta2.AddToScheme(scheme))
	require.NoError(t, kapi.AddToScheme(scheme))

	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
	kcp := &cpv1beta2.K0smotronControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test", Namespace: "default", UID: "hosted-uid",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
		},
	}
	kcp.Status.Initialization.ControlPlaneInitialized = new(true)

	// No k0smotron Cluster, so computeStatus returns at its first early return.
	c := &K0smotronController{
		Client:       fake.NewClientBuilder().WithScheme(scheme).Build(),
		ClusterCache: stubClusterCache{err: errors.New("connection refused")},
	}

	require.NoError(t, c.computeStatus(context.Background(), cluster, kcp, nil))

	require.NotNil(t, conditions.Get(kcp, string(cpv1beta2.ControlPlaneAvailableCondition)),
		"an early return must not skip availability, or a wedged control plane stays available")
}

// hostedStatusScheme is what the hosted fixtures below read and write through.
func hostedStatusScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, cpv1beta2.AddToScheme(scheme))
	require.NoError(t, kapi.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	return scheme
}

// TestHostedComputeStatusDoesNotHalfWriteTheCounters covers the counters moving as one,
// since one of them written against three stale ones reports negative replicas.
func TestHostedComputeStatusDoesNotHalfWriteTheCounters(t *testing.T) {
	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
	kcp := &cpv1beta2.K0smotronControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test", Namespace: "default", UID: "hosted-uid",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
		},
	}
	// Rejected by version.NewVersion, which is the cheapest way to fail after the
	// pods have been listed and before the rest of the counters are known.
	kcp.Spec.Version = "not-a-version"

	// What the last good reconcile persisted, all of which has to survive the failure.
	kcp.Status.Replicas = new(int32(3))
	kcp.Status.ReadyReplicas = new(int32(3))
	kcp.Status.AvailableReplicas = new(int32(3))
	kcp.Status.UpToDateReplicas = new(int32(3))

	cl := fake.NewClientBuilder().WithScheme(hostedStatusScheme(t)).
		WithObjects(&kapi.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}).
		Build()
	c := &K0smotronController{
		Client:       cl,
		ClusterCache: stubClusterCache{err: errors.New("connection refused")},
	}

	err := c.computeStatus(context.Background(), cluster, kcp, &kmcScope{client: cl})

	require.Error(t, err)
	require.NotErrorIs(t, err, kutil.ErrNotReady, "the hard error path is the one under test")

	require.Equal(t, int32(3), ptr.Deref(kcp.Status.Replicas, 0),
		"a lone replica count against stale companions converts to negative replicas")
	require.Equal(t, int32(3), ptr.Deref(kcp.Status.ReadyReplicas, 0))
	require.Equal(t, int32(3), ptr.Deref(kcp.Status.AvailableReplicas, 0))
	require.Equal(t, int32(3), ptr.Deref(kcp.Status.UpToDateReplicas, 0))
}

// controlPlanePod is one hosted control plane pod, ready or not, as computeStatus
// counts them.
func controlPlanePod(name string, ready bool) *corev1.Pod {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				"cluster.x-k8s.io/cluster-name":  "test",
				"cluster.x-k8s.io/control-plane": "true",
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: status}},
		},
	}
}

// TestHostedComputeStatusCounters covers the values the counters take, since holding
// them back only proves they move together and not that any of them is right.
func TestHostedComputeStatusCounters(t *testing.T) {
	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
	kcp := &cpv1beta2.K0smotronControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test", Namespace: "default", UID: "hosted-uid",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
		},
	}
	kcp.Spec.Version = "v1.30.0+k0s.0"

	// None ready, since reading the version out of a ready one needs a pod exec that
	// no fake client can serve. Unready pods reach every counter without it.
	cl := fake.NewClientBuilder().WithScheme(hostedStatusScheme(t)).
		WithObjects(
			&kapi.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}},
			controlPlanePod("cp-0", false),
			controlPlanePod("cp-1", false),
			controlPlanePod("cp-2", false),
		).Build()
	c := &K0smotronController{
		Client:       cl,
		ClusterCache: stubClusterCache{err: errors.New("connection refused")},
	}

	err := c.computeStatus(context.Background(), cluster, kcp, &kmcScope{client: cl})

	require.ErrorIs(t, err, kutil.ErrNotReady,
		"unavailable replicas have to ask for another look rather than settle")

	require.Equal(t, int32(3), ptr.Deref(kcp.Status.Replicas, 0),
		"every control plane pod counts as a replica")
	require.Equal(t, int32(0), ptr.Deref(kcp.Status.ReadyReplicas, 0))
	require.Equal(t, int32(0), ptr.Deref(kcp.Status.AvailableReplicas, 0),
		"available is the replicas less the unready ones, so all three cancel out here")
	require.Equal(t, int32(0), ptr.Deref(kcp.Status.UpToDateReplicas, 0))
}

// hostedReconcileFixture is a hosted control plane a reconcile can be driven all the
// way through, so only the injected failures decide what comes back.
func hostedReconcileFixture() (*clusterv1.Cluster, *cpv1beta2.K0smotronControlPlane, *kapi.Cluster) {
	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{
		Name: "test", Namespace: "default", UID: "cluster-uid",
	}}
	// The infrastructure ref is set, or the patch at the tail of the deferred block
	// fails on its own and stands in for the error under test.
	cluster.Spec.InfrastructureRef = clusterv1.ContractVersionedObjectReference{
		APIGroup: "infrastructure.cluster.x-k8s.io", Kind: "DockerCluster", Name: "test",
	}

	kcp := &cpv1beta2.K0smotronControlPlane{ObjectMeta: metav1.ObjectMeta{
		Name:      "test",
		Namespace: "default",
		UID:       "hosted-uid",
		// Already there, since adding it returns before the deferred block is armed.
		Finalizers: []string{cpv1beta2.K0smotronControlPlaneFinalizer},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Cluster",
			Name:       cluster.Name,
			UID:        cluster.UID,
		}},
	}}
	// Valid, so the reconcile body gets all the way through. A malformed one fails both
	// the body and the status and proves neither.
	kcp.Spec.Version = "v1.30.0+k0s.0"
	// Reachable once, so a failed read is reported rather than left to the fallback.
	kcp.Status.Initialization.ControlPlaneInitialized = new(true)

	// The address is set, or ensureExternalAddress polls for it for three minutes.
	kmc := &kapi.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       kapi.ClusterSpec{ExternalAddress: "203.0.113.7"},
	}

	return cluster, kcp, kmc
}

// failPodList is what makes computeStatus fail hard while the reconcile body succeeds.
func failPodList() interceptor.Funcs {
	return interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*corev1.PodList); ok {
				return errors.New("status boom")
			}

			return cl.List(ctx, list, opts...)
		},
	}
}

// TestHostedReconcileKeepsBothErrors covers the status error being folded in rather than
// replacing what the reconcile body reported, which used to be dropped outright.
func TestHostedReconcileKeepsBothErrors(t *testing.T) {
	cluster, kcp, kmc := hostedReconcileFixture()

	funcs := failPodList()
	// The body syncs the spec onto the k0smotron Cluster, which the fixture leaves
	// unannotated so this is reached.
	funcs.Patch = func(ctx context.Context, cl client.WithWatch, obj client.Object,
		patch client.Patch, opts ...client.PatchOption,
	) error {
		if _, ok := obj.(*kapi.Cluster); ok {
			return errors.New("body boom")
		}

		return cl.Patch(ctx, obj, patch, opts...)
	}

	c := &K0smotronController{
		Client: fake.NewClientBuilder().WithScheme(hostedStatusScheme(t)).
			WithObjects(cluster, kcp, kmc).
			WithStatusSubresource(cluster, kcp).
			WithInterceptorFuncs(funcs).Build(),
		ClusterCache: stubClusterCache{err: errors.New("connection refused")},
	}

	_, err := c.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: client.ObjectKey{Namespace: "default", Name: "test"},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "body boom",
		"the reconcile error must not be replaced by the status one")
	require.Contains(t, err.Error(), "status boom",
		"and the status error has to be folded in alongside it")
}

// TestHostedReconcilePersistsAvailabilityOnAStatusError covers a status error not taking
// the patch with it, which is what left a control plane that went away looking available.
func TestHostedReconcilePersistsAvailabilityOnAStatusError(t *testing.T) {
	cluster, kcp, kmc := hostedReconcileFixture()

	c := &K0smotronController{
		Client: fake.NewClientBuilder().WithScheme(hostedStatusScheme(t)).
			WithObjects(cluster, kcp, kmc).
			WithStatusSubresource(cluster, kcp).
			WithInterceptorFuncs(failPodList()).Build(),
		ClusterCache: stubClusterCache{err: errors.New("connection refused")},
	}

	// The status computation fails here, which is the point. What matters is what the
	// deferred block persisted on the way out.
	res, err := c.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: client.ObjectKey{Namespace: "default", Name: "test"},
	})

	// Handed back, or nothing brings the control plane round again and the grace
	// period never advances, so the outage is never reported.
	require.ErrorContains(t, err, "status boom",
		"the status error itself has to reach the caller, not merely some error")
	require.Zero(t, res, "a result alongside an error is dropped by controller-runtime")

	persisted := &cpv1beta2.K0smotronControlPlane{}
	require.NoError(t, c.Get(context.Background(),
		client.ObjectKey{Namespace: "default", Name: "test"}, persisted))

	require.NotNil(t, conditions.Get(persisted, string(cpv1beta2.ControlPlaneAvailableCondition)),
		"a status error must not take the availability patch with it")
}

// TestHostedReconcileDeletePersistsFinalizerRemoval covers the deletion patch, since
// reconcileDelete drops the finalizer in memory and nothing else persists it.
func TestHostedReconcileDeletePersistsFinalizerRemoval(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, cpv1beta2.AddToScheme(scheme))
	require.NoError(t, kapi.AddToScheme(scheme))

	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{
		Name: "test", Namespace: "default", UID: "cluster-uid",
	}}
	kcp := &cpv1beta2.K0smotronControlPlane{ObjectMeta: metav1.ObjectMeta{
		Name:              "test",
		Namespace:         "default",
		UID:               "hosted-uid",
		DeletionTimestamp: &metav1.Time{Time: time.Unix(1, 0)},
		Finalizers:        []string{cpv1beta2.K0smotronControlPlaneFinalizer},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Cluster",
			Name:       cluster.Name,
			UID:        cluster.UID,
		}},
	}}

	// No k0smotron Cluster, so reconcileDelete takes its already gone path and drops
	// the finalizer.
	c := &K0smotronController{
		Client: fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(cluster, kcp).
			WithStatusSubresource(cluster, kcp).Build(),
	}

	// Seeded, or the assertion below that deleting forgets the count would hold for a
	// map that was empty all along.
	c.availabilityFailures.Store(availabilityKey(kcp), availabilityFailures{since: time.Now(), seen: 1})

	// The infrastructure patch at the end of the defer has nothing to patch here and
	// errors, which is fine. What matters is that the patch before it ran.
	_, _ = c.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: client.ObjectKey{Namespace: "default", Name: "test"},
	})

	_, ok := c.availabilityFailures.Load(availabilityKey(kcp))
	require.False(t, ok,
		"deleting has to forget the count, or the entry outlives every interest in it")

	// Gone entirely once the last finalizer is dropped, which is the whole point.
	persisted := &cpv1beta2.K0smotronControlPlane{}
	err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "test"}, persisted)
	if err == nil {
		require.NotContains(t, persisted.Finalizers, cpv1beta2.K0smotronControlPlaneFinalizer,
			"the finalizer removal has to be persisted, or the object never goes away")
	} else {
		require.True(t, apierrors.IsNotFound(err))
	}
}
