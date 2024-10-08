# Configuration

K0smotron managed control planes are managed using custom resource objects.

```yaml
apiVersion: k0smotron.io/v1beta1
kind: Cluster
metadata:
  name: k0smotron-test
spec:
  replicas: 1
  image: k0sproject/k0s
  version: v1.27.1-k0s.0
  service:
    type: NodePort
    apiPort: 30443
    konnectivityPort: 30132
  persistence:
    type: emptyDir
```

For full reference of the fields check out the [reference docs](resource-reference.md#cluster).

## Persistence

K0smotron persists data related to each Cluster. Specifically, it persists the `/var/lib/k0s` directory of the k0s controller which is the default data directory used by k0s. 

The `/var/lib/k0s` directory contains essential data for the operation of the k0s controller, but its growth over time is primarily driven by the addition of small [manifest](https://docs.k0sproject.io/stable/manifests/) files. Since these manifests are lightweight and in text format, the directory tends to grow gradually and not excessively. Typically, 250 MB of space is sufficient to handle its growth, as the main additions are these small manifests, keeping the overall size manageable.

The type of persistence used for this can be configurable via `spec.persistence`. For more information, check out the [reference docs](resource-reference.md/#clusterspecpersistence) on Cluster persistence.

## K0s configuration

K0smotron allows you to configure k0s via `spec.k0sConfig` field. If empty, the default configuration will be used.

Refer to [k0s docs](https://docs.k0sproject.io/stable/configuration/) for a reference on k0s configuration.

**Note**: Some fields will be overwritten by k0smotron. K0smotron will set the following fields:

- `spec.k0sConfig.spec.api.externalAddress` will be set to the value of `spec.externalAddress` if `spec.externalAddress` is set. 
   If not, k0smotron will use load balancer IP or try to detect `externalAddress` out of nodes IP addresses. 
- `spec.k0sConfig.spec.api.port` will be set to the value of `spec.service.apiPort`.
- `spec.k0sConfig.spec.konnectivity.port` will be set to the value of `spec.service.konnectivityPort`.
- `spec.k0sConfig.spec.storage.kine.dataSource` will be set to the value of `spec.kineDataSourceURL` if `spec.kineDataSourceURL` is set. 
  `spec.k0sConfig.spec.storage.type` will be set to `kine`.


