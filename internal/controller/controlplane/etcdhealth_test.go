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

package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/collections"

	bootstrapv1 "github.com/k0sproject/k0smotron/v2/api/bootstrap/v1beta2"
	cpv1beta2 "github.com/k0sproject/k0smotron/v2/api/controlplane/v1beta2"
)

func TestEtcdManaged(t *testing.T) {
	kcp := func(k0s map[string]any) *cpv1beta2.K0sControlPlane {
		spec := cpv1beta2.K0sControlPlaneSpec{K0sConfigSpec: bootstrapv1.K0sConfigSpec{}}
		if k0s != nil {
			spec.K0sConfigSpec.K0s = &unstructured.Unstructured{Object: k0s}
		}

		return &cpv1beta2.K0sControlPlane{Spec: spec}
	}

	t.Run("no k0s config at all is etcd", func(t *testing.T) {
		managed, err := etcdManaged(kcp(nil))

		require.NoError(t, err)
		require.True(t, managed)
	})

	t.Run("an empty k0s config is etcd", func(t *testing.T) {
		managed, err := etcdManaged(kcp(map[string]any{}))

		require.NoError(t, err)
		require.True(t, managed)
	})

	t.Run("a kine data source is not etcd", func(t *testing.T) {
		managed, err := etcdManaged(kcp(map[string]any{
			"spec": map[string]any{
				"storage": map[string]any{
					"kine": map[string]any{"dataSource": "sqlite:///var/lib/k0s/db.sqlite"},
				},
			},
		}))

		require.NoError(t, err)
		require.False(t, managed)
	})

	// k0s fills in a default data source from the type alone, so the type has to be read.
	t.Run("the kine type alone is not etcd", func(t *testing.T) {
		managed, err := etcdManaged(kcp(map[string]any{
			"spec": map[string]any{"storage": map[string]any{"type": "kine"}},
		}))

		require.NoError(t, err)
		require.False(t, managed)
	})

	t.Run("an external etcd cluster is not ours to track", func(t *testing.T) {
		managed, err := etcdManaged(kcp(map[string]any{
			"spec": map[string]any{
				"storage": map[string]any{
					"type": "etcd",
					"etcd": map[string]any{
						"externalCluster": map[string]any{"endpoints": []any{"https://etcd:2379"}},
					},
				},
			},
		}))

		require.NoError(t, err)
		require.False(t, managed)
	})

	t.Run("the etcd type with no external cluster is etcd", func(t *testing.T) {
		managed, err := etcdManaged(kcp(map[string]any{
			"spec": map[string]any{"storage": map[string]any{"type": "etcd"}},
		}))

		require.NoError(t, err)
		require.True(t, managed)
	})

	// A wrong type at any of the three paths is a config the API server accepts and this
	// cannot read, so it has to surface rather than be guessed at.
	for _, tc := range []struct {
		name string
		k0s  map[string]any
		// wantField is the path the accessor error names. notField is a path it must
		// not name, since a wrapper that labels every failure with one field points a
		// user at the wrong one for the other two.
		wantField string
		notField  string
	}{
		{name: "a storage type that is not a string", wantField: ".spec.storage.type ", notField: "kine", k0s: map[string]any{
			"spec": map[string]any{"storage": map[string]any{"type": int64(1)}},
		}},
		{name: "a kine data source that is not a string", wantField: ".spec.storage.kine.dataSource ", notField: "externalCluster", k0s: map[string]any{
			"spec": map[string]any{"storage": map[string]any{"kine": map[string]any{"dataSource": int64(1)}}},
		}},
		{name: "an external cluster that is not a map", wantField: ".spec.storage.etcd.externalCluster ", notField: "kine", k0s: map[string]any{
			"spec": map[string]any{"storage": map[string]any{"etcd": map[string]any{"externalCluster": "nope"}}},
		}},
	} {
		t.Run(tc.name+" is an error", func(t *testing.T) {
			_, err := etcdManaged(kcp(tc.k0s))

			require.ErrorContains(t, err, tc.wantField)
			require.NotContains(t, err.Error(), tc.notField,
				"naming a field the failure was not about sends the reader to the wrong place")
		})
	}

	t.Run("an empty kine data source is still etcd", func(t *testing.T) {
		managed, err := etcdManaged(kcp(map[string]any{
			"spec": map[string]any{
				"storage": map[string]any{"kine": map[string]any{"dataSource": ""}},
			},
		}))

		require.NoError(t, err)
		require.True(t, managed)
	})
}

func TestEtcdMemberHealthOf(t *testing.T) {
	member := func(reconcile string, conditions ...etcdMemberCondition) etcdMember {
		return etcdMember{Status: etcdMemberStatus{ReconcileStatus: reconcile, Conditions: conditions}}
	}
	joined := func(status string) etcdMemberCondition {
		return etcdMemberCondition{Type: etcdMemberConditionTypeJoined, Status: status}
	}

	for _, tc := range []struct {
		name   string
		member etcdMember
		want   metav1.ConditionStatus
	}{
		{name: "a joined member is healthy", member: member("Success", joined("True")), want: metav1.ConditionTrue},
		{name: "a member that left is unhealthy", member: member("Success", joined("False")), want: metav1.ConditionFalse},
		{name: "a failed reconcile is unhealthy", member: member("Failed", joined("True")), want: metav1.ConditionFalse},
		{name: "a failed reconcile outranks a joined condition that is absent", member: member("Failed"), want: metav1.ConditionFalse},
		// None of these is evidence of health, so none may be reported as healthy.
		{name: "an unknown condition is unknown", member: member("", joined("Unknown")), want: metav1.ConditionUnknown},
		{name: "an empty condition status is unknown", member: member("", joined("")), want: metav1.ConditionUnknown},
		{name: "a status that is not a condition value is unknown", member: member("", joined("yes")), want: metav1.ConditionUnknown},
		{name: "no conditions at all is unknown", member: member(""), want: metav1.ConditionUnknown},
		{name: "another condition type alone is unknown", member: member("", etcdMemberCondition{Type: "Other", Status: "True"}), want: metav1.ConditionUnknown},
		{
			name:   "another condition type does not hide the joined one",
			member: member("", etcdMemberCondition{Type: "Other", Status: "False"}, joined("True")),
			want:   metav1.ConditionTrue,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, etcdMemberHealthOf(tc.member))
		})
	}
}

// etcdMemberRoundTripper serves the etcd member list, and records whether it was asked at all.
type etcdMemberRoundTripper struct {
	members []etcdMember
	raw     string
	status  int
	called  bool
}

func (f *etcdMemberRoundTripper) run(req *http.Request) (*http.Response, error) {
	header := http.Header{}
	header.Set("Content-Type", "application/json")

	if req.Method != http.MethodGet || req.URL.Path != etcdMembersAPIPath {
		return &http.Response{StatusCode: http.StatusNotFound, Header: header, Body: http.NoBody}, nil
	}

	f.called = true
	if f.status != 0 {
		return &http.Response{StatusCode: f.status, Header: header, Body: http.NoBody}, nil
	}

	if f.raw != "" {
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(f.raw))}, nil
	}

	body, err := json.Marshal(etcdMemberList{Items: f.members})
	if err != nil {
		return nil, err
	}

	return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(bytes.NewReader(body))}, nil
}

// The round tripper below marshals the production types, so the path and the field names
// are checked against literals here instead.
func TestEtcdMemberListWireContract(t *testing.T) {
	require.Equal(t, "/apis/etcd.k0sproject.io/v1beta1/etcdmembers", etcdMembersAPIPath)

	var list etcdMemberList
	require.NoError(t, json.Unmarshal([]byte(`{
	  "apiVersion": "etcd.k0sproject.io/v1beta1",
	  "kind": "EtcdMemberList",
	  "items": [
	    {
	      "metadata": {"name": "cp-0"},
	      "status": {
	        "peerAddress": "10.0.0.1",
	        "reconcileStatus": "Failed",
	        "conditions": [{"type": "Joined", "status": "False", "message": "left"}]
	      }
	    }
	  ]
	}`), &list))

	require.Len(t, list.Items, 1)
	require.Equal(t, "cp-0", list.Items[0].Name)
	require.Equal(t, "Failed", list.Items[0].Status.ReconcileStatus)
	require.Equal(t, etcdMemberConditionTypeJoined, list.Items[0].Status.Conditions[0].Type)
	require.Equal(t, metav1.ConditionFalse, etcdMemberHealthOf(list.Items[0]))
}

func etcdMemberNamed(name, joined string) etcdMember {
	return etcdMember{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: etcdMemberStatus{Conditions: []etcdMemberCondition{
			{Type: etcdMemberConditionTypeJoined, Status: joined},
		}},
	}
}

// etcdReader returns a controller whose workload cluster serves the given member list.
func etcdReader(t *testing.T, frt *etcdMemberRoundTripper) *K0sController {
	t.Helper()

	restClient, err := rest.RESTClientFor(&rest.Config{
		ContentConfig: rest.ContentConfig{
			NegotiatedSerializer: scheme.Codecs,
			GroupVersion:         &metav1.SchemeGroupVersion,
		},
	})
	require.NoError(t, err)
	restClient.Client = restfake.CreateHTTPClient(frt.run)

	return &K0sController{workloadClusterKubeClient: kubernetes.New(restClient)}
}

func TestEtcdMemberHealth(t *testing.T) {
	machine := func(name string) *clusterv1.Machine {
		return &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}}
	}
	member := etcdMemberNamed
	controller := func(frt *etcdMemberRoundTripper) *K0sController { return etcdReader(t, frt) }

	machines := collections.FromMachines(machine("cp-0"), machine("cp-1"), machine("cp-2"))
	cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"}}

	t.Run("each machine gets the state of its own member", func(t *testing.T) {
		frt := &etcdMemberRoundTripper{members: []etcdMember{
			member("cp-0", "True"), member("cp-1", "False"), member("cp-2", "True"),
		}}

		got := controller(frt).etcdMemberHealth(context.Background(), cluster, machines)

		require.Equal(t, map[string]metav1.ConditionStatus{
			"cp-0": metav1.ConditionTrue,
			"cp-1": metav1.ConditionFalse,
			"cp-2": metav1.ConditionTrue,
		}, got)
	})

	// Absent from the map rather than reported either way, so a machine still joining is
	// neither healthy nor broken.
	t.Run("a machine with no member yet is left out", func(t *testing.T) {
		frt := &etcdMemberRoundTripper{members: []etcdMember{member("cp-0", "True")}}

		got := controller(frt).etcdMemberHealth(context.Background(), cluster, machines)

		require.Equal(t, map[string]metav1.ConditionStatus{"cp-0": metav1.ConditionTrue}, got)
	})

	// The state is not known, so it is neither published as healthy nor as broken. The
	// condition that reads this map reports Unknown for an absent entry.
	t.Run("a member whose joined state is unknown is left out", func(t *testing.T) {
		frt := &etcdMemberRoundTripper{members: []etcdMember{
			member("cp-0", "True"), member("cp-1", "Unknown"), member("cp-2", "False"),
		}}

		got := controller(frt).etcdMemberHealth(context.Background(), cluster, machines)

		require.Equal(t, map[string]metav1.ConditionStatus{
			"cp-0": metav1.ConditionTrue,
			"cp-2": metav1.ConditionFalse,
		}, got)
	})

	t.Run("a member with no conditions at all is left out", func(t *testing.T) {
		bare := etcdMember{ObjectMeta: metav1.ObjectMeta{Name: "cp-1"}}
		frt := &etcdMemberRoundTripper{members: []etcdMember{member("cp-0", "True"), bare}}

		got := controller(frt).etcdMemberHealth(context.Background(), cluster, machines)

		require.Equal(t, map[string]metav1.ConditionStatus{"cp-0": metav1.ConditionTrue}, got)
	})

	t.Run("a member for something that is not a machine is ignored", func(t *testing.T) {
		frt := &etcdMemberRoundTripper{members: []etcdMember{member("some-other-node", "False")}}

		got := controller(frt).etcdMemberHealth(context.Background(), cluster, machines)

		require.Empty(t, got)
	})

	// An unreachable cluster must not read as a wholly broken control plane, which would
	// send every machine into the unhealthy tier at once.
	t.Run("a failed list reports nothing rather than everything", func(t *testing.T) {
		frt := &etcdMemberRoundTripper{status: http.StatusInternalServerError}

		got := controller(frt).etcdMemberHealth(context.Background(), cluster, machines)

		require.Nil(t, got)
		require.True(t, frt.called, "the list has to be attempted before it can be given up on")
	})

	t.Run("a body that is not a member list reports nothing", func(t *testing.T) {
		frt := &etcdMemberRoundTripper{raw: "<html>gateway error</html>"}

		got := controller(frt).etcdMemberHealth(context.Background(), cluster, machines)

		require.Nil(t, got)
	})

	t.Run("a failed reconcile is unhealthy too", func(t *testing.T) {
		failed := etcdMember{
			ObjectMeta: metav1.ObjectMeta{Name: "cp-0"},
			Status:     etcdMemberStatus{ReconcileStatus: "Failed"},
		}
		frt := &etcdMemberRoundTripper{members: []etcdMember{failed}}

		got := controller(frt).etcdMemberHealth(context.Background(), cluster, machines)

		require.Equal(t, map[string]metav1.ConditionStatus{"cp-0": metav1.ConditionFalse}, got)
	})

	// Only k0s v1.31.1 and above names the member after the machine, so an older cluster
	// has to be matched through the node.
	t.Run("a member named after the node is still matched", func(t *testing.T) {
		old := &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{Name: "cp-9", Namespace: "default"}}
		old.Status.NodeRef = clusterv1.MachineNodeReference{Name: "ip-10-0-0-9"}
		frt := &etcdMemberRoundTripper{members: []etcdMember{member("ip-10-0-0-9", "False")}}

		got := controller(frt).etcdMemberHealth(context.Background(), cluster, collections.FromMachines(old))

		require.Equal(t, map[string]metav1.ConditionStatus{"cp-9": metav1.ConditionFalse}, got)
	})
}

// The tier is covered elsewhere from a hand written health map. This covers the seam, a
// member list read off the workload cluster deciding which machine the election picks.
func TestElectionRunsOnTheDerivedMemberHealth(t *testing.T) {
	machine := func(name string, age time.Duration) *clusterv1.Machine {
		return &clusterv1.Machine{ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-age)),
		}}
	}

	// The broken member is neither the oldest nor the newest, so electing it can only
	// come from the member list.
	oldest := machine("cp-oldest", time.Hour)
	broken := machine("cp-broken", 30*time.Minute)
	newest := machine("cp-newest", time.Minute)
	active := collections.FromMachines(oldest, broken, newest)

	frt := &etcdMemberRoundTripper{members: []etcdMember{
		etcdMemberNamed("cp-oldest", "True"),
		etcdMemberNamed("cp-broken", "False"),
		etcdMemberNamed("cp-newest", "True"),
	}}
	c := etcdReader(t, frt)

	// Every machine is outdated, which is the rollout the tier exists for.
	scope := &controlplane{
		cluster:             &clusterv1.Cluster{},
		kcp:                 &cpv1beta2.K0sControlPlane{Spec: cpv1beta2.K0sControlPlaneSpec{Replicas: 3}},
		activeMachines:      active,
		deletedMachines:     collections.Machines{},
		upToDateMachines:    collections.New(),
		notUpToDateMachines: active,
		etcdManaged:         true,
	}
	scope.etcdMemberHealth = c.etcdMemberHealth(context.Background(), scope.cluster, active)

	require.True(t, frt.called, "the member list has to be read for the election to mean anything")

	got, reason := selectMachineToDelete(context.Background(), scope)

	require.NotNil(t, got)
	require.Equal(t, "cp-broken", got.Name, "the machine whose member left has to go, not the oldest")
	require.Equal(t, "outdated with an unhealthy etcd member", reason)
}
