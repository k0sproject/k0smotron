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
