# k0smotron Benchmark Infrastructure

Terraform + Makefile for a reproducible k0smotron benchmark environment on AWS EC2.
All resources land in a **single AZ** to eliminate cross-AZ latency variance.

## What gets provisioned

| Role                       | Instance     | Storage                                     | Count |
|----------------------------|--------------|---------------------------------------------|-------|
| k0s control-plane          | `c5.2xlarge` | gp3 20 GB root + io2 50 GB/3000 IOPS (etcd) | 3     |
| k0s worker (HCP pods)      | `m5.4xlarge` | gp3 50 GB root                              | 3     |
| Observer (runs benchmarks) | `t3.medium`  | gp3 20 GB root                              | 1     |
| PostgreSQL 16              | `r6i.xlarge` | gp3 root + io2 100 GB/5000 IOPS             | 1     |
| MySQL 8                    | `r6i.xlarge` | gp3 root + io2 100 GB/5000 IOPS             | 1     |

The observer node clones the k0smotron repo and runs `go test -tags bench` directly.
t4 storage uses S3 — no dedicated node required. Set `T4_BUCKET` to enable.
Bootstrap coordination uses SSM Parameter Store (`/k0smotron-bench/` prefix).

## Full run (one command)

```sh
make all KEY=my-ec2-key CIDR=$(curl -s ifconfig.me)/32
```

This runs: `provision` → `wait` → `monitoring` → `bench`.
`go test` writes `bench-results.csv` directly as each scenario completes.

## Step-by-step

```sh
# 1. Provision infrastructure (~5 min)
make provision KEY=my-ec2-key CIDR=$(curl -s ifconfig.me)/32

# 2. Wait for observer cloud-init to finish (~10 min, polls automatically)
make wait

# 3. Install metrics-server + Prometheus on the management cluster
make monitoring

# 4. Run the benchmark — CSV written by go test as each scenario finishes
make bench
#    → ~/bench-results.csv on the observer node

# 5. (Optional) Copy Prometheus TSDB snapshot for time-series analysis
make prom-snapshot
#    → ./prom-snapshot-<timestamp>.tar.gz locally

# 6. Tear down
make destroy
```

## Benchmark options

```sh
# Single storage backend
make bench BENCH_STORAGE=etcd

# Multiple backends
make bench BENCH_STORAGE=etcd,kine-postgres,kine-t4

# Add 500-cluster scenarios
make bench BENCH_LARGE=--bench.large

# Higher parallelism, specific k0s version
make bench BENCH_PARALLEL=20 BENCH_K0S_VER=v1.32.0-k0s.0 BENCH_TIMEOUT=8h
```

## Makefile variables

| Variable         | Default         | Description                                   |
|------------------|-----------------|-----------------------------------------------|
| `KEY`            | —               | EC2 key pair name (required for `provision`)  |
| `CIDR`           | `0.0.0.0/0`     | CIDR for SSH + kubectl access                 |
| `T4_BUCKET`      | —               | S3 bucket for t4 storage backend              |
| `SSH_KEY`        | `~/.ssh/id_rsa` | Local private key for SSH to observer         |
| `SSH_TIMEOUT`    | `600`           | Seconds to wait for observer bootstrap        |
| `BENCH_TIMEOUT`  | `4h`            | `go test` timeout                             |
| `BENCH_PARALLEL` | `10`            | Concurrent cluster creates                    |
| `BENCH_K0S_VER`  | `v1.31.2-k0s.0` | k0s version for HCPs                          |
| `BENCH_STORAGE`  | _(all)_         | Comma-separated storage backends              |
| `BENCH_LARGE`    | —               | Set to `--bench.large` to add 500-cluster run |

## Terraform variables

| Variable            | Default                  | Description                         |
|---------------------|--------------------------|-------------------------------------|
| `aws_region`        | `us-east-1`              | AWS region                          |
| `az`                | `us-east-1a`             | Single AZ — all resources land here |
| `key_name`          | —                        | EC2 key pair name                   |
| `allowed_cidr`      | `0.0.0.0/0`              | CIDR for SSH + kubectl access       |
| `k0s_version`       | `v1.31.2+k0s.0`          | k0s version for management cluster  |
| `postgres_password` | `bench_secret_change_me` | PostgreSQL bench user password      |
| `mysql_password`    | `bench_secret_change_me` | MySQL bench user password           |
| `s3_bucket_t4`      | —                        | S3 bucket for t4 storage backend    |

## Teardown

```sh
make destroy
```

All EBS volumes have `delete_on_termination = true` — no orphaned storage after destroy.
SSH to the observer to grab `~/bench-results.csv` before destroying if needed.
