# K0sControlPlane reconciliation workflow

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
