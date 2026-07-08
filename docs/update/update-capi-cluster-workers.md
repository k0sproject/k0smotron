# Update worker nodes in Cluster API clusters (VMs)

This page explains how k0s gets updated in-place on worker machines of a Cluster API–managed cluster running on VMs, and why coverage depends on which of the two [`InPlace` mechanisms](update-capi-cluster.md#use-inplace-strategy) is active in your management cluster.

Just like for control plane nodes, both mechanisms described below only update **the k0s version** running on a worker node, they don't touch its configuration or machine template. Changes to those still go through Cluster API's regular machine-replacement lifecycle.

## Why worker node coverage differs

For control plane nodes, k0smotron always ends up creating an autopilot `Plan`, either through its own `K0sControlPlane` controller (standalone) or through the [Cluster API in-place updates extension](update-capi-cluster.md#cluster-api-in-place-updates-experimental). There is no equivalent k0smotron controller that watches worker `Machine`/`MachineDeployment` objects, so worker coverage depends entirely on which path is active:

| Mechanism | Worker nodes covered? |
|---|---|
| Standalone (k0s autopilot driven by `K0sControlPlane`) | No. You must create the autopilot `Plan` yourself. |
| Cluster API in-place updates extension | Yes, automatically. The same webhook server that handles control plane machines also receives the hook calls Cluster API sends for machines owned by a `MachineDeployment`/`MachineSet`. |

## Updating workers with the Cluster API in-place updates extension

This requires the same prerequisites as for control plane nodes. See [Cluster API in-place updates](update-capi-cluster.md#cluster-api-in-place-updates-experimental).

Once the extension is active, bump `spec.template.spec.version` on the `MachineDeployment` (and the referenced `K0sWorkerConfigTemplate`, if you keep the two in sync) to the target k0s version. Cluster API calls into the extension for each affected `Machine`, and the extension creates and monitors the autopilot `Plan` on your behalf, no manual `Plan` needed.

!!! warning
    Set `spec.rollout.strategy.rollingUpdate.maxUnavailable` to `1` on the `MachineDeployment` (it cannot be `0`) for in-place updates to work correctly.

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: MachineDeployment
metadata:
  name: docker-test-md
spec:
  replicas: 2
  clusterName: docker-test
  selector:
    matchLabels:
      cluster.x-k8s.io/cluster-name: docker-test
      pool: worker-pool-1
  rollout:
    strategy:
      rollingUpdate:
        maxSurge: 1
        maxUnavailable: 1 # required for in-place updates, cannot be 0
      type: RollingUpdate
  template:
    metadata:
      labels:
        cluster.x-k8s.io/cluster-name: docker-test
        pool: worker-pool-1
    spec:
      version: v1.31.3+k0s.0 # updated version
      clusterName: docker-test
      bootstrap:
        configRef:
          apiGroup: bootstrap.cluster.x-k8s.io
          kind: K0sWorkerConfigTemplate
          name: docker-test-wct
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: DevMachineTemplate
        name: docker-test-worker-dmt
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfigTemplate
metadata:
  name: docker-test-wct
spec:
  template:
    spec:
      version: v1.31.3+k0s.0 # updated version
```

Applying this change is enough to trigger the rollout; you can monitor it the same way as [control plane upgrades](update-capi-cluster.md#monitoring-upgrade-status), by inspecting the autopilot `Plan` in the workload cluster:

```bash
kubectl --kubeconfig <workload-cluster-kubeconfig> get plan autopilot -o yaml
```

## Updating workers manually with k0s autopilot (standalone)

When the Cluster API in-place updates extension isn't installed, or its requirements aren't met, k0smotron has no controller that manages worker updates. Bumping the version on a `MachineDeployment` in this mode follows Cluster API's default machine lifecycle instead (replacing machines), not an in-place update.

To update k0s in-place on worker nodes without replacing the underlying machines, create the autopilot `Plan` yourself directly in the workload cluster, targeting the worker nodes by name:

!!! warning
    A manually created `Plan` only updates k0s on the target nodes — it does not touch the corresponding `Machine`/`MachineDeployment` objects. So even though Cluster API's view (`Machine.spec.version`/`status`) will keep showing the old version, the node itself is actually running the version applied by the `Plan`.

!!! note
    The node name used in `discovery.static.nodes` must match the `Machine` name, so make sure `AUTOPILOT_HOSTNAME` is set as described in [Bring your own machines with Cluster API](../capi-adoption.md).

```yaml
apiVersion: autopilot.k0sproject.io/v1beta2
kind: Plan
metadata:
  name: autopilot
spec:
  id: id123 # Unique ID for the plan
  timestamp: now
  commands:
  - k0supdate:
      version: v1.31.3+k0s.0 # Target k0s version
      platforms:
        linux-amd64:
          url: https://get.k0sproject.io/v1.31.3+k0s.0/k0s-v1.31.3+k0s.0-amd64
        linux-arm64:
          url: https://get.k0sproject.io/v1.31.3+k0s.0/k0s-v1.31.3+k0s.0-arm64
      targets:
        workers:
          discovery:
            static:
              nodes:
              - docker-test-md-0 # Machine name of the worker node to update
              - docker-test-md-1
```

See [k0s autopilot configuration](https://docs.k0sproject.io/stable/autopilot/#configuration) for other discovery options, such as targeting nodes by label with a `selector`.
