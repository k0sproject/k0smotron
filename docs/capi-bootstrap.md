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
  version: v1.34.3+k0s.0
  # Details of the worker configuration can be set here
```

This configuration sets up a `Machine` object that will trigger the k0smotron controller to create a cloud-init secret and prepare the machine for bootstrapping. Note that the specific parameters in the `K0sWorkerConfig` spec will depend on your worker node configuration requirements.

For reference on what can be configured via `K0sWorkerConfig` see the [reference docs](resource-reference/bootstrap.cluster.x-k8s.io-v1beta1.md).

## Pre/Post Start Commands

k0smotron supports executing custom commands before and after starting k0s on worker and controller nodes. This feature is useful for:

- Installing additional packages or dependencies
- Configuring system settings
- Setting up monitoring agents
- Running health checks
- Performing cleanup operations

### PreStartCommands

Commands specified in `preStartCommands` are executed before k0s binary is downloaded and installed.

```yaml
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: worker-config
spec:
  version: v1.34.3+k0s.0
  preStartCommands:
    - "apt-get update && apt-get install -y curl jq"
    - "mkdir -p /etc/k0s/monitoring"
    - "echo 'export MONITORING_ENABLED=true' >> /etc/environment"
```

### PostStartCommands

Commands specified in `postStartCommands` are executed after k0s has started successfully. These commands run after the k0s service is running and the node is ready.

```yaml
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: worker-config
spec:
  version: v1.34.3+k0s.0
  postStartCommands:
    - "systemctl enable monitoring-agent"
    - "systemctl start monitoring-agent"
    - "kubectl get nodes --kubeconfig=/var/lib/k0s/pki/admin.conf"
```

### Command Execution Order

The commands are executed in the following order:

1. **PreStartCommands** - Custom commands before k0s starts
2. **Download and Install** - k0s binary download and installation
3. **k0s start** - k0s service startup
4. **PostStartCommands** - Custom commands after k0s starts

### Use Cases

#### Installing Monitoring Agents

```yaml
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: worker-with-monitoring
spec:
  version: v1.34.3+k0s.0
  preStartCommands:
    - "curl -fsSL https://get.docker.com | sh"
    - "systemctl enable docker"
    - "systemctl start docker"
  postStartCommands:
    - "docker run -d --name node-exporter -p 9100:9100 prom/node-exporter"
    - "echo 'Node exporter started on port 9100'"
```

#### Configuring System Settings

```yaml
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: worker-with-config
spec:
  version: v1.34.3+k0s.0
  preStartCommands:
    - "echo 'vm.max_map_count=262144' >> /etc/sysctl.conf"
    - "sysctl -p"
    - "echo 'net.core.somaxconn=65535' >> /etc/sysctl.conf"
    - "sysctl -p"
  postStartCommands:
    - "echo 'System configuration applied successfully'"
    - "sysctl vm.max_map_count net.core.somaxconn"
```

#### Health Checks and Validation

```yaml
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: worker-with-health-checks
spec:
  version: v1.34.3+k0s.0
  postStartCommands:
    - "kubectl get nodes --kubeconfig=/var/lib/k0s/pki/admin.conf"
    - "kubectl describe node $(hostname) --kubeconfig=/var/lib/k0s/pki/admin.conf"
    - "echo 'Health check completed successfully'"
```

### Important Notes

- Commands are executed as root user
- Each command is executed in a separate shell session
- If any command fails, the bootstrap process will fail
- Commands are executed in the order they appear in the array
- Environment variables from the system are available to the commands
- The k0s kubeconfig is available at `/var/lib/k0s/pki/admin.conf` for PostStartCommands

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
      version: v1.34.3+k0s.0
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