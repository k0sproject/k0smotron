# Configuration

K0smotron managed control planes are managed using custom resource objects.

```yaml
apiVersion: k0smotron.io/v1beta2
kind: Cluster
metadata:
  name: k0smotron-test
spec:
  replicas: 1
  image: quay.io/k0sproject/k0s
  version: v1.27.1-k0s.0
  service:
    type: NodePort
    apiPort: 30443
    konnectivityPort: 30132
```

For full reference of the fields check out the [reference docs](resource-reference/k0smotron.io-v1beta2.md#cluster).

## Persistence

Control plane pods are stateless. The `/var/lib/k0s` data directory of the k0s controller is rebuilt on every container start and nothing in it has to outlive the pod:

- Cluster data lives in the storage backend, which is persisted independently of the control plane pod: the etcd StatefulSet's own volume, an external kine datasource, or the NATS JetStream volume.
- Certificates are stored in Secrets and copied into the data directory on start.
- Control plane binaries ship in the k0s image.
- [Manifests](https://docs.k0sproject.io/stable/manifests/) are mounted from ConfigMaps and Secrets via `spec.manifests`.

!!! warning "Deprecated"

    `spec.persistence` is deprecated and will be removed in a future API version. Setting it still mounts a volume over the whole `/var/lib/k0s` directory, for clusters that rely on files written into it out of band, but there is no longer a reason to configure it. To ship extra manifests to the control plane, use `spec.manifests` rather than writing files into the data directory.

## K0s configuration

K0smotron allows you to configure k0s via `spec.k0sConfig` field. This field expects a k0s **ClusterConfig** resource as value, which defines the configuration parameters for k0s. If `spec.k0sConfig` is left empty, the default k0s configuration will be applied.

Refer to [k0s docs](https://docs.k0sproject.io/stable/configuration/) for a reference on configuring k0s via the ClusterConfig resource.

### ClusterConfig for K0smotron

K0smotron can automatically generate `spec.k0sConfig` or override some fields (if provided) based on the values provided for the [Cluster](resource-reference.md/#clusterspec) resource, following specific configuration rules:

<table>
  <tr>
    <th style="width: 30%;">ClusterConfig Field</th>
    <th style="width: 30%;">Value</th>
    <th>Condition</th>
  </tr>
  <tr>
    <td><code>api.externalAddress</code></td>
    <td>Value in <code>spec.externalAddress</code> if provided. Otherwise, depending of the service type, K0smotron attempts to detect an external address from the load balancer or available node IPs.</td>
    <td>Only set if <code>nodeLocalLoadBalancing.enabled</code> is <code>false</code>.</td>
  </tr>
  <tr>
    <td><code>api.port</code></td>
    <td>Value in <code>spec.service.apiPort</code></td>
    <td>Always.</td>
  </tr>
  <tr>
    <td><code>api.sans</code></td>
    <td><code>[&lt;spec.externalAddress&gt;, &lt;cluster-svc-ips&gt;, &lt;cluster-service-name&gt;, &lt;cluster-service-name-namespaced&gt;, &lt;cluster-service-name-DNS&gt;], &lt;cluster-service-name-FQDNS&gt;</code> plus the possible provided ones.</td>
    <td>Always.</td>
  </tr>
  <tr>
    <td><code>konnectivity.port</code></td>
    <td>Value in <code>spec.service.konnectivityPort</code></td>
    <td>Always.</td>
  </tr>
  <tr>
    <td><code>storage.kine.dataSource</code></td>
    <td>Value in <code>spec.storage.kine.dataSourceURL</code></td>
    <td>Only set if <code>spec.storage.kine.dataSourceURL</code> is defined.</td>
  </tr>
  <tr>
    <td><code>storage.type</code></td>
    <td><code>kine</code></td>
    <td>Only set if <code>spec.storage.kine.dataSourceURL</code> is defined.</td>
  </tr>
  <tr>
    <td><code>storage.type</code></td>
    <td><code>etcd</code></td>
    <td>Only set if <code>spec.storage.kine.dataSourceURL</code> is not defined.</td>
  </tr>
  <tr>
    <td><code>storage.etcd.externalCluster.endpoints</code></td>
    <td><code>[https://kmc-&lt;cluster.name&gt;-etcd:2379]</code></td>
    <td>Only set if <code>spec.storage.kine.dataSourceURL</code> is not defined.</td>
  </tr>
  <tr>
    <td><code>storage.etcd.externalCluster.etcdPrefix</code></td>
    <td>Value in <code>metadata.name</code></td>
    <td>Only set if <code>spec.storage.kine.dataSourceURL</code> is not defined.</td>
  </tr>
  <tr>
    <td><code>storage.etcd.externalCluster.caFile</code></td>
    <td><code>/var/lib/k0s/pki/etcd-ca.crt</code></td>
    <td>Only set if <code>spec.storage.kine.dataSourceURL</code> is not defined.</td>
  </tr>
  <tr>
    <td><code>storage.etcd.externalCluster.clientCertFile</code></td>
    <td><code>/var/lib/k0s/pki/apiserver-etcd-client.crt</code></td>
    <td>Only set if <code>spec.storage.kine.dataSourceURL</code> is not defined.</td>
  </tr>
  <tr>
    <td><code>storage.etcd.externalCluster.clientKeyFile</code></td>
    <td><code>/var/lib/k0s/pki/apiserver-etcd-client.key</code></td>
    <td>Only set if <code>spec.storage.kine.dataSourceURL</code> is not defined.</td>
  </tr>
</table>
