# Update Cluster API cluster

To update k0smotron cluster deployed with Cluster API, you need to update
the k0s version and machine names in the YAML configuration file.

!!! warning "Data loss" 

    The described example does not include persistence.
    If you proceed with it, you will lose all your data during the update process.
    To prevent data loss, you can update workers manually or use [k0s autopilot](https://docs.k0sproject.io/stable/autopilot/) instead.

1. Localize configuration of deployed k0smotron cluster in your repository:

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

2. Change all the k0s versions to the latest one:

   ```yaml
   apiVersion: controlplane.cluster.x-k8s.io/v1beta1
   kind: K0smotronControlPlane
   metadata:
     name: cp-test
   spec:
     version: v1.28.7-k0s.0 # new k0s version
   ```

3. Replace old machines in your cluster with new ones. Next, update the names of the machines in the configuration:

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
   
4. Run the `kubectl apply -f ./path-to-file.yaml` command to update the resources.