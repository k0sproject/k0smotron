//go:build bench

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
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig    = flag.String("bench.kubeconfig", os.Getenv("KUBECONFIG"), "path to kubeconfig file")
	reportPath    = flag.String("bench.report", "bench-results.csv", "path to CSV report output file")
	k0sVersion    = flag.String("bench.k0s-version", "v1.35.3-k0s.0", "k0s version to deploy in each HCP")
	parallelism   = flag.Int("bench.parallel", 10, "concurrent cluster creates")
	storageFilter = flag.String("bench.storage", "", "comma-separated storage types to run, empty=all")
	largeCounts   = flag.Bool("bench.large", false, "include 500-cluster scenario (slow)")

	globalKC *kubernetes.Clientset
	globalRC *rest.Config
)

type resultRecorder func(RunResult) error

func TestMain(m *testing.M) {
	flag.Parse()

	if *kubeconfig == "" {
		fmt.Fprintln(os.Stderr, "bench: KUBECONFIG or -bench.kubeconfig is required")
		os.Exit(1)
	}

	var err error
	globalRC, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bench: failed to build rest config: %v\n", err)
		os.Exit(1)
	}

	// Bench hits the API server hard — raise QPS/burst well above the
	// client-go default (5/10) to avoid client-side throttling delays.
	globalRC.QPS = 200
	globalRC.Burst = 400

	globalKC, err = kubernetes.NewForConfig(globalRC)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bench: failed to build kubernetes client: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// TestScaleMatrix runs a subtest for each (storage, clusterCount) combination.
func TestScaleMatrix(t *testing.T) {
	if err := checkOperatorReady(context.Background(), globalKC); err != nil {
		t.Fatalf("k0smotron operator preflight: %v", err)
	}

	reporter, err := NewCSVReporter(*reportPath)
	if err != nil {
		t.Fatalf("failed to create CSV reporter: %v", err)
	}
	defer reporter.Close()

	counts := []int{10, 50, 100}
	if *largeCounts {
		counts = append(counts, 500)
	}

	enabledFilter := parseFilter(*storageFilter)

	configs := storageConfigs(*k0sVersion)

	for _, sc := range configs {
		sc := sc
		if len(enabledFilter) > 0 && !enabledFilter[sc.StorageName] {
			continue
		}
		if !sc.Enabled {
			t.Logf("skipping storage %q: required env var not set", sc.StorageName)
			continue
		}
		for _, n := range counts {
			n := n
			name := fmt.Sprintf("%s/n%d", sc.StorageName, n)
			t.Run(name, func(t *testing.T) {
				// Scenarios run sequentially on purpose — running them in
				// parallel overwhelms a single-node management cluster's API
				// server and skews measurements.

				cfg := ScenarioConfig{
					StorageName:  sc.StorageName,
					StorageType:  sc.StorageType,
					StorageKine:  sc.StorageKine,
					StorageEtcd:  sc.StorageEtcd,
					StorageNATS:  sc.StorageNATS,
					ClusterCount: n,
					Parallelism:  *parallelism,
					K0sVersion:   *k0sVersion,
					Namespace:    fmt.Sprintf("bench-%s-%d", sc.StorageName, n),
				}

				result, recorded, err := runScenario(t, cfg, reporter.Append)
				if err != nil {
					t.Errorf("scenario %s failed: %v", name, err)
					return
				}

				if recorded {
					return
				}
				if err := reporter.Append(result); err != nil {
					t.Logf("warning: failed to write result row: %v", err)
				}
			})
		}
	}
}

// runScenario executes the full benchmark scenario for one (storage, count) combination.
func runScenario(t *testing.T, cfg ScenarioConfig, record resultRecorder) (RunResult, bool, error) {
	t.Helper()
	ctx := context.Background()
	recorded := false

	result := RunResult{
		Timestamp:    time.Now().UTC(),
		StorageName:  cfg.StorageName,
		ClusterCount: cfg.ClusterCount,
		Parallelism:  cfg.Parallelism,
	}

	// 1. Create namespace (idempotent).
	t.Logf("creating namespace %q", cfg.Namespace)
	if err := ensureNamespace(ctx, globalKC, cfg.Namespace); err != nil {
		return result, recorded, fmt.Errorf("ensure namespace: %w", err)
	}
	defer func() {
		t.Logf("cleaning up namespace %q", cfg.Namespace)
		if err := globalKC.CoreV1().Namespaces().Delete(
			context.Background(), cfg.Namespace, metav1.DeleteOptions{},
		); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("warning: failed to delete namespace %q: %v", cfg.Namespace, err)
		}
	}()

	// 2 & 3 & 4. Parallel-create N clusters, record timings, wait for ready.
	t.Logf("creating %d clusters with parallelism %d", cfg.ClusterCount, cfg.Parallelism)
	timings, err := createClusters(ctx, globalKC, cfg)
	if err != nil {
		return result, recorded, fmt.Errorf("create clusters: %w", err)
	}

	provDurations := make([]time.Duration, 0, len(timings))
	for _, tm := range timings {
		provDurations = append(provDurations, tm.Duration)
	}
	result.ProvisionP50, result.ProvisionP95, result.ProvisionP99, result.ProvisionMax = percentiles(provDurations)
	t.Logf("provisioning p50=%s p95=%s p99=%s max=%s",
		result.ProvisionP50, result.ProvisionP95, result.ProvisionP99, result.ProvisionMax)

	// 5. Steady-state window.
	t.Log("entering 30s steady-state window")
	select {
	case <-ctx.Done():
		return result, recorded, ctx.Err()
	case <-time.After(30 * time.Second):
	}

	// 6. Collect metrics.
	mc, err := newMetricsClient(globalRC)
	if err != nil {
		t.Logf("warning: cannot build metrics client, skipping resource metrics: %v", err)
	} else {
		hcpSamples, err := collectHCPMetrics(ctx, mc, globalKC, cfg.Namespace)
		if err != nil {
			t.Logf("warning: HCP metrics unavailable: %v", err)
		} else {
			cpuVals := make([]int64, 0, len(hcpSamples))
			memVals := make([]int64, 0, len(hcpSamples))
			for _, s := range hcpSamples {
				cpuVals = append(cpuVals, s.CPUMillis)
				memVals = append(memVals, s.MemoryMiB)
				result.HCPTotalCPUm += s.CPUMillis
				result.HCPTotalMemMi += s.MemoryMiB
			}
			result.HCPP50CPUm, _ = int64Percentiles(cpuVals)
			result.HCPP50MemMi, result.HCPP95MemMi = int64Percentiles(memVals)
		}

		opSample, err := collectOperatorMetrics(ctx, mc)
		if err != nil {
			t.Logf("warning: operator metrics unavailable: %v", err)
		} else {
			result.OperatorCPUm = opSample.CPUMillis
			result.OperatorMemMi = opSample.MemoryMiB
		}
	}

	if record != nil {
		if err := record(result); err != nil {
			t.Logf("warning: failed to write result row before churn and cleanup: %v", err)
		} else {
			recorded = true
			t.Logf("wrote result row for %s/%d before churn and cleanup", cfg.StorageName, cfg.ClusterCount)
		}
	}

	// 7. Churn: delete 10% of clusters, recreate, measure recovery.
	churnCount := cfg.ClusterCount / 10
	if churnCount < 1 {
		churnCount = 1
	}
	t.Logf("churn: cycling %d clusters", churnCount)
	churnDurations, err := runChurn(ctx, t, cfg, timings, churnCount)
	if err != nil {
		t.Logf("warning: churn phase error: %v", err)
	} else {
		result.ChurnRecoveryP50, result.ChurnRecoveryP95, _, _ = percentiles(churnDurations)
		t.Logf("churn recovery p50=%s p95=%s", result.ChurnRecoveryP50, result.ChurnRecoveryP95)
	}

	// 8. Delete all clusters.
	t.Logf("deleting all clusters in namespace %q", cfg.Namespace)
	if err := deleteAllClusters(ctx, globalKC, cfg.Namespace); err != nil {
		t.Logf("warning: failed to delete all clusters: %v", err)
	}

	return result, recorded, nil
}

// runChurn deletes churnCount random clusters, recreates them, and returns recovery durations.
func runChurn(
	ctx context.Context,
	t *testing.T,
	cfg ScenarioConfig,
	timings []ClusterTiming,
	churnCount int,
) ([]time.Duration, error) {
	t.Helper()

	// Pick random victims.
	victims := make([]ClusterTiming, churnCount)
	indices := rand.Perm(len(timings))[:churnCount]
	for i, idx := range indices {
		victims[i] = timings[idx]
	}

	// Delete victims in parallel.
	delEg, delCtx := errgroup.WithContext(ctx)
	for _, v := range victims {
		v := v
		delEg.Go(func() error {
			return deleteCluster(delCtx, globalKC, v.Name, cfg.Namespace)
		})
	}
	if err := delEg.Wait(); err != nil {
		return nil, fmt.Errorf("churn delete: %w", err)
	}

	// Kubernetes DELETE is asynchronous. Wait until the Cluster CR names are
	// actually free before recreating, otherwise POST can race finalization and
	// fail with "object is being deleted".
	waitEg, waitCtx := errgroup.WithContext(ctx)
	for _, v := range victims {
		v := v
		waitEg.Go(func() error {
			deletedCtx, cancel := context.WithTimeout(waitCtx, 5*time.Minute)
			defer cancel()
			if err := waitClusterDeleted(deletedCtx, globalKC, v.Name, cfg.Namespace); err != nil {
				return fmt.Errorf("wait cluster %s deleted: %w", v.Name, err)
			}
			return nil
		})
	}
	if err := waitEg.Wait(); err != nil {
		return nil, fmt.Errorf("churn wait deleted: %w", err)
	}

	// Recreate victims in parallel and measure recovery.
	sem := make(chan struct{}, cfg.Parallelism)
	type churnResult struct {
		dur time.Duration
		err error
	}
	results := make([]churnResult, len(victims))

	recEg, recCtx := errgroup.WithContext(ctx)
	for i, v := range victims {
		i, v := i, v
		recEg.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()

			start := time.Now()
			if err := createCluster(recCtx, globalKC, v.Name, cfg.Namespace, cfg); err != nil {
				results[i] = churnResult{err: err}
				return nil // report but don't abort
			}

			waitCtx, cancel := context.WithTimeout(recCtx, 15*time.Minute)
			defer cancel()
			if err := waitClusterReady(waitCtx, globalKC, v.Name, cfg.Namespace); err != nil {
				results[i] = churnResult{err: err}
				return nil
			}
			results[i] = churnResult{dur: time.Since(start)}
			return nil
		})
	}
	if err := recEg.Wait(); err != nil {
		return nil, err
	}

	var durations []time.Duration
	for _, r := range results {
		if r.err != nil {
			t.Logf("churn recovery error for cluster: %v", r.err)
			continue
		}
		durations = append(durations, r.dur)
	}
	return durations, nil
}

// ensureNamespace creates the namespace if it doesn't already exist.
func ensureNamespace(ctx context.Context, kc *kubernetes.Clientset, name string) error {
	_, err := kc.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// parseFilter converts a comma-separated list into a set. Empty string means allow-all.
func parseFilter(s string) map[string]bool {
	if s == "" {
		return nil
	}
	m := make(map[string]bool)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			m[part] = true
		}
	}
	return m
}
