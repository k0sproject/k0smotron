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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func pod(name string, ready bool, terminating bool) corev1.Pod {
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
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

func TestScrapeDeploymentReconcileCountersSkipsPodsThatCannotAnswer(t *testing.T) {
	pods := []corev1.Pod{
		pod("current", true, false),
		pod("old", true, true),
		pod("starting", false, false),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/apis/apps/v1/namespaces/ns/deployments/d":
			_ = json.NewEncoder(w).Encode(&appsv1.Deployment{
				TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
				ObjectMeta: metav1.ObjectMeta{Name: "d", Namespace: "ns"},
				Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "d"}}},
			})
		case r.URL.Path == "/api/v1/namespaces/ns/pods":
			_ = json.NewEncoder(w).Encode(&corev1.PodList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"}, Items: pods})
		case strings.HasSuffix(r.URL.Path, "/proxy/metrics"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(reconcileMetricName + `{controller="kcp"} 3` + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cs, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	require.NoError(t, err)

	got, err := ScrapeDeploymentReconcileCounters(t.Context(), cs, "d", "ns")
	require.NoError(t, err)
	require.Equal(t, PodReconcileCounters{"current": {"kcp": 3}}, got)
}

func TestPodReconcileCountersDiffSkipsPodsWithoutABaseline(t *testing.T) {
	base := PodReconcileCounters{"a": {"kcp": 10}}
	cur := PodReconcileCounters{"a": {"kcp": 12}, "b": {"kcp": 900}}

	require.Equal(t, ReconcileCounters{"kcp": 2}, cur.Diff(base))
}
