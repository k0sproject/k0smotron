# k0smotron Helm Chart

Helm chart for [k0smotron](https://k0smotron.io) — operator for managing k0s-based Kubernetes clusters, with optional [Cluster API](https://cluster-api.sigs.k8s.io/) provider integration.

## Prerequisites

- Kubernetes 1.25+
- Helm 3.10+
- [cert-manager](https://cert-manager.io) v1.0+ (required when `certManager.enabled=true`, which is the default)

Install cert-manager if not already present:
```sh
helm repo add jetstack https://charts.jetstack.io
helm upgrade --install cert-manager jetstack/cert-manager \
  --namespace cert-manager --create-namespace \
  --set installCRDs=true
```

## Installation

### Standalone mode (default)

Manages `Cluster` and `JoinTokenRequest` resources in the `k0smotron.io` API group.

```sh
helm upgrade --install k0smotron oci://ghcr.io/k0sproject/charts/k0smotron \
  --namespace k0smotron --create-namespace
```

### ClusterAPI mode

Full ClusterAPI provider: bootstrap + control plane + infrastructure providers.

```sh
helm upgrade --install k0smotron oci://ghcr.io/k0sproject/charts/k0smotron \
  --namespace k0smotron --create-namespace \
  --set mode=clusterapi
```

> **Note:** ClusterAPI core components (`cluster-api`, `kubeadm-bootstrap-provider`, etc.) must be installed separately before using k0smotron in clusterapi mode.

### Install from source

```sh
git clone https://github.com/k0sproject/k0smotron
helm upgrade --install k0smotron ./charts/k0smotron \
  --namespace k0smotron --create-namespace
```

## Upgrading

```sh
helm upgrade k0smotron oci://ghcr.io/k0sproject/charts/k0smotron \
  --namespace k0smotron
```

CRDs are stored in the `crds/` directory and are **not** upgraded automatically by Helm (this is Helm's built-in CRD behaviour). To upgrade CRDs manually:

```sh
kubectl apply --server-side -f https://github.com/k0sproject/k0smotron/releases/download/v<VERSION>/install.yaml
```

Or apply only the CRDs from the chart tarball:
```sh
helm show crds oci://ghcr.io/k0sproject/charts/k0smotron | kubectl apply --server-side -f -
```

## Uninstalling

```sh
helm uninstall k0smotron --namespace k0smotron
```

> **Warning:** CRDs are not removed on uninstall. Delete them manually if desired:
> ```sh
> kubectl get crd | grep -E 'k0smotron\.io|cluster\.x-k8s\.io' | awk '{print $1}' | xargs kubectl delete crd
> ```

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `mode` | string | `standalone` | Installation mode: `standalone` or `clusterapi` |
| `image.repository` | string | `quay.io/k0sproject/k0smotron` | Controller image repository |
| `image.tag` | string | `""` | Image tag; defaults to chart `appVersion` |
| `image.pullPolicy` | string | `IfNotPresent` | Image pull policy |
| `replicaCount` | int | `1` | Number of controller replicas |
| `leaderElect` | bool | `true` | Enable leader election (required when `replicaCount > 1`) |
| `resources.limits.cpu` | string | `500m` | CPU limit |
| `resources.limits.memory` | string | `128Mi` | Memory limit |
| `resources.requests.cpu` | string | `10m` | CPU request |
| `resources.requests.memory` | string | `64Mi` | Memory request |
| `webhook.enabled` | bool | `true` | Deploy admission webhook server |
| `certManager.enabled` | bool | `true` | Use cert-manager for webhook TLS certificates |
| `createNamespace` | bool | `true` | Create the release namespace as part of the chart |
| `nodeSelector` | object | `{}` | Node selector for the controller pod |
| `tolerations` | list | `[]` | Tolerations for the controller pod |
| `affinity` | object | `{}` | Affinity rules for the controller pod |
| `nameOverride` | string | `""` | Override the chart name |
| `fullnameOverride` | string | `""` | Override the fully qualified app name |
| `commonLabels` | object | `{}` | Additional labels added to all resources |
| `commonAnnotations` | object | `{}` | Additional annotations added to all resources |

## Disabling webhooks

If cert-manager is not available:

```sh
helm upgrade --install k0smotron oci://ghcr.io/k0sproject/charts/k0smotron \
  --namespace k0smotron --create-namespace \
  --set webhook.enabled=false \
  --set certManager.enabled=false
```

> Disabling webhooks removes defaulting and validation for k0smotron resources. Not recommended for production.

## Managing CRDs separately

Skip CRD installation and manage them out-of-band:

```sh
helm upgrade --install k0smotron oci://ghcr.io/k0sproject/charts/k0smotron \
  --namespace k0smotron --create-namespace \
  --skip-crds
```

## Limitations

### All CRDs always installed

The Helm `crds/` directory does not support templating or conditional inclusion. All 14 CRDs (standalone + ClusterAPI providers) are installed regardless of `mode`. CRDs for unused providers are harmless but add API surface to the cluster. Use `--skip-crds` and manage CRDs externally if this is a concern.

### CRDs not upgraded automatically

Helm never upgrades resources in the `crds/` directory after the initial install. Apply CRD updates manually (see [Upgrading](#upgrading)) or via the kustomize-based `install.yaml` on each release.

### No CRD conversion webhooks

The base CRDs do not include conversion webhook configuration. The kustomize-based install adds these via patches at build time. In practice this is only relevant when accessing `v1beta1` resources — all current resources are stored as `v1beta2`.

### Large CRD schema files

Some ClusterAPI CRDs contain large embedded schemas (up to ~1.6 MB raw YAML). These compress well in etcd and work on standard clusters. If you run a constrained etcd configuration, use `--skip-crds` and apply CRDs with `kubectl apply --server-side`.
