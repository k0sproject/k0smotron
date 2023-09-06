# Cluster API - Control Plane Bootstrap provider

k0smotron can act as a control plane bootstrap provider via usage of `K0sControlPlane` CRDs.

When creating a Cluster with Cluster API you typically need to create a `Cluster` object. With k0smotron there needs to be a link to the control plane provider `K0sControlPlane`:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
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
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: K0sControlPlane
    name: cp-test
```

Next we need to provide the configuration for the actual `K0sControlPlane` and `machineTemplate`:

```yaml
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
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
    infrastructureRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: DockerMachineTemplate
      name: cp-test-machine-template
      namespace: default
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerMachineTemplate
metadata:
  name: cp-test-machine-template
  namespace: default
spec:
  template:
    spec: {}
```

By applying this yaml, k0smotron will create 3 machines based on the `MachineTemplate` configuration, installs k0s with the role controller on each machine and bootstraps the k0s control plane.

For a full reference on `K0sControlPlane` configurability see the [reference docs](resource-reference.md#controlplaneclusterx-k8siov1beta1).

## Downscaling the control plane

**WARNING: Downscaling is a dangerous operation and should only be done if you know what you are doing.**

Kubernetes using etcd as its backing store. It's crucial to have a quorum of etcd nodes available at all times. Always run etcd as a cluster of **odd** members.
    
When downscaling the control plane, you need firstly to deregister the node from the etcd cluster. This can be done by running the following command on the node that will be removed:

```bash
k0s etcd leave
``` 

**NOTE:** k0smotron gives node names sequentially and on downscaling it will remove the "latest" nodes. For instance, if you have `k0smotron-test` cluster of 5 nodes and you downscale to 3 nodes, the nodes `k0smotron-test-3` and `k0smotron-test-4` will be removed. 

After removing members from etcd cluster, you can simply edit the `K0sControlPlane` object and change the `spec.replicas` field to the desired number of replicas. k0smotron will then automatically scale down the control plane to the desired number of replicas.

## Running workloads on the control plane

By default, k0s and k0smotron don't run kubelet and any workloads on control plane nodes. But you can enable it by adding `--enable-worker` flag to the `spec.k0sConfigSpec.args` in the `K0sControlPlane` object. This will enable the kubelet on control plane nodes and allow you to run workloads on them.

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
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
    infrastructureRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: DockerMachineTemplate
      name: docker-test-cp-template
      namespace: default
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerMachineTemplate
metadata:
  name: docker-test-cp-template
  namespace: default
spec:
  template:
    spec: {}
```

**Note:** Controller nodes running with `--enable-worker` are assigned `node-role.kubernetes.io/master:NoExecute` taint automatically. You can disable default taints using `--no-taints`  parameter.
