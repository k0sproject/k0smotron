# Reconciliation workflows

This section describes how the k0smotron controllers reconcile each resource, in the style of the
[Cluster API provider contracts](https://cluster-api.sigs.k8s.io/developer/providers/contracts/infra-cluster.html#typical-infracluster-reconciliation-workflow).
It helps to understand in which order things happen, what a resource waits for, and where to look when a
resource does not make progress.

The diagrams show the main path of a reconcile. Some details are left out to keep them readable:

- An error at any step ends the reconcile, and the controller retries it later with a backoff.
- Most steps also set a condition on the resource, so `kubectl describe` usually tells which step a resource is waiting at.
- The Cluster API resources follow the Cluster API contract for paused clusters: while the `Cluster` or the resource
  is paused, the controller only sets the `Paused` condition and does nothing else.

The names of the resources that the controllers create are listed in [Generated Resources](../generated-resources.md).

## Hosted control planes

A hosted control plane is reconciled by two controllers. The `K0smotronControlPlane` controller translates the
Cluster API resource to a `k0smotron.io` `Cluster` and reports its state back to Cluster API, and the `Cluster`
controller creates the control plane pods and the resources around them.

- [K0smotronControlPlane](k0smotroncontrolplane.md)
- [Cluster (k0smotron.io)](k0smotron-cluster.md)
- [JoinTokenRequest](jointokenrequest.md)

## Control planes on machines

- [K0sControlPlane](k0scontrolplane.md)

## Bootstrap providers

Both bootstrap providers generate the bootstrap data of a Machine once. After that, they only keep track of the
bootstrap token embedded in it.

- [K0sControllerConfig](k0scontrollerconfig.md)
- [K0sWorkerConfig](k0sworkerconfig.md)
- [Machine provider ID](machine-provider-id.md)

## Infrastructure provider

- [RemoteCluster](remotecluster.md)
- [RemoteMachine](remotemachine.md)
