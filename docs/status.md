# Project status

We’re really early on with the project and as always with any young project there are probably some sharp corners. But with the help of the open source community we plan to iron out those in the coming months.

At this point we can not give much guarantees over backwards and forwards compatibility so expect things to break a bit with upcoming releases of k0smotron.

We wanted to release k0smotron as early as possible to see whether there is in general interest of running k0s control planes as Kubernetes resources. We are excited about the potential that k0smotron brings to the table, and we look forward to seeing how it can transform the way you manage your Kubernetes deployments.


## Cluster API

One of the directions we're looking at is the have k0smotron working as a Cluster API provider for both `ControlPlane` and worker `Bootstrap` providers. What this means is that you could utilize Cluster API to provision both the control plane, within the management cluster, and worker nodes in your favourite infrastructure supporting cluster API.

## Known limitations

Some of the areas we know have lot of shortcomings currently:
- Control Plane configurability: As you know, k0s itself has lot of configurability, we plan to enable full configurability of the k0s control plane
- Control plane exposing: Currently k0smotron only supports `NodePort` and `LoadBalancer` type services. While that is in itself quite ok quite often there's need to further configure e.g. annotations etc. on the created service to make them play nice with cloud provider implementations.
- Updates: While k0smotron is able to update the cluster controlplane easily the update does not strecth into worker nodes.
- 
