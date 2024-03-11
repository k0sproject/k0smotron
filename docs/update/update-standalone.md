# Update k0smotron in standalone cluster

To update a standalone k0smotron cluster, you need to update the k0s version
in the YAML configuration file.

!!! warning "Data loss"

    The procedure below lacks persistence and should be applied only if the cluster data is 
    insignificant. To prevent data loss, update workers manually or use
    [k0s autopilot](https://docs.k0sproject.io/stable/autopilot/), which ensures data persistence.

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

2. Change all the k0s versions to
[the target one](https://docs.k0sproject.io/v1.29.2+k0s.0/releases/#k0s-release-and-support-model). For example:

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

3. Update the resources:

   ```bash
   kubectl apply -f ./path-to-file.yaml
   ```
