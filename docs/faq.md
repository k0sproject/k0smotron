
# FAQ

The k0smotron FAQ is designed to quickly provide answers to common questions
about the project. We strive to include answers to the most common questions and
uncertainties that users encounter, along with helpful tips and references
to step-by-step instructions whenever possible.

If you are unable to find the information you seek in this FAQ, please extend
your search into the overall k0smotron documentation set.

Of course, we welcome your suggestions for the k0smotron FAQ, as well as your contributions!

## How does k0smotron differ from other multi-cluster management solutions?

As a multi-cluster management solution, k0smotron provides a distinct advantage
over the competition in that it allows you to leverage the full capabilities of
the management cluster. Most other such solutions, such as Tanzu and Rancher,
are unable to offer the same, as they typically provision a VM-based control
plane and worker planes in the same infrastructure.

## How does k0smotron differ from managed Kubernetes providers?

k0smotron offers heightened functionality over such managed Kubernetes
providers as GKE and AKS, including:

- Greater control and flexibility

    With k0smotron, you have full control over your cluster configurations
    within your existing Kubernetes cluster.

- Agnostic worker node connectability

    By allowing you to connect worker nodes from any infrastructure, k0smotron
    provides greater freedom and compatibility.

- Cost efficiency

    In leveraging your existing Kubernetes cluster, k0smotron can reduce your
    costs, such as those associated with managing separate clusters or
    funding additional resources.

- Homogeneous setup

    k0smotron installs with a single command. This one command installs the
    k0smotron controller manager, all of the related CRD definitions, and the
    necessary RBAC rules. This approach ensures that you have a consistent
    configuration across clusters. It also simplifies maintenance and
    management tasks.

## What is the relation of k0smotron to Cluster API?

k0smotron is a fully compliant [Cluster API](https://cluster-api.sigs.k8s.io/)
provider for [k0s](https://k0sproject.io/) that can be used with any Cluster
API compatible tooling. In addition, k0smotron is a Cluster API infrastructure
provider with which you can use SSH connections to provision clusters on remote
machines.

## What do we mean by "from pets to cattle"?

A *pets* service model describes carefully tended resources that are nurtured
with care and given relatable names. When such resources have issues, it is
immediately noticed, and time and effort are expended to bring them back to a
healthy state.

In a *cattle* model, the resources in question are not given the same level of
careful attention, and they are tagged rather than named. Such resources are
typically configured in an identical sense, and if the health of one fails it
is quickly replaced without much thought.

As cluster control planes are somewhat static, these are usually managed in a
*pets* sense. In contrast, using an operator such as k0smotron to manage k0s
control planes within an existing Kubernetes cluster is more of a *cattle*
approach, allowing for cluster management that is more scalable and flexible.
Such an approach makes it easier to maintain a consistent and homogeneous setup
across all your clusters, while also allowing you to take advantage of the high
availability and auto-healing features of Kubernetes.
