# Cluster API - Control Plane provider

k0smotron can act as a control plane provider via usage of `K0smotronControlPlane` CRDs.

As per usual, you need to define a `Cluster` object given with a reference to control plane provider:
```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: cp-test
spec:
  clusterNetwork:
    pods:
      cidrBlocks:
        - 10.244.0.0/16
    services:
      cidrBlocks:
        - 10.96.0.0/12
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: K0smotronControlPlane
    name: cp-test
```

Next we need to provide the configuration for the actual `K0smotronControlPlane`:

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0smotronControlPlane
metadata:
  name: cp-test
spec:
  k0sVersion: v1.27.2-k0s.0
  persistence:
    type: emptyDir
  service:
    type: LoadBalancer
    # apiPort: 6443
    # konnectivityPort: 8132
    annotations:
      load-balancer.hetzner.cloud/location: fsn1
```

The `K0smotronControlPlane.spec` field is a direct mapping of the "standalone" k0smotron cluster [configuration](configuration.md).