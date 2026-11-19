# Reconciliation workflows

This page describes how the k0smotron controllers reconcile each resource, in the style of the
[Cluster API provider contracts](https://cluster-api.sigs.k8s.io/developer/providers/contracts/infra-cluster.html#typical-infracluster-reconciliation-workflow).
It helps to understand in which order things happen, what a resource waits for, and where to look when a
resource does not make progress.

The diagrams show the main path of a reconcile. Some details are left out to keep them readable:

- An error at any step ends the reconcile, and the controller retries it later with a backoff.
- Most steps also set a condition on the resource, so `kubectl describe` usually tells which step a resource is waiting at.
- The Cluster API resources follow the Cluster API contract for paused clusters: while the `Cluster` or the resource
  is paused, the controller only sets the `Paused` condition and does nothing else.

The names of the resources that the controllers create are listed in [Generated Resources](generated-resources.md).

## Hosted control planes

A hosted control plane is reconciled by two controllers. The `K0smotronControlPlane` controller translates the
Cluster API resource to a `k0smotron.io` `Cluster` and reports its state back to Cluster API, and the `Cluster`
controller creates the control plane pods and the resources around them.

### K0smotronControlPlane

```mermaid
flowchart TD
    start(["Reconcile K0smotronControlPlane"]) --> finalizer["Add the finalizer"]
    finalizer --> owner{"Owned by a Cluster?"}
    owner -- No --> waitOwner(["Wait for the Cluster controller to set the owner reference"])
    owner -- Yes --> paused{"Paused?"}
    paused -- Yes --> stopPaused(["Set the Paused condition and stop"])
    paused -- No --> deleting{"Being deleted?"}
    deleting -- Yes --> deleteKmc["Delete the k0smotron Cluster and wait until it is gone"]
    deleteKmc --> removeFinalizer(["Remove the finalizer"])
    deleting -- No --> remote{"spec.remoteHostCluster set?"}
    remote -- Yes --> externalOwner["Ensure the namespace and the external owner exist in the host cluster"]
    remote -- No --> certs
    externalOwner --> certs["Generate the cluster certificates, unless spec.certificateRefs is set"]
    certs --> kmcExists{"k0smotron Cluster exists?"}
    kmcExists -- No --> createKmc["Create the k0smotron Cluster from the spec"]
    createKmc --> requeueCreated(["Requeue"])
    kmcExists -- Yes --> syncKmc["Update the k0smotron Cluster when the spec changed"]
    syncKmc --> endpoint["Wait for the external address, or use the ingress host, and set it as the control plane endpoint of the Cluster and the infrastructure cluster"]
    endpoint --> status["Compute the status from the control plane pods and from the workload cluster API"]
    status --> infra["Mark the infrastructure cluster as provisioned, if it has the cluster.x-k8s.io/managed-by: k0smotron annotation"]
    infra --> settled{"All replicas ready, at the desired version and the API available?"}
    settled -- No --> requeueStatus(["Requeue"])
    settled -- Yes --> done(["Done"])
```

### Cluster (k0smotron.io)

The `Cluster` controller is also used without Cluster API, to [create a cluster in standalone mode](cluster.md).
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

### JoinTokenRequest

```mermaid
flowchart TD
    start(["Reconcile JoinTokenRequest"]) --> finalizer["Add the finalizer"]
    finalizer --> owner["Set the k0smotron Cluster as the owner"]
    owner --> pod["Find a control plane pod of the Cluster"]
    pod --> deleting{"Being deleted?"}
    deleting -- Yes --> invalidate["Invalidate the token with k0s token invalidate in the pod"]
    invalidate --> removeFinalizer(["Remove the finalizer"])
    deleting -- No --> created{"status.tokenID set?"}
    created -- Yes --> done(["Done"])
    created -- No --> create["Create the token with k0s token create in the pod"]
    create --> ingress["Point the token at the ingress host, if the Cluster has spec.ingress"]
    ingress --> secret["Store the token in a Secret named after the JoinTokenRequest"]
    secret --> tokenID(["Set status.tokenID"])
```

## Control planes on machines

### K0sControlPlane

```mermaid
flowchart TD
    start(["Reconcile K0sControlPlane"]) --> finalizer["Add the finalizer"]
    finalizer --> owner{"Owned by a Cluster?"}
    owner -- No --> waitOwner(["Wait for the Cluster controller to set the owner reference"])
    owner -- Yes --> paused{"Paused?"}
    paused -- Yes --> stopPaused(["Set the Paused condition and stop"])
    paused -- No --> state["Collect the control plane Machines, their infrastructure machines and K0sControllerConfigs"]
    state --> deleting{"Being deleted?"}
    deleting -- Yes --> workers{"Worker Machines left?"}
    workers -- Yes --> waitWorkers(["Wait for the worker Machines to be deleted"])
    workers -- No --> cpMachines{"Control plane Machines left?"}
    cpMachines -- Yes --> deleteMachines(["Delete them and wait"])
    cpMachines -- No --> removeFinalizer(["Remove the finalizer"])
    deleting -- No --> certs["Generate the cluster certificates, until the control plane is initialized"]
    certs --> tunneling["FRP server ConfigMap, Deployment and Service, if tunneling is enabled"]
    tunneling --> config["Set the control plane endpoint as external address or SAN in the k0s config, and apply the dynamic config to the workload cluster"]
    config --> kubeconfig["Admin kubeconfig Secret, rotating its client certificate when due"]
    kubeconfig --> preflight["Remediate unhealthy Machines and complete the Machine deletions"]
    preflight --> latest{"Newest Machine ready and no Machine updating in place?"}
    latest -- No --> requeueLatest(["Requeue"])
    latest -- Yes --> inplace{"In-place version update needed?"}
    inplace -- Yes --> plan(["Create or follow the autopilot plan, and requeue"])
    inplace -- No --> scale{"Scale?"}
    scale -- Up --> scaleUp["Create an infrastructure machine, a K0sControllerConfig and a Machine"]
    scale -- Down --> scaleDown["Delete one Machine: annotated for deletion, outdated or in excess"]
    scale -- No --> desired
    scaleUp --> desired{"Desired number of up-to-date Machines reached?"}
    scaleDown --> desired
    desired -- No --> requeueDesired(["Requeue"])
    desired -- Yes --> done(["Done"])
```

After every reconcile, the controller updates the status of the `K0sControlPlane`, and once the control plane is
initialized also the `Cluster`, from the Machines and from the workload cluster API.

## Bootstrap providers

Both bootstrap providers generate the bootstrap data of a Machine once. After that, they only keep track of the
bootstrap token embedded in it.

### K0sControllerConfig

```mermaid
flowchart TD
    start(["Reconcile K0sControllerConfig"]) --> owner{"Owner Machine and Cluster found?"}
    owner -- No --> waitOwner(["Wait for the owner to be set"])
    owner -- Yes --> paused{"Paused?"}
    paused -- Yes --> stopPaused(["Set the Paused condition and stop"])
    paused -- No --> created{"Bootstrap data already created?"}
    created -- Yes --> joined{"Controller joined the cluster?"}
    joined -- Yes --> revoke(["Revoke its bootstrap token"])
    joined -- No --> refresh(["Refresh the bootstrap token before it expires, and requeue"])
    created -- No --> endpoint{"Control plane endpoint set?"}
    endpoint -- No --> requeueEndpoint(["Requeue"])
    endpoint -- Yes --> machines{"Control plane Machines exist?"}
    machines -- No --> requeueMachines(["Requeue"])
    machines -- Yes --> init{"Oldest control plane Machine, and the control plane may be initialized?"}
    init -- Yes --> initFiles["Add the cluster certificates"]
    init -- No --> joinHost{"Join API reachable at the control plane endpoint, or a running controller with an address?"}
    joinHost -- No --> requeueJoin(["Requeue"])
    joinHost -- Yes --> joinFiles["Create a controller join token in the workload cluster"]
    initFiles --> data["Generate the bootstrap data: k0s config, files, tunneling and install commands"]
    joinFiles --> data
    data --> secret["Create the bootstrap data Secret"]
    secret --> dataCreated(["Set status.initialization.dataSecretCreated, and requeue to track the join token, if any"])
```

The control plane may be initialized while the `Cluster` does not have the `ControlPlaneInitialized` condition yet,
or when it stores its data in kine or an external etcd cluster. A controller has joined when its Machine has a node
reference or, for a controller without a worker, when the workload cluster has a `ControlNode` for it.

### K0sWorkerConfig

```mermaid
flowchart TD
    start(["Reconcile K0sWorkerConfig"]) --> owner{"Owner and Cluster found?"}
    owner -- No --> waitOwner(["Wait for the owner to be set"])
    owner -- Yes --> paused{"Paused?"}
    paused -- Yes --> stopPaused(["Set the Paused condition and stop"])
    paused -- No --> created{"Bootstrap data already created?"}
    created -- Yes --> token["Revoke the bootstrap token once the node of a Machine has joined, otherwise refresh it before it expires"]
    token --> rotate{"MachinePool token expiring or gone?"}
    rotate -- No --> requeueToken(["Requeue"])
    rotate -- Yes --> cpReady
    created -- No --> cpReady{"Control plane endpoint set and control plane initialized?"}
    cpReady -- No --> requeueCp(["Requeue"])
    cpReady -- Yes --> joinToken["Create a worker join token in the workload cluster"]
    joinToken --> data["Generate the bootstrap data for the platform: files, ingress certificates and install commands"]
    data --> secret["Create or update the bootstrap data Secret"]
    secret --> dataCreated(["Set status.initialization.dataSecretCreated, and requeue to track the token"])
```

### Machine provider ID

k0smotron also sets the provider ID of the nodes. This applies to every `Machine` that is bootstrapped by a
`K0sWorkerConfig`, or by a `K0sControllerConfig` with the worker enabled, and to every `Machine` backed by a
`RemoteMachine`. Once the `Machine` has a `spec.providerID`, the controller finds its node in the workload cluster, by
name, `k0smotron.io/machine-name` label or address, and sets the provider ID of the `Machine` on the node.

## Infrastructure provider

### RemoteCluster

A `RemoteCluster` has nothing to provision. Once it is owned by a `Cluster`, the controller marks it as provisioned
and sets its `Ready` condition. Its `spec.controlPlaneEndpoint` is either set by the user or, for a hosted control
plane, by the `K0smotronControlPlane` controller.

### RemoteMachine

```mermaid
flowchart TD
    start(["Reconcile RemoteMachine"]) --> finalizer["Add the finalizer"]
    finalizer --> owner{"Owner Machine and Cluster found?"}
    owner -- No --> requeueOwner(["Requeue"])
    owner -- Yes --> paused{"Paused?"}
    paused -- Yes --> stopPaused(["Set the Paused condition and stop"])
    paused -- No --> ready{"Cluster infrastructure ready and Machine bootstrap data set?"}
    ready -- No --> requeueReady(["Requeue"])
    ready -- Yes --> deleting{"Being deleted?"}
    deleting -- Yes --> cleanup["Over SSH, run spec.cleanUpCommands or remove k0s from the machine. Return the machine to its pool"]
    cleanup --> removeFinalizer(["Remove the finalizer"])
    deleting -- No --> pool{"spec.pool set?"}
    pool -- Yes --> reserve["Reserve a PooledRemoteMachine of the pool and copy its address, SSH key and settings"]
    pool -- No --> provisioned
    reserve --> provisioned{"Already provisioned?"}
    provisioned -- Yes --> done(["Done"])
    provisioned -- No --> bootstrap["Run the bootstrap data on the machine, with a Job if spec.provisionJob is set, otherwise over SSH"]
    bootstrap --> providerID["Set spec.providerID, and copy the labels and annotations to the Machine"]
    providerID --> provisionedStatus(["Set status.initialization.provisioned and the machine address"])
```
