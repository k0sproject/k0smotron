# Machine provider ID reconciliation workflow

k0smotron also sets the provider ID of the nodes. This applies to every `Machine` that is bootstrapped by a
`K0sWorkerConfig`, or by a `K0sControllerConfig` with the worker enabled, and to every `Machine` backed by a
`RemoteMachine`. Once the `Machine` has a `spec.providerID`, the controller finds its node in the workload cluster, by
name, `k0smotron.io/machine-name` label or address, and sets the provider ID of the `Machine` on the node.
