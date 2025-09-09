# Installation

This section describes how to install k0smotron on top of an existing k0s
Kubernetes cluster that allows for creation and management of the k0s
control plane.

!!! warning "Pre-requisites"

    Before installing k0smotron, ensure that you have [cert-manager](https://cert-manager.io/docs/installation/) installed when using the `kubectl apply` method.  
    If you use `clusterctl`, cert-manager is installed automatically as part of the process.

!!! note "TL;DR"

    ```bash
    kubectl apply --server-side=true -f https://docs.k0smotron.io/{{{ extra.k0smotron_version }}}/install.yaml
    ```

## Installation modes

k0smotron can be deployed in two modes:

### 1. Standalone

This mode installs only the k0smotron operator: 

```bash
kubectl apply --server-side=true -f https://docs.k0smotron.io/{{{ extra.k0smotron_version }}}/install-standalone.yaml
```

For more details, see the [Standalone](usage-overview.md#standalone) usage section.

### 2. Cluster API integration

Deploys k0smotron as a full Cluster API provider (**bootstrap, control plane, and infrastructure**).  
This installation embeds the standalone components, so there is no need to install standalone separately. For more details, see the [Cluster API integration](usage-overview.md#cluster-api-integration) usage section.


There are two options for installing in CAPI mode:

#### Declarative deployment with `kubectl apply`:

```bash
kubectl apply --server-side=true -f https://docs.k0smotron.io/{{{ extra.k0smotron_version }}}/install.yaml
```

This installs **all components**: k0smotron operator, bootstrap provider, control plane provider, and infrastructure provider.
It requires cert-manager to be preinstalled.

!!! note "TL;DR"

    In order to run k0smotron CAPI controllers, Cluster API controllers must be installed first.

#### Per-module installation for Cluster API

```bash
clusterctl init --bootstrap k0sproject-k0smotron \
                --control-plane k0sproject-k0smotron \
                --infrastructure k0sproject-k0smotron
```

In this case, `clusterctl` also ensures that `cert-manager` is installed automatically.

To start using the k0smotron Cluster API, refer to [Cluster API](cluster-api.md).

## Known limitations

Areas in which k0smotron is currently limited include:

* Control plane exposure

    Currently k0smotron only supports `ClusterIP`, `NodePort`, and
    `LoadBalancer` services, and often it is necessary to further configure
    created services to ease their interaction with cloud provider
    implementations.

* Updates prevalence

    Although k0smotron can easily update the cluster control plane, in
    standalone mode such updates do not extend to worker nodes.

## Hardware requirements

k0smotron does not require any special hardware for workloads aside from
the one required for k0s. For details on k0s hardware requirements for
workloads, see [k0s documentation](https://docs.k0sproject.io/stable/system-requirements/).

## Software prerequisites

k0smotron requires the following software to be preinstalled:

* Kubernetes management cluster.
  In this documentation set, we use the
  [k0s Kubernetes distribution](https://docs.k0sproject.io/stable/install/)
  as a management cluster.
  For Cluster API integration, you can use a
  [Cluster API cluster](https://cluster-api.sigs.k8s.io/reference/glossary.html#management-cluster).
* `kubectl` installed locally.
* [cert-manager](https://cert-manager.io/docs/installation/) installed in the management cluster.
* For Cluster API integration:

  * [clusterctl](https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl)
    installed locally.
  * Configured cloud provider. In this documentation set, we describe
    configuration examples for the following providers: AWS, Docker,
    Hetzner Cloud, OpenStack, vSphere. For setup instructions, refer to the
    official documentation of the selected cloud provider.

* Optional. CSI provider for persistent storage in managed clusters.
* Optional. Load balancer provider for ensuring high availability of the
  control plane.