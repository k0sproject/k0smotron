# Provisioning RemoteMachine Workers with Terradev

This guide shows how to use [Terradev](https://github.com/theoddden/Terradev) to provision
the underlying VMs for k0smotron's RemoteMachine provider and commit the resulting manifests
to a Flux-managed GitOps repository.

Terradev supports 17 GPU and general-purpose cloud providers. It provisions the VM,
waits for an SSH-reachable IP, generates the `RemoteMachine` (and associated `Machine`,
`K0sWorkerConfig`) manifests, and commits them to your GitOps repo — where Flux
reconciles them into the k0smotron control plane automatically.

## Prerequisites

- k0smotron installed on a management cluster with the RemoteMachine infrastructure
  provider enabled
- [Terradev CLI](https://github.com/theoddden/Terradev) ≥ 6.2.15:
  `pip install terradev-cli`
- At least one Terradev provider configured:
  `terradev configure --provider runpod`
- A Flux-managed GitOps repository cloned locally
- An SSH key pair; the public key stored on the provider, the private key in a
  Kubernetes Secret named `gpu-ssh-key` in the `default` namespace

## Directory layout

Terradev writes manifests to `mgmt/gpu-remote/clusters/gpu-workers/` in your
GitOps repo:

```
mgmt/gpu-remote/clusters/gpu-workers/
├── kustomization.yaml          # updated automatically by Terradev
└── <node-id>.yaml              # one file per node (RemoteMachine + Machine + K0sWorkerConfig)
```

## Add a worker node

```bash
terradev k8s node add gpu-worker-01 \
  --gpu H100 \
  --provider runpod \
  --repo /path/to/gitops-repo
```

This command:
1. Provisions an H100 instance on RunPod
2. Waits up to 5 minutes for a public IP
3. Generates the `RemoteMachine`, `Machine`, and `K0sWorkerConfig` manifests
4. Patches `kustomization.yaml` to include the new file
5. Commits everything to the local repo with a `feat(gpu-remote):` message

If the provider has not assigned an IP by the time provisioning returns, the
command prints a `node ready` hint. Once the IP is available:

```bash
terradev k8s node ready gpu-worker-01 \
  --address 1.2.3.4 \
  --provider runpod \
  --gpu H100 \
  --instance-id <provider-instance-id> \
  --repo /path/to/gitops-repo
```

## Push and reconcile

```bash
git -C /path/to/gitops-repo push origin main
```

Flux picks up the commit and applies the manifests. Watch the node join:

```bash
flux get kustomizations -w
kubectl get machines -n default -w
kubectl get remotemachines -n default -w
```

Once the `RemoteMachine` reports `Ready`, k0smotron SSHs into the instance,
installs k0s, and registers the node with the control plane:

```bash
kubectl get nodes -l terradev.io/gpu-type=H100
```

## List managed nodes

```bash
terradev k8s node list --repo /path/to/gitops-repo
```

Output:

```
NODE ID                        PROVIDER       GPU        ADDRESS            PROVISIONED
gpu-worker-01                  runpod         H100       1.2.3.4            2026-09-04T21:00:00
```

Node state is derived entirely from the GitOps manifests (the
`terradev.io/provider`, `terradev.io/gpu-type`, and `terradev.io/instance-id`
annotations on each `RemoteMachine`), not from a separate state file.

## Remove a worker node

```bash
terradev k8s node rm gpu-worker-01 \
  --repo /path/to/gitops-repo \
  --terminate
```

With `--terminate`, Terradev also calls the provider API to terminate the
underlying instance. Without it, only the GitOps manifests are removed.

Push the repo to have Flux remove the `Machine` from the k0smotron control plane.

## Generated manifest structure

Each `<node-id>.yaml` contains three documents:

```yaml
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteMachine
metadata:
  name: gpu-worker-01
  annotations:
    terradev.io/provider: runpod
    terradev.io/gpu-type: H100
    terradev.io/instance-id: runpod-abc123
    terradev.io/provisioned-at: "2026-09-04T21:00:00Z"
spec:
  address: 1.2.3.4
  port: 22
  user: ubuntu
  sshKeyRef:
    name: gpu-ssh-key
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: Machine
metadata:
  name: gpu-worker-01
spec:
  clusterName: gpu-cluster
  bootstrap:
    configRef:
      apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
      kind: K0sWorkerConfig
      name: gpu-worker-01
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: RemoteMachine
    name: gpu-worker-01
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta1
kind: K0sWorkerConfig
metadata:
  name: gpu-worker-01
spec:
  version: v1.33.0+k0s.0
```

## Supported providers

Terradev supports 17 providers including RunPod, Vast.ai, TensorDock, Crusoe,
Hyperstack, Latitude, AWS, GCP, and Azure. Run `terradev providers list --gpu H100`
to see current availability and pricing.

## See also

- [k0smotron RemoteMachine overview](capi-remote.md)
- [Terradev CLI on GitHub](https://github.com/theoddden/Terradev)
- [knr-ops reference implementation](https://github.com/polarsquad/knr-ops)
