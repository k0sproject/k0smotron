# Cilium with kube-proxy replacement on Hosted Control Plane

This example shows how to deploy [Cilium](https://cilium.io/) with `kubeProxyReplacement: true` on a k0smotron Hosted Control Plane (HCP) cluster using a `LoadBalancer` service type.

## The problem

Cilium's kube-proxy replacement mode requires `k8sServiceHost` and `k8sServicePort` at install time — the address where the Kubernetes API server is reachable from worker nodes. When the HCP is exposed via a `LoadBalancer` Service, the endpoint (IP or hostname) is only known **after** the Service is reconciled.

## How k0smotron solves it

K0smotron automatically creates a `control-plane-endpoint` ConfigMap in the child cluster's `kube-system` namespace once the API address is available. This ConfigMap contains:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: control-plane-endpoint
  namespace: kube-system
data:
  apiServerHost: "<API external address>"
  apiServerPort: "<API port>"
```

You can reference these values in Cilium's Helm chart configuration, so no manual, pre-known addresses are required.

## Prerequisites

- A management cluster with k0smotron installed ([Installation guide](../install.md))
- Cluster API with the desired infrastructure provider configured
- The `ClusterTopology` feature gate enabled on the CAPI controller

## Example: ClusterClass with Cilium

The ClusterClass below creates a Hosted Control Plane exposed via LoadBalancer and installs Cilium with kube-proxy replacement on the worker nodes.

### K0smotronControlPlaneTemplate

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0smotronControlPlaneTemplate
metadata:
  name: cilium-hcp-template
  namespace: default
spec:
  template:
    spec:
      version: v1.31.5-k0s.0
      service:
        type: LoadBalancer
        apiPort: 6443
        konnectivityPort: 8132
      k0sConfig:
        apiVersion: k0s.k0sproject.io/v1beta1
        kind: ClusterConfig
        metadata:
          name: k0s
        spec:
          network:
            provider: custom
            kubeProxy:
              disabled: true
          extensions:
            helm:
              repositories:
                - name: cilium
                  url: https://helm.cilium.io/
              charts:
                - name: cilium
                  chartname: cilium/cilium
                  version: "1.17.1"
                  namespace: kube-system
                  values: |
                    kubeProxyReplacement: true
                    k8sServiceHostRef:
                      name: control-plane-endpoint
                      key: apiServerHost
                    k8sServicePort: __YOUR_API_PORT__  # e.g. 6443

                    routingMode: tunnel
                    tunnelProtocol: vxlan

                    operator:
                      replicas: 1

                    ipam:
                      mode: cluster-pool
                      operator:
                        clusterPoolIPv4PodCIDRList:
                          - 10.244.0.0/16
                        clusterPoolIPv4MaskSize: 24
```

## ClusterClass definition

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: ClusterClass
metadata:
  name: cilium-hcp
  namespace: default
spec:
  controlPlane:
    templateRef:
      apiVersion: controlplane.cluster.x-k8s.io/v1beta1
      kind: K0smotronControlPlaneTemplate
      name: cilium-hcp-template
  infrastructure:
    templateRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
      kind: DockerClusterTemplate
      name: docker-cluster-template
  workers:
    machineDeployments:
    - class: default-worker
      bootstrap:
        templateRef:
          apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
          kind: K0sWorkerConfigTemplate
          name: k0s-worker-config-template
      infrastructure:
        templateRef:
          apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
          kind: DockerMachineTemplate
          name: worker-docker-machine-template
```

## Creating a Cluster from the ClusterClass

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: cilium-cluster
  namespace: default
spec:
  topology:
    classRef:
      name: cilium-hcp
    version: v1.31.5
    workers:
      machineDeployments:
      - class: default-worker
        name: md-0
        replicas: 3
```

## Verifying the endpoint ConfigMap

Once the cluster is provisioned and the LoadBalancer gets an address, verify the ConfigMap in the child cluster:

```shell
kubectl --kubeconfig child.conf get configmap control-plane-endpoint -n kube-system -o yaml
```

Expected output:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: control-plane-endpoint
  namespace: kube-system
data:
  apiServerHost: "192.168.1.100"   # your LB IP or hostname
  apiServerPort: "6443"
```

## Verifying Cilium

```shell
kubectl --kubeconfig child.conf -n kube-system exec ds/cilium -- cilium status
```

Confirm that `KubeProxyReplacement` shows as `True` and the API server address matches the LoadBalancer endpoint.
