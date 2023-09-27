# Cluster API - Remote machine provider

k0smotron servers as a Cluster API provider for remote machines. A "remote machine" in this context means a machine (VM, bare metal) which can be remotely connected via SSH.

Just like any other Cluster API provider, k0smotron remote machine provider fullfils the Cluster APi contracts and thus can work with any bootstrap providers.

!!! note
    k0smotron dev team naturally focuses on testing the remote machine provider with k0s related bootstrap provider. If you see issues with any other bootstrap provider, please create issue in the Github repo.

## Using `RemoteMachine`s

To use `RemoteMachine`s in your cluster, you naturally need the top-level `Cluster` definition and control plane:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: remote-test
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
    name: remote-test
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: RemoteCluster
    name: remote-test
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0smotronControlPlane
metadata:
  name: remote-test
  namespace: default
spec:
  k0sVersion: v1.27.2-k0s.0
  persistence:
    type: emptyDir
  service:
    type: NodePort
```

To use `RemoteMachine` instances as part of the cluster, we need to initialize a `RemoteCluster` object. As in this use case there's really nothing we need to configure on the infrastructure, this is merely a "placeholder" object to fullfill Cluster API contracts.

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteCluster
metadata:
  name: remote-test
  namespace: default
spec:
```

The bootstrap a `Machine`, we need to specify the usual Cluster API objects:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Machine
metadata:
  name:  remote-test-0
  namespace: default
spec:
  clusterName: remote-test
  bootstrap:
    configRef:
      apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
      kind: K0sWorkerConfig
      name: remote-test-0
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: RemoteMachine
    name: remote-test-0
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: remote-test-0
  namespace: default
spec:
  version: v1.27.2+k0s.0
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteMachine
metadata:
  name: remote-test-0
  namespace: default
spec:
  address: 1.2.3.4
  port: 22
  user: root
  sshKeyRef:
    # This defines which SSH key to use for connecting to the machine. The Secret needs to have key 'value' with the SSH private key in it.
    name: footloose-key
```
