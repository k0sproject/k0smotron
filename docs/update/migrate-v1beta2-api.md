# Migrate to v1beta2 APIs

This guide explains changes introduced in new `v1beta2` and how to migrate manifests from current `v1beta1` to the new k0smotron API.

## Deprecation Policy

Starting from `PLACCEHOLDER`, k0smotron uses new `v1beta2` API version as storage version and deprecate `v1beta1`, which still served for compatibility. K0smotron follows the [Kubernetes API deprecation policy](https://kubernetes.io/docs/reference/using-api/deprecation-policy/) for beta APIs:

- `v1beta1` is deprecated once `v1beta2` is the preferred/storage version.
- `v1beta1` will be removed no earlier than `max(9 months, 3 minor releases)` after deprecation.
- Until removal, `v1beta1` may still be served via conversion webhooks.
- After removal, clients must use `v1beta2`.

## Changes per API group

### k0smotron.io

#### Cluster

- Storage config was restructured:
  - `spec.kineDataSourceURL` -> `spec.storage.kine.dataSourceURL`
  - `spec.kineDataSourceSecretName` -> `spec.storage.kine.dataSourceSecretName`
  - `spec.etcd` -> `spec.storage.etcd`
  - New selector field: `spec.storage.type` with values `etcd`, `kine`, `nats`.
- Now, a Cluster state is reported using conditions as Kubernetes conventions follows. Details about introduced conditions can be found [here](https://github.com/k0sproject/k0smotron/pull/1365). This implies:
  - Move `Cluster` status reporting to Conditions.
  - Deprecate legacy status fields such as `status.ready` and `status.reconciliationStatus`.
- TODO: check fields that can be removed in favor of patches.
  
!!! note

    Downgrade caveat: `nats` has no `v1beta1` equivalent and is dropped on conversion back to `v1beta1`

#### JoinTokenRequest

- No changes between versions.

### bootstrap.cluster.x-k8s.io

#### K0sControllerConfig and K0sWorkerConfig

- `spec.ignition` and `spec.customUserDataRef` moved under `spec.provisioner`.
- Renamed command fields:
  - `spec.preStartCommands` -> `spec.preK0sCommands`
  - `spec.postStartCommands` -> `spec.postK0sCommands`
- Status moved from boolean ready flag to initialization struct:
  - `status.ready` -> `status.initialization.dataSecretCreated`

TODO: check CAPI changes 

### controlplane.cluster.x-k8s.io

#### K0sControlPlane

TODO: check CAPI changes 

- Status fields aligned to CAPI v1beta2 style:
  - `status.updatedReplicas` -> `status.upToDateReplicas`
  - `status.unavailableReplicas` replaced by `status.availableReplicas`
  - `status.initialized` -> `status.initialization.controlPlaneInitialized`
- Several status counters are now optional pointers (may be unset early in reconciliation).

### infrastructure.cluster.x-k8s.io

TODO: check CAPI changes 