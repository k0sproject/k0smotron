# Update control nodes in Cluster API clusters

k0smotron uses [k0s autopilot](https://docs.k0sproject.io/stable/autopilot/)
to seamlessly update the k0s version on the control plane **in-place**.

k0smotron does not deploy new machines for the new nodes
to make the control plane upgrade process faster by avoiding the need to spin up
and configure the new machine, and safer by keeping any data on the machine secure.
This differs from the usual Cluster API workflow,
where deploying the new control plane is followed by decommissioning of the old one.

1. Localize the configuration of deployed k0smotron cluster in your repository. For example:

    ```yaml 
    apiVersion: cluster.x-k8s.io/v1beta1
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
        apiVersion: controlplane.cluster.x-k8s.io/v1beta1
        kind: K0sControlPlane
        name: docker-test-cp
      infrastructureRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
        kind: DockerCluster
        name: docker-test
    ---
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: K0sControlPlane
    metadata:
      name: docker-test-cp
    spec:
      replicas: 3
      version: v1.28.7+k0s.0
      machineTemplate:
        infrastructureRef:
          apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
          kind: DockerMachineTemplate
          name: docker-test-cp-template
          namespace: default
    ---
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: DockerMachineTemplate
    metadata:
      name: docker-test-cp-template
      namespace: default
    spec:
      template:
        spec: {}
    ```

2. Change all the k0s versions to [the target one](https://docs.k0sproject.io/v1.29.2+k0s.0/releases/#k0s-release-and-support-model). For example:

   ```yaml
   apiVersion: controlplane.cluster.x-k8s.io/v1beta1
   kind: K0sControlPlane
   metadata:
     name: docker-test-cp
   spec:
     replicas: 3
     version: v1.29.2+k0s.0
     machineTemplate:
       infrastructureRef:
         apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
         kind: DockerMachineTemplate
         name: docker-test-cp-template
         namespace: default
   ```

3. Update the resources:

   ```bash
   kubectl apply -f ./path-to-file.yaml


## Known issues

Due to the bug in the older k0s autopilot versions,
the control plane upgrade may get stuck on the `Cordoning` stage
if the control plane is running on the same node as
the worker nodes. For example, `--enable-worker` flag was used during
the control plane deployment.

To fix this issue:
- Check the current node that is being updated from the `kubectl get plan autopilot -o yaml` output.
- Manually drain the node.
- In `Controlnode` object patch the corresponding `k0sproject.io/autopilot-signal-data` annotation:
  change the `version` field in the JSON from `Cordoning` to `ApplyingUpdate`.
- Repeat the steps for all the nodes that are being updated.
