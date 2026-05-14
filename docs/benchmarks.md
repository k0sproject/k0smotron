# Benchmarks

We ran a benchmark comparing storage backends for hosted control planes (HCPs) to answer:

- How much CPU and memory does it take to run 10, 50, and 100 HCPs?
- How do different storage backends (etcd, kine on PostgreSQL/MySQL/SQLite/T4, embedded NATS) perform under realistic Kubernetes API load?
- Is the k0smotron operator itself a bottleneck, and how many resources does it use?

The full benchmark report is available on [our blog](https://blog.k0sproject.io/posts/k0smotron-big-bang-benchmark/).

Key takeaways:

- **Operator overhead**: the k0smotron operator stays under 0.5 CPU cores and ~100 MiB RAM even at 100 HCPs. The cost is dominated by HCP pods and storage backends, not the operator.
- **etcd**: safe default. Zero write errors and complete watch delivery across the tested range, predictable and well understood.
- **PostgreSQL**: handles create/list cleanly after raising connection limits, but watch-churn exposes write errors (9–15%) and database tuning sensitivity. Connection count was the only DB tuning applied.
- **MySQL**: strongest SQL write throughput on create/list, but watch-churn write error rate climbs to 12–45% and watch delivery drops to 60–89%.
- **SQLite**: suitable only as a dependency-minimizing option for very small or low-load setups. Write error rate reaches 32–89% under concurrent writes.
- **Embedded NATS**: accepts create/list load but watch delivery falls far behind under churn (p99 watch lag ~50s+), causing controllers to lag.
- **Scalability**: resource usage scales roughly linearly with HCP count. At 100 HCPs the per-cluster envelope is roughly 0.4–1.3 CPU cores and 210–560 MiB of RAM, depending on backend.
- **HA (3 replicas)**: changes cost but not the ranking. etcd and both T4 modes still deliver the full watch stream; weaker backends do not become reliable by adding replicas.
