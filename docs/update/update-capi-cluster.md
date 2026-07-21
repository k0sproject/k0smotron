# Update control plane nodes in Cluster API clusters (VMs)

This page explains what can trigger a control plane upgrade in k0smotron and how to perform one.

!!! info "Worker nodes"
    This page only covers control plane machines. See [Update worker nodes in Cluster API clusters (VMs)](update-capi-cluster-workers.md) for how (and whether) worker machines are covered by each mechanism described below.

## What triggers a control plane upgrade

k0smotron continuously reconciles each `K0sControlPlane` resource and detects that an upgrade is needed when any of the following change:

### k0s version change

The most common trigger is changing `spec.version` in the `K0sControlPlane` resource. k0smotron compares this value against the version reported by each control plane machine and starts the upgrade process if they differ.

Version values are normalized before comparison, so for example `v1.31.2` and `v1.31.2+k0s.0` are treated as equivalent.

### Configuration change

k0smotron handles configuration changes differently depending on which part of `spec.k0sConfigSpec` is modified:

- **Changes to `spec.k0sConfigSpec.k0s`** (the k0s `ClusterConfig` object) are applied using [k0s dynamic configuration](https://docs.k0sproject.io/stable/dynamic-configuration/). k0smotron patches the `ClusterConfig` resource directly in the workload cluster without replacing any machines. Note that some fields cannot be changed via dynamic configuration and are ignored. See the [k0s documentation](https://docs.k0sproject.io/stable/dynamic-configuration/#cluster-configuration-vs-controller-node-configuration) for the full list.

- **Changes to any other field in `spec.k0sConfigSpec`** (such as `args`, `files`, `preStartCommands`, etc.) are detected by comparing a hash of the bootstrap config stored in each machine's annotations against the current spec. Machines whose config no longer matches are marked for replacement, using the Recreate workflow regardless of `spec.updateStrategy`. 

!!! warning
    k0smotron only detects configuration changes made **directly in the `K0sControlPlane` spec**. The `spec.k0sConfigSpec.files` field supports loading file content from external `Secret` or `ConfigMap` objects via `contentFrom`, but if only the content of those objects changes, k0smotron will **not** detect it and no upgrade will be triggered. To propagate updated file content, create a new `Secret` or `ConfigMap` and update `contentFrom` in the `K0sControlPlane` spec to reference the new object.

### Machine template change

When `spec.machineTemplate.infrastructureRef` points to a new or changed infrastructure template, machines that were cloned from an older template revision are marked for replacement. k0smotron detects this by inspecting the `cluster.x-k8s.io/cloned-from-name` and `cluster.x-k8s.io/cloned-from-groupkind` annotations on each infrastructure machine.

Like configuration changes, template changes always trigger machine recreation.

## Update strategies

k0smotron supports three update strategies, configured via `spec.updateStrategy`:

| Strategy | Behavior |
|---|---|
| `InPlace` (default) | Updates k0s on existing machines without replacing them, using [k0s autopilot](https://docs.k0sproject.io/stable/autopilot/) |
| `Recreate` | Creates new machines first, then removes old ones |
| `RecreateDeleteFirst` | Removes old machines first, then creates new ones |

!!! warning
    The `Recreate` strategy is not supported for clusters running in `--single` mode.

!!! warning
    The `RecreateDeleteFirst` strategy requires at least 3 control plane nodes.

### Use InPlace strategy

Under the `InPlace` strategy, control plane nodes are always updated the same way under the hood: k0smotron creates a k0s [autopilot](https://docs.k0sproject.io/stable/autopilot/) `Plan` in the workload cluster, and autopilot rolls the new k0s version onto each control plane node without replacing the machine.

This in-place mechanism only updates **the k0s version** running on a node. It does not apply configuration or machine template changes, those are always handled by recreating machines regardless of `spec.updateStrategy`, as described above.

#### Standalone (default)

k0smotron's `K0sControlPlane` controller manages the autopilot `Plan` itself, independently of Cluster API's machine lifecycle. No webhook server or extra component is required.

#### Cluster API in-place updates (experimental)

k0smotron instead delegates the rollout to Cluster API's own [in-place updates mechanism](https://github.com/kubernetes-sigs/cluster-api/blob/main/docs/proposals/20240807-in-place-updates.md): a k0smotron webhook server, installed as a Cluster API runtime extension, receives the `UpdateMachine` hook calls that Cluster API core sends for each machine and creates the autopilot `Plan` on its behalf. This path is used automatically once all of the following are satisfied:

  - Cluster API core **v1.12.0 or newer**.
  
  - The `InPlaceUpdates` feature gate enabled on Cluster API core (`EXP_IN_PLACE_UPDATES=true`).
  
  - The k0smotron in-place version update extension installed in the management cluster:

```bash
kubectl apply --server-side=true -f https://docs.k0smotron.io/{{{ extra.k0smotron_version }}}/install-extension-webhook.yaml
```

!!! note

    If any of the requirements above aren't met, k0smotron transparently falls back to the standalone path, no further action is required.

!!! info "Worker nodes"
    Because the standalone path has no controller watching worker `Machine`/`MachineDeployment` objects, only the Cluster API in-place updates extension can perform in-place updates of worker nodes — the same webhook server also handles the hook calls Cluster API sends for machines owned by a `MachineDeployment`/`MachineSet`. See [Update worker nodes in Cluster API clusters (VMs)](update-capi-cluster-workers.md) for details.

## Monitoring upgrade status

The `K0sControlPlane` status fields give visibility into an in-progress upgrade:

```bash
kubectl get k0scontrolplane <name> -o yaml
```

Relevant status fields:

| Field | Description |
|---|---|
| `status.replicas` | Total number of non-terminated control plane machines |
| `status.readyReplicas` | Machines that are fully running and ready |
| `status.upToDateReplicas` | Machines running the desired k0s version |
| `status.availableReplicas` | Machines currently available to serve traffic |
| `status.version` | Minimum Kubernetes version across all machines |

For `InPlace` upgrades, you can also inspect the autopilot plan running inside the workload cluster:

```bash
kubectl --kubeconfig <workload-cluster-kubeconfig> get plan autopilot -o yaml
```

See the [k0s autopilot documentation](https://docs.k0sproject.io/stable/autopilot/?h=plan#how-it-works) for a description of the plan states.

## Updating the control plane using k0s autopilot (InPlace)

When `spec.updateStrategy` is `InPlace` (or omitted), k0smotron uses [k0s autopilot](https://docs.k0sproject.io/stable/autopilot/) to update k0s on each control plane node without replacing the machines. This is faster than recreating machines and keeps any local data on the node intact.

1. Check the configuration of your deployed cluster. For example:

    ```yaml
    apiVersion: cluster.x-k8s.io/v1beta2
    kind: Cluster
    metadata:
      name: docker-test
      namespace: default
    spec:
      clusterNetwork:
        pods:
          cidrBlocks:
          - 192.168.0.0/16
        serviceDomain: cluster.local
        services:
          cidrBlocks:
          - 10.128.0.0/12
      controlPlaneRef:
        apiGroup: controlplane.cluster.x-k8s.io
        kind: K0sControlPlane
        name: docker-test-cp
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: DevCluster
        name: docker-test
    ---
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: DevCluster
    metadata:
      name: docker-test
      namespace: default
    spec:
      backend:
        docker: {}
    ---
    apiVersion: controlplane.cluster.x-k8s.io/v1beta2
    kind: K0sControlPlane
    metadata:
      name: docker-test-cp
    spec:
      replicas: 3
      version: v1.31.2+k0s.0
      updateStrategy: InPlace
      k0sConfigSpec:
        args:
          - --enable-worker
        k0s:
          apiVersion: k0s.k0sproject.io/v1beta1
          kind: ClusterConfig
          metadata:
            name: k0s
          spec:
            api:
              extraArgs:
                anonymous-auth: "true" # anonymous-auth=true is needed for k0s to allow unauthorized health-checks on /healthz
            telemetry:
              enabled: true
      machineTemplate:
        infrastructureRef:
          apiGroup: infrastructure.cluster.x-k8s.io
          kind: DevMachineTemplate
          name: docker-test-cp-template
    ---
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: DevMachineTemplate
    metadata:
      name: docker-test-cp-template
      namespace: default
    spec:
      template:
        spec:
          backend:
            docker:
              customImage: kindest/node:v1.31.0
    ```

2. Update `spec.version` to the [target k0s release](https://docs.k0sproject.io/stable/releases/#k0s-release-and-support-model):

   ```yaml
   apiVersion: controlplane.cluster.x-k8s.io/v1beta2
   kind: K0sControlPlane
   metadata:
     name: docker-test-cp
   spec:
     replicas: 3
     version: v1.31.3+k0s.0 # updated version
     updateStrategy: InPlace
     k0sConfigSpec:
       args:
         - --enable-worker
       k0s:
         apiVersion: k0s.k0sproject.io/v1beta1
         kind: ClusterConfig
         metadata:
           name: k0s
         spec:
           api:
             extraArgs:
               anonymous-auth: "true"
           telemetry:
             enabled: true
     machineTemplate:
       infrastructureRef:
         apiGroup: infrastructure.cluster.x-k8s.io
         kind: DevMachineTemplate
         name: docker-test-cp-template
   ```

3. Apply the change:

   ```bash
   kubectl apply -f ./path-to-file.yaml
   ```

   k0smotron creates an autopilot `Plan` resource inside the workload cluster that orchestrates the rolling update across all control plane nodes.

## Updating the control plane using the Cluster API workflow (Recreate)

When `spec.updateStrategy` is `Recreate`, k0smotron replaces control plane machines one at a time: it creates new machines at the desired version, waits for them to become ready, then removes the old ones.

When `spec.updateStrategy` is `RecreateDeleteFirst`, it removes an old machine first before creating the replacement. This is useful when resources are constrained, but requires at least 3 control plane nodes to maintain quorum during the rollout.

!!! warning
    The `Recreate` strategy is not supported for clusters running in `--single` mode.

1. Check the configuration of your deployed cluster. For example:

    ```yaml
    apiVersion: cluster.x-k8s.io/v1beta2
    kind: Cluster
    metadata:
      name: docker-test
      namespace: default
    spec:
      clusterNetwork:
        pods:
          cidrBlocks:
          - 192.168.0.0/16
        serviceDomain: cluster.local
        services:
          cidrBlocks:
          - 10.128.0.0/12
      controlPlaneRef:
        apiGroup: controlplane.cluster.x-k8s.io
        kind: K0sControlPlane
        name: docker-test-cp
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: DevCluster
        name: docker-test
    ---
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: DevCluster
    metadata:
      name: docker-test
      namespace: default
    spec:
      backend:
        docker: {}
    ---
    apiVersion: controlplane.cluster.x-k8s.io/v1beta2
    kind: K0sControlPlane
    metadata:
      name: docker-test-cp
    spec:
      replicas: 3
      version: v1.31.2+k0s.0
      updateStrategy: Recreate
      k0sConfigSpec:
        args:
          - --enable-worker
        k0s:
          apiVersion: k0s.k0sproject.io/v1beta1
          kind: ClusterConfig
          metadata:
            name: k0s
          spec:
            api:
              extraArgs:
                anonymous-auth: "true" # anonymous-auth=true is needed for k0s to allow unauthorized health-checks on /healthz
            telemetry:
              enabled: true
      machineTemplate:
        infrastructureRef:
          apiGroup: infrastructure.cluster.x-k8s.io
          kind: DevMachineTemplate
          name: docker-test-cp-template
    ---
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: DevMachineTemplate
    metadata:
      name: docker-test-cp-template
      namespace: default
    spec:
      template:
        spec:
          backend:
            docker:
              customImage: kindest/node:v1.31.0
    ```

2. Update `spec.version` to the [target k0s release](https://docs.k0sproject.io/stable/releases/#k0s-release-and-support-model):

   ```yaml
   apiVersion: controlplane.cluster.x-k8s.io/v1beta2
   kind: K0sControlPlane
   metadata:
     name: docker-test-cp
   spec:
     replicas: 3
     version: v1.31.3+k0s.0 # updated version
     updateStrategy: Recreate
     k0sConfigSpec:
      args:
        - --enable-worker
      k0s:
        apiVersion: k0s.k0sproject.io/v1beta1
        kind: ClusterConfig
        metadata:
          name: k0s
        spec:
          api:
            extraArgs:
              anonymous-auth: "true" # anonymous-auth=true is needed for k0s to allow unauthorized health-checks on /healthz
          telemetry:
            enabled: true
     machineTemplate:
       infrastructureRef:
         apiGroup: infrastructure.cluster.x-k8s.io
         kind: DevMachineTemplate
         name: docker-test-cp-template
   ```

3. Update the resources:

   ```bash
   kubectl apply -f ./path-to-file.yaml
   ```

## Known issues

Due to a bug in older k0s autopilot versions, the control plane upgrade may get stuck on the `Cordoning` stage when control plane nodes also run workloads (for example, when the `--enable-worker` flag is used). This bug is fixed in the latest patch versions of k0s.

If the upgrade stalls, use the following steps to recover:

1. Identify the node that is stuck:

   ```bash
   kubectl --kubeconfig <workload-cluster-kubeconfig> get plan autopilot -o yaml
   ```

2. Manually drain the node.

3. Patch the `k0sproject.io/autopilot-signal-data` annotation on the corresponding `ControlNode` object: change the `status` field in the JSON value from `Cordoning` to `ApplyingUpdate`.

4. Repeat for any other nodes that are stuck.
