# k0smotron

An open source control plane manager for unified cluster management.

k0smotron allows you to unify your Kubernetes cluster management for an efficient use of resources. It’s designed for [k0s](https://k0sproject.io/).

## Features

### Control Plane-as-a-Service

k0smotron streamlines k0s control plane creation and management within your management cluster, reducing traditional operational overhead (e.g  installation, configuration, upgrades or backups). k0smotron encapsulates the control plane service as a pods (and other Kubernetes constructs) and provides an intuitive approach to cluster lifecycle management through ClusterAPI integration.

### Enhanced High Availability

k0smotron allows you to leverage the power of SQL databases as a data store for your control planes through Kine. This flexibility means you can choose from a variety of data storage operators like Postgres, MySQL, or your cloud provider's managed databases. This offers a robust, high-availability solution, eliminating the dependency on Etcd and allows to leverage Kubernetes self-healing capabilities.

### Bring Your Own Worker Nodes

k0smotron prioritizes flexibility in the integration of worker nodes and allows easy connection or creation of nodes from any infrastructure. This ensures node isolation and flexible scaling, minimizing interference with the control plane. k0smotron operates with or without the ClusterAPI, offering you the freedom to select your preferred operational mode.

## How does it work
The k0smotron controller manager is a service that will be installed into an existing Kubernetes cluster. 
### Control Plane
k0smotron will create and manage k0s control planes in the management cluster like a workload. It leverages the natural pattern of working with custom resources to manage the lifecycle of the k0s control planes. k0smotron automatically creates all the needed Kubernetes lower level resources, such as pods, configmaps, etc. 
By running the control plane on a Kubernetes cluster we can enjoy and leverage the high availability and self-healing capabilities of Kubernetes.

### Worker Plane
With k0s it's easy to install a Kubernetes Worker Node. The worker nodes will connect to the Control Plane with a Join Token, created by k0smotron. When it comes to clusters with dozens or hundreds of worker nodes you do not want to install k0s manually. For these cases you can leverage k0smotron as ClusterAPI Bootstrap Provider. 

### ClusterAPI
k0smotron can be used with ClusterAPI as Bootstrap Provider. This allows to use k0s Control Planes, created by k0smotron, as `Control Plane` and k0s worker nodes or `MachineDeployment` in various clouds.

***Note:*** Currently, we only support creating the `Control Plane` in the Management Cluster. In the next versions of k0smotron we will add full cluster bootstrapping in public and private clouds with with ClusterAPI.

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
kubectl apply -f https://docs.k0smotron.io/stable/install.yaml
```

## Creating a cluster

To create a cluster, you need to create a `Cluster` resource. The `spec` field is used for optional settings, so you can just pass `null` as the value.
For more information about the settings you can check the following [documention](https://docs.k0smotron.io/stable/resource-reference/#cluster).

``` bash
kubectl apply -f - <<EOF
apiVersion: k0smotron.io/v1beta1
kind: Cluster
metadata:
name: my-k0smotron
spec: {}
EOF
```

## Contributing

Please refer to our [contributor's guide](contributors/).