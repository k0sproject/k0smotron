# Cluster API - Examples

On these pages you'll find set of examples how to use k0smotron as a Cluster API provider for various cloud providers.

# Prerequisites

All the examples assume following prerequisites.

## Management cluster

You need to have an existing cluster you'll use as the [management cluster](https://cluster-api.sigs.k8s.io/reference/glossary.html#management-cluster). Naturally we expect you point your `kubectl` or any other client tooling you use to use that cluster.

If you do not yet have a management cluster in your hands remember that you can create one using [k0s](https://docs.k0sproject.io/stable/install/) super easily.

## k0smotron

To install k0smotron on the management cluster follow the installation [guide](install.md).

## clusterctl

You need to have `clusterctl` [installed](https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl).


