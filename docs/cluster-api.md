# k0smotron as Cluster API provider

k0smotron can act as a [Cluster API](https://cluster-api.sigs.k8s.io/) provider for both control planes and for `Machine` bootstrapping.

## Control Plane provider

When k0smotron acts as a [control plane provider](https://cluster-api.sigs.k8s.io/developer/architecture/controllers/control-plane.html) it will create and manage the cluster control plane within the management cluster, just as in the [standalone](cluster.md) case.

## Bootstrap provider

k0smotron can also act as a [bootstrap provider](https://cluster-api.sigs.k8s.io/developer/architecture/controllers/bootstrap.html) for worker nodes you want to manage via Cluster API. When k0smotron detects a new node that needs to be added to the cluster it will automatically create a new [join token]() needed for the node and creates the provisioning cloud-init script for the node. Once Cluster API controllers sees the node initialization script in place (in a secret) the [infrastructure provider](https://cluster-api.sigs.k8s.io/developer/providers/machine-infrastructure.html) will create the needed resources (usually VMs in cloud provider infrastructure) with the k0smotron created cloud-init script.
