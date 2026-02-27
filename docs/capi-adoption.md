# Adopting an existing k0s cluster into CAPI management

If you have a k0s cluster that was deployed manually — on VMs or bare-metal, outside of Cluster API — you can bring it under k0smotron management without rebuilding it or disrupting running workloads.

The adoption process works by creating CAPI resource objects that represent the existing cluster's current state. All objects are initially created in a **paused** state so that controllers do not attempt to reconcile them before the full picture is established. Once the state is consistent, you unpause and hand over control.

!!! note

    This guide covers adoption of k0s clusters over SSH using **RemoteMachines** (VMs or bare-metal). If you use another infrastructure provider, such as vSphere or AWS, the idea stands the same. Check the provider documentation for additional details.

## Prerequisites

- A running k0s cluster with a reachable API server
- A kubeconfig for the existing cluster with cluster-admin privileges
- A management cluster with k0smotron and CAPI installed
- SSH access to the existing control plane and worker nodes

## Adopting the control plane

### Step 1: Prepare control plane nodes

Before creating CAPI objects, each control plane node must be reconfigured with two additional settings that k0smotron relies on:

- **`--enable-dynamic-config`**: Allows k0smotron reconcile k0s config changes. See [k0s config documentation](https://docs.k0sproject.io/stable/dynamic-configuration/) for details.
- **`AUTOPILOT_HOSTNAME`**: Required for Autopilot-based in-place upgrades to function. Must match the `Machine` object name you will assign to this node (e.g., `my-cluster-cp-0`). Autopilot uses this hostname to identify the node during upgrade operations.

!!! note

    If you will use update strategies other than InPlace (e.g. Recreate), the `AUTOPILOT_HOSTNAME` is not strictly required. However, we recommend setting it anyway to keep future upgrade options open.
    If you don't plan to change the control plane configuration after adoption, you can skip `--enable-dynamic-config` as well.

For each control plane node, SSH in and perform the following:

```bash
# 1. Read the current ExecStart flags from the existing unit file
grep ExecStart /etc/systemd/system/k0scontroller.service
# Example output:
# ExecStart=/usr/local/bin/k0s controller --token-file /etc/k0s/token --config /etc/k0s/k0s.yaml

# 2. Re-install the service with the same flags plus the new ones.
#    --force overwrites the existing unit file in place.
#    Replace <existing-flags> with the flags from the ExecStart line above,
#    and <machine-name> with this node's Machine object name (e.g. my-cluster-cp-0).
k0s install controller \
  --force \
  <existing-flags> \
  --enable-dynamic-config \
  --env AUTOPILOT_HOSTNAME=<machine-name>

# 3. Restart k0s
k0s stop && k0s start
```

!!! warning

    Restarting the k0s service causes a brief API server interruption. For multi-node clusters, we recommend performing this one node at a time.

Repeat for all control plane nodes, using the Machine object names you plan to assign, e.g.: `my-cluster-cp-0`, `my-cluster-cp-1`, etc.

### Step 2: Collect control plane node information

You will need the following for each existing control plane node:

- IP address
- SSH port (default: 22)
- SSH user and private key

Collect it from the existing cluster:

```bash
kubectl get nodes --selector node-role.kubernetes.io/control-plane \
  -o custom-columns=\
NAME:.metadata.name,\
INTERNAL_IP:.status.addresses[0].address
```

You will also need the pod and service CIDRs:

```bash
k0s config create | grep -E 'podCIDR|serviceCIDR'
```

### Step 3: Store the kubeconfig

Create a Secret in the management cluster containing the existing cluster's kubeconfig. The Secret name **must** follow the CAPI convention: `{cluster-name}-kubeconfig`.

```bash
kubectl create secret generic my-cluster-kubeconfig \
  --from-file=value=./my-cluster.kubeconfig \
  --namespace=default \
  --type=cluster.x-k8s.io/secret
```

Replace `my-cluster` with the name you will use for all CAPI objects in this guide.

### Step 4: Import PKI secrets

k0smotron looks for the cluster's PKI secrets at reconcile time and generates new CAs if they are absent. If new control plane nodes are ever added (via scaling or node replacement), they will be bootstrapped using whatever CA is stored in these secrets. Importing the existing PKI ensures new nodes receive the same CAs as the running cluster.

Copy the four PKI secrets from the first control plane node to the management cluster:

```bash
CP_NODE=1.2.3.4   # IP of any existing control plane node
SSH_USER=root

# Kubernetes CA
kubectl create secret tls my-cluster-ca \
  --namespace=default \
  --cert=<(ssh $SSH_USER@$CP_NODE cat /var/lib/k0s/pki/ca.crt) \
  --key=<(ssh $SSH_USER@$CP_NODE cat /var/lib/k0s/pki/ca.key)

# etcd CA
kubectl create secret tls my-cluster-etcd \
  --namespace=default \
  --cert=<(ssh $SSH_USER@$CP_NODE cat /var/lib/k0s/pki/etcd/ca.crt) \
  --key=<(ssh $SSH_USER@$CP_NODE cat /var/lib/k0s/pki/etcd/ca.key)

# Front proxy CA
kubectl create secret tls my-cluster-proxy \
  --namespace=default \
  --cert=<(ssh $SSH_USER@$CP_NODE cat /var/lib/k0s/pki/front-proxy-ca.crt) \
  --key=<(ssh $SSH_USER@$CP_NODE cat /var/lib/k0s/pki/front-proxy-ca.key)

# Service account signing keys
kubectl create secret tls my-cluster-sa \
  --namespace=default \
  --cert=<(ssh $SSH_USER@$CP_NODE cat /var/lib/k0s/pki/sa.pub) \
  --key=<(ssh $SSH_USER@$CP_NODE cat /var/lib/k0s/pki/sa.key)
```

!!! note

    If your cluster uses external etcd, skip the `my-cluster-etcd` secret and refer to your etcd provider's documentation for the appropriate CA configuration.

### Step 5: Create the core CAPI objects

Create the `Cluster`, `K0sControlPlane`, `RemoteCluster`, and `RemoteMachineTemplate` objects. All are annotated with `cluster.x-k8s.io/paused: "true"` so controllers do not act on them until you are ready.

The `RemoteMachineTemplate` is referenced by `K0sControlPlane.spec.machineTemplate` and defines the pool from which k0smotron draws machines when scaling or replacing control plane nodes. The existing nodes are represented by direct `Machine` and `RemoteMachine` objects (Steps 6–7), not by this pool.

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: my-cluster
  namespace: default
  annotations:
    cluster.x-k8s.io/paused: "true"
spec:
  clusterNetwork:
    pods:
      cidrBlocks:
        - 10.244.0.0/16       # replace with your cluster's pod CIDR
    services:
      cidrBlocks:
        - 10.96.0.0/12        # replace with your cluster's service CIDR
    serviceDomain: cluster.local
  controlPlaneEndpoint:
    host: 1.2.3.4             # replace with your control plane endpoint
    port: 6443
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: K0sControlPlane
    name: my-cluster
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: RemoteCluster
    name: my-cluster
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta1
kind: K0sControlPlane
metadata:
  name: my-cluster
  namespace: default
  annotations:
    cluster.x-k8s.io/paused: "true"
spec:
  replicas: 3                 # number of existing control plane nodes
  version: v1.29.2+k0s.0     # k0s version currently running on the cluster
  updateStrategy: InPlace     # uses k0s Autopilot for upgrades
  k0sConfigSpec: {}
  machineTemplate:
    infrastructureRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: RemoteMachineTemplate
      name: my-cluster-cp-template
      namespace: default
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteCluster
metadata:
  name: my-cluster
  namespace: default
  annotations:
    cluster.x-k8s.io/paused: "true"
spec:
  controlPlaneEndpoint:
    host: 1.2.3.4             # same as Cluster.spec.controlPlaneEndpoint
    port: 6443
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteMachineTemplate
metadata:
  name: my-cluster-cp-template
  namespace: default
spec:
  template:
    spec:
      pool: my-cluster-cp-pool
```

To make machines available for future scaling or node replacement, add one or more `PooledRemoteMachine` objects referencing the same pool:

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: PooledRemoteMachine
metadata:
  name: my-cluster-cp-spare-0
  namespace: default
spec:
  pool: my-cluster-cp-pool
  machine:
    address: 5.6.7.8          # a pre-provisioned spare VM
    port: 22
    user: root
    sshKeyRef:
      name: my-cluster-cp-ssh-key
```

!!! note

    You do not need spare machines available at adoption time. `PooledRemoteMachine` objects can be added at any point before you need k0smotron to scale or replace a control plane node.

Apply all objects:

```bash
kubectl apply -f cluster.yaml
```

### Step 6: Create Machine objects for each control plane node

First, retrieve the `K0sControlPlane` UID — you will set it as the owner reference on each `Machine`:

```bash
KCP_UID=$(kubectl get k0scontrolplane my-cluster \
  --namespace=default \
  -o jsonpath='{.metadata.uid}')
```

For each existing control plane node, create a `Machine` object.

Set `spec.providerID` to `remote-machine://<IP>:<SSH_PORT>`. k0smotron's `ProviderIDController` reads this value and writes it to the matching Node in the workload cluster, which is how CAPI links the `Machine` object to the Node.

If the control plane nodes also run workloads (you use k0s with `--enable-worker`), include the `k0smotron.io/control-plane-worker-enabled: "true"` label so that replacement nodes created during scaling or remediation are configured the same way.

Setting `spec.bootstrap.dataSecretName` to the kubeconfig Secret tells CAPI that this machine is already bootstrapped — no bootstrap data will be generated or pushed to the node.

```yaml
# Repeat this block for each control plane node, changing the name and providerID
apiVersion: cluster.x-k8s.io/v1beta1
kind: Machine
metadata:
  name: my-cluster-cp-0
  namespace: default
  annotations:
    cluster.x-k8s.io/paused: "true"
  labels:
    cluster.x-k8s.io/cluster-name: my-cluster
    cluster.x-k8s.io/control-plane: "true"
    cluster.x-k8s.io/control-plane-name: my-cluster
    k0smotron.io/control-plane-worker-enabled: "true"  # omit if CP nodes do not run workloads
  ownerReferences:
  - apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: K0sControlPlane
    name: my-cluster
    uid: ${KCP_UID}            # substitute with the value from above
    controller: true
    blockOwnerDeletion: true
spec:
  clusterName: my-cluster
  version: v1.29.2            # Kubernetes version (without k0s suffix)
  providerID: remote-machine://1.2.3.4:22  # format: remote-machine://<IP>:<SSH_PORT>
  bootstrap:
    dataSecretName: my-cluster-kubeconfig  # marks machine as already bootstrapped
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: RemoteMachine
    name: my-cluster-cp-0
```

Apply with UID substitution:

```bash
KCP_UID=$KCP_UID envsubst < machines.yaml | kubectl apply -f -
```

!!! note

    Create one `Machine` per control plane node. Use a consistent naming scheme such as `{cluster-name}-cp-0`, `{cluster-name}-cp-1`, etc.

### Step 7: Create RemoteMachine objects for each control plane node

For each `Machine`, create a matching `RemoteMachine` with the node's SSH connection details. These are used by k0smotron for future lifecycle operations (upgrades, replacement).

Two annotations are required on each `RemoteMachine`. The `K0sControlPlane` controller uses them to verify that each machine was provisioned from the expected template — machines that fail this check are marked for deletion and replaced. Set them to the name and group kind of the `RemoteMachineTemplate` you created in Step 5:

```yaml
# Repeat for each control plane node
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteMachine
metadata:
  name: my-cluster-cp-0
  namespace: default
  annotations:
    cluster.x-k8s.io/paused: "true"
    cluster.x-k8s.io/cloned-from-name: my-cluster-cp-template
    cluster.x-k8s.io/cloned-from-groupkind: RemoteMachineTemplate.infrastructure.cluster.x-k8s.io
  labels:
    cluster.x-k8s.io/cluster-name: my-cluster
    cluster.x-k8s.io/control-plane: "true"
    cluster.x-k8s.io/control-plane-name: my-cluster
spec:
  address: 1.2.3.4            # IP address of this control plane node
  port: 22
  user: root
  sshKeyRef:
    name: my-cluster-cp-ssh-key   # Secret containing the SSH private key
```

The SSH key Secret must have a key named `value` containing the private key:

```bash
kubectl create secret generic my-cluster-cp-ssh-key \
  --from-file=value=./id_rsa \
  --namespace=default
```

!!! warning

    The `RemoteMachine` name must match the `infrastructureRef.name` in the corresponding `Machine` object.

!!! note

    The owner reference from `RemoteMachine` back to its `Machine` is set automatically by the Machine controller — no manual action needed.

### Step 8: Label existing Nodes

k0smotron's `ProviderIDController`writes `Machine.spec.providerID` to `Node.spec.providerID`, which is how CAPI links the Machine to its Node.

Label each existing Node in the **workload cluster** with the name of its corresponding `Machine` object:

```bash
# Run against the existing workload cluster
kubectl --kubeconfig=./my-cluster.kubeconfig label node <cp-node-0-name> \
  k0smotron.io/machine-name=my-cluster-cp-0

kubectl --kubeconfig=./my-cluster.kubeconfig label node <cp-node-1-name> \
  k0smotron.io/machine-name=my-cluster-cp-1

kubectl --kubeconfig=./my-cluster.kubeconfig label node <cp-node-2-name> \
  k0smotron.io/machine-name=my-cluster-cp-2
```

Use `kubectl get nodes --kubeconfig=./my-cluster.kubeconfig` to list the actual Node names.

### Step 9: Unpause

Once all objects are created and you have verified the names, providerIDs, and addresses are correct, remove the paused annotation from all objects to hand over control to k0smotron.

```bash
for kind in cluster k0scontrolplane remotecluster machine remotemachine; do
  kubectl annotate "$kind" \
    --all \
    --namespace=default \
    cluster.x-k8s.io/paused-
done
```

k0smotron will now reconcile the objects against the existing cluster. Because the Machine `providerID` values match the existing Nodes, the controllers will recognize the nodes as already provisioned and will not attempt to re-install or modify them.

### Step 10: Verify

Check that all objects reach a ready state:

```bash
kubectl get cluster,k0scontrolplane,remotecluster,machine,remotemachine \
  --namespace=default \
  -o wide
```

Expected output:

```
NAME                              PHASE
cluster.../my-cluster             Provisioned

NAME                              READY   REPLICAS   READY REPLICAS
k0scontrolplane.../my-cluster     true    3          3

NAME                              READY
remotecluster.../my-cluster       true

NAME                              PHASE     NODE
machine.../my-cluster-cp-0        Running   cp-node-0
machine.../my-cluster-cp-1        Running   cp-node-1
machine.../my-cluster-cp-2        Running   cp-node-2

NAME                              READY
remotemachine.../my-cluster-cp-0  true
remotemachine.../my-cluster-cp-1  true
remotemachine.../my-cluster-cp-2  true
```

## Adopting worker nodes

Worker nodes can be adopted the same way as control plane nodes. The only differences are:

- No `cluster.x-k8s.io/control-plane` label on the `Machine`
- Workers are not owned by `K0sControlPlane` — they stand as individual `Machine` objects

Worker adoption can be done alongside the control plane adoption or independently at any later point.

### Step 1: Collect worker node information

```bash
kubectl get nodes --selector '!node-role.kubernetes.io/control-plane' \
  -o custom-columns=\
NAME:.metadata.name,\
PROVIDER_ID:.spec.providerID,\
INTERNAL_IP:.status.addresses[0].address
```

### Step 2: Create Machine and RemoteMachine objects

Create a paused `Machine` and `RemoteMachine` for each worker node:

```yaml
# Repeat for each worker node, changing name, providerID and address
apiVersion: cluster.x-k8s.io/v1beta1
kind: Machine
metadata:
  name: my-cluster-worker-0
  namespace: default
  annotations:
    cluster.x-k8s.io/paused: "true"
  labels:
    cluster.x-k8s.io/cluster-name: my-cluster
spec:
  clusterName: my-cluster
  version: v1.29.2            # Kubernetes version (without k0s suffix)
  providerID: remote-machine://10.0.0.11:22  # format: remote-machine://<IP>:<SSH_PORT>
  bootstrap:
    dataSecretName: my-cluster-kubeconfig  # marks machine as already bootstrapped
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: RemoteMachine
    name: my-cluster-worker-0
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteMachine
metadata:
  name: my-cluster-worker-0
  namespace: default
  annotations:
    cluster.x-k8s.io/paused: "true"
spec:
  address: 10.0.0.11
  port: 22
  user: root
  sshKeyRef:
    name: my-cluster-worker-ssh-key
```

### Step 3: Label existing worker Nodes

Label each existing worker Node in the workload cluster with the name of its corresponding `Machine` object:

```bash
kubectl --kubeconfig=./my-cluster.kubeconfig label node <worker-node-0-name> \
  k0smotron.io/machine-name=my-cluster-worker-0

kubectl --kubeconfig=./my-cluster.kubeconfig label node <worker-node-1-name> \
  k0smotron.io/machine-name=my-cluster-worker-1
```

### Step 4: Unpause

```bash
kubectl apply -f workers.yaml

kubectl annotate machine,remotemachine \
  --selector=cluster.x-k8s.io/cluster-name=my-cluster \
  --namespace=default \
  cluster.x-k8s.io/paused-
```

### Adding new worker nodes

For new workers going forward, use a `MachineDeployment` backed by a `PooledRemoteMachine` pool. This gives you declarative scaling and rolling updates. See the [remote machine guide](capi-remote.md) for the full setup.
