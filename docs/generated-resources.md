# Generated Resources

This document describes all Kubernetes resources created and managed by a `k0smotron.io/v1beta1` Cluster (K0smotron Cluster). This information is essential when you need to customize these resources.

When using Cluster API (CAPI), a `K0smotronControlPlane` resource creates a `k0smotron.io/v1beta1` Cluster, which then produces the resources documented here.

## Scope and audience

This page is written for **users who need to know the names and naming patterns of resources** created by K0smotron Clusters. It intentionally focuses on:

- resource **names and name patterns**
- **where** resources are created (management/external/workload cluster)
- **when** resources are created (conditions / feature flags)

It does **not** describe controller implementation details (for example, internal reconciliation logic), since those tend to change over time and are not needed for patching.

## Naming Conventions Overview

### Prefix Pattern

Most resources created and managed by a `k0smotron.io/v1beta1` Cluster use the `kmc-<cluster-name>` prefix. For most of those resources, a descriptive suffix is added, resulting in the pattern:

- `kmc-<cluster-name>-<suffix>`

Where `<suffix>` is a stable, purpose-driven identifier (for example `config`, `entrypoint-config`, `etcd`, or `nodeport`). An empty suffix is allowed for resources that intentionally use the base name `kmc-<cluster-name>`.

Some resources follow other naming conventions (for example, the kubeconfig Secret and Cluster API-managed certificate Secrets). These are called out in the relevant sections below.

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
- **Component**: `control-plane`
- **Purpose**: Manages the k0s control plane pods
- **Example**: For cluster `docker-test`, the StatefulSet is named `kmc-docker-test`

#### ConfigMaps

##### Main Configuration ConfigMap

- **Name**: `kmc-<cluster-name>-config`
- **Component**: `cluster-config`
- **Purpose**: Contains the k0s configuration YAML (`K0SMOTRON_K0S_YAML`)
- **Example**: `kmc-docker-test-config`

##### Entrypoint ConfigMap

- **Name**: `kmc-<cluster-name>-entrypoint-config`
- **Component**: `entrypoint`
- **Purpose**: Contains the entrypoint script (`k0smotron-entrypoint.sh`) executed by the control plane container
- **Example**: `kmc-docker-test-entrypoint-config`

##### Telemetry ConfigMap

- **Name**: `kmc-<cluster-name>-telemetry-config`
- **Component**: `telemetry`
- **Purpose**: Contains k0s telemetry configuration
- **Condition**: Always created (even if telemetry is disabled, it contains a minimal config)
- **Example**: `kmc-docker-test-telemetry-config`

##### Monitoring ConfigMaps (when monitoring is enabled)

- **Prometheus ConfigMap**: `kmc-<cluster-name>-prometheus-config`
  - **Component**: `monitoring`
  - **Purpose**: Contains Prometheus and Nginx configuration for metrics collection
- **Nginx ConfigMap**: `kmc-<cluster-name>-prometheus-config-nginx`
  - **Component**: `monitoring`
  - **Purpose**: Contains Nginx configuration for metrics proxy
- **Condition**: Only created when `spec.monitoring.enabled` is `true`
- **Example**: `kmc-docker-test-prometheus-config` and `kmc-docker-test-prometheus-config-nginx`

##### Ingress Manifests ConfigMaps (when ingress is enabled)

- **API Ingress ConfigMap**: `kmc-<cluster-name>-ingress`
  - **Component**: `ingress`
  - **Purpose**: Contains HAProxy manifests for API server ingress
- **Konnectivity Ingress ConfigMap**: `kmc-<cluster-name>-ingress-konnectivity`
  - **Component**: `ingress`
  - **Purpose**: Contains Konnectivity agent manifests
- **Condition**: Only created when `spec.ingress` is specified
- **Example**: `kmc-docker-test-ingress` and `kmc-docker-test-ingress-konnectivity`

### Services

The Service name depends on the `spec.service.type` configuration:

#### ClusterIP Service (default)

- **Name**: `kmc-<cluster-name>`
- **Component**: `control-plane`
- **Condition**: When `spec.service.type` is `ClusterIP` or not specified
- **Example**: `kmc-docker-test`

#### NodePort Service

- **Name**: `kmc-<cluster-name>-nodeport`
- **Component**: `control-plane`
- **Condition**: When `spec.service.type` is `NodePort`
- **Example**: `kmc-docker-test-nodeport`

#### LoadBalancer Service

- **Name**: `kmc-<cluster-name>-lb`
- **Component**: `control-plane`
- **Condition**: When `spec.service.type` is `LoadBalancer`
- **Example**: `kmc-docker-test-lb`

#### etcd Service

- **Name**: `kmc-<cluster-name>-etcd`
- **Component**: `etcd`
- **Purpose**: Headless service for etcd StatefulSet
- **Condition**: Created when `spec.kineDataSourceURL` is not set (etcd mode)
- **Example**: `kmc-docker-test-etcd`

### Secrets

#### Kubeconfig Secret

- **Name**: `<cluster-name>-kubeconfig`
- **Component**: `kubeconfig`
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
- **Component**: `etcd`
- **Purpose**: Manages etcd pods for the control plane storage
- **Condition**: Created when `spec.kineDataSourceURL` is not set (etcd mode)
- **Example**: `kmc-docker-test-etcd`

#### etcd CronJob (when defrag is enabled)

- **Name**: `kmc-<cluster-name>-defrag`
- **Component**: `etcd`
- **Purpose**: Periodic etcd defragmentation job
- **Condition**: Created when `spec.etcd.defragJob.enabled` is `true`
- **Example**: `kmc-docker-test-defrag`

### Ingress Resource

#### Ingress

- **Name**: `kmc-<cluster-name>`
- **Component**: `ingress`
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
- `ConfigMap/kmc-docker-test-entrypoint-config` (entrypoint script)

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

In addition to the resources above, Kubernetes will create dependent objects for these controllers (for example `Pod`, `ControllerRevision`, and `EndpointSlice`). These are usually not the direct targets for patch-based customization, but they will show up in ownership/relationship tooling.

If you want to confirm the full ownership tree in your environment, you can use:

```bash
kubectl tree clusters.k0smotron.io docker-test
```

## Notes

- All resource names are scoped to the namespace where the `k0smotron.io/v1beta1` Cluster resource is created (or the `K0smotronControlPlane` resource when using Cluster API)
- Resource names are deterministic and predictable based on the cluster name
- When using external clusters (`spec.kubeconfigRef`), resources are created in the external cluster's namespace
- The external owner ConfigMap (`<cluster-name>-root-owner`) is used for garbage collection when the cluster is deleted
