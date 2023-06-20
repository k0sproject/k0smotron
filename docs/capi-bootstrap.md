# Cluster API - Bootstrap provider

k0smotron can act as a Cluster API Boostrap provider. As k0smotron itself will run the cluster control plane within the management cluster the bootstrap provider is (currently) focusing only on bootstrapping worker nodes.

As with any other Cluster API provider you must create a `Machine` object with a reference to a bootstrap provider:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Machine
metadata:
  name: cp-test-0
spec:
  clusterName: cp-test
  bootstrap:
    configRef: # This triggers our controller to create cloud-init secret
      apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
      kind: K0sWorkerConfig
      name: cp-test-0
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: HCloudMachine
    name: cp-test-0

```

Next we need to provide the configuration for the bootstrapping:

```yaml
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: cp-test-0
spec:
  
```

As k0s comes with all the needed bells and whistles to get k8s worker node up, we do not need to specify any details in this simple example.

Check the [examples](capi-examples.md) pages for more detailed examples how k0smotron can be used with various Cluster API infrastructure providers.

For reference on what can be configured via `K0sWorkerConfig` see the [reference docs](resource-reference.md#bootstrapclusterx-k8siov1beta1).

## MachineDeployments, ..Sets etc.

To use k0smotron as Bootstrap provider for `MachineDeployment`s and other such multi-machine controller you can use `K0sWorkerConfigTemplate` type:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachineDeployment
metadata:
  name: docker-md-test
  namespace: default
spec:
  replicas: 2
  clusterName: docker-md-test
  selector:
    matchLabels:
      cluster.x-k8s.io/cluster-name: docker-md-test
      pool: worker-pool-1
  template:
    metadata:
      labels:
        cluster.x-k8s.io/cluster-name: docker-md-test
        pool: worker-pool-1
    spec:
      clusterName: docker-md-test
      bootstrap:
        configRef:
          apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
          kind: K0sWorkerConfigTemplate
          name: docker-md-test
      infrastructureRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
        kind: DockerMachineTemplate
        name: docker-md-test
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfigTemplate
metadata:
  name: docker-md-test
  namespace: default
spec:
  template:
    spec:
      version: v1.27.2+k0s.0
```

Naturally you must tie to abstract `MachineDeployment` to corresponding infrastructure providers Machine template type. In this example we use Docker as the infrastructure provider so we tie it up with `DockerMachineTemplate`:

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerMachineTemplate
metadata:
  name: docker-md-test
  namespace: default
spec:
  template:
    spec: {}
```
