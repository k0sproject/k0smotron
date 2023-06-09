# Configuration

## Configuration file reference#

The default k0smotron configuration file is a YAML file that contains the following values:

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

## `spec` Key Detail

| Element             | Description                                                                                                                                                                                                         |
|---------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `replicas`          | Replicas is the desired number of replicas of the k0s control planes. If unspecified, defaults to 1. If the value is above 1, k0smotron requires kine datasource URL to be set.                                     |
| `k0sImage`          | The k0s image to be deployed.                                                                                                                                                                                       |
| `K0sVersion`        | The k0s version to be deployed.                                                                                                                                                                                     |
| `externalAddress`   | ExternalAddress defines k0s external address. See [https://docs.k0sproject.io/stable/configuration/#specapi](https://docs.k0sproject.io/stable/configuration/#specapi)                                              |
| `service`           | `Service` defines the service configuration.                                                                                                                                                                        |
| `persistence`       | `Persistence` defines the persistence configuration.                                                                                                                                                                |
| `kineDataSourceURL` | Defines the kine datasource URL. Required for HA controlplane setup. Must be set if replicas > 1.                                                                                                                   |
| `k0sConfig`         | Defines k0sConfig defines the k0s configuration. Note, that some fields will be overwritten by k0smotron. If empty, will be used default configuration. More info: https://docs.k0sproject.io/stable/configuration/ |

### `spec.service`

| Element            | Description                                              |
|--------------------|----------------------------------------------------------|
| `type`             | Service type. Possible values: `NodePort`,`LoadBalancer` |
| `apiPort`          | Defines the kubernetes API port.                         |
| `konnectivityPort` | Defines the konnectivity port.                           |

### `spec.persistence`

| Element                 | Description                                                                                                |
|-------------------------|------------------------------------------------------------------------------------------------------------|
| `type`                  | Persistence type. Possible values: `emptyDir`,`hostPath`,`pvc`                                             |
| `hostPath`              | Defines the host path configuration. Will be used as is in case of `.spec.persistence.type` is `hostPath`. |
| `persistentVolumeClaim` | Defines the PVC configuration. Will be used as is in case of `.spec.persistence.type` is `pvc`.            |


## K0s configuration

K0smotron allows you to configure k0s via `spec.k0sConfig` field. If empty, will be used default configuration. 
More info about the k0s config: https://docs.k0sproject.io/stable/configuration/ 

**Note**: some fields will be overwritten by k0smotron. K0smotron will set the following fields:

- `spec.k0sConfig.spec.api.externalAddress` will be set to the value of `spec.externalAddress` if `spec.externalAddress` is set. 
   If not, k0smotron will use load balancer IP or try to detect `externalAddress` out of nodes IP addresses. 
- `spec.k0sConfig.spec.api.port` will be set to the value of `spec.service.apiPort`.
- `spec.k0sConfig.spec.konnectivity.port` will be set to the value of `spec.service.konnectivityPort`.
- `spec.k0sConfig.spec.storage.kine.dataSource` will be set to the value of `spec.kineDataSourceURL` if `spec.kineDataSourceURL` is set. 
  `spec.k0sConfig.spec.storage.type` will be set to `kine`.


