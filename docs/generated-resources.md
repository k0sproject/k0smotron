# Generated Resources and Naming Conventions

This document describes all Kubernetes resources created and managed by a `k0smotron.io/v1beta1` Cluster (K0smotron Cluster) and their naming conventions. This information is essential when you need to customize these resources.

When using Cluster API (CAPI), a `K0smotronControlPlane` resource creates a child `k0smotron.io/v1beta1` Cluster, which then produces the resources documented here.

## Scope and audience

This page is written for **users who need to know the names and naming patterns of resources** created by K0smotron Clusters, such as when customizing deployments via `customizeComponents.patches`. It intentionally focuses on:

- resource **names and name patterns**
- **where** resources are created (management/external/workload cluster)
- **when** resources are created (conditions / feature flags)

It does **not** describe controller implementation details (for example, internal reconciliation logic), since those tend to change over time and are not needed for patching.

## Naming Conventions Overview

### Prefix Pattern

All resources created by a `k0smotron.io/v1beta1` Cluster use the `kmc-<cluster-name>` prefix. This prefix is always applied to generated resource names, making them predictable and reusable across different clusters.

### Name Length Limit

Kubernetes resource names are limited to 63 characters. Names that exceed this limit are automatically shortened using an MD5 hash suffix:

- If a name exceeds 63 characters, it is truncated to 57 characters
- A hyphen (`-`) is appended, followed by the first 5 characters of the MD5 hash (in hexadecimal) of the original name
- The final name is exactly 63 characters: `{first-57-chars}-{md5-prefix}`

**Exception**: The kubeconfig Secret name (`<cluster-name>-kubeconfig`) is **not** shortened, as it must match the format expected by Cluster API.

## Resources Created in the Management/External Cluster

The following resources are created and managed by the K0smotron Cluster in the cluster where the control plane pods run (the management cluster by default, or an external cluster if `spec.kubeconfigRef` is specified).

### Control Plane (k0s) Resources

#### StatefulSet

- **Name**: `kmc-<cluster-name>`
- **Purpose**: Manages the k0s control plane pods
- **Example**: For cluster `docker-test`, the StatefulSet is named `kmc-docker-test`

#### ConfigMaps

##### Main Configuration ConfigMap

- **Name**: `kmc-<cluster-name>-config`
- **Purpose**: Contains the k0s configuration YAML (`K0SMOTRON_K0S_YAML`)
- **Example**: `kmc-docker-test-config`

##### Entrypoint ConfigMap

- **Name**: `kmc-entrypoint-<cluster-name>-config`
- **Purpose**: Contains the entrypoint script (`k0smotron-entrypoint.sh`) executed by the control plane container
- **Example**: `kmc-entrypoint-docker-test-config`

##### Telemetry ConfigMap (when monitoring is enabled)

- **Name**: `kmc-<cluster-name>-telemetry-config`
- **Purpose**: Contains k0s telemetry configuration
- **Condition**: Always created (even if telemetry is disabled, it contains a minimal config)
- **Example**: `kmc-docker-test-telemetry-config`

##### Monitoring ConfigMaps (when monitoring is enabled)

- **Prometheus ConfigMap**: `kmc-prometheus-<cluster-name>-config`
  - **Purpose**: Contains Prometheus and Nginx configuration for metrics collection
- **Nginx ConfigMap**: `kmc-prometheus-<cluster-name>-config-nginx`
  - **Purpose**: Contains Nginx configuration for metrics proxy
- **Condition**: Only created when `spec.monitoring.enabled` is `true`
- **Example**: `kmc-prometheus-docker-test-config` and `kmc-prometheus-docker-test-config-nginx`

##### Ingress Manifests ConfigMaps (when ingress is enabled)

- **API Ingress ConfigMap**: `kmc-<cluster-name>-ingress`
  - **Purpose**: Contains HAProxy manifests for API server ingress
- **Konnectivity Ingress ConfigMap**: `kmc-<cluster-name>-ingress-konnectivity`
  - **Purpose**: Contains Konnectivity agent manifests
- **Condition**: Only created when `spec.ingress` is specified
- **Example**: `kmc-docker-test-ingress` and `kmc-docker-test-ingress-konnectivity`

### Services

The Service name depends on the `spec.service.type` configuration:

#### ClusterIP Service (default)

- **Name**: `kmc-<cluster-name>`
- **Condition**: When `spec.service.type` is `ClusterIP` or not specified
- **Example**: `kmc-docker-test`

#### NodePort Service

- **Name**: `kmc-<cluster-name>-nodeport`
- **Condition**: When `spec.service.type` is `NodePort`
- **Example**: `kmc-docker-test-nodeport`

#### LoadBalancer Service

- **Name**: `kmc-<cluster-name>-lb`
- **Condition**: When `spec.service.type` is `LoadBalancer`
- **Example**: `kmc-docker-test-lb`

#### etcd Service

- **Name**: `kmc-<cluster-name>-etcd`
- **Purpose**: Headless service for etcd StatefulSet
- **Condition**: Created when `spec.kineDataSourceURL` is not set (etcd mode)
- **Example**: `kmc-docker-test-etcd`

### Secrets

#### Kubeconfig Secret

- **Name**: `<cluster-name>-kubeconfig`
- **Purpose**: Contains the admin kubeconfig for the workload cluster
- **Note**: This name is **not** shortened (must match CAPI expectations)
- **Location**: Management cluster (even when `spec.kubeconfigRef` is set)
- **Example**: `docker-test-kubeconfig`

#### Certificate Secrets

Certificate secrets are created using Cluster API's naming convention. These are created in the management cluster (or external cluster if `spec.kubeconfigRef` is specified) by the K0smotron Cluster.

The certificate secrets follow the pattern: `<cluster-name>-<certificate-type>`

- **Cluster CA**: `<cluster-name>-ca`
  - **Type**: `ca` (ClusterCA)
  - **Condition**: Always created
- **Service Account**: `<cluster-name>-sa`
  - **Type**: `sa` (ServiceAccount)
  - **Condition**: Always created
- **Front Proxy CA**: `<cluster-name>-proxy`
  - **Type**: `proxy` (FrontProxyCA)
  - **Condition**: Always created
- **etcd CA**: `<cluster-name>-etcd`
  - **Type**: `etcd` (EtcdCA)
  - **Condition**: Always created
- **API Server etcd Client**: `<cluster-name>-apiserver-etcd-client`
  - **Type**: `apiserver-etcd-client`
  - **Condition**: Created when etcd is used (not using Kine)
- **etcd Server**: `<cluster-name>-etcd-server`
  - **Type**: `etcd-server`
  - **Condition**: Created when etcd is used
- **etcd Peer**: `<cluster-name>-etcd-peer`
  - **Type**: `etcd-peer`
  - **Condition**: Created when etcd is used
- **Ingress HAProxy** (when ingress is enabled): `<cluster-name>-ingress-haproxy`
  - **Type**: `ingress-haproxy`
  - **Condition**: Created when `spec.ingress` is specified

**Example**: For cluster `docker-test`, the certificate secrets are:

- `docker-test-ca`
- `docker-test-sa`
- `docker-test-proxy`
- `docker-test-etcd`
- `docker-test-apiserver-etcd-client` (if etcd is used)
- `docker-test-etcd-server` (if etcd is used)
- `docker-test-etcd-peer` (if etcd is used)

### etcd Resources

#### etcd StatefulSet

- **Name**: `kmc-<cluster-name>-etcd`
- **Purpose**: Manages etcd pods for the control plane storage
- **Condition**: Created when `spec.kineDataSourceURL` is not set (etcd mode)
- **Example**: `kmc-docker-test-etcd`

#### etcd CronJob (when defrag is enabled)

- **Name**: `kmc-<cluster-name>-defrag`
- **Purpose**: Periodic etcd defragmentation job
- **Condition**: Created when `spec.etcd.defragJob.enabled` is `true`
- **Example**: `kmc-docker-test-defrag`

### Ingress Resource

#### Ingress

- **Name**: `kmc-<cluster-name>`
- **Purpose**: Kubernetes Ingress resource for API server and Konnectivity access
- **Condition**: Created when `spec.ingress.deploy` is `true` (default)
- **Example**: `kmc-docker-test`

### External Cluster Resources

When `spec.kubeconfigRef` is specified for a K0smotron Cluster, resources are created in an external cluster. In addition to the resources listed above, the following is also created:

#### External Owner ConfigMap

- **Name**: `<cluster-name>-root-owner`
- **Purpose**: Root owner reference for garbage collection of all resources in the external cluster
- **Condition**: Created when `spec.kubeconfigRef` is specified
- **Example**: `docker-test-root-owner`

## Resources Created in the Workload Cluster

When `spec.ingress` is enabled for a K0smotron Cluster, manifests are deployed into the workload cluster (the cluster being managed). These resources are created via ConfigMaps mounted as manifests.

### Ingress-Related Resources (from manifests)

The following resources are created in the **workload cluster** (not the management cluster) when ingress is enabled:

#### HAProxy DaemonSet

- **Name**: `k0smotron-haproxy`
- **Namespace**: `default`
- **Purpose**: Local HAProxy proxy for API server access
- **Source**: Defined in `kmc-<cluster-name>-ingress` ConfigMap

#### HAProxy ConfigMap

- **Name**: `k0smotron-haproxy-config`
- **Namespace**: `default`
- **Location**: Workload cluster
- **Purpose**: HAProxy configuration
- **Source**: Defined in `kmc-<cluster-name>-ingress` ConfigMap

#### Kubernetes Service

- **Name**: `kubernetes`
- **Namespace**: `default`
- **Location**: Workload cluster
- **Purpose**: Service pointing to HAProxy for API server access
- **Source**: Defined in `kmc-<cluster-name>-ingress` ConfigMap

#### Temporary Endpoints / EndpointSlice

- **Name**: `kubernetes`
- **Namespace**: `default`
- **Location**: Workload cluster
- **Purpose**: Temporary Service endpoint representation for worker profile creation
- **Source**: Defined in `kmc-<cluster-name>-ingress` ConfigMap

#### Konnectivity Agent Resources

- **Location**: Workload cluster
- **DaemonSet**: `konnectivity-agent` (namespace: `kube-system`)
- **ServiceAccount**: `konnectivity-agent` (namespace: `kube-system`)
- **ClusterRoleBinding**: `system:konnectivity-server`
- **Source**: Defined in `kmc-<cluster-name>-ingress-konnectivity` ConfigMap

## Example: Complete Resource List

For a cluster named `docker-test` with (at minimum) the following configuration:

```yaml
apiVersion: k0smotron.io/v1beta1
kind: Cluster
metadata:
  name: docker-test
  namespace: default
spec:
  version: v1.27.2-k0s.0
  persistence:
    type: emptyDir
  service:
    type: NodePort
  replicas: 1
```

In the management/external cluster namespace (here: `default`), the K0smotron Cluster creates the following resources (these are the primary patch/customization targets):

### Example: Cluster (K0smotron)

- `Cluster/docker-test` (k0smotron.io/v1beta1)

### Example: ConfigMaps (K0smotron)

- `ConfigMap/kmc-docker-test-config` (k0s configuration)
- `ConfigMap/kmc-docker-test-telemetry-config` (telemetry configuration)
- `ConfigMap/kmc-entrypoint-docker-test-config` (entrypoint script)

### Example: Secrets (K0smotron / CAPI certificates)

- `Secret/docker-test-kubeconfig` (admin kubeconfig)
- `Secret/docker-test-ca` (cluster CA)
- `Secret/docker-test-proxy` (front proxy CA)
- `Secret/docker-test-sa` (service account)
- `Secret/docker-test-etcd` (etcd CA)
- `Secret/docker-test-apiserver-etcd-client` (API server etcd client certificate)
- `Secret/docker-test-etcd-server` (etcd server certificate)
- `Secret/docker-test-etcd-peer` (etcd peer certificate)

### Example: Services (K0smotron)

- `Service/kmc-docker-test-nodeport` (API access via NodePort)
- `Service/kmc-docker-test-etcd` (etcd headless service)

### Example: StatefulSets (K0smotron)

- `StatefulSet/kmc-docker-test` (k0s control plane)
- `StatefulSet/kmc-docker-test-etcd` (etcd)

In addition to the resources above, Kubernetes will create dependent objects for these controllers (for example `Pod`, `ControllerRevision`, and `EndpointSlice`). These are usually not the direct targets for K0smotron `customizeComponents.patches`, but they will show up in ownership/relationship tooling.

If you want to confirm the full ownership tree in your environment, you can use:

```bash
kubectl tree clusters.k0smotron.io docker-test
```

## Using Resource Names for Patching

When customizing resources generated by a K0smotron Cluster using patches, you can target specific resources using their names as documented in this page. Since all resources generated by a K0smotron Cluster follow the pattern `kmc-<cluster-name><suffix>`, you need to specify the suffix part in the `resourceNameSuffix` field (required, but empty string is allowed for no suffix). The system automatically prepends `kmc-<cluster-name>` to your suffix, making your patches reusable across multiple clusters.

When using Cluster API, patches defined in `K0smotronControlPlane.spec.customizeComponents.patches` are applied to the resources generated by the child `k0smotron.io/v1beta1` Cluster.

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0smotronControlPlane
metadata:
  name: docker-test-cp
spec:
  customizeComponents:
    patches:
    - resourceType: StatefulSet
      resourceNameSuffix: ""  # Empty string for no suffix → kmc-<cluster-name>
      patch: '[{"op": "replace", "path": "/spec/template/spec/containers/0/readinessProbe/initialDelaySeconds", "value": 10}]'
      type: json
    - resourceType: Service
      resourceNameSuffix: "-nodeport"  # → kmc-<cluster-name>-nodeport
      patch: '{"metadata":{"annotations":{"custom-annotation":"value"}}}'
      type: strategic
```

In this example:

- An empty `resourceNameSuffix` (`""`) represents no suffix and results in `kmc-<cluster-name>` (e.g., `kmc-docker-test`)
- `-nodeport` suffix results in `kmc-<cluster-name>-nodeport` (e.g., `kmc-docker-test-nodeport`)

### Resource Name Generation

The system automatically:

- Prepends `kmc-<cluster-name>` to your specified suffix
- Applies the 63-character name length limit using MD5 hash suffix if needed (see [Name Length Limit](#name-length-limit))
- Ensures the final resource name matches the actual generated resource name

### Common Suffixes

Here are common suffixes for different resource types:

- **StatefulSet** (k0s control plane): `""` (empty string for no suffix) → `kmc-<cluster-name>`
- **Service** (NodePort): `"-nodeport"` → `kmc-<cluster-name>-nodeport`
- **Service** (LoadBalancer): `"-lb"` → `kmc-<cluster-name>-lb`
- **Service** (ClusterIP): `""` (empty string for no suffix) → `kmc-<cluster-name>`
- **etcd StatefulSet**: `"-etcd"` → `kmc-<cluster-name>-etcd`
- **etcd Service**: `"-etcd"` → `kmc-<cluster-name>-etcd`
- **ConfigMap** (main config): `"-config"` → `kmc-<cluster-name>-config`
- **Ingress**: `""` (empty string for no suffix) → `kmc-<cluster-name>`

For a complete list of resource names and their patterns, see the sections above.

## Notes

- All resource names are scoped to the namespace where the `k0smotron.io/v1beta1` Cluster resource is created (or the `K0smotronControlPlane` resource when using Cluster API)
- Resource names are deterministic and predictable based on the cluster name
- When using external clusters (`spec.kubeconfigRef`), resources are created in the external cluster's namespace
- The external owner ConfigMap (`<cluster-name>-root-owner`) is used for garbage collection when the cluster is deleted
