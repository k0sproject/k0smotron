# Embedded NATS storage

k0smotron supports using an embedded [NATS](https://nats.io) server
with [JetStream](https://docs.nats.io/nats-concepts/jetstream) as the storage backend for the k0s control plane. This is
an alternative to etcd or external kine data sources.

Each k0s pod runs an embedded NATS server co-located in the same container,
using [kine](https://github.com/k3s-io/kine) to translate Kubernetes API server storage operations into NATS
JetStream key-value operations. For multi-replica clusters, the embedded servers form a NATS cluster with JetStream
replication, providing high availability without any external dependency.

## Comparison with other storage backends

| Feature             | etcd                      | kine (SQL/PostgreSQL)  | NATS                      |
|---------------------|---------------------------|------------------------|---------------------------|
| External dependency | Separate etcd StatefulSet | External database      | None                      |
| HA support          | Yes                       | Yes (depends on DB HA) | Yes (built-in clustering) |
| Persistence         | PVC per etcd pod          | Managed by DB          | PVC per control plane pod |

## Basic configuration

To use NATS as the storage backend, set `spec.storage.type` to `nats` in the `Cluster` resource:

```yaml
apiVersion: k0smotron.io/v1beta2
kind: Cluster
metadata:
  name: k0smotron-test
spec:
  replicas: 1
  storage:
    type: nats
```

k0smotron automatically:

- Provisions a PersistentVolumeClaim (default: 1 Gi) for the JetStream store
- Generates a random auth token and stores it in a Secret
- Configures the embedded NATS server at pod startup via the entrypoint script

## Persistence

By default, each pod gets a 1 Gi PersistentVolumeClaim for the JetStream store. You can customise the size and storage
class:

```yaml
apiVersion: k0smotron.io/v1beta2
kind: Cluster
metadata:
  name: k0smotron-test
spec:
  replicas: 1
  storage:
    type: nats
    nats:
      persistence:
        size: 5Gi
        storageClass: fast-ssd
```

## High availability

For multi-replica clusters, k0smotron creates a NATS cluster with JetStream replication across all pods. Each pod's
embedded NATS server joins the cluster using the headless Service created by k0smotron for pod-to-pod routing.

```yaml
apiVersion: k0smotron.io/v1beta2
kind: Cluster
metadata:
  name: k0smotron-ha
spec:
  replicas: 3
  storage:
    type: nats
    nats:
      persistence:
        size: 5Gi
```

!!! note

    All pods are started simultaneously (`Parallel` pod management policy) so that pod DNS entries are resolvable by the time the NATS cluster is forming. This differs from the default `OrderedReady` policy used in the etcd backend.

!!! note

    For a 3-replica cluster, JetStream replicates data across all 3 nodes. Quorum requires at least 2 nodes to be running, meaning the cluster can tolerate 1 node failure.

## Resources created by k0smotron

When `storage.type: nats` is set, k0smotron creates the following additional resources:

| Resource                          | Name pattern                        | Purpose                                      |
|-----------------------------------|-------------------------------------|----------------------------------------------|
| `Secret`                          | `kmc-<cluster>-nats-token`          | Holds the randomly generated NATS auth token |
| `Service` (headless)              | `kmc-<cluster>-nats`                | Enables pod-to-pod NATS cluster routing      |
| `PersistentVolumeClaim` (per pod) | `nats-data-kmc-<cluster>-<ordinal>` | Stores the JetStream data                    |

## Migrating from etcd or kine

NATS storage cannot be migrated to or from etcd/kine in-place. If you need to change the storage backend, you must:

1. Back up the workload cluster's state (e.g. with Velero)
2. Delete the existing `Cluster` resource
3. Recreate it with the new `storage.type`
4. Restore the workload cluster's state

