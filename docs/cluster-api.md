# k0smotron as Cluster API provider

k0smotron can act as a [Cluster API](https://cluster-api.sigs.k8s.io/) provider in several different roles.

## Control Plane provider - in-cluster

When k0smotron acts as a [control plane provider](https://cluster-api.sigs.k8s.io/developer/architecture/controllers/control-plane.html) it will create and manage the cluster control plane within the management cluster, just as in the [standalone](cluster.md) case.

## Control Plane - out-of-cluster

k0smotron can function also as a "traditional" contol plane provider where the control plane is running on CAPI managed `Machines`s.

## Bootstrap provider

k0smotron can also act as a [bootstrap provider](https://cluster-api.sigs.k8s.io/developer/architecture/controllers/bootstrap.html) for worker nodes you want to manage via Cluster API. When k0smotron detects a new node that needs to be added to the cluster it will automatically create a new [join token]() needed for the node and creates the provisioning cloud-init script for the node. Once Cluster API controllers sees the node initialization script in place (in a secret) the [infrastructure provider](https://cluster-api.sigs.k8s.io/developer/providers/machine-infrastructure.html) will create the needed resources (usually VMs in cloud provider infrastructure) with the k0smotron created cloud-init script.

## Remote Machine provider

k0smotron also provides a ClusterAPI provider to manage and bootstrap cluster `Machines` remotely via SSH connection. This allows managing the clusters in environment where there's no existing ClusterAPi provider available. Such environments could be for example bare metal environments.

## Cluster autoscaling

[Cluster Autoscaler](https://github.com/kubernetes/autoscaler) works with [ClusterAPI](https://cluster-api.sigs.k8s.io/tasks/automated-machine-management/autoscaling). You need to deploy an "instance" of autoscaler per child cluster in order for it to work properly. If you deploy autoscaler via Helm, here's some values to look out:

| Value                        | Why?                                                                                                                                                                               |
|------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `autodiscovery.clusterName`  | Select the child cluster you want to autoscale. E.g. `my-cluster`                                                                                                                  |
| `cloudProvider`              | Set to `clusterapi` to make autoscaler work with ClusterAPI                                                                                                                        |
| `clusterAPIKubeconfigSecret` | Set to the kubeconfig secret created by CAPI. E.g. `my-cluster-kubeconfig`                                                                                                         |
| `clusterAPIMode`             | Set to `kubeconfig-incluster`. Essentially this tells that the child cluster API is accessed with the kubeconfig from the secret and management cluster via `incluster` kubeconfig |

!!! note "RBAC finetuning needed with Helm deployed autoscaler"
     The Helm chart does not take into account the need for autoscaler to access the implementation specific resources in `infrastructure.cluster.x-k8s.io` group. To fix that you need to modify* the `ClusterRole` to include e.g.
     
     ```yaml
     - verbs:
        - get
        - list
        - update
        - watch
      apiGroups:
        - infrastructure.cluster.x-k8s.io
      resources:
        - '*' # You can of course limit this to your specific infrastructure types only, e.g. `AWSMachineDeployment` etc.
     ```

     *) Happy to get feedback whether there's a better workaround for this.
