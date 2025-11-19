# Updating self-managed worker nodes using autopilot for HCP

In hybrid control plane (HCP) setups, the Kubernetes control plane runs inside pods, while worker nodes are self-managed and connect remotely. Keeping both the control plane and worker nodes up to date is essential for security and compatibility.


This guide demonstrates how to use the autopilot feature to update self-managed worker nodes after the control plane has been upgraded.

!!! note
    Everything described in this article is applicable to and all commands are run in the workload cluster.

## Update the control plane

First, update the control plane to the desired k0s version by modifying the `Cluster` resource:

```yaml
apiVersion: k0smotron.io/v1beta1
kind: Cluster
metadata:
  name: k0smotron-test
spec:
  replicas: 1
  k0sImage: quay.io/k0sproject/k0s
  version: v1.33.1+k0s.0 # new k0s version
```

!!! warning
    Always update the control plane components first before updating the worker nodes.
    Refer to the [Kubernetes version skew policy](https://kubernetes.io/releases/version-skew-policy/).

## Update self-managed worker nodes using autopilot

Autopilot is the easiest way to update self-managed worker nodes.

Read more about [k0s autopilot configuration](https://docs.k0sproject.io/stable/autopilot/#configuration).

!!! warning
    The Plan name should always be "autopilot" and the Plan resource is immutable. To make changes, remove old Plan and create a new one with a different `spec.id`.

!!! note
    The `selector` field in the `discovery` section can be adjusted to target specific nodes based on labels. An empty selector `{}` targets all worker nodes.
    To target specific nodes, use a `static` discovery like `discovery: { "static": ["node-name1", "node-name2"] }`.

!!! note
    `get.k0sproject.io` is a simple proxy service to the GitHub release assets. GitHub CDN may answer with a 403 error for automated downloads, so using `get.k0sproject.io` helps avoid this issue.
    `https://get.k0sproject.io/v1.33.1+k0s.0/k0s-v1.33.1+k0s.0-amd64` will proxy to `https://github.com/k0sproject/k0s/releases/download/v1.33.1+k0s.0/k0s-v1.33.1+k0s.0-amd64`.

Create a `Plan` resource that specifies the desired k0s version and targets the worker nodes:

```yaml
apiVersion: autopilot.k0sproject.io/v1beta2
kind: Plan
metadata:
  name: autopilot
spec:
  id: id123 # Unique ID for the plan
  timestamp: now
  commands:
  - k0supdate:
      version: v1.33.1+k0s.0 # Target k0s version
      platforms:
        linux-amd64:
          url: https://get.k0sproject.io/v1.33.1+k0s.0/k0s-v1.33.1+k0s.0-amd64
        linux-arm64:
          url: https://get.k0sproject.io/v1.33.1+k0s.0/k0s-v1.33.1+k0s.0-arm64
      targets:
        # We target only workers, since we updated control planes using the Cluster object
        workers:
          discovery:
            selector: {} # Select all worker nodes
```

Autopilot will automatically apply the update to the selected worker nodes.
