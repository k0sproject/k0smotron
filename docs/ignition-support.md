# Ignition Support

k0smotron generates bootstrap data in *cloud-init* format by default. Some OS distributions used for workload clusters nodes rely on **Ignition** for early-boot provisioning instead of cloud-init, requiring bootstrap data to be provided in Ignition format. To support these environments, k0smotron can alternatively generate bootstrap data in Ignition format for distributions such [Fedora CoreOS](https://docs.fedoraproject.org/en-US/fedora-coreos/producing-ign/) or [Flatcar Container Linux](https://www.flatcar.org/docs/latest/provisioning/ignition/).

This guide explains how to deploy an AWS workload cluster using Ignition. More documentation about Ignition support in AWS infrastructure provider can be found [here](https://cluster-api-aws.sigs.k8s.io/topics/ignition-support).

### Initialize the management cluster

Before workload clusters can be deployed, Cluster API components must be deployed to the management cluster.

Initialize the management cluster:

```sh
export AWS_REGION=eu-west-1
export AWS_ACCESS_KEY_ID=<your-access-key>
export AWS_SECRET_ACCESS_KEY=<your-secret-access-key>
export AWS_SESSION_TOKEN=<your-session-token> 

# Workload clusters need to call the AWS API as part of their normal operation.
# The following command creates a CloudFormation stack which provisions the
# necessary IAM resources to be used by workload clusters.
clusterawsadm bootstrap iam create-cloudformation-stack

# The management cluster needs to call the AWS API in order to manage cloud
# resources for workload clusters. The following command tells clusterctl to
# store the AWS credentials provided before in a Kubernetes secret where they
# can be retrieved by the AWS provider running on the management cluster.
export AWS_B64ENCODED_CREDENTIALS=$(clusterawsadm bootstrap credentials encode-as-profile)

# Enable the feature gates controlling Ignition bootstrap.
export EXP_BOOTSTRAP_FORMAT_IGNITION=true # Used by the AWS provider

# Initialize the management cluster.
clusterctl init --infrastructure aws --control-plane k0sproject-k0smotron --bootstrap k0sproject-k0smotron
```

!!! warning "Set `EXP_BOOTSTRAP_FORMAT_IGNITION` environment variable"
    Before deploying CAPA using `clusterctl`, make sure you set `EXP_KUBEADM_BOOTSTRAP_FORMAT_IGNITION=true` environment variables to enable experimental Ignition bootstrap support.

### Create a workload cluster

After deploying the bootstrap, control plane, and infrastructure provider controllers, we can create a workload cluster configured to use Ignition as bootstraping engine when using OS distributions supporting this engine. In our example, we will use Flatcar Container Linux as AMI for the cluster nodes.

Configure access to the machines by using your *SSH Key Name* and *SSH Public Key* in the following following manifest and apply it:

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: ignition-test-cluster
  namespace: ignition-test
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
    apiGroup: controlplane.cluster.x-k8s.io
    kind: K0sControlPlane
    name: ignition-test-cluster-aws-test
  infrastructureRef:
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: AWSCluster
    name: ignition-test-cluster
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSMachineTemplate
metadata:
  name: ignition-test-cluster-aws-test-mt
  namespace: ignition-test
spec:
  template:
    spec:
      ignition:
        storageType: ClusterObjectStore
        version: "3.4"
      ami:
        id: ami-00d12617b68dbc62f # Flatcar Container Linux stable 3975.2.1 (HVM) in eu-west-1
      instanceType: t3.large
      publicIP: true
      iamInstanceProfile: control-plane.cluster-api-provider-aws.sigs.k8s.io
      uncompressedUserData: false
      sshKeyName: <your-ssh-key-name> # Your SSH key here
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlane
metadata:
  name: ignition-test-cluster-aws-test
  namespace: ignition-test
spec:
  replicas: 3
  version: v1.30.2+k0s.0
  updateStrategy: Recreate
  k0sConfigSpec:
    # Flatcar, as inmutable OS, needs k0s in /opt/bin. It cannot write k0s binary in the default /usr/local/bin.
    k0sInstallDir: /opt/bin 
    ignition:
      variant: flatcar
      version: 1.1.0
      additionalConfig: |
        variant: flatcar
        version: 1.1.0
        passwd:
          users:
            - name: core
              ssh_authorized_keys:
                - ssh-rsa <your-ssh-public-key>
    args:
      - --enable-worker
      - --debug
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
    spec:
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: AWSMachineTemplate
        name: ignition-test-cluster-aws-test-mt
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: AWSCluster
metadata:
  name: ignition-test-cluster
  namespace: ignition-test
spec:
  # Ignition bootstrap data needs to be stored in an S3 bucket so that nodes can
  # read them at boot time. Store Ignition bootstrap data in the following bucket.
  s3Bucket:
    controlPlaneIAMInstanceProfile: control-plane.cluster-api-provider-aws.sigs.k8s.io
    # For simplicity and following AWS documentation: by default clusterawsadm creates IAM roles 
    # to only allow interacting with buckets with cluster-api-provider-aws- prefix to reduce the 
    # permissions of CAPA controller, so all bucket names should use this prefix.
    name: cluster-api-provider-aws-ignition-test
    nodesIAMInstanceProfiles:
    - nodes.cluster-api-provider-aws.sigs.k8s.io
  region: eu-west-1
  sshKeyName: <your-ssh-key-name> # Your SSH key here
  controlPlaneLoadBalancer:
    healthCheckProtocol: TCP
  network:
    additionalControlPlaneIngressRules:
      - description: "k0s controller join API"
        protocol: tcp
        fromPort: 9443
        toPort: 9443
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: MachineDeployment
metadata:
  name: ignition-test-cluster-aws-test-md
  namespace: ignition-test
spec:
  clusterName: ignition-test-cluster
  replicas: 1
  template:
    spec:
      clusterName: ignition-test-cluster
      bootstrap:
        configRef: # This triggers our controller to create cloud-init secret
          apiGroup: bootstrap.cluster.x-k8s.io
          kind: K0sWorkerConfigTemplate
          name: ignition-test-cluster-machine-config
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: AWSMachineTemplate
        name: ignition-test-cluster}-aws-test-mt
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: K0sWorkerConfigTemplate
metadata:
  name: ignition-test-cluster-machine-config
  namespace: ignition-test
spec:
  template:
    spec:
      version: v1.30.2+k0s.0
      # Flatcar, as inmutable OS, needs k0s in /opt/bin. It cannot write k0s binary in the default /usr/local/bin.
      k0sInstallDir: /opt/bin 
      ignition:
        variant: flatcar
        version: 1.1.0
        additionalConfig: |
          variant: flatcar
          version: 1.1.0
          passwd:
            users:
              - name: core
                ssh_authorized_keys:
                  - ssh-rsa <your-ssh-public-key>
```

Ignition bootstrap is enabled implicitly by configuring an `ignition` section for `K0sControlPlane.spec.k0sConfigSpec` and `K0sWorkerConfigTemplate.spec.template.spec` resources. When an ignition section with a supported variant and version is present (for example flatcar), k0smotron generates Ignition-formatted bootstrap data instead of the default cloud-init format. Supported variants and their versions can be found [here](https://coreos.github.io/butane/specs/#butane-specifications-and-ignition-specifications).

k0smotron allows extending the generated Ignition bootstrap data using the `additionalConfig` field. In this example, `additionalConfig` is used to configure SSH access for the default `core` user by injecting an SSH public key, allowing access to the machine after provisioning.

Additionally, because Flatcar is an immutable operating system, it does not allow writing the k0s binary to the default `/usr/local/bin` location. For this reason, an alternative installation directory is specified using `k0sInstallDir`.

We can check the state of our recently created cluster by running:

```shell
$ clusterctl describe cluster ignition-test-cluster -nignition-test
NAME                                                             REPLICAS AVAILABLE READY UP TO DATE STATUS REASON            SINCE  MESSAGE
Cluster/ignition-test-cluster                                    4/4      4         4     4          True   Available         7m19s
├─ClusterInfrastructure - AWSCluster/ignition-test-cluster                                           True   NoReasonReported  9m29s
├─ControlPlane - K0sControlPlane/ignition-test-cluster-aws-test  3/3                3     3
│ └─3 Machines...                                                         3         3     0          True   Ready             7m25s  See ignition-test-cluster-aws-test-86hdh, ignition-test-cluster-aws-test-8qtnr, ...
└─Workers
  └─MachineDeployment/ignition-test-cluster-aws-test-md          1/1      1         1     1          True   Available         7m19s
    └─Machine/ignition-test-cluster-aws-test-md-9fbps-t77jh      1        1         1     1          True   Ready             7m19s
```

Once cluster is created, we can access to it and check everything is working as expected.

```shell
# Retrieve workload cluster kubeconfig for accesing it
$ clusterctl get kubeconfig ignition-test-cluster -nignition-test > ignition-test-cluster.conf
```

```shell
# List nodes in the workload cluster
$ kubectl --kubeconfig ignition-test-cluster.conf get nodes
NAME                                            STATUS     ROLES           AGE     VERSION
ignition-test-cluster-aws-test-86hdh            Ready      control-plane   8m15s   v1.30.2+k0s
ignition-test-cluster-aws-test-8qtnr            Ready      control-plane   10m     v1.30.2+k0s
ignition-test-cluster-aws-test-bw4b5            Ready      control-plane   9m21s   v1.30.2+k0s
ignition-test-cluster-aws-test-md-9fbps-t77jh   Ready      <none>          10m     v1.30.2+k0s
```


