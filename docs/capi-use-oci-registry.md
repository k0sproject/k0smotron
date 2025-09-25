# OCI Registry Integration for k0smotron

This example demonstrates how to configure k0smotron to use a k0s binary from an OCI registry instead of relying on the default installation script.


## Prerquisites

For this setup, you need to use the control plane and bootstrap providers for k0smotron, together with your desired infrastructure provider. In this example, we’ll use the AWS infrastructure provider.

(See the [tutorial](https://cluster-api-aws.sigs.k8s.io/quick-start) on how to use AWS in CAPI for more details). Once you have a valid cluster to deploy the providers, run:

```cmd
clusterctl init --control-plane k0sproject-k0smotron --bootstrap k0sproject-k0smotron --infrastructure aws
```

## Configure `K0sControlPlane` for using and OCI registry

Configuring the `K0sControlPlane` to pull k0s from an OCI registry is straightforward. **The only requirement is that the machine being bootstrapped needs Oras CLI installed**. You can achieve this in two ways:

- By using `.preStartCommands` to install the Oras CLI on the machine before pulling the binary.
- By using a machine image with the Oras CLI pre-installed.

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: aws-test
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
    name: aws-test
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSMachineTemplate
metadata:
  name: aws-test
  namespace: default
spec:
  template:
    spec:
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
      uncompressedUserData: false
      sshKeyName: <your-ssh-key-name>
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0sControlPlane
metadata:
  name: aws-test
spec:
  replicas: 3
  version: v1.33.4+k0s.0
  updateStrategy: Recreate
  k0sConfigSpec:
    # OCI URL (digest reference) for the k0s binary blob
    downloadURL: oci://example.com/my-repo/k0s@sha256:abcdefg123456789
    # Install Oras CLI
    preStartCommands:
      - VERSION="1.3.0"
      - curl -LO "https://github.com/oras-project/oras/releases/download/v${VERSION}/oras_${VERSION}_linux_amd64.tar.gz"
      - mkdir -p oras-install/
      - tar -zxf oras_${VERSION}_*.tar.gz -C oras-install/
      - sudo mv oras-install/oras /usr/local/bin/
      - rm -rf oras_${VERSION}_*.tar.gz oras-install/
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
      name: aws-test
      namespace: default
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSCluster
metadata:
  name: aws-test
  namespace: default
spec:
  region: eu-west-1
  sshKeyName: <your-ssh-key-name>
  controlPlaneLoadBalancer:
    healthCheckProtocol: TCP
  network:
    additionalControlPlaneIngressRules:
      - description: "k0s controller join API"
        protocol: tcp
        fromPort: 9443
        toPort: 9443
```

As shown above, we use the `downloadURL` field to reference a k0s binary blob via its digest. The URL must use the `oci://` schema.

## Authentication

If your OCI registry requires authentication, you need to provide credentials in a `config.json` file, following the [Oras CLI authentication mechanism](https://oras.land/docs/how_to_guides/authentication/). You can make this file available to the node by adding it as a *file* entry containing the authentication credentials under the `files` field in the `K0sControlPlane` spec. For example:

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0sControlPlane
metadata:
  name: aws-test
spec:
  replicas: 3
  version: v1.33.4+k0s.0
  updateStrategy: Recreate
  k0sConfigSpec:
    # OCI URL (digest reference) for the k0s binary blob
    downloadURL: oci://example.com/my-private-repo/k0s@sha256:abcdefg123456789
    # We add a new file with a secret reference for the needed credentials used by Oras
    files:
    - contentFrom:
      secretRef:
        name: my-oras-config
        key: .dockerconfigjson
      path: /root/.docker/config.json
    preStartCommands:
      - VERSION="1.3.0"
      - curl -LO "https://github.com/oras-project/oras/releases/download/v${VERSION}/oras_${VERSION}_linux_amd64.tar.gz"
      - mkdir -p oras-install/
      - tar -zxf oras_${VERSION}_*.tar.gz -C oras-install/
      - sudo mv oras-install/oras /usr/local/bin/
      - rm -rf oras_${VERSION}_*.tar.gz oras-install/
      - export DOCKER_CONFIG=/root/.docker # In addition to downloading hours, we need to make oras use the proper `.docker/config.json` by setting the directoty of the desired config
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
      name: aws-test
      namespace: default
```

In this example, a new file entry is configured that references a secret containing the authentication credentials.

!!! note "Do not forget to set `DOCKER_CONFIG`"
    To let the Oras CLI use the authentication credentials, export the `DOCKER_CONFIG` environment variable in your `.preStartCommands`, so that it points to the directory containing `config.json` when the machine boots.