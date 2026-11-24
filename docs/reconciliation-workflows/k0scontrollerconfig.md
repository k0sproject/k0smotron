# K0sControllerConfig reconciliation workflow

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
