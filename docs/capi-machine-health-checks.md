# MachineHealthChecks with k0smotron

k0smotron provides built-in support for MachineHealthChecks (MHC), a core Cluster API feature that automatically detects and remediates unhealthy control plane machines.

## Overview

k0smotron's control plane controller automatically handles the remediation process when machines are marked as unhealthy by MHC.

Read more about MachineHealthChecks in the [Cluster API documentation](https://cluster-api.sigs.k8s.io/tasks/automated-machine-management/healthchecking.html#configure-a-machinehealthcheckbb).

## How it works

When a MachineHealthCheck detects an unhealthy control plane machine:

1. The MHC controller marks the machine as unhealthy
2. k0smotron's control plane controller detects this condition
3. The controller safely deletes the unhealthy machine
4. A new machine is automatically created to replace it

## Prerequisites

- A k0smotron control plane with at least 2 replicas (required for safe remediation)
- MachineHealthCheck controller running in your management cluster

## Example Configuration

Here's a simple example of how to set up MachineHealthChecks for a k0smotron control plane:

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachineHealthCheck
metadata:
  name: k0smotron-controlplane-mhc
  namespace: default
spec:
  clusterName: my-cluster
  selector:
    matchLabels:
      cluster.x-k8s.io/control-plane: "true"
  unhealthyConditions:
  - type: Ready
    status: Unknown
    timeout: 300s
  - type: Ready
    status: "False"
    timeout: 300s
  nodeStartupTimeout: 10m
```

## Safety Features

k0smotron includes several safety mechanisms to prevent cluster disruption:

- **Minimum replicas**: Remediation only occurs when there are more than 1 control plane replica
- **No concurrent operations**: Waits for provisioning/deletion operations to complete
- **Graceful deletion**: Properly removes machines from the k0s cluster before deletion

## Best Practices

- Always use at least 3 control plane replicas in production
- Set appropriate timeouts based on your infrastructure
- Monitor remediation events and adjust thresholds as needed
- Test remediation in non-production environments first