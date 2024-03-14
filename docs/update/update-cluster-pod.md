# Update k0smotron in Cluster API integrated cluster

To update k0smotron cluster deployed with Cluster API, you need to update
the k0s version and machine names in the YAML configuration file.

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
        kind: K0smotronControlPlane
        name: docker-test-cp
      infrastructureRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
        kind: DockerCluster
        name: docker-test
    ---
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: K0smotronControlPlane
    metadata:
      name: docker-test-cp
    spec:
      version: v1.27.2-k0s.0
    ```
2. Configure [persistence](https://docs.k0smotron.io/stable/resource-reference/#clusterspecpersistence)
to prevent data loss. For example:

   ```yaml
    ---
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: K0smotronControlPlane
    metadata:
      name: docker-test-cp
    spec:
      version: v1.27.2-k0s.0
      persistence:
        type: hostPath
        hostPath: "/tmp/kmc-test" # k0smotron will mount a basic hostPath volume to avoid data loss.
   ```

   Do not configure `hostPath` persistence in production environment. 
   Learn more from the official Kubernetes documentation on [hostPath](https://kubernetes.io/docs/concepts/storage/volumes/#hostpath).

3. Change all the k0s versions to [the target one](https://docs.k0sproject.io/v1.29.2+k0s.0/releases/#k0s-release-and-support-model). For example:

   ```yaml
   apiVersion: controlplane.cluster.x-k8s.io/v1beta1
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
   apiVersion: cluster.x-k8s.io/v1beta1
   kind: Machine
   metadata:
     name:  docker-test-1 # new machine
     namespace: default
   spec:
     version: v1.28.7 # new version
     clusterName: docker-test
     bootstrap:
       configRef:
         apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
         kind: K0sWorkerConfig
         name: docker-test-1 # new machine
     infrastructureRef:
       apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
       kind: DockerMachine
       name: docker-test-1 # new machine
   ---
   apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
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