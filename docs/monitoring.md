# Control plane monitoring

For the standalone and Cluster API in-cluster use cases, k0smotron provides
a mechanism to expose control plane metrics of a managed cluster to
a Prometheus instance running in the management cluster. This allows you to
monitor the control plane components of the managed cluster as a usual
Kubernetes workload using the same Prometheus instance that is used to monitor
the management cluster.

To enable monitoring for a k0smotron cluster, set `spec.monitoring.enabled=true`
in the `Cluster` resource:

```yaml
apiVersion: k0smotron.io/v1beta1
kind: Cluster
metadata:
  name: k0smotron-test
spec:
  monitoring:
    enabled: true
``` 

Once done, two sidecar containers are added to the control plane pods:

* `monitoring-agent` - a container that scrapes metrics from the control plane
  components.
* `monitoring-proxy` - a container with a proxy that exposes metrics scraped by
  `monitoring-agent` to the management cluster.

All metrics contain the `k0smotron_cluster` label with the name of the managed
cluster.
