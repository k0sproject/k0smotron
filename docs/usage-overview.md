# k0smotrong usage

k0smotron can be use either as a "standalone" manager for Kubernetes control planes or as a Cluster API provider for several ClusterAPI "roles".

## Standalone

In standalone mode k0smotron will manage ONLY the controlplanes running in the management cluster. To get started creating and managing control planes with k0smotron see [cluster creation docs](cluster.md).

## Cluster API provider

k0smotron can act as a ClusterAPI provider in several cases:

- [ControlPlane provider](capi-controlplane.md): k0smotron manages the controlplane _within_ the management cluster
- [Control plane Bootstrap provider](capi-controlplane-bootstrap.md): k0smotron acts as a bootstrap provider for `Machine`s running the control plane
- [Bootstrap provider](capi-bootstrap.md): k0smotron acts as the bootstrap (config) provider for worker machines
- [Remote machine provider](capi-remote.md): k0smotron acts as a infrastructure provider, enabling configuring `Machine`s on existing infrastructure over SSH