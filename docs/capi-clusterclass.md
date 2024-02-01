# ClusterClass

K0smotron supports ClusterClass, a simple way to create many clusters of a similar shape. 

For instance, we will create a ClusterClass that will create a cluster running control plane and worker nodes on DockerMachines:

```yaml
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: ClusterClass
metadata:
  name: k0smotron-clusterclass
spec:
  controlPlane:
    ref:
      apiVersion: controlplane.cluster.x-k8s.io/v1beta1
      kind: K0sControlPlaneTemplate
      name: k0s-controlplane-template
      namespace: default
    machineInfrastructure:
      ref:
        kind: DockerMachineTemplate
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
        name: cp-docker-machine-template
        namespace: default
  infrastructure:
    ref:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: DockerClusterTemplate
      name: docker-cluster-template
      namespace: default
  workers:
    machineDeployments:
    - class: default-worker
      template:
        bootstrap:
          ref:
            apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
            kind: K0sWorkerConfigTemplate
            name: k0s-worker-config-template
            namespace: default
        infrastructure:
          ref:
            apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
            kind: DockerMachineTemplate
            name: worker-docker-machine-template
            namespace: default
---
… # other objects omitted for brevity, see full example below
```

Then we can easily create a Cluster using the ClusterClass:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: k0smotron-test-cluster
  namespace: default
spec:
  topology:
    class: k0smotron-clusterclass
    version: v1.27.2
    workers:
      machineDeployments:
      - class: default-worker
        name: md
        replicas: 3
```

You can read more about ClusterClass in the [Cluster API documentation](https://cluster-api.sigs.k8s.io/tasks/experimental-features/cluster-class/).

## K0smotronControlPlaneTemplate for ClusterClass

`K0smotronControlPlane` is a custom resource that is used to create a control planes as pods in the managing cluster. It does not create any machines, but instead creates a pod that runs the k0s control plane.
Here is the example of `ClusterClass` that uses `K0smotronControlPlaneTemplate`:

```yaml
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: ClusterClass
metadata:
  name: k0smotron-clusterclass
spec:
  controlPlane:
    ref:
      apiVersion: controlplane.cluster.x-k8s.io/v1beta1
      kind: K0smotronControlPlaneTemplate
      name: k0s-controlplane-template
      namespace: default
  infrastructure:
    ref:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: DockerClusterTemplate
      name: docker-cluster-template
      namespace: default
  workers:
    machineDeployments:
    - class: default-worker
      template:
        bootstrap:
          ref:
            apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
            kind: K0sWorkerConfigTemplate
            name: k0s-worker-config-template
            namespace: default
        infrastructure:
          ref:
            apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
            kind: DockerMachineTemplate
            name: worker-docker-machine-template
            namespace: default
---
… # other objects omitted for brevity, see full example below
```

```yaml

## Full example

```yaml
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0sControlPlaneTemplate
metadata:
  name: k0s-controlplane-template
spec:
  template:
    spec:
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
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerMachineTemplate
metadata:
  name: cp-docker-machine-template
  namespace: default
spec:
  template:
    spec: {}
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerClusterTemplate
metadata:
  name: docker-cluster-template
spec:
  template:
    spec: {}
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfigTemplate
metadata:
  name: k0s-worker-config-template
  namespace: default
spec:
  template:
    spec:
      version: v1.27.2+k0s.0
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerMachineTemplate
metadata:
  name: worker-docker-machine-template
  namespace: default
spec:
  template:
    spec: {}
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: ClusterClass
metadata:
  name: k0smotron-clusterclass
spec:
  controlPlane:
    ref:
      apiVersion: controlplane.cluster.x-k8s.io/v1beta1
      kind: K0sControlPlaneTemplate
      name: k0s-controlplane-template
      namespace: default
    machineInfrastructure:
      ref:
        kind: DockerMachineTemplate
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
        name: cp-docker-machine-template
        namespace: default
  infrastructure:
    ref:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: DockerClusterTemplate
      name: docker-cluster-template
      namespace: default
  workers:
    machineDeployments:
    - class: default-worker
      template:
        bootstrap:
          ref:
            apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
            kind: K0sWorkerConfigTemplate
            name: k0s-worker-config-template
            namespace: default
        infrastructure:
          ref:
            apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
            kind: DockerMachineTemplate
            name: worker-docker-machine-template
            namespace: default
```
