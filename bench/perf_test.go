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
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	perfOps         = flag.Int("bench.perf-ops", 500, "configmap operations per phase (write + read) per backend")
	perfConcurrency = flag.Int("bench.perf-concurrency", 10, "concurrent workers for load phases")
	perfWarmup      = flag.Int("bench.perf-warmup", 50, "warmup operations discarded from measurements")
	perfReportPath  = flag.String("bench.perf-report", "bench-perf-results.csv", "CSV output for storage performance results")
)

// TestStoragePerformance creates one HCP per enabled storage backend,
// drives write (ConfigMap create) and read (ConfigMap list) load against
// each HCP's API server, and records latency percentiles + throughput.
//
// This test answers: how does each storage backend perform under K8s API load?
// Run it independently from TestScaleMatrix.
func TestStoragePerformance(t *testing.T) {
	reporter, err := NewPerfCSVReporter(*perfReportPath)
	if err != nil {
		t.Fatalf("create perf reporter: %v", err)
	}
	defer reporter.Close()

	filter := parseFilter(*storageFilter)

	for _, sc := range storageConfigs(*k0sVersion) {
		sc := sc
		if len(filter) > 0 && !filter[sc.StorageName] {
			continue
		}
		if !sc.Enabled {
			t.Logf("skipping %q: required env var not set", sc.StorageName)
			continue
		}

		t.Run(sc.StorageName, func(t *testing.T) {
			// Sequential — each backend gets a clean run without interference.
			ctx := context.Background()

			safeName := strings.ReplaceAll(sc.StorageName, "_", "-")
			clusterName := "perf-" + safeName
			mgmtNS := "bench-perf-" + safeName // namespace in the management cluster

			nodeAddrs, err := hcpNodeAddresses(ctx, globalKC)
			if err != nil {
				t.Fatalf("pick node address: %v", err)
			}
			nodeAddr := nodeAddrs[0]
			t.Logf("[%s] using externalAddress %s:%d for HCP apiserver", sc.StorageName, nodeAddr, hcpAPINodePort)

			if err := ensureNamespace(ctx, globalKC, mgmtNS); err != nil {
				t.Fatalf("ensure namespace: %v", err)
			}
			defer func() {
				_ = globalKC.CoreV1().Namespaces().Delete(
					context.Background(), mgmtNS, metav1.DeleteOptions{})
			}()

			cfg := ScenarioConfig{
				StorageName: sc.StorageName,
				StorageType: sc.StorageType,
				StorageKine: sc.StorageKine,
				StorageEtcd: sc.StorageEtcd,
				StorageNATS: sc.StorageNATS,
				// NodePort: perf load bypasses port-forward (single-SPDY
				// bottleneck) and hits the HCP apiserver directly on a
				// worker node IP.
				ServiceType:     corev1.ServiceTypeNodePort,
				ExternalAddress: nodeAddr,
				APISANs:         nodeAddrs,
				K0sVersion:      *k0sVersion,
				Namespace:       mgmtNS,
			}

			t.Logf("[%s] creating HCP (NodePort)", sc.StorageName)
			if err := createCluster(ctx, globalKC, clusterName, mgmtNS, cfg); err != nil {
				t.Fatalf("create cluster: %v", err)
			}
			defer func() {
				_ = deleteCluster(context.Background(), globalKC, clusterName, mgmtNS)
			}()

			readyCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			if err := waitClusterReady(readyCtx, globalKC, clusterName, mgmtNS); err != nil {
				t.Fatalf("wait ready: %v", err)
			}
			t.Logf("[%s] HCP ready", sc.StorageName)

			t.Logf("[%s] using node %s:%d for HCP apiserver", sc.StorageName, nodeAddr, hcpAPINodePort)

			hcpKC, err := buildHCPClient(ctx, globalKC, clusterName, mgmtNS, nodeAddr)
			if err != nil {
				t.Fatalf("build HCP client: %v", err)
			}
			if err := waitHCPReachable(ctx, hcpKC); err != nil {
				t.Fatalf("HCP unreachable: %v", err)
			}

			const loadNS = "load-test"

			// ── Write load ────────────────────────────────────────────────────
			t.Logf("[%s] write load: %d ops, concurrency %d, warmup %d",
				sc.StorageName, *perfOps, *perfConcurrency, *perfWarmup)
			writeStart := time.Now()
			writeDurs, err := runWriteLoad(ctx, hcpKC, loadNS, *perfOps, *perfConcurrency, *perfWarmup)
			writeElapsed := time.Since(writeStart)
			if err != nil {
				t.Logf("[%s] write load error: %v", sc.StorageName, err)
			}

			// ── Read load ─────────────────────────────────────────────────────
			t.Logf("[%s] read load: %d ops, concurrency %d, warmup %d",
				sc.StorageName, *perfOps, *perfConcurrency, *perfWarmup)
			readStart := time.Now()
			readDurs, err := runReadLoad(ctx, hcpKC, loadNS, *perfOps, *perfConcurrency, *perfWarmup)
			readElapsed := time.Since(readStart)
			if err != nil {
				t.Logf("[%s] read load error: %v", sc.StorageName, err)
			}

			wp50, wp95, wp99, _ := percentiles(writeDurs)
			rp50, rp95, rp99, _ := percentiles(readDurs)

			result := PerfResult{
				Timestamp:       time.Now().UTC(),
				StorageName:     sc.StorageName,
				Concurrency:     *perfConcurrency,
				Ops:             *perfOps,
				WriteP50:        wp50,
				WriteP95:        wp95,
				WriteP99:        wp99,
				WriteThroughput: opsThroughput(len(writeDurs), writeElapsed),
				ReadP50:         rp50,
				ReadP95:         rp95,
				ReadP99:         rp99,
				ReadThroughput:  opsThroughput(len(readDurs), readElapsed),
			}

			t.Logf("[%s] write p50=%s p95=%s p99=%s  %.1f ops/s",
				sc.StorageName, wp50, wp95, wp99, result.WriteThroughput)
			t.Logf("[%s] read  p50=%s p95=%s p99=%s  %.1f ops/s",
				sc.StorageName, rp50, rp95, rp99, result.ReadThroughput)

			if err := reporter.Append(result); err != nil {
				t.Logf("warning: write perf result: %v", err)
			}
		})
	}
}
