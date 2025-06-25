# Cluster API - AWS on ec2 instances

This example demonstrates how k0smotron can be used with CAPA (Cluster API Provider Amazon Web Services) to deploy 
a cluster with hosted control plane and workers in AWS.

## Prerequisites

Before starting this example, ensure that you have met the [general prerequisites](capi-examples.md#prerequisites). In addition to those, you should also have appropriate AWS credentials available and the AWS CLI configured on your local machine.

### Prepare the AWS infra provider

Before launching a cluster, it's crucial to set up your infrastructure provider. Each provider has its unique prerequisites and configuration steps.

Follow the AWS Provider [installation guide](https://cluster-api-aws.sigs.k8s.io/getting-started.html#initialize-the-management-cluster) for detailed steps.

## Creating a child cluster

Once all the controllers are up and running, you can apply the cluster manifests containing the specifications of the cluster you want to provision.

!!! warning "AWS limits userdata to 16kb"
    AWS has a limit of 16kb for userdata. As k0smotron generates certificates and other files it might reach the limit, so you may need to compress it.
    This can be done by setting `AWSMachineTemplate.spec.template.spec.uncompressedUserData` to `false` in the AWSMachineTemplate manifest. 

Here is an example:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: aws-test-cluster
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
    kind: K0sControlPlane
    name: aws-test
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: AWSCluster
    name: k0s-aws-test
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSMachineTemplate
metadata:
  name: k0s-aws-test-mt
  namespace: default
spec:
  template:
    spec:
      uncompressedUserData: false 
      ami:
        # Replace with your AMI ID
        id: ami-0008aa5cb0cde3400 # Ubuntu 20.04 in eu-west-1
      instanceType: t3.large
      publicIP: true
      iamInstanceProfile: nodes.cluster-api-provider-aws.sigs.k8s.io # Instance Profile created by `clusterawsadm bootstrap iam create-cloudformation-stack`
      cloudInit:
        # Makes CAPA use k0s bootstrap cloud-init directly and not via SSM
        # Simplifies the VPC setup as we do not need custom SSM endpoints etc.
        insecureSkipSecretsManager: true
      sshKeyName: <your-ssh-key-name>
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0sControlPlane
metadata:
  name: aws-test
spec:
  replicas: 3
  version: v1.30.3+k0s.0
  updateStrategy: Recreate
  k0sConfigSpec:
    args:
      - --enable-worker
    k0s:
      apiVersion: k0s.k0sproject.io/v1beta1
      kind: ClusterConfig
      metadata:
        name: k0s
      spec:
        api:
          extraArgs:
            anonymous-auth: "true"
        telemetry:
          enabled: false
  machineTemplate:
    infrastructureRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
      kind: AWSMachineTemplate
      name: k0s-aws-test-mt
      namespace: default
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSCluster
metadata:
  name: k0s-aws-test
  namespace: default
spec:
  region: eu-west-1
  sshKeyName: <your-ssh-key-name>
  controlPlaneLoadBalancer:
    loadBalancerType: nlb
    healthCheckProtocol: TCP
  network:
    additionalControlPlaneIngressRules:
      - description: "k0s controller join API"
        protocol: tcp
        fromPort: 9443
        toPort: 9443
```

```shell
% kubectl get cluster,machine
NAME                                        CLUSTERCLASS   PHASE         AGE   VERSION
cluster.cluster.x-k8s.io/aws-test-cluster                  Provisioned   24h   

NAME                                     CLUSTER            NODENAME        PROVIDERID                              PHASE      AGE    VERSION
machine.cluster.x-k8s.io/aws-test-0      aws-test-cluster   aws-test-0      aws:///eu-west-1c/i-04ea1b27f52210bec   Running    24h    v1.30.3+k0s.0
machine.cluster.x-k8s.io/aws-test-1      aws-test-cluster   aws-test-1      aws:///eu-west-1a/i-0c34ca4e0450acd64   Running    23h    v1.30.3+k0s.0
machine.cluster.x-k8s.io/aws-test-2      aws-test-cluster   aws-test-2      aws:///eu-west-1b/i-0ac2d7fb7ad92dff6   Running    23h    v1.30.3+k0s.0
   
```
