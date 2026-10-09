//go:build e2e

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

package util

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func pod(name string, phase corev1.PodPhase, ready bool, terminating bool) corev1.Pod {
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.PodStatus{
			Phase: phase,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionFalse,
			}},
		},
	}
	if ready {
		p.Status.Conditions[0].Status = corev1.ConditionTrue
	}
	if terminating {
		now := metav1.Now()
		p.DeletionTimestamp = &now
	}

	return p
}

// TestScrapablePods covers the pods that cannot answer being left out, since the pod
// proxy reports an unreachable port as a bare 400 that fails the whole suite.
func TestScrapablePods(t *testing.T) {
	for _, tc := range []struct {
		name string
		pod  corev1.Pod
		want bool
	}{
		{
			name: "running and ready is scraped",
			pod:  pod("current", corev1.PodRunning, true, false),
			want: true,
		},
		{
			// The case the failing run hit. A pod from the replaced ReplicaSet stays
			// Running for its grace period while its metrics server has already gone.
			name: "terminating is skipped even though it is still running and ready",
			pod:  pod("old", corev1.PodRunning, true, true),
			want: false,
		},
		{
			name: "running but not ready is skipped",
			pod:  pod("starting", corev1.PodRunning, false, false),
			want: false,
		},
		{
			name: "pending is skipped",
			pod:  pod("pending", corev1.PodPending, false, false),
			want: false,
		},
		{
			name: "succeeded is skipped",
			pod:  pod("done", corev1.PodSucceeded, false, false),
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := scrapablePods([]corev1.Pod{tc.pod})

			if !tc.want {
				require.Empty(t, got)

				return
			}

			require.Len(t, got, 1)
			require.Equal(t, tc.pod.Name, got[0].Name)
		})
	}

	t.Run("a mixed list keeps only the pod that can answer", func(t *testing.T) {
		got := scrapablePods([]corev1.Pod{
			pod("old", corev1.PodRunning, true, true),
			pod("current", corev1.PodRunning, true, false),
			pod("starting", corev1.PodRunning, false, false),
		})

		require.Len(t, got, 1)
		require.Equal(t, "current", got[0].Name)
	})
}

// TestPodReconcileCountersDiff covers the rate being measured only where there is a
// baseline to measure it against.
func TestPodReconcileCountersDiff(t *testing.T) {
	t.Run("a pod in both snapshots contributes its delta", func(t *testing.T) {
		base := PodReconcileCounters{"a": {"kcp": 10}}
		cur := PodReconcileCounters{"a": {"kcp": 25}}

		require.Equal(t, ReconcileCounters{"kcp": 15}, cur.Diff(base))
	})

	t.Run("a pod that appeared after the baseline contributes nothing", func(t *testing.T) {
		base := PodReconcileCounters{"a": {"kcp": 10}}
		cur := PodReconcileCounters{"a": {"kcp": 12}, "b": {"kcp": 900}}

		require.Equal(t, ReconcileCounters{"kcp": 2}, cur.Diff(base),
			"the new pod has no rate to measure, so counting it reports a storm that did not happen")
	})

	t.Run("a pod that went away between snapshots is ignored", func(t *testing.T) {
		base := PodReconcileCounters{"a": {"kcp": 10}, "gone": {"kcp": 500}}
		cur := PodReconcileCounters{"a": {"kcp": 11}}

		require.Equal(t, ReconcileCounters{"kcp": 1}, cur.Diff(base))
	})

	t.Run("a restart inside the window counts the current value", func(t *testing.T) {
		base := PodReconcileCounters{"a": {"kcp": 100}}
		cur := PodReconcileCounters{"a": {"kcp": 7}}

		require.Equal(t, ReconcileCounters{"kcp": 7}, cur.Diff(base))
	})
}
