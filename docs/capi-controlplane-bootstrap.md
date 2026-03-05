# Cluster API - Control Plane Bootstrap provider

k0smotron can act as a control plane bootstrap provider via usage of `K0sControlPlane` CRDs.

When creating a Cluster with Cluster API you typically need to create a `Cluster` object. With k0smotron there needs to be a link to the control plane provider `K0sControlPlane`:

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: cp-test
spec:
  clusterNetwork:
    pods:
      cidrBlocks:
        - 10.244.0.0/16
    services:
      cidrBlocks:
        - 10.96.0.0/12
  controlPlaneRef:
    apiGroup: controlplane.cluster.x-k8s.io
    kind: K0sControlPlane
    name: cp-test
```

Next we need to provide the configuration for the actual `K0sControlPlane` and `machineTemplate`:

```yaml
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlane
metadata:
  name: cp-test
spec:
  replicas: 3
  k0sConfigSpec:
    k0s:
      apiVersion: k0s.k0sproject.io/v1beta1
      kind: ClusterConfig
      metadata:
        name: k0s
      spec:
        api:
          extraArgs:
            anonymous-auth: "true" # anonymous-auth=true is needed for k0s to allow unauthorized health-checks on /healthz
  machineTemplate:
    spec:
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: DockerMachineTemplate
        name: cp-test-machine-template
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: DockerMachineTemplate
metadata:
  name: cp-test-machine-template
  namespace: default
spec:
  template:
    spec: {}
```

By applying this yaml, k0smotron will create 3 machines based on the `MachineTemplate` configuration, installs k0s with the role controller on each machine and bootstraps the k0s control plane.

For a full reference on `K0sControlPlane` configurability see the [reference docs](resource-reference/controlplane.cluster.x-k8s.io-v1beta1.md).

## Pre/Post Start Commands

k0smotron supports executing custom commands before and after starting k0s on control plane nodes. This feature is useful for:

- Installing additional packages or dependencies
- Configuring system settings
- Setting up monitoring agents
- Running health checks
- Performing cleanup operations

### PreK0sCommands

Commands specified in `preK0sCommands` are executed before k0s binary is downloaded and installed.

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlane
metadata:
  name: cp-test
spec:
  replicas: 3
  k0sConfigSpec:
    preK0sCommands:
      - "apt-get update && apt-get install -y curl jq"
      - "mkdir -p /etc/k0s/monitoring"
      - "echo 'export MONITORING_ENABLED=true' >> /etc/environment"
```

### PostK0sCommands

Commands specified in `postK0sCommands` are executed after k0s has started successfully. These commands run after the k0s service is running and the control plane is ready.

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlane
metadata:
  name: cp-test
spec:
  replicas: 3
  k0sConfigSpec:
    postK0sCommands:
      - "systemctl enable monitoring-agent"
      - "systemctl start monitoring-agent"
      - "kubectl get nodes --kubeconfig=/var/lib/k0s/pki/admin.conf"
```

### Command Execution Order

The commands are executed in the following order:

1. **PreK0sCommands** - Custom commands before k0s starts
2. **Download and Install** - k0s binary download and installation
3. **k0s start** - k0s service startup
4. **PostK0sCommands** - Custom commands after k0s starts

### Use Cases

#### Installing Monitoring Agents on Control Plane

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlane
metadata:
  name: cp-with-monitoring
spec:
  replicas: 3
  k0sConfigSpec:
    preK0sCommands:
      - "curl -fsSL https://get.docker.com | sh"
      - "systemctl enable docker"
      - "systemctl start docker"
    postK0sCommands:
      - "docker run -d --name node-exporter -p 9100:9100 prom/node-exporter"
      - "echo 'Node exporter started on port 9100'"
```

#### Configuring System Settings for Control Plane

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlane
metadata:
  name: cp-with-config
spec:
  replicas: 3
  k0sConfigSpec:
    preK0sCommands:
      - "echo 'vm.max_map_count=262144' >> /etc/sysctl.conf"
      - "sysctl -p"
      - "echo 'net.core.somaxconn=65535' >> /etc/sysctl.conf"
      - "sysctl -p"
    postK0sCommands:
      - "echo 'System configuration applied successfully'"
      - "sysctl vm.max_map_count net.core.somaxconn"
```

#### Health Checks and Validation for Control Plane

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlane
metadata:
  name: cp-with-health-checks
spec:
  replicas: 3
  k0sConfigSpec:
    postK0sCommands:
      - "kubectl get nodes --kubeconfig=/var/lib/k0s/pki/admin.conf"
      - "kubectl describe node $(hostname) --kubeconfig=/var/lib/k0s/pki/admin.conf"
      - "echo 'Control plane health check completed successfully'"
```

## Downscaling the control plane

**WARNING: Downscaling is a potentially dangerous operation.**

Kubernetes using etcd as its backing store. It's crucial to have a quorum of etcd nodes available at all times. Always run etcd as a cluster of **odd** members.

When downscaling the control plane, you need firstly to deregister the node from the etcd cluster. k0smotron will do it automatically for you.

**NOTE:** k0smotron gives node names sequentially and on downscaling it will remove the "latest" nodes. For instance, if you have `k0smotron-test` cluster of 5 nodes and you downscale to 3 nodes, the nodes `k0smotron-test-3` and `k0smotron-test-4` will be removed.

## Recovering from a lost control plane node

If you lose a control plane node, you need to recover the cluster. First, you need to remove the lost node from the etcd cluster. You can do this by running the following command on the remaining control plane nodes:

```bash
k0s etcd leave --peer-address <peer-address>
```

Then you need to remove old objects from the management cluster with the name of the lost node. You can do this by running the following command:

```bash
kubectl delete machine <lost-node-name>
kubectl delete <infra-provider-specific-machine-object> <lost-node-name>
kubectl delete secret <lost-node-name>
kubectl delete k0scontrollerconfig <lost-node-name>
```

After that you need to trigger the reconciliation of the control plane object by updating the `K0sControlPlane` object or restarting the controller manager.

```bash

## Running workloads on the control plane

By default, k0s and k0smotron don't run kubelet and any workloads on control plane nodes. But you can enable it by adding `--enable-worker` flag to the `spec.k0sConfigSpec.args` in the `K0sControlPlane` object. This will enable the kubelet on control plane nodes and allow you to run workloads on them.

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlane
metadata:
  name: docker-test
spec:
  replicas: 1
  k0sConfigSpec:
    args:
      - --enable-worker
      - --no-taints # disable default taints
  machineTemplate:
    spec:
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: DockerMachineTemplate
        name: docker-test-cp-template
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: DockerMachineTemplate
metadata:
  name: docker-test-cp-template
  namespace: default
spec:
  template:
    spec: {}
```

**Note:** Controller nodes running with `--enable-worker` are assigned `node-role.kubernetes.io/master:NoExecute` taint automatically. You can disable default taints using `--no-taints`  parameter.

## Client connection tunneling

k0smotron supports client connection tunneling to the child cluster's control plane nodes. This is useful when you want to access the control plane nodes from a remote location.
To enable tunneling, you need to set `spec.k0sConfigSpec.tunneling.enabled` to `true` in the `K0sControlPlane` object.

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlane
metadata:
  name: docker-test
spec:
  replicas: 1
  k0sConfigSpec:
    tunneling:
      enabled: true
      mode: tunnel # Tunneling mode: tunnel or proxy (default: tunnel)
```

K0smotron supports two tunneling modes: `tunnel` and `proxy`. You can set the tunneling mode using `spec.k0sConfigSpec.tunneling.mode` field. The default mode is `tunnel`.

K0smotron will create a kubeconfig file for the tunneling client in the `K0sControlPlane` object's namespace. You can find the kubeconfig file in the `<cluster-name>-<mode>-kubeconfig` secret.
You can use this kubeconfig file to access the control plane nodes from a remote location.

**Note:** Parent cluster's worker nodes must be accessible from the child cluster's nodes. You can use `spec.k0sConfigSpec.tunneling.serverAddress` to set the address of the parent cluster's node or load balancer. If you don't set this field, k0smotron will use the random worker node's address as the default address.

Currently, k0smotron supports only NodePort service type for tunneling. You can set the tunneling service port using `spec.k0sConfigSpec.tunneling.tunnelingNodePort` field. The default port is `31443`.
