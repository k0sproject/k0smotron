# Set up CAPI development environment

To contribute to k0smotron, ensure you have set up the development environment properly.
For simple development tasks, it is enough to install Go,
and follow [k0smotron GitHub workflow](contribute-workflow.md).
For more complicated changes that require running CAPI (Common Application Programming Interface)
tests, follow the steps below to configure your environment:

1. Use KinD (Kubernetes in Docker) to create a Kubernetes cluster based on the
provided configuration file:

    ```bash
    kind create cluster --config config/samples/capi/docker/kind.yaml
    ```

2. Generate a custom image bundle and load it into the KinD cluster:

    ```bash
    make k0smotron-image-bundle.tar && kind load image-archive k0smotron-image-bundle.tar
    ```

3. Release the necessary components and install them into the Kubernetes cluster:

    ```bash
    make release && kubectl apply --server-side=true -f install.yaml
    ```

4. Initialize the cluster, patch configurations, and enable features:

    ```bash
    clusterctl init --infrastructure docker
              kubectl patch -n capi-system deployment/capi-controller-manager -p \
                '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--leader-elect", "--metrics-bind-addr=localhost:8080", "--feature-gates=ClusterTopology=true"]}]}}}}'
              kubectl patch -n capd-system deployment/capd-controller-manager -p \
                '{"spec":{"template":{"spec":{"containers":[{"name":"manager","args":["--leader-elect", "--metrics-bind-addr=localhost:8080", "--feature-gates=ClusterTopology=true"]}]}}}}'
    ```

5. Deploy the Local Path Provisioner for storage provisioning:

    ```bash
    kubectl apply -f https://raw.githubusercontent.com/rancher/local-path-provisioner/v0.0.24/deploy/local-path-storage.yaml
    ```

6. Extract the Kubernetes configuration for the KinD cluster and save it
to a `kind.conf` file:

    ```bash
    kind get kubeconfig > kind.conf
    ```

7. Run tests using the following command:

    ```bash
    make -C inttest check-capi-controlplane-docker KUBECONFIG=$(realpath kind.conf)
    ```

   This command runs tests against the control plane of the Kubernetes cluster
   deployed using Docker. It uses the Kubernetes configuration from `kind.conf`.

## Use Tilt for Development

Using [Tilt](https://docs.tilt.dev/index.html) can help when you want a fast development loop without spending time setting up a full environment. **k0smotron** provides different development environment setups depending on the mode you want to work in: [standalone or CAPI integration](../../usage-overview). 

### Prerequisites

Before starting, ensure the following tools are installed and available in your environment:

- **Go**: Required to build k0smotron components.

- **Docker**: Used by Tilt to build container images.

- **kind**: Used to create local Kubernetes clusters for development.

- **kubectl**: Required to interact with Kubernetes clusters.

- **Tilt**: Used to manage the development environment and enable live reload.

---

### Standalone Mode

For standalone mode, you first need a management cluster where the required providers will be deployed. You can create a local cluster using `kind` by running:

```bash
kind create cluster --config config/samples/capi/docker/kind.yaml
```

Once the management cluster is created, deploy your development version of k0smotron in standalone mode by running:

```bash
make tilt-standalone-env
```

In k0smotron, the Tilt development environment for standalone mode is configured to **rebuild the k0smotron controller manager and redeploy it to the cluster whenever standalone-related code changes.** This allows developers to quickly verify how changes behave in the cluster without manually rebuilding, loading images, or restarting the k0smotron deployment.

---

#### Run headless delve debugger for k0smotron controller manager

Alternatively, you can run the k0smotron controller manager with a **headless Delve debugger** and attach to it remotely.

To start the development environment with Delve enabled, use the `DEBUG` variable set to `true`:

```cmd
make tilt-standalone-env DEBUG=true
```

Once the k0smotron controller manager is listening on port `30000` for external connections, you can attach to it from your IDE.
For example, in **VS Code**, you can add the following debug configuration to attach to the k0smotron controller manager:

```json
{
    // Use IntelliSense to learn about possible attributes.
    // Hover to view descriptions of existing attributes.
    // For more information, visit: https://go.microsoft.com/fwlink/?linkid=830387
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Connect to k0smotron controller manager",
            "type": "go",
            "request": "attach",
            "mode": "remote",
            "port": 30001,
            "host": "127.0.0.1",
            "apiVersion": 2,
            "showLog": true,
            "substitutePath": [
                {
                    "from": "${workspaceFolder}",
                    "to": "${workspaceFolder}"
                }
            ]
        }
    ]
}
```

---

### CAPI Integration Mode

To set up a development environment using Tilt with **k0smotron integrated as a CAPI provider**, you need to clone both the **cluster-api** repository and the **k0smotron** repository locally. The general steps to set up a CAPI development environment using Tilt can be found in the [Cluster API documentation](https://cluster-api.sigs.k8s.io/developer/core/tilt).

The k0smotron-specific configuration required to enable k0smotron as a provider and to create a development environment integrated with Cluster API is located in `tilt-provider.yaml`. For an initial setup, the configuration provided in this file should be sufficient. To activate it in your local **cluster-api** development environment, follow these steps:

1. In the `tilt-settings.yaml` file of your local **cluster-api** project, set the path to your local **k0smotron** repository.
2. Enable the k0smotron provider by adding `k0smotron` to the `enable_providers` list.

A minimal example of a `tilt-settings.yaml` file in the **cluster-api** project looks like this:

```yaml
default_registry: gcr.io/your-project-name-here

enable_providers:
  - docker
  - kubeadm-bootstrap
  - kubeadm-control-plane
  - k0smotron

provider_repos:
  - <k0smotron-project-path> # Path to your local k0smotron repository

debug:
  core:
    continue: false
    port: 30000
  k0smotron:
    continue: false
    port: 30001

kustomize_substitutions:
  EXP_IN_PLACE_UPDATES: "true"
```

Now you can run `make tilt-up` target in the **cluster-api** project to start the development environment.

!!! note

    The Cluster API `make tilt-up` target already provisions and deploys the management cluster, so no additional management cluster setup is required.