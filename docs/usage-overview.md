# Usage

Users can use k0smotron in two distinct ways:

## Standalone

:   In this mode, standalone k0smotron manages only k0s control planes
    running in the management cluster offering streamlined control and
    monitoring capabilities for k0s clusters.

    [comment]: # (k0smotron.io/v1beta1.Cluster)

## Cluster API integration

:   Alternatively, users can opt for k0smotron integration with Cluster API,
    enabling k0smotron to manage Kubernetes clusters across various infrastructure
    providers. This integration extends k0smotron management capabilities to a broader
    range of Kubernetes deployments.

    [comment]: # (k0smotron.io/v1beta1.Cluster)

    Within the context of Cluster API, k0smotron can serve several roles:

    * Control plane provider: k0smotron manages the control plane within the management cluster.
      It orchestrates the creation, scaling, and management of the Kubernetes control plane
      components, ensuring their proper functioning and high availability.

      [comment]: # (controlplane.cluster.x-k8s.io/v1beta1.K0smotronControlPlane)

    * Control plane bootstrap provider: k0smotron acts as a bootstrap provider for `Machine`
      resources that run the control plane components. It handles the initialization and
      configuration of these machines, ensuring they are properly set up to serve as part
      of the cluster control plane.

    * Bootstrap provider: k0smotron serves as the bootstrap provider for worker machines.
      It manages provisioning and configuring worker nodes, ensuring they are ready
      to run containerized workloads within the Kubernetes cluster.

    * Remote machine provider: k0smotron acts as an infrastructure provider, enabling
      the configuration of `Machine` resources on existing infrastructure over SSH.
      This enables users to leverage their existing infrastructure resources while
      still benefiting from the management capabilities provided by ClusterAPI and k0smotron.

!!! note "See also"

    [k0smotron installation](install.md)
