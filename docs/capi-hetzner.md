# Cluster API - Hetzner

In this guide we will show you how to use Hetzner infrastructure for the worker plane while using k0smotron control plane.

## Preparations


To initialize the management cluster with Hetzner infrastrcture provider you can run:

```
clusterctl init --core cluster-api --infrastructure hetzner
```

For more details on Hetzner Cluster API provider see it's [docs](https://github.com/syself/cluster-api-provider-hetzner/tree/main/docs).

### Token

To be able to provision the infrastructure Hetzner provider will need a token to interact with Hetzner API.  You'll find this if you click on the project and go to "security" on Hetzner console.

## Creating a cluster

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
    kind: K0smotronControlPlane # This tells that k0smotron should create the controlplane
    name: cp-test
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: HetznerCluster
    name: cp-test
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0smotronControlPlane # This is the config for the controlplane
metadata:
  name: cp-test
spec:
  k0sVersion: v1.27.2-k0s.0
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
  name: cp-test
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
kind: Machine
metadata:
  name: cp-test-0
spec:
  clusterName: cp-test
  bootstrap:
    configRef: # This triggers our controller to create cloud-init secret
      apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
      kind: K0sWorkerConfig
      name: cp-test-0
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: HCloudMachine
    name: cp-test-0
  
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: HCloudMachine
metadata:
  name: cp-test-0
spec:
  imageName: ubuntu-22.04
  type: cx21
  sshKeys:
    - name: your-ssh-key-name
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: cp-test-0
spec:
---
apiVersion: v1
kind: Secret
data:
  hcloud: <base64 encoded token>
metadata:
  name: hetzner
```

In the case of `HetznerCluster.spec.controlPlaneEndpoint` you can add any valid address. k0smotron will overwrite these are automatically once it gets the control plane up-and-running. You do need to specify some placeholder address as the `HetznerCluster` object has those marked as mandatory fields.

Once you apply the manifests to the management cluster it'll take couple of minutes to provision everything. In the end you should see something like this:


```
% kubectl get cluster,machine
NAME                               PHASE         AGE     VERSION
cluster.cluster.x-k8s.io/cp-test   Provisioned   3m51s   

NAME                                 CLUSTER   NODENAME   PROVIDERID          PHASE         AGE     VERSION
machine.cluster.x-k8s.io/cp-test-0   cp-test              hcloud://12345678   Provisioned   3m50s
```

## Accessing the workload cluster

To access the workload (a.k.a child) cluster we can get the kubeconfig for it with `clusterctl get kubeconfig cp-test`. You can then save it to disk and/or import to your favorite tooling like [Lens](https://k8slens.dev)