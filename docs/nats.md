# Embedded NATS storage

!!! warning "Experimental"

    The embedded NATS storage backend is experimental. It does not support scaling — the replica count must be fixed at creation time and cannot be changed afterwards.


!!! note 

    You can also use the `kine` storage type with an external NATS cluster:

    ```yaml
    spec:
      storage:
        type: kine
        kine:
          dataSourceURL: "nats://user:password@nats.example.com:4222?bucket=kine&replicas=3"
    ```

k0smotron supports using an embedded [NATS](https://nats.io) server with
[JetStream](https://docs.nats.io/nats-concepts/jetstream) as the storage backend for the k0s control plane.
Each k0s pod runs an embedded NATS server, using [kine](https://github.com/k3s-io/kine) to translate
Kubernetes API server storage operations into NATS JetStream key-value operations.

## Comparison with other storage backends

| Feature             | etcd                      | kine (SQL/PostgreSQL)  | NATS (embedded)             |
|---------------------|---------------------------|------------------------|-----------------------------|
| External dependency | Separate etcd StatefulSet | External database      | None                        |
| HA support          | Yes                       | Yes (depends on DB HA) | Yes (built-in clustering)   |
| Scaling             | Yes                       | Yes                    | **No — fixed at creation**  |
| Persistence         | PVC per etcd pod          | Managed by DB          | PVC per control plane pod   |
| Status              | Stable                    | Stable                 | **Experimental**            |

## Basic configuration

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

```yaml
spec:
  replicas: 3
  storage:
    type: nats
    nats:
      persistence:
        size: 5Gi
        storageClass: fast-ssd
```

## High availability

For multi-replica clusters, the embedded NATS servers form a cluster with JetStream replication across all pods.

!!! warning "Replica count is immutable"

    The replica count cannot be changed after the cluster is created. NATS JetStream uses RAFT for
    consensus and does not support adding or removing members without risking quorum loss and data
    unavailability. Set `spec.replicas` to its final value before creating the cluster.

!!! note

All pods start simultaneously (`Parallel` pod management policy) so that the NATS cluster
    can form before kine initialises. For a 3-replica cluster, quorum requires at least 2 nodes,
    meaning the cluster can tolerate 1 node failure.

## Resources created by k0smotron

| Resource                          | Name pattern                        | Purpose                                      |
|-----------------------------------|-------------------------------------|----------------------------------------------|
| `Secret`                          | `kmc-<cluster>-nats-token`          | Holds the randomly generated NATS auth token |
| `Service` (headless)              | `kmc-<cluster>-nats`                | Enables pod-to-pod NATS cluster routing      |
| `PersistentVolumeClaim` (per pod) | `nats-data-kmc-<cluster>-<ordinal>` | Stores the JetStream data                    |

## Migrating from etcd or kine

NATS storage cannot be migrated to or from etcd/kine in-place. If you need to change the storage backend:

1. Back up the workload cluster's state (e.g. with Velero)
2. Delete the existing `Cluster` resource
3. Recreate it with the new `storage.type`
4. Restore the workload cluster's state
