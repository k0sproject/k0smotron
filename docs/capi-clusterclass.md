# ClusterClass

K0smotron supports ClusterClass, a simple way to create many clusters of a similar shape.

For instance, we will create a ClusterClass that will create a cluster running control plane and worker nodes on DockerMachines:

```yaml
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: ClusterClass
metadata:
  name: k0smotron-clusterclass
  namespace: cluster-classes
spec:
  controlPlane:
    templateRef:
      apiVersion: controlplane.cluster.x-k8s.io/v1beta2
      kind: K0sControlPlaneTemplate
      name: k0s-controlplane-template
    machineInfrastructure:
      templateRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
        kind: DockerMachineTemplate
        name: cp-docker-machine-template
  infrastructure:
    templateRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
      kind: DockerClusterTemplate
      name: docker-cluster-template
  workers:
    machineDeployments:
    - class: default-worker
      bootstrap:
        templateRef:
          apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
          kind: K0sWorkerConfigTemplate
          name: k0s-worker-config-template
      infrastructure:
        templateRef:
          apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
          kind: DockerMachineTemplate
          name: worker-docker-machine-template
  patches:
    # control plane: set DockerMachineTemplate.spec.template.spec.customImage
    # k0s appends "+k0s.N" to the version (e.g. v1.27.2 -> v1.27.2+k0s.0),
    # which is not a valid Docker image tag. Use a patch to derive a valid
    # kindest/node image tag from the Kubernetes minor version.
    - name: controlPlaneCustomImageFromVersion
      definitions:
        - selector:
            apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
            kind: DockerMachineTemplate
            matchResources:
              controlPlane: true
          jsonPatches:
            - op: add
              path: /spec/template/spec/customImage
              valueFrom:
                template: |
                  kindest/node:{{ regexFind "^v[0-9]+\\.[0-9]+" .builtin.controlPlane.version }}.0
    # workers: set DockerMachineTemplate.spec.template.spec.customImage
    - name: workerCustomImageFromVersion
      definitions:
        - selector:
            apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
            kind: DockerMachineTemplate
            matchResources:
              machineDeploymentClass:
                names:
                  - default-worker
          jsonPatches:
            - op: add
              path: /spec/template/spec/customImage
              valueFrom:
                template: |
                  kindest/node:{{ regexFind "^v[0-9]+\\.[0-9]+" .builtin.machineDeployment.version }}.0
---
… # other objects omitted for brevity, see full example below
```

Then we can easily create a Cluster using the ClusterClass. With `classRef.namespace`, you can reference a ClusterClass from a different namespace:

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: my-cluster
  namespace: team-a
spec:
  topology:
    classRef:
      name: k0smotron-clusterclass
      namespace: cluster-classes
    version: v1.27.2
    controlPlane:
      replicas: 1
    workers:
      machineDeployments:
      - class: default-worker
        name: md
        replicas: 3
```

You can read more about ClusterClass in the [Cluster API documentation](https://cluster-api.sigs.k8s.io/tasks/experimental-features/cluster-class/).

## K0sControlPlane full example

```yaml
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlaneTemplate
metadata:
  name: k0s-controlplane-template
  namespace: cluster-classes
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
                anonymous-auth: "true"
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: DockerClusterTemplate
metadata:
  name: docker-cluster-template
  namespace: cluster-classes
spec:
  template:
    spec: {}
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: DockerMachineTemplate
metadata:
  name: cp-docker-machine-template
  namespace: cluster-classes
spec:
  template:
    spec: {}
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: K0sWorkerConfigTemplate
metadata:
  name: k0s-worker-config-template
  namespace: cluster-classes
spec:
  template:
    spec: {}
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: DockerMachineTemplate
metadata:
  name: worker-docker-machine-template
  namespace: cluster-classes
spec:
  template:
    spec: {}
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: ClusterClass
metadata:
  name: k0smotron-clusterclass
  namespace: cluster-classes
spec:
  controlPlane:
    templateRef:
      apiVersion: controlplane.cluster.x-k8s.io/v1beta2
      kind: K0sControlPlaneTemplate
      name: k0s-controlplane-template
    machineInfrastructure:
      templateRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
        kind: DockerMachineTemplate
        name: cp-docker-machine-template
  infrastructure:
    templateRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
      kind: DockerClusterTemplate
      name: docker-cluster-template
  workers:
    machineDeployments:
    - class: default-worker
      bootstrap:
        templateRef:
          apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
          kind: K0sWorkerConfigTemplate
          name: k0s-worker-config-template
      infrastructure:
        templateRef:
          apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
          kind: DockerMachineTemplate
          name: worker-docker-machine-template
  patches:
    # control plane: set DockerMachineTemplate.spec.template.spec.customImage
    # k0s appends "+k0s.N" to the version (e.g. v1.27.2 -> v1.27.2+k0s.0),
    # which is not a valid Docker image tag. Use a patch to derive a valid
    # kindest/node image tag from the Kubernetes minor version.
    - name: controlPlaneCustomImageFromVersion
      definitions:
        - selector:
            apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
            kind: DockerMachineTemplate
            matchResources:
              controlPlane: true
          jsonPatches:
            - op: add
              path: /spec/template/spec/customImage
              valueFrom:
                template: |
                  kindest/node:{{ regexFind "^v[0-9]+\\.[0-9]+" .builtin.controlPlane.version }}.0
    # workers: set DockerMachineTemplate.spec.template.spec.customImage
    - name: workerCustomImageFromVersion
      definitions:
        - selector:
            apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
            kind: DockerMachineTemplate
            matchResources:
              machineDeploymentClass:
                names:
                  - default-worker
          jsonPatches:
            - op: add
              path: /spec/template/spec/customImage
              valueFrom:
                template: |
                  kindest/node:{{ regexFind "^v[0-9]+\\.[0-9]+" .builtin.machineDeployment.version }}.0
```

## K0smotronControlPlaneTemplate for ClusterClass

`K0smotronControlPlane` is a custom resource that is used to create a control planes as pods in the managing cluster. It does not create any machines, but instead creates a pod that runs the k0s control plane.
Here is the example of `ClusterClass` that uses `K0smotronControlPlaneTemplate`:

```yaml
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: ClusterClass
metadata:
  name: k0smotron-clusterclass
  namespace: cluster-classes
spec:
  controlPlane:
    templateRef:
      apiVersion: controlplane.cluster.x-k8s.io/v1beta2
      kind: K0smotronControlPlaneTemplate
      name: k0s-controlplane-template
  infrastructure:
    templateRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
      kind: DockerClusterTemplate
      name: docker-cluster-template
  workers:
    machineDeployments:
    - class: default-worker
      bootstrap:
        templateRef:
          apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
          kind: K0sWorkerConfigTemplate
          name: k0s-worker-config-template
      infrastructure:
        templateRef:
          apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
          kind: DockerMachineTemplate
          name: worker-docker-machine-template
  patches:
    # workers: set DockerMachineTemplate.spec.template.spec.customImage
    - name: workerCustomImageFromVersion
      definitions:
        - selector:
            apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
            kind: DockerMachineTemplate
            matchResources:
              machineDeploymentClass:
                names:
                  - default-worker
          jsonPatches:
            - op: add
              path: /spec/template/spec/customImage
              valueFrom:
                template: |
                  kindest/node:{{ regexFind "^v[0-9]+\\.[0-9]+" .builtin.machineDeployment.version }}.0
---
… # other objects omitted for brevity, see full example below
```

## K0smotronControlPlane full example

```yaml
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0smotronControlPlaneTemplate
metadata:
  name: k0s-controlplane-template
  namespace: cluster-classes
spec:
  template:
    spec:
      service:
        type: LoadBalancer
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: DockerClusterTemplate
metadata:
  name: docker-cluster-template
  namespace: cluster-classes
spec:
  template:
    spec: {}
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: K0sWorkerConfigTemplate
metadata:
  name: k0s-worker-config-template
  namespace: cluster-classes
spec:
  template:
    spec: {}
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: DockerMachineTemplate
metadata:
  name: worker-docker-machine-template
  namespace: cluster-classes
spec:
  template:
    spec: {}
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: ClusterClass
metadata:
  name: k0smotron-clusterclass
  namespace: cluster-classes
spec:
  controlPlane:
    templateRef:
      apiVersion: controlplane.cluster.x-k8s.io/v1beta2
      kind: K0smotronControlPlaneTemplate
      name: k0s-controlplane-template
  infrastructure:
    templateRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
      kind: DockerClusterTemplate
      name: docker-cluster-template
  workers:
    machineDeployments:
    - class: default-worker
      bootstrap:
        templateRef:
          apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
          kind: K0sWorkerConfigTemplate
          name: k0s-worker-config-template
      infrastructure:
        templateRef:
          apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
          kind: DockerMachineTemplate
          name: worker-docker-machine-template
  patches:
    # workers: set DockerMachineTemplate.spec.template.spec.customImage
    - name: workerCustomImageFromVersion
      definitions:
        - selector:
            apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
            kind: DockerMachineTemplate
            matchResources:
              machineDeploymentClass:
                names:
                  - default-worker
          jsonPatches:
            - op: add
              path: /spec/template/spec/customImage
              valueFrom:
                template: |
                  kindest/node:{{ regexFind "^v[0-9]+\\.[0-9]+" .builtin.machineDeployment.version }}.0
```
