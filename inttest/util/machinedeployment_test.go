/*
Copyright 2026 k0s authors

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

//nolint:revive
package util

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	"github.com/stretchr/testify/require"
)

// topologyState is what the fake API serves, and how a test makes one read fail.
type topologyState struct {
	version    string
	mds        []clusterv1.MachineDeployment
	machines   []clusterv1.Machine
	noVersion  bool
	failOn     string
	failAfter  int32
	served     atomic.Int32
	complaints complaints
}

// complaints collects what the handler noticed. Failing an assertion on the server
// goroutine aborts the response instead of reporting, so the test asserts these itself.
type complaints struct {
	mu   sync.Mutex
	seen []string
}

func (c *complaints) add(problem string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.seen = append(c.seen, problem)
}

func (c *complaints) list() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]string(nil), c.seen...)
}

// topologyAPI serves the three reads the wait makes. Paths are matched exactly, or a
// read aimed at the wrong group or resource still answers and nothing notices.
func topologyAPI(t *testing.T, state *topologyState) *kubernetes.Clientset {
	t.Helper()

	const base = "/apis/cluster.x-k8s.io/v1beta2/namespaces/default/"

	write := func(w http.ResponseWriter, obj any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(obj)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Counted per matching read, so a test can fail the second one without
		// depending on how fast the wall clock got there.
		if state.failOn != "" && strings.Contains(r.URL.Path, state.failOn) &&
			state.served.Add(1) > state.failAfter {
			w.WriteHeader(http.StatusUnauthorized)
			write(w, &metav1.Status{
				TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
				Status:   metav1.StatusFailure, Code: http.StatusUnauthorized,
				Reason: metav1.StatusReasonUnauthorized, Message: "the bearer token has expired",
			})

			return
		}

		selector := r.URL.Query().Get("labelSelector")

		switch r.URL.Path {
		case base + "machinedeployments":
			// The webhook ignores MachineDeployments the topology does not own, so a
			// selector that stopped saying so would gate a bump on a foreign one.
			if !strings.Contains(selector, clusterv1.ClusterTopologyOwnedLabel) ||
				!strings.Contains(selector, clusterv1.ClusterNameLabel+"=test") {
				state.complaints.add("machinedeployments selector was " + selector)
			}

			write(w, &clusterv1.MachineDeploymentList{Items: state.mds})
		case base + "machines":
			if !strings.Contains(selector, clusterv1.ClusterNameLabel+"=test") {
				state.complaints.add("machines selector was " + selector)
			}

			write(w, &clusterv1.MachineList{Items: state.machines})
		case base + "clusters/test":
			topology := clusterv1.Topology{Version: state.version}
			if state.noVersion {
				topology = clusterv1.Topology{}
			}

			write(w, &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec:       clusterv1.ClusterSpec{Topology: topology},
			})
		default:
			// Not silently empty, since an empty list reads as a settled rollout.
			state.complaints.add("unexpected path " + r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	// QPS off, or on a short deadline the client rate limiter becomes the error being
	// reported instead of the one the server sent.
	kc, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL, QPS: -1})
	require.NoError(t, err)

	return kc
}

// machineDeployment is one topology owned MachineDeployment at a version.
func machineDeployment(name, version string, replicas int32) clusterv1.MachineDeployment {
	return clusterv1.MachineDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: clusterv1.MachineDeploymentSpec{
			Replicas: new(replicas),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{Version: version},
			},
		},
	}
}

// machine is one Machine owned by a MachineDeployment, terminating or not.
func machine(name, mdName, version string, terminating bool) clusterv1.Machine {
	m := clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{clusterv1.MachineDeploymentNameLabel: mdName},
		},
		Spec: clusterv1.MachineSpec{Version: version},
	}
	if terminating {
		m.DeletionTimestamp = &metav1.Time{Time: time.Unix(1, 0)}
	}

	return m
}

// TestWaitForMachineDeploymentsUpToDate covers the rule the webhook applies, since
// returning early here is what let the version bump be refused.
func TestWaitForMachineDeploymentsUpToDate(t *testing.T) {
	const version = "v1.30.0+k0s.0"

	for _, tc := range []struct {
		name      string
		mds       []clusterv1.MachineDeployment
		machines  []clusterv1.Machine
		noVersion bool
		failOn    string
		wantDone  bool
		wantErr   string
	}{
		{
			name:     "one deployment with its machine up to date is done",
			mds:      []clusterv1.MachineDeployment{machineDeployment("md", version, 1)},
			machines: []clusterv1.Machine{machine("m1", "md", version, false)},
			wantDone: true,
		},
		{
			name:     "a machine still on the old version is not done",
			mds:      []clusterv1.MachineDeployment{machineDeployment("md", version, 1)},
			machines: []clusterv1.Machine{machine("m1", "md", "v1.29.0+k0s.0", false)},
			wantErr:  "machine m1 is at v1.29.0+k0s.0",
		},
		{
			// The webhook lists terminating machines too and refuses while one is off
			// version, so skipping them here reported done too early.
			name: "a terminating machine on the old version is not done",
			mds:  []clusterv1.MachineDeployment{machineDeployment("md", version, 1)},
			machines: []clusterv1.Machine{
				machine("old", "md", "v1.29.0+k0s.0", true),
				machine("new", "md", version, false),
			},
			wantErr: "machine old is at v1.29.0+k0s.0",
		},
		{
			// Counted while live only, so the terminating one cannot stand in for the
			// replacement that has not been created yet.
			name: "a terminating machine does not fill the replica count",
			mds:  []clusterv1.MachineDeployment{machineDeployment("md", version, 2)},
			machines: []clusterv1.Machine{
				machine("m1", "md", version, false),
				machine("m2", "md", version, true),
			},
			wantErr: "has 1 machines",
		},
		{
			name:    "no machines yet is not done",
			mds:     []clusterv1.MachineDeployment{machineDeployment("md", version, 1)},
			wantErr: "has 0 machines",
		},
		{
			// Surplus is not waited out, since the webhook does not count at all and
			// exact equality would never be satisfied during a scale down.
			name: "surplus machines at the right version are done",
			mds:  []clusterv1.MachineDeployment{machineDeployment("md", version, 1)},
			machines: []clusterv1.Machine{
				machine("m1", "md", version, false),
				machine("m2", "md", version, false),
			},
			wantDone: true,
		},
		{
			name:     "a deployment still on the old version is not done",
			mds:      []clusterv1.MachineDeployment{machineDeployment("md", "v1.29.0+k0s.0", 1)},
			machines: []clusterv1.Machine{machine("m1", "md", "v1.29.0+k0s.0", false)},
			wantErr:  "machine deployment md is at v1.29.0+k0s.0",
		},
		{
			// Not created yet is a race to wait out, since this runs before the webhook
			// rather than inside it.
			name:    "no deployments yet is not done",
			wantErr: "no topology owned machine deployments",
		},
		{
			name: "a machine of another deployment is ignored",
			mds:  []clusterv1.MachineDeployment{machineDeployment("md", version, 1)},
			machines: []clusterv1.Machine{
				machine("m1", "md", version, false),
				machine("other", "other-md", "v1.29.0+k0s.0", false),
			},
			wantDone: true,
		},
		{
			// Every deployment is checked, or a second one still rolling is missed and
			// the webhook refuses the bump this wait exists to make safe.
			name: "a second deployment still rolling is not done",
			mds: []clusterv1.MachineDeployment{
				machineDeployment("md-a", version, 1),
				machineDeployment("md-b", version, 1),
			},
			machines: []clusterv1.Machine{
				machine("a1", "md-a", version, false),
				machine("b1", "md-b", "v1.29.0+k0s.0", false),
			},
			wantErr: "machine b1 is at v1.29.0+k0s.0",
		},
		{
			// The webhook treats a versionless machine as a hard error, so reporting
			// done here would hand the caller a bump that cannot succeed.
			name:     "a machine with no version is not done",
			mds:      []clusterv1.MachineDeployment{machineDeployment("md", version, 1)},
			machines: []clusterv1.Machine{machine("m1", "md", "", false)},
			wantErr:  "machine m1 is at ,",
		},
		{
			// The list read has its own reporting, and a wrong group or a missing
			// permission shows up here rather than on the cluster read.
			name:     "a machine deployment list that fails reports the read",
			mds:      []clusterv1.MachineDeployment{machineDeployment("md", version, 1)},
			machines: []clusterv1.Machine{machine("m1", "md", version, false)},
			failOn:   "machinedeployments",
			wantErr:  "listing machine deployments",
		},
		{
			name:     "a machine list that fails reports the read",
			mds:      []clusterv1.MachineDeployment{machineDeployment("md", version, 1)},
			machines: []clusterv1.Machine{machine("m1", "md", version, false)},
			failOn:   "namespaces/default/machines",
			wantErr:  "listing machines",
		},
		{
			// A cluster that declares no topology version has nothing to compare against,
			// so reporting done would hand the caller a bump with no target.
			name:      "a cluster with no topology version is not done",
			mds:       []clusterv1.MachineDeployment{machineDeployment("md", version, 1)},
			machines:  []clusterv1.Machine{machine("m1", "md", version, false)},
			noVersion: true,
			wantErr:   "declares no topology version",
		},
		{
			// The read failure has to reach the caller, since a wrong path or an expired
			// credential otherwise looks exactly like a rollout still in progress.
			name:     "a read that keeps failing reports the read",
			mds:      []clusterv1.MachineDeployment{machineDeployment("md", version, 1)},
			machines: []clusterv1.Machine{machine("m1", "md", version, false)},
			failOn:   "clusters/test",
			wantErr:  "the bearer token has expired",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &topologyState{
				version: version, mds: tc.mds, machines: tc.machines,
				noVersion: tc.noVersion, failOn: tc.failOn,
			}
			kc := topologyAPI(t, state)

			// Short, so a case that never settles reports rather than polling on.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			err := WaitForMachineDeploymentsUpToDate(ctx, kc, "test", "default")

			require.Empty(t, state.complaints.list(),
				"the reads the wait makes have to stay on the paths and selectors above")

			if tc.wantDone {
				require.NoError(t, err)

				return
			}

			require.Error(t, err, "this state has to be waited out, not reported done")
			require.Contains(t, err.Error(), tc.wantErr,
				"the reason has to reach the caller rather than a bare deadline")
		})
	}
}

// TestWaitForMachineDeploymentsUpToDateReportsTheCurrentReason covers the report being
// what the last tick found, since a reason from ten minutes ago misdirects the reader.
func TestWaitForMachineDeploymentsUpToDateReportsTheCurrentReason(t *testing.T) {
	const version = "v1.30.0+k0s.0"

	state := &topologyState{
		version: version,
		// One machine short, so the first tick records a state reason.
		mds:      []clusterv1.MachineDeployment{machineDeployment("md", version, 2)},
		machines: []clusterv1.Machine{machine("m1", "md", version, false)},
		// Served once, so the state reason is recorded before reads start failing.
		failOn:    "clusters/test",
		failAfter: 1,
	}
	kc := topologyAPI(t, state)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := WaitForMachineDeploymentsUpToDate(ctx, kc, "test", "default")

	require.Error(t, err)
	require.Contains(t, err.Error(), "the bearer token has expired",
		"the read that is failing now is what the caller needs")
	require.NotContains(t, err.Error(), "has 1 machines",
		"and the state from before it started failing has gone stale")
}

// TestMachineRolloutTimeout pins the budget with a literal, since asserting it through
// the symbol would pass whatever it was changed to.
func TestMachineRolloutTimeout(t *testing.T) {
	require.Equal(t, 10*time.Minute, machineRolloutTimeout)
}
