# Cluster API - OpenStack

This example demonstrates how k0smotron can be used with CAPO (Cluster API Provider OpenStack).

## Preparations

Before proceeding, ensure your management cluster meets the following requirements:

### Management Cluster Requirements

- A healthy Kubernetes cluster with cluster-admin access
- Cluster API initialized with the OpenStack infrastructure provider:
  ```bash
  clusterctl init --infrastructure openstack
  ```
- k0smotron installed (docs: https://docs.k0smotron.io/stable/install/#software-prerequisites)
- A LoadBalancer implementation for the hosted control plane service (OpenStack CCM/Octavia or MetalLB)
- OpenStack Cinder CSI driver installed and operational on the management cluster with:
  - Valid OpenStack credentials secret in the `kube-system` namespace
  - A default StorageClass configured (if you install the openstack-cinder-csi helm chart, then the StorageClass will be created for you)

For more details on Cluster API Provider OpenStack see it's [docs](https://github.com/kubernetes-sigs/cluster-api-provider-openstack/tree/main/docs).

### Architecture Considerations

The hosted control plane etcd persistent volume resides on the management cluster, requiring functional storage. The workload cluster receives its own Cloud Controller Manager (CCM) and Container Storage Interface (CSI) drivers through k0s configuration extensions.

## Setup Instructions

### 1. OpenStack Credentials

To be able to provision the OpenStack provider infrastructure, you will need to setup your OpenStack credentials.

**Get the openstack Clouds.yaml**

Download your “OpenStack clouds.yaml file” (Login -> API Access -> Download OpenStack clouds.yaml file)

Add "verify: false" to your clouds.yaml to avoid having the "x509: certificate signed by unknown authority" error.

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

Create a base64-encoded OpenStack configuration file. The cluster manifest expects a secret named `openstack-cloud-config` in the `default` namespace with a `clouds.yaml` key.

Encode your `clouds.yaml` file:

**Linux:**
```bash
base64 -w0 clouds.yaml > clouds.yaml.b64
```

**macOS:**
```bash
base64 -b0 clouds.yaml > clouds.yaml.b64
```

Copy the single-line contents of `clouds.yaml.b64` and paste it into the cluster manifest at `data.clouds.yaml` (replace `<BASE64_OF_clouds.yaml_HERE>`).

### 2. Install Cinder CSI on Management Cluster

Create the OpenStack credentials secret and install the Cinder CSI driver:

```bash
kubectl -n kube-system create secret generic openstack-cloud-config \
  --from-file=clouds.yaml=./clouds.yaml

helm repo add openstack https://kubernetes.github.io/cloud-provider-openstack/
helm repo update

helm upgrade --install openstack-csi openstack/openstack-cinder-csi -n kube-system \
  --set secret.enabled=true \
  --set secret.create=false \
  --set secret.name=openstack-cloud-config \
  --set storageClass.enabled=true \
  --set storageClass.name=cinder-sc \
  --set storageClass.defaultClass=true \
  --set storageClass.allowVolumeExpansion=true \
  --set csi.plugin.nodePlugin.kubeletDir=/var/lib/k0s/kubelet
```

*Note: If you are using a kubernetes based management cluster, you will need to set the `csi.plugin.nodePlugin.kubeletDir` to `/var/lib/kubelet`.*

Verify the installation:

```bash
kubectl get csidriver
kubectl -n kube-system get pods | grep -i cinder
kubectl get sc
```


### 3. Configure and Deploy the Cluster

Before applying the manifests, update the following fields:

- Update network, router, and subnet names in `OpenStackCluster` and `OpenStackMachineTemplate` to match your OpenStack environment
- Insert the base64-encoded `clouds.yaml` content prepared in step 1

#### Hosted Control Plane Cluster Manifests (`openstack-hcp-cluster.yaml`)

```yaml
apiVersion: v1
data:
  cacert: null
  clouds.yaml: <BASE64_OF_clouds.yaml_HERE>
kind: Secret
metadata:
  name: openstack-cloud-config
  namespace: default
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: openstack-hcp-cluster
  namespace: default
spec:
  clusterNetwork:
    pods:
      cidrBlocks: [10.244.0.0/16] # Adjust accordingly
    serviceDomain: cluster.local
    services:
      cidrBlocks: [10.96.0.0/12] # Adjust accordingly
  controlPlaneRef:
    apiGroup: controlplane.cluster.x-k8s.io
    kind: K0smotronControlPlane
    name: openstack-hcp-cluster-cp
  infrastructureRef:
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: OpenStackCluster
    name: openstack-hcp-cluster
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: OpenStackCluster
metadata:
  name: openstack-hcp-cluster
  namespace: default
spec:
  externalNetwork:
    filter:
      name: public
  identityRef:
    cloudName: openstack
    name: openstack-cloud-config
    region: RegionOne
  network:
    filter:
      name: k8s-clusterapi-cluster-default-capo-test
  router:
    filter:
      name: k8s-clusterapi-cluster-default-capo-test
  subnets:
  - filter:
      name: k8s-clusterapi-cluster-default-capo-test
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: MachineDeployment
metadata:
  name: openstack-hcp-cluster-md
  namespace: default
spec:
  clusterName: openstack-hcp-cluster
  replicas: 1
  selector:
    matchLabels:
      cluster.x-k8s.io/cluster-name: openstack-hcp-cluster
  template:
    metadata:
      labels:
        cluster.x-k8s.io/cluster-name: openstack-hcp-cluster
    spec:
      bootstrap:
        configRef:
          apiGroup: bootstrap.cluster.x-k8s.io
          kind: K0sWorkerConfigTemplate
          name: openstack-hcp-cluster-machine-config
      clusterName: openstack-hcp-cluster
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: OpenStackMachineTemplate
        name: openstack-hcp-cluster-mt
      version: v1.32.6
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: OpenStackMachineTemplate
metadata:
  name: openstack-hcp-cluster-mt
  namespace: default
spec:
  template:
    spec:
      flavor: m1.medium
      identityRef:
        cloudName: openstack
        name: openstack-cloud-config
        region: RegionOne
      image:
        filter:
          name: ubuntu-22.04-x86_64
      ports:
      - network:
          filter:
            name: k8s-clusterapi-cluster-default-capo-test
      securityGroups:
      - filter:
          name: default
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0smotronControlPlane
metadata:
  name: openstack-hcp-cluster-cp
  namespace: default
spec:
  controllerPlaneFlags:
  - --enable-cloud-provider=true
  - --debug=true
  etcd:
    autoDeletePVCs: false
    image: quay.io/k0sproject/etcd:v3.5.13
    persistence:
      size: 1Gi
  image: ghcr.io/k0sproject/k0s:v1.32.6-k0s.0  # pinned GHCR tag to avoid rate limits with docker hub
  k0sConfig:
    apiVersion: k0s.k0sproject.io/v1beta1
    kind: ClusterConfig
    metadata:
      name: k0s
    spec:
      extensions:
        helm:
          charts:
          - chartname: openstack/openstack-cloud-controller-manager
            name: openstack-ccm
            namespace: kube-system
            order: 1
            values: |
              secret:
                enabled: true
                name: openstack-cloud-config
                create: false
              nodeSelector: null
              tolerations:
                - key: node.cloudprovider.kubernetes.io/uninitialized
                  value: "true"
                  effect: NoSchedule
                - key: node-role.kubernetes.io/control-plane
                  effect: NoSchedule
                - key: node-role.kubernetes.io/master
                  effect: NoSchedule
              extraEnv:
                - name: OS_CCM_REGIONAL
                  value: "true"
              extraVolumes:
                - name: flexvolume-dir
                  hostPath:
                    path: /usr/libexec/kubernetes/kubelet-plugins/volume/exec
                - name: k8s-certs
                  hostPath:
                    path: /etc/kubernetes/pki
              extraVolumeMounts:
                - name: flexvolume-dir
                  mountPath: /usr/libexec/kubernetes/kubelet-plugins/volume/exec
                  readOnly: true
                - name: k8s-certs
                  mountPath: /etc/kubernetes/pki
                  readOnly: true
            version: 2.31.1
          - chartname: openstack/openstack-cinder-csi
            name: openstack-csi
            namespace: kube-system
            order: 2
            values: |
              storageClass:
                enabled: true
                delete:
                  isDefault: true
                  allowVolumeExpansion: true
                retain:
                  isDefault: false
                  allowVolumeExpansion: false
              secret:
                enabled: true
                name: openstack-cloud-config
                create: false   # set to true if you want the chart to create the Secret in workload cluster
              csi:
                plugin:
                  nodePlugin:
                    kubeletDir: /var/lib/k0s/kubelet   # workload cluster nodes run k0s
            version: 2.31.2
          repositories:
          - name: openstack
            url: https://kubernetes.github.io/cloud-provider-openstack/
      network:
        calico:
          mode: vxlan
        clusterDomain: cluster.local
        podCIDR: 10.244.0.0/16
        provider: calico
        serviceCIDR: 10.96.0.0/12
  replicas: 1
  service:
    apiPort: 6443
    konnectivityPort: 8132
    type: LoadBalancer
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: K0sWorkerConfigTemplate
metadata:
  name: openstack-hcp-cluster-machine-config
  namespace: default
spec:
  template:
    spec:
      args:
      - --enable-cloud-provider
      - --kubelet-extra-args="--cloud-provider=external"
      - --debug=true
      version: v1.32.6+k0s.0
```

## Deployment and Monitoring

### Deploy the Cluster

Apply the cluster manifest:

```bash
kubectl apply -f openstack-hcp-cluster.yaml
```

### Monitor Cluster Creation

Monitor the hosted control plane deployment:

```bash
# Watch etcd PVC binding and pod startup on the management cluster
kubectl -n default get pvc
kubectl -n default get pods -w

# Verify the LoadBalancer service receives an external IP address
kubectl -n default get svc openstack-hcp-cluster-cp -o wide
```

Expected components in the `default` namespace:
- `kmc-openstack-hcp-cluster-etcd-0` pod in Running state
- `kmc-openstack-hcp-cluster-0` (controller) pod in Running state  
- `openstack-hcp-cluster-cp` service (LoadBalancer) with an assigned EXTERNAL-IP

The control plane will become operational within a few minutes, followed by worker nodes joining the cluster.

### Verify Cluster Creation

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

## Post-Deployment Configuration

### Retrieve Workload Cluster Access

Obtain the workload cluster kubeconfig:

```bash
kubectl -n default get secrets | grep -i kubeconfig
kubectl -n default get secret <NAME> -o jsonpath='{.data.value}' | base64 -d > workload-cluster.kubeconfig
```

You can also save it to disk and/or import to your favorite tooling like [Lens](https://k8slens.dev).

### Configure OpenStack Integration

If the cluster manifest has `create: false` for secrets (as shown in the example), manually create the OpenStack credentials in the workload cluster:

```bash
kubectl --kubeconfig workload-cluster.kubeconfig -n kube-system create secret generic openstack-cloud-config \
  --from-file=clouds.yaml=./clouds.yaml
```

### Accessing the workload cluster

Validate the workload cluster components and nodes:

```bash
kubectl --kubeconfig workload-cluster.kubeconfig get nodes
kubectl --kubeconfig workload-cluster.kubeconfig get pods -n kube-system
```
## Deleting the cluster

For cluster deletion, do **NOT** use `kubectl delete -f openstack-hcp-cluster.yaml` as that can result in orphan resources. Instead, delete the top level `Cluster` object. This approach ensures the proper sequence in deleting all child resources, effectively avoiding orphan resources.

To do that, you can use the command `kubectl delete cluster openstack-hcp-cluster`

## Conclusion

This guide demonstrated how to deploy a Kubernetes cluster on OpenStack using a hosted control plane architecture with Cluster API Provider OpenStack (CAPO) and k0smotron. The key benefits of this approach include:

### Advantages of Hosted Control Planes

- **Resource Efficiency**: Control plane components run as pods on the management cluster, reducing the infrastructure footprint
- **Simplified Management**: Centralized control plane management across multiple workload clusters
- **High Availability**: Leverages the management cluster's infrastructure for control plane resilience
- **Cost Optimization**: Eliminates the need for dedicated control plane nodes in each workload cluster

For production deployments, ensure proper sizing of the management cluster to handle multiple hosted control planes and their associated workloads.

