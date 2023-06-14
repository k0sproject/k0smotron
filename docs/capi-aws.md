# Cluster API - AWS

In this guide, we show you how to use AWS for the worker plane with the k0smotron control plane.

## Prerequisites

We assume that a k0s cluster has already been installed and is ready for use. 
The kubeconfig should be available in ~/.kube/config and the context should already be set to the k0s cluster. 

Next to the cluster we will need the following tools installed on your system:

1. clusterctl CLI
    - https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl
2. clusterawsadm CLI
    - https://github.com/kubernetes-sigs/cluster-api-provider-aws#clusterawsadm

Also, you need to have an AWS account and your AWS CLI configured with your credentials.

### Prepare the infra provider

Prior to initiating a cluster, the configuration of the infrastructure provider is necessary. Each provider comes with its own unique set of prerequisites.

The AWS infrastructure provider requires the `clusterawsadm` to be installed:
``` bash
curl -L https://github.com/kubernetes-sigs/cluster-api-provider-aws/releases/download/v0.0.0/clusterawsadm-darwin-amd64 -o clusterawsadm
chmod +x clusterawsadm
sudo mv clusterawsadm /usr/local/bin
```

`clusterawsadm` is CLI which will prepare your AWS account, so it can be used as ClusterAPI Infrastructure Provider. 

To begin, create the environment variables that specify the AWS account to be utilized, in case they have not been previously defined:
``` bash
export AWS_REGION=<your-region-eg-us-east-1>
export AWS_ACCESS_KEY_ID=<your-access-key>
export AWS_SECRET_ACCESS_KEY=<your-secret-access-key>
```

In case you are using multi-factor authentication, you will need:

``` bash
export AWS_SESSION_TOKEN=<session-token> 
```

`clusterawsadm` will use these details to create a CloudFormation stack in your AWS account with IAM resources:

``` bash
clusterawsadm bootstrap iam create-cloudformation-stack
```

Ensure that the credentials are encoded and securely stored as a Kubernetes secret:

``` bash
export AWS_B64ENCODED_CREDENTIALS=$(clusterawsadm bootstrap credentials encode-as-profile)
```

### Initialize the management cluster

The initialization of Cluster API components requires the `clusterctl` to be installed:
``` bash
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.4.3/clusterctl-darwin-amd64 -o clusterctl
chmod +x clusterctl
sudo mv clusterctl /usr/local/bin
```

To initialize the management cluster with AWS infrastrcture provider you need to run:

```
clusterctl init --core cluster-api --infrastructure aws
```

For more details on AWS Kubernetes Cluster API Provider AWS see it's [docs](https://cluster-api-aws.sigs.k8s.io/).


## Creating a cluster

As soon as the bootstrap and control-plane controllers are up and running you can apply the cluster manifests with the specifications of the cluster you want to provision.

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
spec:
  region: eu-central-1
  sshKeyName: jhennig-key
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: Machine
metadata:
  name: k0s-aws-test-0
  namespace: default
spec:
  clusterName: k0s-aws-test
  bootstrap:
    configRef: # This triggers our controller to create cloud-init secret
      apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
      kind: K0sWorkerConfig
      name: k0s-aws-test-0
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: AWSMachineTemplate
    name: k0s-aws-test-0
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSMachineTemplate
metadata:
  name: k0s-aws-test-0
  namespace: default
spec:
  template:
    spec:
      instanceType: t3.large
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

In the case of `AWSCluster.spec.controlPlaneEndpoint` you can add any valid address. k0smotron will overwrite these are automatically once it gets the control plane up-and-running. You do need to specify some placeholder address as the `AWSCluster` object has those marked as mandatory fields.

Once you apply the manifests to the management cluster it'll take couple of minutes to provision everything. In the end you should see something like this:


```
% kubectl get cluster,machine
NAME                                    PHASE         AGE     VERSION
cluster.cluster.x-k8s.io/k0s-aws-test   Provisioned   4m14s   

NAME                                         CLUSTER        NODENAME   PROVIDERID          PHASE         AGE     VERSION
machine.cluster.x-k8s.io/k0s-aws-test-0      k0s-aws-test                                  Provisioned   4m15s
```

## Accessing the workload cluster

To access the workload (a.k.a child) cluster we can get the kubeconfig for it with `clusterctl get kubeconfig k0s-aws-test`. You can then save it to disk and/or import to your favorite tooling like [Lens](https://k8slens.dev)