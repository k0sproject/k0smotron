# Cluster API - AWS

In this guide, we show you how to use AWS for the worker plane with the k0smotron control plane.

## Prerequisites

See the common [prerequisites](capi-examples.md#prerequisites) for k0smotron.

Also, you need to have an AWS account and your AWS CLI configured with your credentials.

### Prepare the AWS infra provider

Prior to initiating a cluster, the configuration of the infrastructure provider is necessary. Each provider comes with its own unique set of prerequisites.

Follow the AWS Provider [installation guide](https://cluster-api-aws.sigs.k8s.io/getting-started.html#initialize-the-management-cluster) for detailed steps.

## Creating a cluster

As soon as the bootstrap and control-plane controllers are up and running you can apply the cluster manifests with the specifications of the cluster you want to provision.

!!! note "k0smotron is currently only able to work with [externally managed](https://cluster-api-aws.sigs.k8s.io/topics/bring-your-own-aws-infrastructure.html) cluster infrastructure."
    This is because in CAPA there is no way to disable it to provision all control plane related infrastructure (VPC, ELB, etc.).
    This also renders k0smotron unable to dynamically edit the `AWSCluster` API endpoint details. Make sure your VPC and subnets you are planning to use fullfill the [needed prerequisites](https://cluster-api-aws.sigs.k8s.io/topics/bring-your-own-aws-infrastructure.html#prerequisites).

Here is an example:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: k0s-aws-test
  namespace: default
spec:
  clusterNetwork:
    pods:
      cidrBlocks: [10.244.0.0/16]
    services:
      cidrBlocks: [10.96.0.0/12]
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: K0smotronControlPlane # This tells that k0smotron should create the controlplane
    name: k0s-aws-test
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: AWSCluster
    name: k0s-aws-test
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0smotronControlPlane # This is the config for the controlplane
metadata:
  name: k0s-aws-test
spec:
  k0sVersion: v1.27.2-k0s.0
  persistence:
    type: emptyDir
  service:
    type: LoadBalancer
    apiPort: 6443
    konnectivityPort: 8132
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSCluster
metadata:
  name: k0s-aws-test
  namespace: default
  annotations:
    cluster.x-k8s.io/managed-by: k0smotron # This marks the base infra to be self managed. The value of the annotation is irrelevant, as long as there is a value.
spec:
  region: eu-central-1
  sshKeyName: jhennig-key
  network:
    vpc:
      id: vpc-12345678901234567 # default VPCs ID
    subnets:
      - id: subnet-099730c9ea2e42134
        availabilityZone: eu-central-1a
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: Machine
metadata:
  name: k0s-aws-test-0
  namespace: default
spec:
  clusterName: k0s-aws-test
  failureDomain: eu-central-1a
  bootstrap:
    configRef: # This triggers our controller to create cloud-init secret
      apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
      kind: K0sWorkerConfig
      name: k0s-aws-test-0
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: AWSMachine
    name: k0s-aws-test-0
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSMachine
metadata:
  name: k0s-aws-test-0
  namespace: default
spec:
  ami:
    # Ubuntu 22.04
    id: ami-0989fb15ce71ba39e
  instanceType: t3.large
  iamInstanceProfile: nodes.cluster-api-provider-aws.sigs.k8s.io
  cloudInit:
    # Makes CAPA use k0s bootstrap cloud-init directly and not via SSM
    # Simplifies the VPC setup as we do not need custom SSM endpoints etc.
    insecureSkipSecretsManager: true
  subnet:
    # Make sure this matches the failureDomain in the Machine, i.e. you pick the subnet ID for the AZ
    id: subnet-099730c9ea2e42134
  iamInstanceProfile: nodes.cluster-api-provider-aws.sigs.k8s.io
  sshKeyName: jhennig-key
    
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: k0s-aws-test-0
spec:
---
```

As we are using self-managed infrastructure we need to manually mark the infrastructure ready. This can be done with `kubectl patch AWSCluster k0s-aws-test --type=merge --subresource status --patch 'status: {ready: true}'`.

Once you apply the manifests to the management cluster and mark the infrastructure ready it'll take couple of minutes to provision everything. In the end you should see something like this:

```shell
% kubectl get cluster,machine
NAME                                    PHASE         AGE   VERSION
cluster.cluster.x-k8s.io/k0s-aws-test   Provisioned   46m   

NAME                                      CLUSTER        NODENAME   PROVIDERID                                 PHASE         AGE   VERSION
machine.cluster.x-k8s.io/k0s-aws-test-0   k0s-aws-test              aws:///eu-central-1a/i-05f2de7da41dc542a   Provisioned   46m   
```

## Accessing the workload cluster

To access the workload (a.k.a child) cluster we can get the kubeconfig for it with `clusterctl get kubeconfig k0s-aws-test`. You can then save it to disk and/or import to your favorite tooling like [Lens](https://k8slens.dev)

## Deleting the cluster

When deleting the cluster, do **NOT** use `kubectl delete -f my-aws-cluster.yaml` as that'll easily result into dangling AWS resources. Instead only delete the top level `Cluster` object. Deleting the top level `Cluster` object will propagate the deletion of all child resources and with a proper ordering.
