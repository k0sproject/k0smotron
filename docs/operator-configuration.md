# Operator Configuration

k0smotron operator accepts the following command-line flags.

## Flags

| Flag                          | Type   | Default    | Description                                                                                                                                                                                                                         |
|-------------------------------|--------|------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `--health-probe-bind-address` | string | `:8081`    | Address the health probe endpoint binds to.                                                                                                                                                                                         |
| `--leader-elect`              | bool   | `false`    | Enable leader election for controller manager. Ensures only one active controller manager at a time.                                                                                                                                |
| `--feature-gates`             | string | `""`       | Feature gates to enable, as a comma-separated list of `key=value` pairs. Can also be set via the `K0SMOTRON_FEATURE_GATES` environment variable.                                                                                    |
| `--concurrency`               | int    | `5`        | Number of concurrent reconciliations per controller.                                                                                                                                                                                |
| `--enable-controller`         | string | `""` (all) | The controller to enable. Valid values: `bootstrap`, `control-plane`, `infrastructure`, `standalone`. Defaults to all controllers.                                                                                                  |
| `--watch-filter`              | string | `""`       | Label value used to filter reconciled objects. Only resources with label `cluster.x-k8s.io/watch-filter=<value>` are reconciled. See [Running multiple instances](#running-multiple-instances).                                     |
| `--namespace`                 | string | `""`       | Namespace that the controller watches. If unspecified, all namespaces are watched.                                                                                                                                                  |

### Deprecated flags

| Flag                     | Type   | Default | Description                                                                 |
|--------------------------|--------|---------|-----------------------------------------------------------------------------|
| `--metrics-bind-address` | string | `:8443` | Address the metrics endpoint binds to. Use `--diagnostics-address` instead. |
| `--metrics-secure`       | bool   | `true`  | Serve metrics endpoint over HTTPS. Use `--insecure-diagnostics` instead.    |
| `--enable-http2`         | bool   | `false` | Enable HTTP/2 for the metrics and webhook servers.                          |

## Feature gates

Feature gates can be set via `--feature-gates` flag or the `K0SMOTRON_FEATURE_GATES` environment variable. The
environment variable takes precedence over the flag.

Format: `--feature-gates=FeatureName=true,OtherFeature=false`

| Feature         | Default | Description                                                           |
|-----------------|---------|-----------------------------------------------------------------------|
| `CloudInitVars` | `false` | Store k0smotron-generated commands and files in cloud-init variables. |

## Running multiple instances

To run multiple k0smotron instances in the same cluster without reconcile conflicts, use `--watch-filter` to restrict
each instance to a subset of resources. This follows the [CAPI multi-instance support](https://cluster-api.sigs.k8s.io/developer/core/support-multiple-instances) convention.

Pass a distinct filter value to each instance:

```yaml
# Instance A deployment args
args:
  - --watch-filter=instance-a

# Instance B deployment args
args:
  - --watch-filter=instance-b
```

Then label your `Cluster`, `K0smotronControlPlane`, and other managed resources with the standard CAPI watch-filter label:

```yaml
metadata:
  labels:
    cluster.x-k8s.io/watch-filter: instance-a
```

Each instance only reconciles resources matching its filter value, preventing conflicts.
