# Introduction

This section describes how to install k0smotron on top of an existing k0s
Kubernetes cluster that allows for creation and management of the k0s
control plane.

# Hardware requirements

k0smotron does not require any special hardware aside from the one required for
k0s. For details on k0s hardware requirements, see
[k0s documentation](https://docs.k0sproject.io/stable/system-requirements/).

# Software prerequisites

k0smotron requires the following software to be preinstalled:

* Kubernetes cluster with `kubectl` installed locally. In this documentation
  set, we use the [k0s Kubernetes distribution](https://docs.k0sproject.io/stable/install/).
  For Cluster API integration, use a Cluster API
  [management cluster](https://cluster-api.sigs.k8s.io/reference/glossary.html#management-cluster).
* For Cluster API integration, [clusterctl](https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl)
  installed locally.
* Optional. CSI provider for persistent storage in managed clusters.
* Optional. Load balancer provider for ensuring high availability of the
  control plane.

# Full installation

A full k0smotron installation implies the following components:

* k0smotron operator
* Custom Resource Definitions
* Role-based access control rules
* Bootstrap provider
* Infrastructure provider
* Control plane provider

To install the full version of k0smotron:

```bash
kubectl apply -f https://docs.k0smotron.io/{{{ extra.k0smotron_version }}}/install.yaml
```

Before creating your first control planes, select the required use case described in
[k0smotron usage](usage-overview.md).

# Per-module installation for Cluster API

k0smotron is compatible with `clusterctl` and can act as a Cluster API
bootstrap, infrastructure, and control plane provider. You can use
`clusterctl` to install each k0smotron Cluster API module separately:

```bash
clusterctl init --bootstrap k0sproject-k0smotron \
                --control-plane k0sproject-k0smotron \
                --infrastructure k0sproject-k0smotron
```

To start using the k0smotron Cluster API, refer to [Cluster API](cluster-api.md).
