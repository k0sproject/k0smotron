# Migrate to v1beta2 APIs

This guide explains changes introduced in new `v1beta2` and how to migrate manifests from current `v1beta1` to the new k0smotron API. For more details about each Api Group, please check current custom resource references.

## Migration tool

k0smotron ships an experimental CLI tool that automates the manifest conversion described in this guide.

### Download pre-built binary

Pre-built binaries are published with every release for the following platforms:

| Platform | Binary |
|---|---|
| Linux x86-64 | `convert_linux_amd64` |
| Linux ARM 64-bit | `convert_linux_arm64` |
| macOS x86-64 | `convert_darwin_amd64` |
| macOS Apple Silicon | `convert_darwin_arm64` |
| Windows x86-64 | `convert_windows_amd64.exe` |

Download the binary for your platform from the [GitHub releases page](https://github.com/k0sproject/k0smotron/releases), make it executable, and run it:

```shell
# Example for Linux x86-64
curl -L https://github.com/k0sproject/k0smotron/releases/latest/download/convert_linux_amd64 -o convert
chmod +x convert

# Convert a file
./convert my-cluster.yaml > my-cluster-v1beta2.yaml

# Read from stdin
cat my-cluster.yaml | ./convert
```

### Build from source

If you have the repository cloned, you can build or run the tool directly with `make`:

```shell
# Run without building (uses go run)
make convert ARGS="my-cluster.yaml"
make convert ARGS="my-cluster.yaml" > my-cluster-v1beta2.yaml
cat my-cluster.yaml | make convert

# Build a binary for the current platform
make build-convert    # output: bin/convert

# Build binaries for all release platforms
make build-convert-all  # output: bin/convert_<os>_<arch>[.exe]
```

The tool processes multi-document YAML files and applies all the field renames listed below. Resources from other API groups are passed through unchanged.

!!! warning
    `convert` is a best-effort staging tool. Always review the output before applying it to a cluster, and keep a backup of the originals. This feature may be removed in future releases.


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