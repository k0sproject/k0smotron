# Migrate to v1beta2 APIs

This guide explains changes introduced in new `v1beta2` and how to migrate manifests from current `v1beta1` to the new k0smotron API. For more details about each Api Group, please check current custom resource references.

## Deprecation Policy

Starting from `v1.11.0`, k0smotron uses new `v1beta2` API version as [storage version](https://kubernetes.io/docs/concepts/overview/working-with-objects/storage-version/) and deprecate `v1beta1`, which still served for compatibility. K0smotron follows the [Kubernetes API deprecation policy](https://kubernetes.io/docs/reference/using-api/deprecation-policy/) for beta APIs:

- `v1beta1` is deprecated once `v1beta2` is the preferred/storage version.
- `v1beta1` will be removed no earlier than `max(9 months, 3 minor releases)` after deprecation.
- Until removal, `v1beta1` may still be served via conversion webhooks.
- After removal, clients must use `v1beta2`.

## Changes per API group in `v1beta2`

### k0smotron.io

#### Cluster

- Storage config was restructured:
  - `spec.kineDataSourceURL` -> `spec.storage.kine.dataSourceURL`
  - `spec.kineDataSourceSecretName` -> `spec.storage.kine.dataSourceSecretName`
  - `spec.etcd` -> `spec.storage.etcd`
  - New selector field: `spec.storage.type` with values `etcd`, `kine`, `nats`.
- `spec.controllerPlaneFlags` was renamed to `spec.controlPlaneFlags`.
- Introduce of `spec.patches` for customize patching of generated resoruces in `v1beta2`. Check documentation about [patches](../advanced/components.md) for more details.
- Now, a Cluster state is reported using conditions as Kubernetes conventions follows. This implies:
  - Move `Cluster` status reporting to Conditions. Details about introduced conditions can be found in Pull Request [#1365](https://github.com/k0sproject/k0smotron/pull/1365).
  - Deprecate legacy status fields such as `status.ready` and `status.reconciliationStatus`. In **v1beta2**, this fields are stored under `status.deprecated.v1beta1` for backward compatibility, and will be removed once **v1beta1** support is dropped.
  
!!! Warning

    Downgrade caveat: `spec.storage.type` for `nats` type  and `spec.patches` has no `v1beta1` equivalent.

#### JoinTokenRequest

- Changed the status section to use conditions for reporting status. This improvement includes:
  - Following Kubernetes conventions for status reporting by adding `status.conditions` in **v1beta2** (see related Pull Request [#1416](https://github.com/k0sproject/k0smotron/pull/1416)).
  - Deprecating the `status.reconciliationStatus` field. In **v1beta2**, this field is stored under `status.deprecated.v1beta1.reconciliationStatus` for backward compatibility, and will be removed once **v1beta1** support is dropped.


### bootstrap.cluster.x-k8s.io

#### K0sControllerConfig and K0sWorkerConfig

- `spec.ignition` and `spec.customUserDataRef` moved under `spec.provisioner`.
- Renamed command fields:
  - `spec.preStartCommands` -> `spec.preK0sCommands`.
  - `spec.postStartCommands` -> `spec.postK0sCommands`.

- The Status section has been changed to follow the CAPI v1beta2 API proposal.
  - Status moved from boolean ready flag to initialization struct:
    - `status.ready` -> `status.initialization.dataSecretCreated`.

Some other changes may affect the internal Go types in k0smotron.

### controlplane.cluster.x-k8s.io

#### K0sControlPlane

- Status fields aligned to CAPI v1beta2 style:
  - `status.updatedReplicas` -> `status.upToDateReplicas`.
  - `status.unavailableReplicas` replaced by `status.availableReplicas`.
  - `status.initialized` -> `status.initialization.controlPlaneInitialized`.
- Several status counters are now optional pointers (may be unset early in reconciliation).

#### K0smotronControlPlane

- Storage config was restructured:
  - `spec.kineDataSourceURL` -> `spec.storage.kine.dataSourceURL`
  - `spec.kineDataSourceSecretName` -> `spec.storage.kine.dataSourceSecretName`
  - `spec.etcd` -> `spec.storage.etcd`
  - New selector field: `spec.storage.type` with values `etcd`, `kine`, `nats`.
- `spec.controllerPlaneFlags` was renamed to `spec.controlPlaneFlags`.
- Introduce of `spec.patches` for customize patching of generated resoruces in `v1beta2`. Check documentation about [patches](../advanced/components.md) for more details.
- Now, a Cluster state is reported using conditions as Kubernetes conventions follows. This implies:
  - Move `Cluster` status reporting to Conditions. Details about introduced conditions can be found in Pull Request [#1365](https://github.com/k0sproject/k0smotron/pull/1365).
  - Deprecate legacy status fields such as `status.ready` and `status.reconciliationStatus`. In **v1beta2**, this fields are stored under `status.deprecated.v1beta1` for backward compatibility, and will be removed once **v1beta1** support is dropped.
- Status fields aligned to CAPI v1beta2 style:
  - `status.updatedReplicas` -> `status.upToDateReplicas`.
  - `status.unavailableReplicas` replaced by `status.availableReplicas`.
  - `status.initialized` -> `status.initialization.controlPlaneInitialized`.
- Several status counters are now optional pointers (may be unset early in reconciliation).
  
!!! Warning

    Downgrade caveat: `spec.storage.type` for `nats` type  and `spec.patches` has no `v1beta1` equivalent.

### infrastructure.cluster.x-k8s.io

#### RemoteMachine

- Rename `spec.CustomCleanCommands` to `spec.CleanCommands` for simplicity.
- Adapt `status` fields to the ones required by CAPI `v1beta2`:
  - `ready` -> `status.initialization.provisioned`.
  - Remove `status.failureReason` and `status.failureMessage` and use conditions instead to report state related to RemoteMachine.

#### RemoteCluster

- Adapt `status` fields to the ones required by CAPI `v1beta2`:
  - `ready` -> `status.initialization.provisioned`.
  - Use conditions to report state related to RemoteCluster.