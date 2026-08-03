# Update hosted control plane in Cluster API integrated cluster

To update k0smotron cluster deployed with Cluster API, you need to update
the k0s version and machine names in the YAML configuration file:

1. Localize the configuration of deployed k0smotron cluster in your repository. For example:

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
        kind: K0smotronControlPlane
        name: docker-test-cp
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: DevCluster
        name: docker-test
    ---
    apiVersion: controlplane.cluster.x-k8s.io/v1beta2
    kind: K0smotronControlPlane
    metadata:
      name: docker-test-cp
    spec:
      version: v1.27.2-k0s.0
    ```
2. Make sure that any extra manifests the control plane needs are supplied via
`spec.manifests`, so that they are reapplied when the control plane pod is recreated:

   ```yaml
    ---
    apiVersion: controlplane.cluster.x-k8s.io/v1beta2
    kind: K0smotronControlPlane
    metadata:
      name: docker-test-cp
    spec:
      version: v1.27.2-k0s.0
      manifests:
        - name: mystack
          configMap:
            name: mystack-manifests
   ```

   Control plane pods are stateless, so no additional persistence is needed for the
   update. Cluster data is held by the storage backend, which has its own volume.
   See [Persistence](../configuration.md#persistence) for details. Files written into
   `/var/lib/k0s` out of band are lost when the pod is recreated.

3. Change all the k0s versions to the target one. For example:

   ```yaml
   apiVersion: controlplane.cluster.x-k8s.io/v1beta2
   kind: K0smotronControlPlane
   metadata:
     name: cp-test
   spec:
     version: v1.28.7-k0s.0 # new k0s version
   ```

4. In the same configuration, replace the names of machines running the old k0smotron version
with the new names to create machines for the target k0smotron version. For example:

   ```yaml
   ---
   apiVersion: cluster.x-k8s.io/v1beta2
   kind: Machine
   metadata:
     name:  docker-test-1 # new machine
     namespace: default
   spec:
     version: v1.28.7 # new version
     clusterName: docker-test
     bootstrap:
       configRef:
         apiGroup: bootstrap.cluster.x-k8s.io
         kind: K0sWorkerConfig
         name: docker-test-1 # new machine
     infrastructureRef:
       apiGroup: infrastructure.cluster.x-k8s.io
       kind: DevMachine
       name: docker-test-1 # new machine
   ---
   apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
   kind: K0sWorkerConfig
   metadata:
     name: docker-test-1 # new machine
     namespace: default
   spec:
     version: v1.28.7+k0s.0 # new version
   ```

5. Update the resources:

   ```bash
   kubectl apply -f ./path-to-file.yaml
   ```


6. Remove the machines running the old k0smotron version:

   ```bash
   kubectl delete machine docker-test-0
   ```

The update procedure is completed, you now have the target version of k0smotron.
