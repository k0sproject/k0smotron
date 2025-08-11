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
      k0sImage: quay.io/k0sproject/k0s
      version: v1.27.1-k0s.0
    ```

2. Change all the k0s versions to the target one. For example:

    ```yaml
    apiVersion: k0smotron.io/v1beta1
    kind: Cluster
    metadata:
      name: k0smotron-test
    spec:
      replicas: 1
      k0sImage: quay.io/k0sproject/k0s
      version: v1.28.7-k0s.0 # new k0s version
    ```

3. Update the resources:

   ```bash
   kubectl apply -f ./path-to-file.yaml
   ```

The update procedure is completed, you now have the target version of k0smotron.
