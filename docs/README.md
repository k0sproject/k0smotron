# k0smotron - The Kubernetes control plane manager

![k0s logo](img/k0s-logo-full-color-light.svg)

k0smotron is a Kubernetes control plane manager. Deploy and run Kubernetes control planes on any existing cluster. k0smotron is well suited for

- Edge
- IoT
- Dev clusters

## Installation

To install k0smotron on your Kubernetes cluster run the following command:

```bash
kubectl apply -f https://docs.k0smotron.io/stable/install.yaml
```

## FAQ

### How is k0smotron different from typical multi-cluster management solutions such as Tanzu, Rancher etc.?

Most of the existing multi-cluster management solutions provision specific infrastructure for the control planes, in most cases VMs. In all of the cases we've looked at the worker plane infrastructure is also provisioned in the same infrastructure with the control plane and thus not allowing you to fully utilize the capabilities of the management cluster.

### How is this different for managed Kubernetes providers? 

- Control and Flexibility: k0smotron gives you full control over your cluster configurations within your existing Kubernetes cluster, offering unparalleled flexibility.
- Bring Your Own Workers: Unlike managed Kubernetes providers, k0smotron allows you to connect worker nodes from any infrastructure, providing greater freedom and compatibility.
- Cost Efficiency: By leveraging your existing Kubernetes cluster, k0smotron helps reduce costs associated with managing separate clusters or paying for additional resources.
- Homogeneous Setup: k0smotron ensures a consistent configuration across clusters, simplifying maintenance and management tasks.

### What is the relation of k0smotron with [Cluster API](https://cluster-api.sigs.k8s.io/)?

While k0smotron currently is a "standalone" controller for k0s control planes we're looking to expand this as a full Cluster API provider. Or rather set pf providers as were looking to implement both ControlPlane and Bootstrap providers.
