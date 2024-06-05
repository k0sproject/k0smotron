# Set up CAPI development environment

To contribute to k0smotron, ensure you have set up the development environment properly.
For simple development tasks, it is enough to install Go,
and follow [k0smotron GitHub workflow](contribute-workflow.md).
For more complicated changes that require running CAPI (Common Application Programming Interface)
tests, follow the steps below to configure your environment:

1. Create Docker network:

    ```bash
    docker network create kind --opt com.docker.network.bridge.enable_ip_masquerade=true
    ```

2. Use KinD (Kubernetes in Docker) to create a Kubernetes cluster based on the
provided configuration file:

    ```bash
    kind create cluster --config config/samples/capi/docker/kind.yaml
    ```

3. Generate a custom image bundle and load it into the KinD cluster:

    ```bash
    make k0smotron-image-bundle.tar && kind load image-archive k0smotron-image-bundle.tar
    ```

4. Release the necessary components and install them into the Kubernetes cluster:

    ```bash
    make release && kubectl create -f install.yaml
    ```

5. Initialize the cluster, patch configurations, and enable features:

    ```bash
    clusterctl init --infrastructure docker
              kubectl patch -n capi-system deployment/capi-controller-manager -p \
                '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--leader-elect", "--metrics-bind-addr=localhost:8080", "--feature-gates=ClusterTopology=true"]}]}}}}'
              kubectl patch -n capd-system deployment/capd-controller-manager -p \
                '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--leader-elect", "--metrics-bind-addr=localhost:8080", "--feature-gates=ClusterTopology=true"]}]}}}}'
    ```

6. Deploy the Local Path Provisioner for storage provisioning:

    ```bash
    kubectl apply -f https://raw.githubusercontent.com/rancher/local-path-provisioner/v0.0.24/deploy/local-path-storage.yaml
    ```

7. Extract the Kubernetes configuration for the KinD cluster and save it
to a `kind.conf` file:

    ```bash
    kind get kubeconfig > kind.conf
    ```

8. Run tests using the following command:

    ```bash
    make -C inttest check-capi-controlplane-docker KUBECONFIG=$(realpath kind.conf)
    ```
    
   This command runs tests against the control plane of the Kubernetes cluster
   deployed using Docker. It uses the Kubernetes configuration from `kind.conf`.
