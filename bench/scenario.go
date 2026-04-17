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
	"os"
	"strings"
	"time"

	km "github.com/k0sproject/k0smotron/api/k0smotron.io/v1beta2"
	corev1 "k8s.io/api/core/v1"
)

// ScenarioConfig holds the parameters for a single benchmark scenario.
type ScenarioConfig struct {
	StorageName string
	// Storage sub-fields (mirror km.StorageSpec components for JSON serialisation).
	StorageType km.StorageType
	StorageKine km.KineSpec
	StorageEtcd km.EtcdSpec
	StorageNATS km.NATSSpec

	// ServiceType selects how the HCP apiserver is exposed. Empty = ClusterIP.
	// Perf tests set NodePort so the load generator can hit the apiserver
	// directly via a worker node IP — port-forward serializes all traffic
	// through a single SPDY stream and caps throughput.
	ServiceType corev1.ServiceType
	// ExternalAddress is written to spec.externalAddress for NodePort/LoadBalancer
	// HCPs so generated child-cluster kubeconfigs point at a reachable endpoint.
	ExternalAddress string
	// APISANs are merged into k0sConfig.spec.api.sans for the child API server
	// certificate. Perf NodePort tests include all worker external addresses.
	APISANs []string

	ClusterCount int
	Parallelism  int
	K0sVersion   string
	Namespace    string
}

// RunResult holds all measured outcomes from one scenario run.
type RunResult struct {
	Timestamp    time.Time
	StorageName  string
	ClusterCount int
	Parallelism  int

	// Provisioning latency percentiles across all clusters in the scenario.
	ProvisionP50 time.Duration
	ProvisionP95 time.Duration
	ProvisionP99 time.Duration
	ProvisionMax time.Duration

	// Steady-state resource usage aggregated across all HCP pods.
	HCPP50MemMi   int64 // per-pod median memory in MiB
	HCPP95MemMi   int64 // per-pod p95 memory in MiB
	HCPTotalMemMi int64 // sum across all HCP pods
	HCPP50CPUm    int64 // per-pod median CPU in millicores
	HCPTotalCPUm  int64 // sum across all HCP pods

	// Operator resource usage at steady state.
	OperatorMemMi int64
	OperatorCPUm  int64

	// Churn recovery latency percentiles.
	ChurnRecoveryP50 time.Duration
	ChurnRecoveryP95 time.Duration
}

// PerfResult holds API latency and throughput measurements for one storage backend.
// Written by TestStoragePerformance to bench-perf-results.csv.
type PerfResult struct {
	Timestamp   time.Time
	StorageName string
	Concurrency int
	Ops         int

	// ConfigMap create latency (write path → storage backend write)
	WriteP50        time.Duration
	WriteP95        time.Duration
	WriteP99        time.Duration
	WriteThroughput float64 // ops/sec

	// ConfigMap list latency (read path → storage backend read)
	ReadP50        time.Duration
	ReadP95        time.Duration
	ReadP99        time.Duration
	ReadThroughput float64 // ops/sec
}

// storageEntry bundles a ScenarioConfig partial with an Enabled flag.
type storageEntry struct {
	StorageName string
	Enabled     bool
	StorageType km.StorageType
	StorageKine km.KineSpec
	StorageEtcd km.EtcdSpec
	StorageNATS km.NATSSpec
}

// storageConfigs returns the full list of storage configurations.
// Each entry is enabled/disabled by the presence of the required environment variable.
func storageConfigs(k0sVersion string) []storageEntry {
	_ = k0sVersion // reserved for future version-specific config adjustments

	postgresURL := os.Getenv("BENCH_POSTGRES_URL")
	mysqlURL := normalizeMySQLKineURL(os.Getenv("BENCH_MYSQL_URL"))

	return []storageEntry{
		{
			StorageName: "etcd",
			Enabled:     true,
			StorageType: km.StorageTypeEtcd,
			StorageEtcd: km.EtcdSpec{
				Image: km.DefaultEtcdImage,
			},
		},
		{
			StorageName: "kine-postgres",
			Enabled:     postgresURL != "",
			StorageType: km.StorageTypeKine,
			StorageKine: km.KineSpec{
				DataSourceURL: postgresURL,
			},
		},
		{
			StorageName: "kine-mysql",
			Enabled:     mysqlURL != "",
			StorageType: km.StorageTypeKine,
			StorageKine: km.KineSpec{
				DataSourceURL: mysqlURL,
			},
		},
		{
			StorageName: "kine-sqlite",
			Enabled:     true,
			StorageType: km.StorageTypeKine,
			StorageKine: km.KineSpec{
				DataSourceURL: "sqlite:///data/kine.db",
			},
		},
		{
			StorageName: "nats-embedded",
			Enabled:     true,
			StorageType: km.StorageTypeNATS,
		},
	}
}

func normalizeMySQLKineURL(raw string) string {
	const prefix = "mysql://"
	if !strings.HasPrefix(raw, prefix) {
		return raw
	}

	rest := strings.TrimPrefix(raw, prefix)
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return raw
	}

	addrAndSuffix := rest[at+1:]
	if strings.HasPrefix(addrAndSuffix, "tcp(") {
		return raw
	}

	addrEnd := len(addrAndSuffix)
	for _, sep := range []string{"/", "?"} {
		if idx := strings.Index(addrAndSuffix, sep); idx >= 0 && idx < addrEnd {
			addrEnd = idx
		}
	}
	if addrEnd == 0 {
		return raw
	}

	return prefix + rest[:at+1] + "tcp(" + addrAndSuffix[:addrEnd] + ")" + addrAndSuffix[addrEnd:]
}
