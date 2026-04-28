# k0smotron HCP Benchmark Suite

Measures resource consumption and performance of k0smotron hosted control planes
at scale across different storage backends.

## What it measures

| Metric                                | Description                                                 |
|---------------------------------------|-------------------------------------------------------------|
| `provision_p50/p95/p99/max`           | Time from `Cluster` create to StatefulSet ready             |
| `hcp_p50/p95_mem_mib`                 | Per-pod memory at steady state                              |
| `hcp_total_mem_mib`                   | Total memory across all HCP pods                            |
| `hcp_p50_cpu_m` / `hcp_total_cpu_m`   | Per-pod and total CPU (millicores)                          |
| `operator_mem_mib` / `operator_cpu_m` | k0smotron controller resource usage                         |
| `churn_recovery_p50/p95`              | Time to ready after deleting and recreating 10% of clusters |

Storage backends tested: `etcd`, `kine-postgres`, `kine-mysql`, `kine-sqlite`, `nats-embedded`.

Default cluster counts per backend: **10 / 50 / 100** (add 500 with `-bench.large`).

---

## Prerequisites

On the management cluster:

- k0smotron operator installed (`k0smotron` namespace, `k0smotron-controller-manager` deployment)
- [metrics-server](https://github.com/kubernetes-sigs/metrics-server) installed
  (resource metrics are skipped with a warning if unavailable)
- Sufficient worker node capacity — each HCP is one StatefulSet pod; plan
  ~300 MiB RAM + 100m CPU per pod as a starting estimate

For reliable, reproducible numbers use dedicated EC2 instances — see
[`bench/infra/`](./infra/README.md) for the reference Terraform setup.

---

## Provision infrastructure (optional)

```sh
cd bench/infra

# Copy and edit the variables file
cp terraform.tfvars.example terraform.tfvars   # or pass -var on the CLI

terraform init
terraform apply \
  -var key_name=my-ec2-key \
  -var allowed_cidr=$(curl -s ifconfig.me)/32 

# After ~10 minutes the userdata scripts finish bootstrapping k0s.
# Retrieve the kubeconfig from the observer node:
ssh ubuntu@$(terraform output -raw observer_ip) \
  'cat ~/.kube/config' > /tmp/bench-kubeconfig

# Print connection URLs for the storage backends:
terraform output -raw bench_env
```

---

## Environment variables

| Variable             | Required for    | Example                                      |
|----------------------|-----------------|----------------------------------------------|
| `KUBECONFIG`         | all runs        | `/tmp/bench-kubeconfig`                      |
| `BENCH_POSTGRES_URL` | `kine-postgres` | `postgres://bench:pass@10.0.1.10:5432/bench` |
| `BENCH_MYSQL_URL`    | `kine-mysql`    | `mysql://bench:pass@tcp(10.0.1.11:3306)/bench` |

Backends whose env var is unset are skipped automatically.
`etcd`, `kine-sqlite`, and `nats-embedded` always run — no env var needed.

---

## Running the benchmarks

All commands require the `bench` build tag. Two independent test functions:

| Test                     | File            | Question answered                        |
|--------------------------|-----------------|------------------------------------------|
| `TestScaleMatrix`        | `bench_test.go` | How many HCPs fit? What does each cost?  |
| `TestStoragePerformance` | `perf_test.go`  | How fast is each backend under API load? |

Run them separately — each creates its own clusters and cleans up after itself.

---

## TestScaleMatrix

### Full matrix (all enabled backends, counts 10/50/100)

```sh
go test -tags bench -v -timeout 4h \
  -bench.kubeconfig=$KUBECONFIG \
  ./bench/
```

### Single storage backend

```sh
go test -tags bench -v -timeout 2h \
  -bench.kubeconfig=$KUBECONFIG \
  -bench.storage=etcd \
  ./bench/
```

### Multiple backends

```sh
go test -tags bench -v -timeout 4h \
  -bench.kubeconfig=$KUBECONFIG \
  -bench.storage=etcd,kine-postgres \
  ./bench/
```

### Include 500-cluster scenarios

```sh
go test -tags bench -v -timeout 8h \
  -bench.kubeconfig=$KUBECONFIG \
  -bench.large \
  ./bench/
```

### Tune parallelism and output path

```sh
go test -tags bench -v -timeout 4h \
  -bench.kubeconfig=$KUBECONFIG \
  -bench.parallel=20 \
  -bench.report=results-$(date +%Y%m%d).csv \
  -bench.k0s-version=v1.31.2-k0s.0 \
  ./bench/
```

### All flags

| Flag                 | Default             | Description                              |
|----------------------|---------------------|------------------------------------------|
| `-bench.kubeconfig`  | `$KUBECONFIG`       | Path to management cluster kubeconfig    |
| `-bench.report`      | `bench-results.csv` | CSV output path (appends if file exists) |
| `-bench.k0s-version` | `v1.31.2-k0s.0`     | k0s version deployed in each HCP         |
| `-bench.parallel`    | `10`                | Concurrent cluster creates per scenario  |
| `-bench.storage`     | _(all)_             | Comma-separated list of backends to run  |
| `-bench.large`       | `false`             | Add the 500-cluster scenario             |

---

## Output

### What you get

Two independent data sources, both needed for a complete picture:

| Source                | Data                                                        | Requires        |
|-----------------------|-------------------------------------------------------------|-----------------|
| CSV file              | Timing + point-in-time resource snapshots                   | always captured |
| Prometheus (optional) | Time-series: resource usage over time, per-pod, per-backend | separate setup  |

**Timing data** (always captured, self-contained):

- `provision_p50/p95/p99/max_s` — cluster creation latency
- `churn_recovery_p50/p95_s` — recovery latency after delete+recreate

**Resource snapshot data** (captured once during the 30 s steady-state window via `metrics.k8s.io`):

- `hcp_p50/p95_mem_mib`, `hcp_total_mem_mib` — memory across HCP pods
- `hcp_p50/total_cpu_m` — CPU across HCP pods
- `operator_mem_mib`, `operator_cpu_m` — k0smotron controller

Resource columns are zero if [metrics-server](https://github.com/kubernetes-sigs/metrics-server)
is not installed; the benchmark still runs and timing data is written.

The snapshot captures state *after* steady state, not during ramp-up.
To see resource usage over time (e.g. memory growth as N scales from 10 → 500,
CPU spikes during parallel creation), deploy Prometheus before running — see
[Optional: Prometheus time-series](#optional-prometheus-time-series) below.

### CSV columns

Results are appended to the CSV file after each scenario completes.

```
timestamp, storage_name, cluster_count, parallelism,
provision_p50_s, provision_p95_s, provision_p99_s, provision_max_s,
hcp_p50_mem_mib, hcp_p95_mem_mib, hcp_total_mem_mib,
hcp_p50_cpu_m, hcp_total_cpu_m,
operator_mem_mib, operator_cpu_m,
churn_recovery_p50_s, churn_recovery_p95_s
```

Duration columns are decimal seconds.

---

## Results

`go test` writes results to the CSV as each scenario completes — no separate
retrieval step needed. The only post-run step is an optional Prometheus snapshot
for time-series data (resource usage over time, not just the 30 s steady-state
snapshot):

```sh
# from bench/infra/ after make bench
make prom-snapshot   # → ./prom-snapshot-<timestamp>.tar.gz
```

See [`bench/infra/`](./infra/README.md) for the full workflow.

---

## Prometheus time-series

The Prometheus snapshot captures the full time range of the benchmark run,
not just the 30 s steady-state window. Key queries:

```promql
# Total memory across all HCP pods over time
sum(container_memory_working_set_bytes{namespace=~"bench-.*", container!=""}) by (namespace)

# k0smotron operator CPU
rate(container_cpu_usage_seconds_total{
  namespace="k0smotron", pod=~"k0smotron-controller-manager-.*"
}[1m])

# HCP pod count over time (confirm N clusters are running)
count(kube_pod_info{namespace=~"bench-.*"})
```

To query the snapshot locally:

```sh
tar -xzf results/<timestamp>/prom-snapshot.tar.gz -C /tmp/prom-restore
# start a local Prometheus pointed at the snapshot dir, or use promtool:
promtool query instant http://localhost:9090 \
  'sum(container_memory_working_set_bytes{namespace=~"bench-.*"})'
```

Prometheus is installed by `make monitoring` with `--enable-admin-api=true`
(required for the snapshot endpoint) and 14-day retention.

---

## TestStoragePerformance

Creates one HCP per enabled backend, drives ConfigMap write (create) and read
(list) load against each HCP's API server, and records latency + throughput.

### Run all backends

```sh
go test -tags bench -v -timeout 2h \
  -bench.kubeconfig=$KUBECONFIG \
  -run TestStoragePerformance \
  ./bench/
```

### Single backend

```sh
go test -tags bench -v -timeout 30m \
  -bench.kubeconfig=$KUBECONFIG \
  -run TestStoragePerformance \
  -bench.storage=kine-postgres \
  ./bench/
```

### All flags

| Flag                      | Default                  | Description                                     |
|---------------------------|--------------------------|-------------------------------------------------|
| `-bench.perf-ops`         | `500`                    | Operations per phase (write + read) per backend |
| `-bench.perf-concurrency` | `10`                     | Parallel workers                                |
| `-bench.perf-warmup`      | `50`                     | Warmup ops discarded from measurements          |
| `-bench.perf-report`      | `bench-perf-results.csv` | CSV output path                                 |

The `-bench.kubeconfig`, `-bench.storage`, and `-bench.k0s-version` flags
from `TestScaleMatrix` also apply here.

### Output columns

```
timestamp, storage_name, concurrency, ops,
write_p50_s, write_p95_s, write_p99_s, write_throughput_ops,
read_p50_s,  read_p95_s,  read_p99_s,  read_throughput_ops
```

Write = ConfigMap create latency → exercises the backend's **write path**.
Read = ConfigMap list latency → exercises the backend's **read path**.

---

## Cleanup

The benchmark deletes all created `Cluster` resources and their namespaces
at the end of each scenario. If a run is interrupted, clean up manually:

```sh
kubectl get ns -o name | grep '^namespace/bench-' | xargs kubectl delete
```

Teardown the infrastructure (automatically retrieves first):

```sh
cd bench/infra && make destroy
```
