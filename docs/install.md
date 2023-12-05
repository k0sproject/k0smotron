# Installation

To install k0smotron, run the following command:

```bash
kubectl apply -f https://docs.k0smotron.io/{{{ extra.k0smotron_version }}}/install.yaml
```

This install the k0smotron controller manager, all the related CRD definitions and needed RBAC rules.

Once the installation is completed you are ready to [create your first control planes](cluster.md).

## clusterctl

k0smotron is compatible with clusterctl and can act as bootstrap, infrastructure, and control plane provider. To use k0smotron with clusterctl, you need to create a clusterctl configuration file. Here's an example:

```bash
providers:
  - name: "k0smotron"
    url: "https://github.com/k0sproject/k0smotron/releases/latest/bootstrap-components.yaml"
    type: "BootstrapProvider"
  - name: "k0smotron"
    url: "https://github.com/k0sproject/k0smotron/releases/latest/control-plane-components.yaml"
    type: "ControlPlaneProvider"
  - name: "k0smotron"
    url: "https://github.com/k0sproject/k0smotron/releases/latest/infrastructure-components.yaml"
    type: "InfrastructureProvider"
```

Once you have the configuration file in place, you can use clusterctl to create a cluster:

```bash
clusterctl init --bootstrap k0smotron --control-plane k0smotron --config config.yaml
```
