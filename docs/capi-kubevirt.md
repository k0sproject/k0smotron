# Cluster API - Kubevirt

This example demonstrates how k0smotron can be used with CAPK (Cluster API Provider Kubevirt).

## Preparations

Before starting this example, ensure that you have met the [general prerequisites](capi-examples.md#prerequisites).

To install the latest stable version of Kubevirt you can run:

```bash
export VERSION=$(curl -s https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)
kubectl create -f https://github.com/kubevirt/kubevirt/releases/download/${VERSION}/kubevirt-operator.yaml
kubectl create -f https://github.com/kubevirt/kubevirt/releases/download/${VERSION}/kubevirt-cr.yaml
```

To initialize the management cluster with Kubevirt infrastructure provider you can run:

```bash
clusterctl init --core cluster-api --infrastructure kubevirt
```

For more details on Cluster API Provider Kubevirt see it's [docs](https://github.com/kubernetes-sigs/cluster-api-provider-kubevirt).

## Nested Virtualization

If your kubernetes nodes are able to support nested virtualization please make sure it is enabled. If your nodes are not able to support nested virtualization you can work around it by running the following.

```bash
kubectl -n kubevirt patch kubevirt kubevirt --type=merge --patch '{"spec":{"configuration":{"developerConfiguration":{"useEmulation":true}}}}'
```

## Creating a child cluster

Once all the controllers are up and running, you can apply the cluster manifests containing the specifications of the cluster you want to provision.

Here is an example:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: kubevirt-test
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
    name: k0s-test-cp
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: KubevirtCluster
    name: kubevirt-test
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0smotronControlPlane # This is the config for the controlplane
metadata:
  name: k0s-test-cp
spec:
  version: v1.27.4-k0s.0
  persistence:
    type: emptyDir
  service:
    type: LoadBalancer
---
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: KubevirtCluster
metadata:
  name: kubevirt-test
  annotations:
    cluster.x-k8s.io/managed-by: k0smotron
spec:
  controlPlaneServiceTemplate:
    spec:
      type: ClusterIP
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachineDeployment
metadata:
  name: kubevirt-md
spec:
  clusterName: kubevirt-test
  replicas: 1
  selector:
    matchLabels:
      cluster.x-k8s.io/cluster-name: kubevirt-test
  template:
    metadata:
      labels:
        cluster.x-k8s.io/cluster-name: kubevirt-test
    spec:
      clusterName: kubevirt-test
      bootstrap:
        configRef:
          apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
          kind: K0sWorkerConfigTemplate
          name: kubevirt-test-machine-config
      infrastructureRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
        kind: KubevirtMachineTemplate
        name: kubevirt-test-mt
      version: v1.27.4
---
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: KubevirtMachineTemplate
metadata:
  name: kubevirt-test-mt
spec:
  template:
    spec:
      virtualMachineBootstrapCheck:
        checkStrategy: ssh
      virtualMachineTemplate:
        spec:
          runStrategy: Always
          template:
            spec:
              domain:
                cpu:
                  cores: 1
                devices:
                  disks:
                  - disk:
                      bus: virtio
                    name: containervolume
                  networkInterfaceMultiqueue: true
                memory:
                  guest: 1Gi
              evictionStrategy: External
              volumes:
              - containerDisk:
                  image: quay.io/capk/ubuntu-2204-container-disk:v1.27.14
                name: containervolume
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfigTemplate
metadata:
  name: kubevirt-test-machine-config
spec:
  template:
    spec:
      version: v1.27.4+k0s.0
      # More details of the worker configuration can be set here
```

After applying the manifests to the management cluster and confirming the infrastructure readiness, allow a few minutes for all components to provision. Once complete, your command line should display output similar to this:

```bash
% kubectl get cluster,machine
NAME                                   PHASE         AGE     VERSION
cluster.cluster.x-k8s.io/kubevirt-test Provisioned   22h

NAME                                               CLUSTER         NODENAME                  PROVIDERID                           PHASE     AGE     VERSION
machine.cluster.x-k8s.io/kubevirt-md-mdvns-l2rxb   kubevirt-test   kubevirt-md-mdvns-l2rxb   kubevirt://kubevirt-md-mdvns-l2rxb   Running   22h     v1.27.4
```

You can also check the status of the cluster deployment with `clusterctl describe cluster`.

## Accessing the workload cluster

To access the child cluster we can get the kubeconfig for it with `clusterctl get kubeconfig kubevirt-test`. You can then save it to disk and/or import to your favorite tooling like [Lens](https://k8slens.dev)

## Deleting the cluster

For cluster deletion, do **NOT** use `kubectl delete -f my-kubevirt-cluster.yaml` as that can result in orphan resources. Instead, delete the top level `Cluster` object. This approach ensures the proper sequence in deleting all child resources, effectively avoid orphan resources.
