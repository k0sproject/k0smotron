# Configuration

K0smotron managed control planes are managed using custom resource objects.

```yaml
apiVersion: k0smotron.io/v1beta1
kind: Cluster
metadata:
  name: k0smotron-test
spec:
  replicas: 1
  k0sImage: k0sproject/k0s
  k0sVersion: v1.27.1-k0s.0
  service:
    type: NodePort
    apiPort: 30443
    konnectivityPort: 30132
  persistence:
    type: emptyDir
```

For full reference of the fields check out the [reference docs](resource-reference.md#cluster).

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


