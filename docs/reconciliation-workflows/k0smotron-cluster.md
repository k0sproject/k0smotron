# Cluster (k0smotron.io) reconciliation workflow

The `Cluster` controller is also used without Cluster API, to [create a cluster in standalone mode](../cluster.md).
With `spec.remoteHostCluster`, the controller creates its resources in the host cluster, except the admin kubeconfig
Secret, which is always created in the management cluster. In the host cluster, the resources are owned by an external
owner ConfigMap, so that deleting the external owner removes all of them.

```mermaid
flowchart TD
    start(["Reconcile Cluster"]) --> finalizer["Add the finalizer"]
    finalizer --> remote{"spec.remoteHostCluster set?"}
    remote -- Yes --> hostClient["Use the host cluster kubeconfig for the resources below, and ensure the namespace and the external owner exist there"]
    remote -- No --> deleting
    hostClient --> deleting{"Being deleted?"}
    deleting -- Yes --> cleanup["Delete the external owner, if any, and the v1beta1 JoinTokenRequests of the Cluster in other namespaces"]
    cleanup --> removeFinalizer(["Remove the finalizer"])
    deleting -- No --> services["Control plane Service. For a LoadBalancer Service without spec.externalAddress, wait for its address and store it in spec.externalAddress"]
    services --> ingress["Ingress, if spec.ingress is set"]
    ingress --> configMaps["k0s config, entrypoint and, if monitoring is enabled, Prometheus ConfigMaps"]
    configMaps --> certs["Generate the cluster certificates, unless spec.certificateRefs is set. Generate or renew the etcd and ingress certificates when they are used"]
    certs --> pvc["Resize the PersistentVolumeClaims when the requested storage size changed"]
    pvc --> nats["NATS Service and token Secret, if the storage type is nats"]
    nats --> etcd["etcd Service, StatefulSet and defragmentation CronJob, if the storage type is etcd"]
    etcd --> sts["Control plane StatefulSet"]
    sts --> ready{"At least one ready replica?"}
    ready -- No --> requeueReady(["Requeue"])
    ready -- Yes --> kubeconfig["Admin kubeconfig Secret in the management cluster"]
    kubeconfig --> done(["Requeue when the next certificate renewal is due"])
```

The etcd StatefulSet is scaled up one member at a time. A member is only added once every current member is ready.
