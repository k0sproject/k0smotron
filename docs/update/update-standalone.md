# Update hosted control plane in standalone mode

To update a standalone k0smotron cluster, you need to update the k0s version
in the YAML configuration file:

1. Localize the configuration of deployed k0smotron cluster in your repository. For example:

    ```yaml
    apiVersion: k0smotron.io/v1beta1
    kind: Cluster
    metadata:
      name: k0smotron-test
    spec:
      replicas: 1
      k0sImage: k0sproject/k0s
      version: v1.27.1-k0s.0
    ```

2. Make sure that the [persistence](https://docs.k0smotron.io/stable/resource-reference/#clusterspecpersistence) is configured
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

   Using the `hostPath` volume type introduces many security risks.
   Avoid configuring persistence for volumes of the `hostPath` type in production environments. 
   Learn more from [official Kubernetes documentation: hostPath](https://kubernetes.io/docs/concepts/storage/volumes/#hostpath).

3. Change all the k0s versions to the target one. For example:

    ```yaml
    apiVersion: k0smotron.io/v1beta1
    kind: Cluster
    metadata:
      name: k0smotron-test
    spec:
      replicas: 1
      k0sImage: k0sproject/k0s
      version: v1.28.7-k0s.0 # new k0s version
    ```

4. Update the resources:

   ```bash
   kubectl apply -f ./path-to-file.yaml
   ```

The update procedure is completed, you now have the target version of k0smotron.
