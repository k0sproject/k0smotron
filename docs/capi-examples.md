# Cluster API - Examples

This section presents a collection of examples showcasing the use of k0smotron as a Cluster API provider across various cloud platforms.

# Prerequisites

The examples herein require the following prerequisites:

## Management cluster

You must have an existing [management cluster](https://cluster-api.sigs.k8s.io/reference/glossary.html#management-cluster) for these examples. Ensure that `kubectl` or any other client tooling you use is configured to interact with this cluster.

If you don't have a management cluster yet, you can effortlessly create one using [k0s](https://docs.k0sproject.io/stable/install/).

## k0smotron

Install k0smotron on your management cluster by following this installation [guide](install.md).

## clusterctl

Lastly, you'll need `clusterctl` [installed](https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl) on your local machine.

Proceed with these examples once you've fulfilled these prerequisites. Each example demonstrates how k0smotron can serve as an efficient Cluster API provider in different environments.