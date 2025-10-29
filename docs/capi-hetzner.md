# Cluster API - Hetzner

This example demonstrates how k0smotron can be used with CAPH (Cluster API Provider HetznerCloud).

## Preparations

Before starting this example, ensure that you have met the [general prerequisites](capi-examples.md#prerequisites).

To initialize the management cluster with Hetzner infrastructure provider you can run:

```
clusterctl init --core cluster-api:v1.11.2 --infrastructure hetzner:v1.0.7
```

For more details on Cluster API Provider Hetzner see it's [docs](https://github.com/syself/cluster-api-provider-hetzner/tree/main/docs).

### Token

To be able to provision the infrastructure Hetzner provider you will need a token to interact with the Hetzner API. You'll can create & find the token in your project at "Security" in the Hetzner Cloud console.

## Creating a child cluster

Once all the controllers are up and running, you can apply the cluster manifests containing the specifications of the cluster you want to provision.

Here is an example:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: hetzner-test
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
    kind: K0smotronControlPlane # This tells that k0smotron should create the controlplane
    name: hetzner-test-cp
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: HetznerCluster
    name: hetzner-test
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0smotronControlPlane # This is the config for the controlplane
metadata:
  name: hetzner-test-cp
spec:
  version: v1.27.2-k0s.0
  persistence:
    type: emptyDir
  service:
    type: LoadBalancer
    apiPort: 6443
    konnectivityPort: 8132
    annotations:
      load-balancer.hetzner.cloud/location: fsn1
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: HetznerCluster
metadata:
  name: hetzner-test
  namespace: default
  annotations:
    cluster.x-k8s.io/managed-by: k0smotron # This marks the base infra to be self managed. The value of the annotation is irrelevant, as long as there is a value.
spec:
  controlPlaneLoadBalancer:
    enabled: false
  controlPlaneEndpoint: # This is just a placeholder, can be anything as k0smotron will overwrite it
    host: "1.2.3.4"
    port: 6443
  controlPlaneRegions:
    - fsn1
  hetznerSecretRef:
    name: hetzner
    key:
      hcloudToken: hcloud
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachineDeployment
metadata:
  name: hetnzer-test-md
  namespace: default
spec:
  clusterName: hetzner-test
  replicas: 1
  selector:
    matchLabels:
      cluster.x-k8s.io/cluster-name: hetzner-test
      pool: worker-pool-1
  template:
    metadata:
      labels:
        cluster.x-k8s.io/cluster-name: hetzner-test
        pool: worker-pool-1
    spec:
      clusterName: hetzner-test
      failureDomain: fsn1
      bootstrap:
        configRef: # This triggers our controller to create cloud-init secret
          apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
          kind: K0sWorkerConfigTemplate
          name: hetzner-test-machine-config
      infrastructureRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
        kind: HCloudMachineTemplate
        name: hetzner-test-mt
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: HCloudMachineTemplate
metadata:
  name: hetzner-test-mt
  namespace: default
spec:
  imageName: ubuntu-22.04
  type: cx21
  sshKeys:
    - name: ssh-key
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfigTemplate
metadata:
  name: hetzner-test-machine-config
spec:
  template:
    spec:
      version: v1.27.2+k0s.0
      # More details of the worker configuration can be set here
---
apiVersion: v1
kind: Secret
data:
  hcloud: <base64 encoded token>
metadata:
  name: hetzner
```

In the case of `HetznerCluster.spec.controlPlaneEndpoint` you can add any valid address. k0smotron will overwrite these automatically once the control plane is up and running. You do need to specify some placeholder address as the `HetznerCluster` object has those marked as mandatory fields.

After applying the manifests to the management cluster and confirming the infrastructure readiness, allow a few minutes for all components to provision. Once complete, your command line should display output similar to this:

```
% kubectl get cluster,machine
NAME                                   PHASE         AGE     VERSION
cluster.cluster.x-k8s.io/hetzer-test   Provisioned   3m51s

NAME                                         CLUSTER        NODENAME   PROVIDERID          PHASE         AGE     VERSION
machine.cluster.x-k8s.io/hetzner-test-md-0   hetzner-test              hcloud://12345678   Provisioned   3m50s
```

You can also check the status of the cluster deployment with `clusterctl describe cluster`.

## Accessing the workload cluster

To access the child cluster we can get the kubeconfig for it with `clusterctl get kubeconfig hetzner-test`. You can then save it to disk and/or import to your favorite tooling like [Lens](https://k8slens.dev).

## Deleting the cluster

For cluster deletion, do **NOT** use `kubectl delete -f my-hetzner-cluster.yaml` as that can result in orphan resources. Instead, delete the top level `Cluster` object. This approach ensures the proper sequence in deleting all child resources, effectively avoid orphan resources.
