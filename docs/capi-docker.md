# Cluster API - Docker

In this guide we will show you how to use Docker infrastructure for the worker plane while using k0smotron control plane.

Please note, the Docker CAPI provider should only be used for development purposes, it is not recommended to use it for production environments.

## Preparations

To initialize the management cluster with Docker infrastrcture provider you can run:

```bash
clusterctl init --infrastructure docker
```

This command also adds the kubeadm bootstrap and kubeadm control-plane providers by default.

For more details on Docker Cluster API provider see it's [docs](https://github.com/kubernetes-sigs/cluster-api/tree/main/test/infrastructure/docker).

## Create the Docker Kind Network

The Docker CAPI provider uses a network called `kind` by default for some of the components it deploys into the cluster i.e. HAProxy. Create the network as follows:

```bash
docker network create kind --opt com.docker.network.bridge.enable_ip_masquerade=true
```

## Creating a cluster

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
    name: docker-test
    namespace: default
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: DockerCluster
    name: docker-test
    namespace: default
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0smotronControlPlane
metadata:
  name: docker-test
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
spec:
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: Machine
metadata:
  name:  docker-test-0
  namespace: default
spec:
  clusterName: docker-test
  bootstrap:
    configRef:
      apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
      kind: K0sWorkerConfig
      name: docker-test-0
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: DockerMachine
    name: docker-test-0
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: docker-test-0
  namespace: default
spec:
  version: v1.27.2-k0s.0
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: DockerMachine
metadata:
  name: docker-test-0
  namespace: default
spec:
```

Once you apply the manifests to the management cluster it'll take couple of minutes to provision everything. In the end you should see something like this:

```bash
% kubectl get cluster,machine
NAME                                   PHASE         AGE     VERSION
cluster.cluster.x-k8s.io/docker-test   Provisioned   3m51s   

NAME                                     CLUSTER       NODENAME   PROVIDERID          PHASE         AGE     VERSION
machine.cluster.x-k8s.io/docker-test-0   docker-test                                  Provisioned   3m50s
```

## Accessing the workload cluster

To access the workload (a.k.a child) cluster we can get the kubeconfig for it with `clusterctl get kubeconfig docker-test`. You can then save it to disk and/or import to your favorite tooling like [Lens](https://k8slens.dev)
