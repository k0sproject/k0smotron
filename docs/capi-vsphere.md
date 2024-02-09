# Cluster API - VMware

This example demonstrates how k0smotron can be used with CAPV (Cluster API Provider vSphere).

**Table of Contents**

- [Cluster API - VMware](#cluster-api---vmware)
  * [Setting the scene](#setting-the-scene)
  * [Preparations](#preparations)
    + [Configure clusterctl on the local machine](#configure-clusterctl-on-the-local-machine)
    + [Deploy Cluster API in the management cluster](#deploy-cluster-api-in-the-management-cluster)
    + [(Optional) IPAM IP pool creation](#optional-ipam-ip-pool-creation)
  * [(Optional) MetalLB as Load Balancer solution in the management cluster](#optional-metallb-as-load-balancer-solution-in-the-management-cluster)
  * [Operating child clusters](#operating-child-clusters)
    + [Generate a child cluster definition using the template](#generate-a-child-cluster-definition-using-the-template)
      - [Control plane in Pods](#control-plane-in-pods)
      - [Control plane in VMs](#control-plane-in-vms)
    + [Deploy the child clusters](#deploy-the-child-clusters)
    + [Observe the child cluster objects](#observe-the-child-cluster-objects)
    + [Deleting the child cluster](#deleting-the-child-cluster)

## Setting the scene

To use Cluster API we will bring three environments:
1. Local machine -- the machine from where some of the commands will be executed
2. Management cluster -- Kubernetes cluster that will be used to control and manage child clusters
3. Child cluster -- k0s cluster that will be spinned up with the help of k0smotron

## Preparations

Before starting this example, ensure that you have met the [general prerequisites](capi-examples.md#prerequisites).

### Configure clusterctl on the local machine

1. Create the cluster-api configuration file in your development machine.
```sh
mkdir ~/.cluster-api
vim ~/.cluster-api/clusterctl.yaml
```
2. Put values according to your environment in this file:

```yaml
## -- Controller settings -- ##
VSPHERE_USERNAME: "vi-admin@vsphere.local"                    # The username used to access the remote vSphere endpoint
VSPHERE_PASSWORD: "admin!23"                                  # The password used to access the remote vSphere endpoint

## -- Required workload cluster default settings -- ##
VSPHERE_SERVER: "10.0.0.1"                                    # The vCenter server IP or FQDN
VSPHERE_DATACENTER: "SDDC-Datacenter"                         # The vSphere datacenter to deploy the management cluster on
VSPHERE_DATASTORE: "DefaultDatastore"                         # The vSphere datastore to deploy the management cluster on
VSPHERE_NETWORK: "VM Network"                                 # The VM network to deploy the management cluster on
VSPHERE_RESOURCE_POOL: "*/Resources"                          # The vSphere resource pool for your VMs
VSPHERE_FOLDER: "vm"                                          # The VM folder for your VMs. Set to "" to use the root vSphere folder
VSPHERE_TEMPLATE: "ubuntu-1804-kube-v1.17.3"                  # The VM template to use for your management cluster.
CONTROL_PLANE_ENDPOINT_IP: "192.168.9.230"                    # the IP that kube-vip is going to use as a control plane endpoint
VIP_NETWORK_INTERFACE: "ens192"                               # The interface that kube-vip should apply the IP to. Omit to tell kube-vip to autodetect the interface.
VSPHERE_TLS_THUMBPRINT: "..."                                 # sha1 thumbprint of the vcenter certificate: openssl x509 -sha1 -fingerprint -in ca.crt -noout
EXP_CLUSTER_RESOURCE_SET: "true"                              # This enables the ClusterResourceSet feature that we are using to deploy CSI
VSPHERE_SSH_AUTHORIZED_KEY: "ssh-rsa AAAAB3N..."              # The public ssh authorized key on all machines in this cluster.
                                                              #  Set to "" if you don't want to enable SSH, or are using another solution.
VSPHERE_STORAGE_POLICY: ""                                    # This is the vSphere storage policy. Set it to "" if you don't want to use a storage policy.
"CPI_IMAGE_K8S_VERSION": "v1.29.0"                            # The version of the vSphere CPI image to be used by the CPI workloads
                                                              #  Keep this close to the minimum Kubernetes version of the cluster being created.
CSI_INSECURE: "1"
K0S_VERSION: "v1.29.1+k0s.1"
K0S_CP_VERSION: "v1.29.1-k0s.1"
NODE_IPAM_POOL_NAME: "ipam-ip-pool"
NODE_IPAM_POOL_API_GROUP: "ipam.cluster.x-k8s.io"
NODE_IPAM_POOL_KIND: "InClusterIPPool"
NAMESERVER: "8.8.8.8"
providers:
  - name: incluster
    url: https://github.com/kubernetes-sigs/cluster-api-ipam-provider-in-cluster/releases/latest/ipam-components.yaml
    type: IPAMProvider

```

### Deploy Cluster API in the management cluster

From your local machine run the following command to initialize the management cluster with vSphere infrastructure provider and additional IPAM provider:

```
clusterctl init --infrastructure vsphere --ipam incluster
```

*NOTE:* In order to initialize Cluster API on you Kubernetes management cluster, you need to have kubeconfig (or set context with `kubectl`) to the cluster.

*NOTE:* IPAM provider is optional here. If you're using network that has DHCP enabled, you can remove IPAM provider initialization.

For more details on Cluster API Provider vSphere see it's [docs](https://github.com/kubernetes-sigs/cluster-api-provider-vsphere/tree/main/docs).

### (Optional) IPAM IP pool creation

1. Define IP pool that will be used to assign IP addresses to a child cluster machines in `capi-ipam.yaml`:

```yaml
apiVersion: ipam.cluster.x-k8s.io/v1alpha1
kind: InClusterIPPool
metadata:
  name: ipam-ip-pool
spec:
  subnet: 192.168.117.0/24
  gateway: 192.168.117.1
  start: 192.168.117.152
  end: 192.168.117.180
```

2. Deploy the IPAM configuration: 
```
kubectl apply -f capi-ipam.yaml
```

## (Optional) MetalLB as Load Balancer solution in the management cluster

You may require to deploy MetalLB if you don't have Load Balancer solution in your management cluster. 
*NOTE:* Load Balancer service type will be used only for the scenario, when you put you k0s control plane in pods (see [Control plane in Pods](#control-plane-in-pods) section)

Classic vSphere does not have a built-in solution that can be leveraged on Kubernetes level to create LoadBalancer service type. In order to make it possible, one of the common solution is to use MetalLB (please find it's docs [here](https://metallb.universe.tf/))

1. Configure MetalLB IP address pool which will be used to assing IP addresses from (please adapt it for your network):

```yaml
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: example-pool
  namespace: metallb-system
spec:
  addresses:
  - 192.168.10.100-192.168.10.150
```

2. Create MetalLB L2advertisement object:

```yaml
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  annotations:
  name: example-l2-adv
  namespace: metallb-system
spec:
  ipAddressPools:
  - example-pool
```

## Operating child clusters

Once all the controllers are up and running in the management cluster, you can apply the cluster manifests containing the specifications of the child cluster you want to provision.

### Generate a child cluster definition using the template

There are two options for child cluster control plane with k0smotron:

#### Control plane in Pods:

If you want to have child cluster [control plane in a Pods](https://docs.k0smotron.io/stable/capi-controlplane/), use [this template](capi-vsphere-tmpl-cp-in-pods.yaml): 

```sh
clusterctl generate cluster my-cluster --control-plane-machine-count 1 --worker-machine-count 1 --from k0scluster-tmpl-cp-in-pods.yaml > my-cluster.yaml
```

#### Control plane in VMs:

If you want to have child cluster [control plane in separate VMs](https://docs.k0smotron.io/stable/capi-controlplane-bootstrap/), use [that template](capi-vsphere-tmpl-cp-in-pods.yaml) (CP will consist of 3 controllers):

```sh
clusterctl generate cluster my-cluster --worker-machine-count 1 --from k0scluster-tmpl-cp-in-vms.yaml > my-cluster.yaml 
```

### Deploy the child clusters

1. Deploy the child cluster: `kubectl apply -f my-cluster.yaml`. You can check the deployment status in the management cluster.

2. To obtain kubeconfig of the child cluster execute this command: `kubectl get secret my-cluster-kubeconfig -o jsonpath='{.data.value}' | base64 -d > ~/.kube/child.conf`

### Observe the child cluster objects

```shell
# kubectl get cluster,machine,kmc                                                                                                                   

NAME                                  CLUSTERCLASS   PHASE         AGE   VERSION
cluster.cluster.x-k8s.io/my-cluster                  Provisioned   13h   

NAME                                                   CLUSTER      NODENAME                      PROVIDERID                                       PHASE         AGE   VERSION
machine.cluster.x-k8s.io/my-cluster-0                  my-cluster                                 vsphere://4215e794-c281-5cde-8193-95df9521cf08   Provisioned   13h   v1.29.1
machine.cluster.x-k8s.io/my-cluster-1                  my-cluster                                 vsphere://4215efc3-303e-938d-3787-c4cc6143f722   Provisioned   13h   v1.29.1
machine.cluster.x-k8s.io/my-cluster-2                  my-cluster                                 vsphere://4215b207-1052-e130-54fe-fd124594ed2b   Provisioned   13h   v1.29.1
machine.cluster.x-k8s.io/my-cluster-md-0-vr6s6-ndl64   my-cluster   my-cluster-md-0-vr6s6-ndl64   vsphere://4215eae5-3456-9bc5-4b56-b00dda520b7d   Running       13h   v1.29.1+k0s.1

```

You can also check the status of the cluster deployment with `clusterctl`:
```shell
# clusterctl describe cluster my-cluster
NAME                                                        READY  SEVERITY  REASON  SINCE  MESSAGE                             
Cluster/my-cluster                                          True                     13h                                         
├─ClusterInfrastructure - VSphereCluster/my-cluster         True                     13h                                         
├─ControlPlane - K0sControlPlane/my-cluster                                                                                      
│ └─3 Machines...                                           True                     13h    See my-cluster-0, my-cluster-1, ...  
└─Workers                                                                                                                        
  └─MachineDeployment/my-cluster-md-0                       True                     12h                                         
    └─Machine/my-cluster-md-0-vr6s6-ndl64                   True                     12h                                         
      └─BootstrapConfig - K0sWorkerConfig/my-cluster-htrzb                                                          
```

### Deleting the child cluster

For cluster deletion, do **NOT** use `kubectl delete -f my-cluster.yaml` as that can result in orphan resources. Instead, delete the top level `Cluster` object. This approach ensures the proper sequence in deleting all child resources, effectively avoid orphan resources.

To do that, you can use the command `kubectl delete cluster my-cluster`
