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

package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// metricsClient is a thin wrapper around the REST client for metrics.k8s.io.
type metricsClient struct {
	rc *rest.Config
	kc *kubernetes.Clientset
}

// PodMetricSample holds a single pod's CPU and memory observation.
type PodMetricSample struct {
	PodName   string
	CPUMillis int64 // millicores
	MemoryMiB int64 // mebibytes
}

// podMetricsList mirrors the relevant portion of the metrics.k8s.io/v1beta1 PodMetricsList.
type podMetricsList struct {
	Items []podMetricsItem `json:"items"`
}

type podMetricsItem struct {
	Metadata   metav1.ObjectMeta `json:"metadata"`
	Containers []containerUsage  `json:"containers"`
}

type containerUsage struct {
	Name  string            `json:"name"`
	Usage map[string]string `json:"usage"` // {"cpu": "123m", "memory": "456Mi"}
}

// newMetricsClient builds a metricsClient. It returns an error only if the REST config
// cannot be copied; actual API availability is checked lazily at collection time.
func newMetricsClient(rc *rest.Config) (*metricsClient, error) {
	kc, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes client for metrics: %w", err)
	}
	return &metricsClient{rc: rc, kc: kc}, nil
}

// collectHCPMetrics queries metrics.k8s.io for all pods in the given namespace
// and returns per-pod CPU and memory aggregated across all containers.
func collectHCPMetrics(ctx context.Context, mc *metricsClient, _ *kubernetes.Clientset, namespace string) ([]PodMetricSample, error) {
	path := fmt.Sprintf("/apis/metrics.k8s.io/v1beta1/namespaces/%s/pods", namespace)
	raw, err := mc.kc.RESTClient().Get().AbsPath(path).DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("get pod metrics for namespace %s: %w", namespace, err)
	}

	return parsePodMetricsList(raw)
}

// collectOperatorMetrics returns the resource usage for the k0smotron controller-manager pod.
func collectOperatorMetrics(ctx context.Context, mc *metricsClient) (PodMetricSample, error) {
	const operatorNS = "k0smotron"
	const operatorPrefix = "k0smotron-controller-manager-"

	path := fmt.Sprintf("/apis/metrics.k8s.io/v1beta1/namespaces/%s/pods", operatorNS)
	raw, err := mc.kc.RESTClient().Get().AbsPath(path).DoRaw(ctx)
	if err != nil {
		return PodMetricSample{}, fmt.Errorf("get operator pod metrics: %w", err)
	}

	samples, err := parsePodMetricsList(raw)
	if err != nil {
		return PodMetricSample{}, err
	}

	for _, s := range samples {
		if strings.HasPrefix(s.PodName, operatorPrefix) {
			return s, nil
		}
	}
	return PodMetricSample{}, fmt.Errorf("operator pod not found in namespace %s", operatorNS)
}

// parsePodMetricsList decodes a raw metrics.k8s.io PodMetricsList response.
func parsePodMetricsList(raw []byte) ([]PodMetricSample, error) {
	var list podMetricsList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("decode pod metrics list: %w", err)
	}

	samples := make([]PodMetricSample, 0, len(list.Items))
	for _, item := range list.Items {
		var totalCPUm, totalMemMiB int64
		for _, c := range item.Containers {
			cpuM, memMiB := parseContainerUsage(c.Usage)
			totalCPUm += cpuM
			totalMemMiB += memMiB
		}
		samples = append(samples, PodMetricSample{
			PodName:   item.Metadata.Name,
			CPUMillis: totalCPUm,
			MemoryMiB: totalMemMiB,
		})
	}
	return samples, nil
}

// parseContainerUsage parses CPU (millicores) and memory (MiB) from the usage map.
func parseContainerUsage(usage map[string]string) (cpuMillis, memMiB int64) {
	if cpuStr, ok := usage["cpu"]; ok {
		cpuMillis = parseCPUMillis(cpuStr)
	}
	if memStr, ok := usage["memory"]; ok {
		memMiB = parseMemoryMiB(memStr)
	}
	return
}

// parseCPUMillis converts a Kubernetes CPU quantity string to millicores.
// Supported formats: "123m" (millicores), "1.5" or "2" (cores).
func parseCPUMillis(s string) int64 {
	if strings.HasSuffix(s, "m") {
		var v int64
		fmt.Sscanf(s[:len(s)-1], "%d", &v)
		return v
	}
	// Fractional cores.
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return int64(f * 1000)
}

// parseMemoryMiB converts a Kubernetes memory quantity string to mebibytes.
// Supported suffixes: Ki, Mi, Gi, Ti (binary) and k, M, G (decimal).
func parseMemoryMiB(s string) int64 {
	conversions := []struct {
		suffix string
		mib    float64
	}{
		{"Ti", 1024 * 1024},
		{"Gi", 1024},
		{"Mi", 1},
		{"Ki", 1.0 / 1024},
		{"G", 1000.0 / 1.048576},
		{"M", 1.0 / 1.048576},
		{"k", 1000.0 / (1024 * 1024)},
	}
	for _, c := range conversions {
		if strings.HasSuffix(s, c.suffix) {
			var v float64
			fmt.Sscanf(s[:len(s)-len(c.suffix)], "%f", &v)
			return int64(v * c.mib)
		}
	}
	// Raw bytes.
	var v int64
	fmt.Sscanf(s, "%d", &v)
	return v / (1024 * 1024)
}

// percentiles returns p50, p95, p99, and max from a slice of durations.
// Returns zero for all values if the slice is empty.
func percentiles(durations []time.Duration) (p50, p95, p99, maxDuration time.Duration) {
	if len(durations) == 0 {
		return
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	slices.Sort(sorted)

	p50 = sorted[percentileIndex(len(sorted), 50)]
	p95 = sorted[percentileIndex(len(sorted), 95)]
	p99 = sorted[percentileIndex(len(sorted), 99)]
	maxDuration = sorted[len(sorted)-1]
	return
}

// int64Percentiles returns p50 and p95 from a slice of int64 values.
func int64Percentiles(vals []int64) (p50, p95 int64) {
	if len(vals) == 0 {
		return
	}
	sorted := make([]int64, len(vals))
	copy(sorted, vals)
	slices.Sort(sorted)

	p50 = sorted[percentileIndex(len(sorted), 50)]
	p95 = sorted[percentileIndex(len(sorted), 95)]
	return
}

// percentileIndex returns the slice index for the given percentile (0-100) using
// the nearest-rank method, clamped to valid bounds.
func percentileIndex(n, p int) int {
	if n == 0 {
		return 0
	}
	idx := (p*n)/100 - 1
	idx = max(idx, 0)
	if idx >= n {
		idx = n - 1
	}
	return idx
}
