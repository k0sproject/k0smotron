# Update standalone cluster

To update a standalone k0smotron cluster, you need to update the k0s version
in the YAML configuration file.

!!! warning "Data loss"

    The described example does not include persistence. If you proceed with it, 
    you will lose all your data during the update process.
    To prevent data loss, you can update workers manually or use
    [k0s autopilot](https://docs.k0sproject.io/stable/autopilot/) instead.

1. Localize configuration of deployed k0smotron cluster in your repository:

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

2. Change the k0s version to the latest one:

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

3. Run the `kubectl apply -f ./path-to-file.yaml` command to update the resources.
