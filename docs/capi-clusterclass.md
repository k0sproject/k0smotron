# ClusterClass

K0smotron supports ClusterClass, a simple way to create many Cluster of a similar shape. 

For instance, we will create a ClusterClass that will create a cluster running control plane and worker nodes on DockerMachines:

```yaml
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
