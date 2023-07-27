# Cluster API - AWS

This example demonstrates how k0smotron can be used with CAPA (Cluster API Provider Amazon Web Services).

## Prerequisites

Before starting this example, ensure that you have met the [general prerequisites](capi-examples.md#prerequisites). In addition to those, you should also have appropriate AWS credentials available and the AWS CLI configured on your local machine.

### Prepare the AWS infra provider

Before launching a cluster, it's crucial to set up your infrastructure provider. Each provider has its unique prerequisites and configuration steps.

Follow the AWS Provider [installation guide](https://cluster-api-aws.sigs.k8s.io/getting-started.html#initialize-the-management-cluster) for detailed steps.

## Creating a child cluster

Once all the controllers are up and running, you can apply the cluster manifests containing the specifications of the cluster you want to provision.

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
    name: k0s-aws-test-cp
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: AWSCluster
    name: k0s-aws-test
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0smotronControlPlane # This is the config for the controlplane
metadata:
  name: k0s-aws-test-cp
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
  sshKeyName: ssh-key
  network:
    vpc:
      id: vpc-12345678901234567 # Machines will be created in this VPC
    subnets:
      - id: subnet-099730c9ea2e42134 # Machines will be created in this Subnet
        availabilityZone: eu-central-1a
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachineDeployment
metadata:
  name: k0s-aws-test-md
  namespace: default
spec:
  clusterName: k0s-aws-test
  replicas: 1
  selector:
    matchLabels:
      cluster.x-k8s.io/cluster-name: k0s-aws-test
      pool: worker-pool-1
  template:
    metadata:
      labels:
        cluster.x-k8s.io/cluster-name: k0s-aws-test
        pool: worker-pool-1
    spec:
      clusterName: k0s-aws-test
      failureDomain: eu-central-1a
      bootstrap:
        configRef: # This triggers our controller to create cloud-init secret
          apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
          kind: K0sWorkerConfigTemplate
          name: k0s-aws-test-machine-config
      infrastructureRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
        kind: AWSMachineTemplate
        name: k0s-aws-test-mt
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSMachineTemplate
metadata:
  name: k0s-aws-test-mt
  namespace: default
spec:
  template:
    ami:
      # Replace with your AMI ID
      id: ami-0989fb15ce71ba39e # Ubuntu 22.04 in eu-central-1 
    instanceType: t3.large
    iamInstanceProfile: nodes.cluster-api-provider-aws.sigs.k8s.io # Instance Profile created by `clusterawsadm bootstrap iam create-cloudformation-stack`
    cloudInit:
      # Makes CAPA use k0s bootstrap cloud-init directly and not via SSM
      # Simplifies the VPC setup as we do not need custom SSM endpoints etc.
      insecureSkipSecretsManager: true
    subnet:
      # Make sure this matches the failureDomain in the Machine, i.e. you pick the subnet ID for the AZ
      id: subnet-099730c9ea2e42134
    additionalSecurityGroups:
      - id: sg-01ce46c31291e3447 # Needs to be belong to the subnet
    sshKeyName: jhennig-key
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfigTemplate
metadata:
  name: k0s-aws-test-machine-config
spec:
  template:
    spec:
      version: v1.27.2+k0s.0
      # More details of the worker configuration can be set here
---
```

As we are using self-managed infrastructure we need to manually mark the infrastructure ready. This can be accomplished using the following command: `kubectl patch AWSCluster k0s-aws-test --type=merge --subresource status --patch 'status: {ready: true}'.`

After applying the manifests to the management cluster and confirming the infrastructure readiness, allow a few minutes for all components to provision. Once complete, your command line should display output similar to this:

```shell
% kubectl get cluster,machine
NAME                                    PHASE         AGE   VERSION
cluster.cluster.x-k8s.io/k0s-aws-test   Provisioned   16m   

NAME                                         CLUSTER        NODENAME   PROVIDERID                                 PHASE         AGE   VERSION
machine.cluster.x-k8s.io/k0s-aws-test-md-0   k0s-aws-test              aws:///eu-central-1a/i-05f2de7da41dc542a   Provisioned   16m   
```

You can also check the status of the cluster deployment with `clusterctl`:
```shell
% clusterctl describe cluster  
NAME                                                   READY  SEVERITY  REASON                    SINCE  MESSAGE          
Cluster/k0s-aws-test                                   True                                       25m                      
├─ClusterInfrastructure - AWSCluster/k0s-aws-test                                                                             
├─ControlPlane - K0smotronControlPlane/k0s-aws-test-cp                                                                           
└─Workers                                                                                                                  
  └─Other
```

### Networking Options
k0smotron, running in a management cluster in AWS, supports flexible networking options, allowing you to choose between Network Load Balancers (NLB) and Classic Elastic Load Balancers (ELB) based on your requirements for exposing the Control Planes.

If you prefer using an NLB instead of ELB, you must specify annotations for the Service in the `k0smotronControlPlane`. These annotations guide the AWS Cloud Controller Manager (CCM) or the AWS Load Balancer Controller to create the respective services.

```yaml
 [...] 
  service:
    type: LoadBalancer
    apiPort: 6443
    konnectivityPort: 8132
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
```

For scenarios involving Classic ELBs or NLBs without special options, the AWS CCM can be utilized.

#### Internal NLB
If you aim to use the NLB and set the schema to `internal`, the target group attribute `preserve_client_ip.enabled=false` is required due to "hairpinning" or "NAT loopback". In such cases, the AWS CCM cannot be used because it doesn't support setting Target Group Attributes. Therefore, the AWS Load Balancer Controller, which has the ability to set Target Group Attributes, becomes necessary. Follow [this guide](https://docs.aws.amazon.com/eks/latest/userguide/aws-load-balancer-controller.html) to install the AWS Load Balancer Controller.

```yaml
 [...] 
  service:
    type: LoadBalancer
    apiPort: 6443
    konnectivityPort: 8132
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-type: "external" # AWS Loadbalancer Controller creates a NLB when type is "external"
      service.beta.kubernetes.io/aws-load-balancer-internal: "true"
      service.beta.kubernetes.io/aws-load-balancer-target-group-attributes: preserve_client_ip.enabled=false
```

***Note:*** Please make sure that the Security Group does allow the access to the NLB on port 6443 and 8132 from the management cluster nodes. This access is crucial for Cluster API (CAPI), Cluster API Provider for AWS (CAPA), and k0smotron, as they require access to the Control Plane API. Additionally, the port for the Konnectivity service need to be accessible from worker nodes.

## Accessing the workload cluster

To access the child cluster we can get the kubeconfig for it with `clusterctl get kubeconfig k0s-aws-test`. You can then save it to disk and/or import to your favorite tooling like [Lens](https://k8slens.dev).

## Deleting the cluster

For cluster deletion, do **NOT** use `kubectl delete -f my-aws-cluster.yaml` as that will result into orphan AWS resources. Instead, delete the top level `Cluster` object. This approach ensures the proper sequence in deleting all child resources, effectively avoid orphan resources.