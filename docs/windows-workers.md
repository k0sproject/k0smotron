## Overview

k0smotron supports Windows worker nodes as an experimental feature, allowing you to run Linux and Windows workloads in the same Kubernetes cluster. This capability is particularly useful for organizations maintaining both modern Linux services and Windows applications.

## Requirements

- At least one Linux worker node for CoreDNS
- Windows nodes with appropriate PowerShell remoting capabilities
- Calico CNI for networking

## Limitations

- Windows worker support is experimental
- Control plane must run on Linux
- Requires Linux worker for cluster DNS services

## Architecture

The control plane runs on Linux nodes only. Both Linux and Windows worker nodes can join the same cluster:

```
                      ┌─────────────────────────────────────┐
                      │           k0s Control Plane         │
                      │  (Linux - API Server, etcd, etc.)   │
                      └─────────────────────────────────────┘
                                         │
                            ┌─────────────────────────┐
                            ▼                         ▼
                ┌─────────────────────┐      ┌─────────────────────┐
                │  Linux Worker       │      │ Windows Worker      │
                └─────────────────────┘      └─────────────────────┘    
                │ - CNI (Calico)      │      │ - CNI (Calico)      │
                │ - CoreDNS           │      │ - Windows Workloads │
                │ - Linux Workloads   │      │   (.NET, IIS, SQL)  │
                │   (NGINX, Node.js)  │      └─────────────────────┘
                └─────────────────────┘                           
```

### Networking

k0s has native Calico support that provides networking for both Linux and Windows nodes

### DNS

CoreDNS runs on Linux workers and serves the entire cluster, including Windows pods. At least one Linux worker node is required in the cluster.

## Configuration

### K0sWorkerConfig

Windows worker nodes are configured using the `platform` field in `K0sWorkerConfig`:

```yaml
apiVersion: k0smotron.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: windows-worker
spec:
  version: v1.34.2+k0s.0
  platform: windows # Specify Windows platform (default is linux)
  provisioner:
    type: powershell # Specify provisioning format 
```

### Provisioning

Default cloud-init is supported for Windows, as well as two Windows-specific formats available:
- `cloud-init` - e.g. for RemoteMachine provider
- `powershell` - Standard PowerShell script
- `powershell-xml` - PowerShell wrapped in XML (AWS user data compatible)

Provisioner types by provider:

| Provider                | Provisioner Type |
|-------------------------|------------------|
| AWS                     | powershell-xml   |
| k0smotron RemoteMachine | cloud-init       |
| …                       | …                |

## Use Cases

Windows worker support enables:

- **Hybrid clusters**: Run Linux and Windows nodes in the same cluster
- **Gradual migration**: Containerize Windows applications incrementally
- **Unified operations**: Single control plane, API, and operational model
- **Legacy application support**: Run Windows-only workloads alongside modern services

## Full Example with AWS

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
  name: k0s-aws-test-mt-cp
  namespace: default
spec:
  template:
    spec:
      uncompressedUserData: false
      ami:
        # Replace with your AMI ID
        id: ami-0fa91bc90632c73c9 # Ubuntu in eu-north-1
      instanceType: t3.large
      iamInstanceProfile: nodes.cluster-api-provider-aws.sigs.k8s.io # Instance Profile created by `clusterawsadm bootstrap iam create-cloudformation-stack`
      cloudInit:
        # Makes CAPA use k0s bootstrap cloud-init directly and not via SSM
        # Simplifies the VPC setup as we do not need custom SSM endpoints etc.
        insecureSkipSecretsManager: true
      sshKeyName: <your-ssh-key-name>
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSMachineTemplate
metadata:
  name: k0s-aws-test-mt-worker
  namespace: default
spec:
  template:
    spec:
      publicIP: true
      ami:
        # Replace with your AMI ID
        id: ami-010e40c6557403885 # Windows in eu-north-1
      instanceType: t3.medium
      iamInstanceProfile: nodes.cluster-api-provider-aws.sigs.k8s.io # Instance Profile created by `clusterawsadm bootstrap iam create-cloudformation-stack`
      cloudInit:
        # Makes CAPA use k0s bootstrap cloud-init directly and not via SSM
        # Simplifies the VPC setup as we do not need custom SSM endpoints etc.
        insecureSkipSecretsManager: true
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0sControlPlane
metadata:
  name: aws-test
spec:
  replicas: 1
  version: v1.34.2+k0s.0
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
        network:
          provider: calico # Enable Calico networking
  machineTemplate:
    infrastructureRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
      kind: AWSMachineTemplate
      name: k0s-aws-test-mt-cp
      namespace: default
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSCluster
metadata:
  name: k0s-aws-test
  namespace: default
spec:
  region: eu-north-1
  sshKeyName: <your-ssh-key-name>
  controlPlaneLoadBalancer:
    healthCheckProtocol: TCP
  network:
    additionalControlPlaneIngressRules:
      - description: "k0s controller join API"
        protocol: tcp
        fromPort: 9443
        toPort: 9443
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachineDeployment
metadata:
  name: k0s-aws-test-md
  namespace: default
spec:
  clusterName: aws-test-cluster
  replicas: 1
  selector:
    matchLabels:
      cluster.x-k8s.io/cluster-name: aws-test-cluster
      pool: worker-pool-1
  template:
    metadata:
      labels:
        cluster.x-k8s.io/cluster-name: aws-test-cluster
        pool: worker-pool-1
    spec:
      clusterName: aws-test-cluster
      bootstrap:
        configRef:
          apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
          kind: K0sWorkerConfigTemplate
          name: k0s-aws-test-machine-config
      infrastructureRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
        kind: AWSMachineTemplate
        name: k0s-aws-test-mt-worker
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfigTemplate
metadata:
  name: k0s-aws-test-machine-config
spec:
  template:
    spec:
      version: v1.34.2+k0s.0
      platform: windows
      provisioner:
        type: powershell-xml
```