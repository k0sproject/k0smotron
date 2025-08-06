# k0smotron

## Overview

k0smotron is a powerful open-source tool for efficient management of [k0s](https://k0sproject.io/) Kubernetes clusters. It enables you to run Kubernetes control planes within a management cluster and with the integration of [Cluster API](https://cluster-api.sigs.k8s.io/) it streamlines various cluster operations, providing support for tasks such as provisioning, scaling, and upgrading clusters.

## Features

### Embedded Child Cluster Control Planes

k0smotron streamlines k0s control plane creation and management within your management cluster, reducing traditional operational overhead (e.g  installation, configuration, upgrades or backups). k0smotron encapsulates the control plane service as a pods (and other Kubernetes constructs) and provides an intuitive approach to cluster lifecycle management through ClusterAPI integration.

### Cluster API Support

Leverage k0smotron's compatibility with Cluster API. Benefit from Kubernetes cluster management across various infrastructures supported by the Cluster API ecosystem. k0smotron can operate as control plane, bootstrap and infrastructure provider for Cluster API.

### Cluster Provisioning

Easily provision Kubernetes clusters with k0smotron. Whether you are setting up clusters for development, testing, or production, k0smotron provides a straightforward process for cluster creation.

### Scaling Operations

Scale your clusters effortlessly to meet changing workload demands. k0smotron facilitates seamless scaling of both worker nodes and control planes.

### Cluster Upgrades

Stay up-to-date with Kubernetes releases by smoothly upgrading your clusters. k0smotron simplifies the process, ensuring minimal downtime during upgrades.

### Remote Machine Provider

Introducing k0smotron Anywhere, a Cluster API infrastructure provider that enables cluster provisioning on remote machines using SSH connections. Perfect for diverse infrastructure setups and for those environments where there's no existing Cluster API provider available.

## Use cases

### Development and CI/CD

In the process of continuous integration and end-to-end testing, a temporary Kubernetes Cluster is needed. With k0smotron, these clusters can be created on demand in a declarative way and thus integrated into the existing CI process with ease. This avoids cluster sprawl and long-living snowflake clusters.

### Edge Container Management

Kubernetes at the edge typically comes with the requirement of a low resource footprint. As a result, clusters with distributed roles are more challenging and a lot of single-node clusters are created. Managing the large number of clusters confronts us with almost impossible tasks.

Offloading the control plane means that the persistence layer of the cluster can run on dedicated hardware and workloads can be managed at the edge on devices dedicated to their purposes. With k0smotron worker nodes get ephemeral.

### Multi-cloud Cluster LCM

A multi-cloud strategy is essential these days. But managed Kubernetes offerings are often different in versioning or even the built-in tooling.

With k0smotron, you can run the control plane management cluster in a public or private cloud provider of your choice and the worker nodes in various clouds. With this you get a homogenized and unified cluster management, providing one flavor on all clouds and saving the costs for the highly available Control Plane in the cloud.

## Getting Started

Getting started with k0smotron is easy. Simply install the controller into an existing cluster:

```bash
kubectl apply --server-side=true -f https://docs.k0smotron.io/stable/install.yaml
```

You can also install k0smotron ClusterAPI providers via `clusterctl`:

```shell
clusterctl init --bootstrap k0sproject-k0smotron --control-plane k0sproject-k0smotron --infrastructure k0sproject-k0smotron
```

Like with any other Cluster API provider and Kubernetes controllers, the cluster operations are declarative. For example creating a new child cluster control plane within the management cluster can be done via creating a new resource in the Kubernetes API:

``` bash
kubectl apply -f - <<EOF
apiVersion: k0smotron.io/v1beta1
kind: Cluster
metadata:
  name: my-k0smotron
spec: {}
EOF
```

See [docs](https://docs.k0smotron.io/stable/usage-overview/) for further examples how to create different kinds of clusters on using various cloud environments.

## Documentation

Explore the comprehensive [documentation](https://docs.k0smotron.io/stable) for in-depth insights into k0smotron's capabilities, configurations, and usage scenarios.

## Contributing

We invite you to contribute to the growth and enhancement of k0smotron. To learn how you can leave your mark on the project, please read our [Contribution Guidelines.](https://docs.k0smotron.io/stable).

## License

k0smotron is licensed under the [Apache License 2.0](LICENSE).
