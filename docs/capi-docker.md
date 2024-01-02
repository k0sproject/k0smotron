# Cluster API - Docker

This example demonstrates how k0smotron can be used with CAPD (Cluster API Provider Docker).

Please note, CAPD should only be used for development purposes and not for production environments.

## Preparations

Before starting this example, ensure that you have met the [general prerequisites](capi-examples.md#prerequisites). 

To initialize the management cluster with Docker infrastrcture provider you can run:

```bash
clusterctl init --core cluster-api --infrastructure docker
```

For more details on Cluster API Provider Docker see it's [docs](https://github.com/kubernetes-sigs/cluster-api/tree/main/test/infrastructure/docker).

## Create the Docker Kind Network

The Cluster API Provider Docker (CAPD) utilizes a network named kind as the default network for certain components it deploys, such as HAProxy. To establish this network, perform the following step:

```bash
docker network create kind --opt com.docker.network.bridge.enable_ip_masquerade=true
```

By executing this command, you are creating a Docker network named 'kind' with IP masquerade enabled, which is necessary for the proper operation of certain CAPD-deployed components.

## Creating a child cluster

Once all the controllers are up and running, you can apply the cluster manifests containing the specifications of the cluster you want to provision.

Here is an example:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: docker-test
  namespace: default
spec:
  clusterNetwork:
    pods:
      cidrBlocks:
      - 192.168.0.0/16
    serviceDomain: cluster.local
    services:
      cidrBlocks:
      - 10.128.0.0/12
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: K0smotronControlPlane
    name: docker-test-cp
    namespace: default
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: DockerCluster
    name: docker-test
    namespace: default
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0smotronControlPlane # This is the config for the controlplane
metadata:
  name: docker-test-cp
  namespace: default
spec:
  k0sVersion: v1.27.2-k0s.0
  persistence:
    type: emptyDir
  service:
    type: NodePort
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerCluster
metadata:
  name: docker-test
  namespace: default
  annotations:
    cluster.x-k8s.io/managed-by: k0smotron # This marks the base infra to be self managed. The value of the annotation is irrelevant, as long as there is a value.
spec: {}
  # More details of the DockerCluster can be set here
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachineDeployment
metadata:
  name:  docker-test-md
  namespace: default
spec:
  clusterName: docker-test
  replicas: 1
  selector:
    matchLabels:
      cluster.x-k8s.io/cluster-name: docker-test
      pool: worker-pool-1
  template:
    metadata:
      labels:
        cluster.x-k8s.io/cluster-name: docker-test
        pool: worker-pool-1
    spec:
      clusterName: docker-test
      version: v1.27.2 # Docker Provider requires a version to be set (see https://hub.docker.com/r/kindest/node/tags)
      bootstrap:
        configRef:
          apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
          kind: K0sWorkerConfigTemplate
          name: docker-test-machine-config
      infrastructureRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
        kind: DockerMachineTemplate
        name: docker-test-mt
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerMachineTemplate
metadata:
  name: docker-test-mt
  namespace: default
spec:
  template:
    spec: {}
  # More details of the DockerMachineTemplate can be set here
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfigTemplate
metadata:
  name: docker-test-machine-config
spec:
  template:
    spec:
      version: v1.27.2+k0s.0
      # More details of the worker configuration can be set here
```

After applying the manifests to the management cluster and confirming the infrastructure readiness, allow a few minutes for all components to provision. Once complete, your command line should display output similar to this:

```bash
% kubectl get cluster,machine
NAME                                   PHASE         AGE     VERSION
cluster.cluster.x-k8s.io/docker-test   Provisioned   3m51s   

NAME                                        CLUSTER       NODENAME   PROVIDERID          PHASE         AGE     VERSION
machine.cluster.x-k8s.io/docker-test-md-0   docker-test                                  Provisioned   3m50s
```

You can also check the status of the cluster deployment with `clusterctl describe cluster`.

## Accessing the workload cluster

To access the child cluster we can get the kubeconfig for it with `clusterctl get kubeconfig docker-test`. You can then save it to disk and/or import to your favorite tooling like [Lens](https://k8slens.dev)

## Deleting the cluster

For cluster deletion, do **NOT** use `kubectl delete -f my-docker-cluster.yaml` as that can result in orphan resources. Instead, delete the top level `Cluster` object. This approach ensures the proper sequence in deleting all child resources, effectively avoid orphan resources.
