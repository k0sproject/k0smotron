# Cluster API - Bootstrap provider

k0smotron serves as a Cluster API Bootstrap provider. Given that k0smotron runs the cluster control plane within the management cluster, the Bootstrap provider currently concentrates on worker node bootstrapping.

Just like with any other Cluster API provider, you have the flexibility to create either a `Machine` or `MachineDeployment` object. While `MachineDeployment` objects are scalable, certain use-cases necessitate the use of `Machine`.

## Machines

To configure the machine, you first need to create a `Machine` object with a reference to a bootstrap provider and configuration for the bootstrapping `K0sWorkerConfig`:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Machine
metadata:
  name: machine-test-0
  namespace: default
spec:
  clusterName: cp-test
  bootstrap:
    configRef: # This triggers our controller to create cloud-init secret
      apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
      kind: K0sWorkerConfig
      name: machine-test-config
  infrastructureRef: # This references the infrastructure provider machine object
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: AWSMachine
    name: machine-test-0
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: machine-test-config
  namespace: default
spec:
  version: v1.27.2+k0s.0
  # Details of the worker configuration can be set here
```

This configuration sets up a `Machine` object that will trigger the k0smotron controller to create a cloud-init secret and prepare the machine for bootstrapping. Note that the specific parameters in the `K0sWorkerConfig` spec will depend on your worker node configuration requirements.

For reference on what can be configured via `K0sWorkerConfig` see the [reference docs](resource-reference/bootstrap.cluster.x-k8s.io-v1beta1.md).

The `infrastructureRef` in the `Machine` object specifies a reference to the provider-specific infrastructure required for the operation of the machine. In the above example, the kind `AWSMachine` indicates that the machine will be run on AWS. The parameters within `infrastructureRef` will be provider-specific and vary based on your chosen infrastructure.

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSMachine
metadata:
  name: machine-test-0
  namespace: default
spec:
  # More details about the aws machine can be set here
```

## MachineDeployments

To leverage k0smotron as a Bootstrap provider for `MachineDeployment` utilize the `K0sWorkerConfigTemplate` type:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachineDeployment
metadata:
  name: md-test
  namespace: default
spec:
  replicas: 2
  clusterName: cp-test
  selector:
    matchLabels:
      cluster.x-k8s.io/cluster-name: cp-test
      pool: worker-pool-1
  template:
    metadata:
      labels:
        cluster.x-k8s.io/cluster-name: cp-test
        pool: worker-pool-1
    spec:
      clusterName: cp-test
      bootstrap:
        configRef:
          apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
          kind: K0sWorkerConfigTemplate
          name: md-test-config
      infrastructureRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
        kind: AWSMachineTemplate
        name: mt-test
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfigTemplate
metadata:
  name: md-test-config
  namespace: default
spec:
  template:
    spec:
      version: v1.27.2+k0s.0
      # More details of the worker configuration can be set here
```

The `MachineDeployment` configuration must be associated with the appropriate infrastructure provider's machine template type. In this example, AWS is used as the infrastructure provider, hence a `AWSMachineTemplate` is utilized:

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSMachineTemplate
metadata:
  name: mt-test
  namespace: default
spec:
  template:
    spec:
    # More details about the aws machine template can be set here
```

This example creates a `MachineDeployment` with 2 replicas, using k0smotron as the bootstrap provider. The `infrastructureRef` is used to specify the infrastructure requirements for the machines, in this case, AWS. 

Check the [examples](capi-examples.md) pages for more detailed examples how k0smotron can be used with various Cluster API infrastructure providers.