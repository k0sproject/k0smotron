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
      kind: KZerosWorkerConfig
      name: cp-test-0
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: HCloudMachine
    name: cp-test-0

```

Next we need to provide the configuration for the bootstrapping:

```yaml
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: KZerosWorkerConfig
metadata:
  name: cp-test-0
spec:
  
```

As k0s comes with all the needed bells and whistles to get k8s worker node up, we do not need to specify any details in this simple example.

Check the [examples](capi-examples.md) pages for more detailed examples how k0smotron can be used with various Cluster API infrastructure providers.
