# Cluster API - OpenStack

This example demonstrates how k0smotron can be used with CAPO (Cluster API Provider OpenStack).

## Preparations

Before starting this example, ensure that you have met the [general prerequisites](capi-examples.md#prerequisites).

To initialize the management cluster with OpenStack infrastrcture provider you can run:

```
clusterctl init --infrastructure openstack
```

For more details on Cluster API Provider OpenStack see it's [docs](https://github.com/kubernetes-sigs/cluster-api-provider-openstack/tree/main/docs).

### OpenStack Credentials

To be able to provision the OpenStack provider infrastructre, you will need to setup your OpenStack tenant credentials.

**Get the openstack Clouds.yaml**

Download your “OpenStack clouds.yaml file” (Login -> API Access -> Download OpenStack clouds.yaml file)

Add "verify: false" to your clouds.yaml to avoir having the "x509: certificate signed by unknown authority" error.

More information here : [cluster-api-troubleshooting](https://cluster-api-openstack.sigs.k8s.io/topics/troubleshooting)

```yaml
clouds:
  openstack:
    insecure: true
    verify: false
    auth:
      auth_url: https://keystone.yourCloud.yourOrganization.net/
      username: "yourUserName"
      project_id: "yourProjectID"
      project_name: "yourProjectName"
      project_domain_id: "yourProjectID"
      user_domain_name: "Default"
      password: YourPassWord
    region_name: "RegionOne"
    interface: "public"
    identity_api_version: 3
```

Convert it to base64 to be used in the k0smotron yaml file. See an example below:

```sh
Y2xvdWRzOgogIG9wZW5zdGFjazoKICAgIGluc2VjdXJlOiB0cnVlCiAgICB2ZXJpZnk6IGZhbHNlCiAgICBhdXRoOgogICAgICBhdXRoX3VybDogaHR0cHM6Ly9rZXlzdG9uZS55b3VyQ2xvdWQueW91ck9yZ2FuaXphdGlvbi5uZXQvCiAgICAgIHVzZXJuYW1lOiAieW91clVzZXJOYW1lIgogICAgICBwcm9qZWN0X2lkOiAieW91clByb2plY3RJRCIKICAgICAgcHJvamVjdF9uYW1lOiAieW91clByb2plY3ROYW1lIgogICAgICBwcm9qZWN0X2RvbWFpbl9pZDogInlvdXJQcm9qZWN0SUQiCiAgICAgIHVzZXJfZG9tYWluX25hbWU6ICJEZWZhdWx0IgogICAgICBwYXNzd29yZDogWW91clBhc3NXb3JkCiAgICByZWdpb25fbmFtZTogIlJlZ2lvbk9uZSIKICAgIGludGVyZmFjZTogInB1YmxpYyIKICAgIGlkZW50aXR5X2FwaV92ZXJzaW9uOiAz
```

## Creating a child cluster

Once all the controllers are up and running, you can apply the cluster manifests containing the specifications of the cluster you want to provision.

Here is an example:
```yaml
# k0smotron-cluster-with-capo.yaml
apiVersion: v1
data:
  cacert: Cg==
  clouds.yaml: Y2xvdWRzOgogIG9wZW5zdGFjazoKICAgIGluc2VjdXJlOiB0cnVlCiAgICB2ZXJpZnk6IGZhbHNlCiAgICBhdXRoOgogICAgICBhdXRoX3VybDogaHR0cHM6Ly9rZXlzdG9uZS5pYy1wcy5zc2wubWlyYW50aXMubmV0LwogICAgICB1c2VybmFtZTogIndzb3VhbGhpIgogICAgICBwcm9qZWN0X2lkOiA0MTUzYmFiNDQ2YmY0NDRmYjkzMDY3NzEzODIwNDc1NgogICAgICBwcm9qZWN0X25hbWU6ICJ3c291YWxoaSIKICAgICAgcHJvamVjdF9kb21haW5faWQ6IDQxNTNiYWI0NDZiZjQ0NGZiOTMwNjc3MTM4MjA0NzU2CiAgICAgIHVzZXJfZG9tYWluX25hbWU6ICJEZWZhdWx0IgogICAgICBwYXNzd29yZDogWW91clBhc3NXb3JkCiAgICByZWdpb25fbmFtZTogIlJlZ2lvbk9uZSIKICAgIGludGVyZmFjZTogInB1YmxpYyIKICAgIGlkZW50aXR5X2FwaV92ZXJzaW9uOiAzCg==
kind: Secret
metadata:
  labels:
    clusterctl.cluster.x-k8s.io/move: "true"
  name: my-cluster-cloud-config
  namespace: default
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: cluster-openstack
spec:
  clusterNetwork:
    pods:
      cidrBlocks:
      - 192.168.0.0/16
    serviceDomain: cluster.local
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: K0smotronControlPlane # This tells that k0smotron should create the controlplane
    name: cluster-openstack
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1alpha7
    kind: OpenStackCluster  # This tells that CAPO should create the the worker
    name: cluster-openstack
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0smotronControlPlane # This is the config for the controlplane
metadata:
  name: cluster-openstack
spec:
  version: v1.27.2-k0s.0
  persistence:
    type: emptyDir
  service:
    type: LoadBalancer
    apiPort: 6443
    konnectivityPort: 8132
---
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha7
kind: OpenStackCluster
metadata:
  name: cluster-openstack
  namespace: default
  annotations:
    cluster.x-k8s.io/managed-by: k0smotron # This marks the base infra to be self managed. The value of the annotation is irrelevant, as long as there is a value.
spec:
  cloudName: openstack
  identityRef:
    kind: Secret
    name: cluster-openstack-cloud-config
  nodeCidr: a.b.c.d/24 #This node cidr corresponds to a network already created in OpenStack
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachineDeployment
metadata:
  labels:
    cluster.x-k8s.io/cluster-name: cluster-openstack
  name: cluster-openstack-worker-vms
  namespace: default
spec:
  clusterName: cluster-openstack
  replicas: 1
  selector:
    matchLabels: {}
  template:
    metadata:
      labels:
        cluster.x-k8s.io/cluster-name: cluster-openstack
    spec:
      clusterName: cluster-openstack
      failureDomain: nova
      bootstrap:
        configRef:
          apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
          kind: K0sWorkerConfigTemplate
          name: cluster-openstack-machine-config
      infrastructureRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1alpha7
        kind: OpenStackMachineTemplate
        name: cluster-openstack-worker-vm-template
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfigTemplate
metadata:
  name: cluster-openstack-machine-config
spec:
  template:
    spec:
      version: v1.27.2+k0s.0
      args:
        - --enable-cloud-provider
        - --kubelet-extra-args="--cloud-provider=external"
      # More details of the worker configuration can be set here
---
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha7
kind: OpenStackMachineTemplate
metadata:
  name: cluster-openstack-worker-vm-template
  namespace: default
spec:
  template:
    spec:
      cloudName: openstack
      flavor: dev.cfg # your flavor
      identityRef:
        kind: Secret
        name: cluster-openstack-cloud-config
      image: ubuntu-20.04 # your image
      sshKeyName: rsa2 # your RSA key name
      ports:
      - network:
          id: YourNetworkID # this corresponds to the network with the cidr provided above a.b.c.d/24
```

Once complete, your command line should display output similar to this:

```shell
kubectl get cluster,machine,kmc

NAME                                   CLUSTERCLASS   PHASE         AGE     VERSION
cluster.cluster.x-k8s.io/cluster-openstack                     Provisioned   135m

NAME                                                          CLUSTER       NODENAME   PROVIDERID                                          PHASE         AGE     VERSION
machine.cluster.x-k8s.io/cluster-openstack-worker-vms-drjzw-7699d      cluster2                 openstack:///f8f41440-36e6-4e9c-b941-16b95ee95277   Provisioned   135m

```

You can also check the status of the cluster deployment with `clusterctl`:
```shell
❯ clusterctl describe cluster cluster3
NAME                                                                     READY  SEVERITY  REASON                       SINCE  MESSAGE
Cluster/cluster3                                                         True                                          5d4h
├─ClusterInfrastructure - OpenStackCluster/cluster3
├─ControlPlane - K0smotronControlPlane/cluster3
└─Workers
    └─Machine/cluster3-worker-vms-929sw-nkhht                            True                                          5d4h
      └─BootstrapConfig - K0sWorkerConfig/cluster3-machine-config-tlg78
```


## Accessing the workload cluster

To access the child cluster we can get the kubeconfig for it with `clusterctl get kubeconfig cluster-openstack`. You can then save it to disk and/or import to your favorite tooling like [Lens](https://k8slens.dev).

## Deleting the cluster

For cluster deletion, do **NOT** use `kubectl delete -f k0smotron-cluster-with-capo.yaml` as that can result in orphan resources. Instead, delete the top level `Cluster` object. This approach ensures the proper sequence in deleting all child resources, effectively avoid orphan resources.

To do that, you can use the command `kubectl delete cluster cluster-openstack`
