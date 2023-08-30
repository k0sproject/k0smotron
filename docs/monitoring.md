# Control Plane Monitoring

K0smotron provides a mechanism to expose child cluster control plane metrics to a Prometheus instance running in the parent cluster. 
This allows you to monitor the control plane components of the child cluster as the usual Kubernetes workload, using the same Prometheus instance that is used to monitor the parent cluster.

To enable the monitoring for the k0smotron cluster you need to set `spec.enableMonitoring=true` in the `Cluster` resource:

```yaml
apiVersion: k0smotron.io/v1beta1
kind: Cluster
metadata:
  name: k0smotron-test
spec:
  enableMonitoring: true
``` 

This will add two sidecar containers to the control plane pods:
- `monitoring-agent` - a container that will scrape the metrics from the control plane components.
- `monitoring-proxy` - a container with a proxy that will expose the metrics scraped by the `monitoring-agent` to the parent cluster.

All metrics contain the `k0smotron_cluster` label with the name of the cluster.
