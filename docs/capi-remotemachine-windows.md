# Windows worker with the Remote Machine provider

This example provisions a hybrid cluster with a Linux control plane and a
**Windows worker** using the k0smotron [Remote Machine provider](capi-remote.md).
The Remote Machine provider connects to pre-existing machines over SSH, so you
bring your own VMs (any cloud, bare metal, etc.) — here a Linux box for the
control plane and a Windows Server box for the Windows workload.

!!! note "Experimental"
    Windows worker support is experimental. The control plane must run on Linux,
    and at least one Linux worker is required for CoreDNS. Networking uses Calico.

    Read the k0s docs for more details on [Windows support](https://docs.k0sproject.io/stable/experimental-windows/).

## Prerequisites

- A management cluster with k0smotron installed (provides the bootstrap,
  control plane and Remote Machine infrastructure providers).
- **Linux machine** (e.g. Ubuntu) reachable over SSH — used for the control
  plane with `--enable-worker`, which also satisfies the Linux-worker
  requirement for CoreDNS.
- **Windows Server machine** (2019/2022) reachable over SSH.

### Preparing the Windows machine

The Remote Machine provider needs key-based SSH access **before** it bootstraps
the node — k0smotron does not install SSH for you. On the Windows box, install
and enable the OpenSSH Server, authorize your public key for the administrator
account, and open the firewall. Run in an elevated PowerShell (or via EC2
user-data at first boot):

```powershell
# Install + enable OpenSSH Server (includes the sftp-server subsystem)
Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
Set-Service -Name sshd -StartupType Automatic
Start-Service sshd

# Authorize your SSH public key for the Administrator account
$pubKey   = "ssh-ed25519 AAAA... your public key"
$sshDir   = "C:\ProgramData\ssh"
$authKeys = Join-Path $sshDir "administrators_authorized_keys"
New-Item -ItemType Directory -Force -Path $sshDir | Out-Null
Set-Content -Path $authKeys -Value $pubKey -Encoding ascii
# Admin authorized_keys MUST be locked to Administrators+SYSTEM, or sshd ignores it
icacls $authKeys /inheritance:r | Out-Null
icacls $authKeys /grant "Administrators:F" "SYSTEM:F" | Out-Null

# Allow inbound SSH
New-NetFirewallRule -Name sshd -DisplayName 'OpenSSH Server (sshd)' `
  -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22
```

!!! warning "Firewall"
    Installing OpenSSH with `Add-WindowsCapability` does **not** create the
    inbound firewall rule. Without the `New-NetFirewallRule` above, SSH
    connections hang (the SYN is dropped) rather than being refused.

Verify from your workstation that SSH works before continuing:

```shell
ssh Administrator@<windows-ip> hostname
```

## SSH key secrets

Create a secret per machine holding the **private** key under the `value` key:

```shell
kubectl create secret generic linux-ssh-key   --from-file=value=</path/to/linux/key>
kubectl create secret generic windows-ssh-key --from-file=value=</path/to/windows/key>
```

!!! note
    Use an OpenSSH-format key (`BEGIN OPENSSH PRIVATE KEY`). Convert an old PEM
    key with `ssh-keygen -p -m RFC4716 -f <key>`.

## Cluster definition

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: win-remote
  namespace: default
spec:
  # Required when the control plane runs on Remote Machines.
  controlPlaneEndpoint:
    host: <linux-ip>
    port: 6443
  clusterNetwork:
    pods:
      cidrBlocks:
        - 192.168.0.0/16
    serviceDomain: cluster.local
    services:
      cidrBlocks:
        - 10.128.0.0/12
  controlPlaneRef:
    apiGroup: controlplane.cluster.x-k8s.io
    kind: K0sControlPlane
    name: win-remote
  infrastructureRef:
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: RemoteCluster
    name: win-remote
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteCluster
metadata:
  name: win-remote
  namespace: default
spec: {}
```

## Linux control plane

The control plane runs on the Linux machine with `--enable-worker` (so it also
serves as the required Linux worker) and Calico networking.

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0sControlPlane
metadata:
  name: win-remote
  namespace: default
spec:
  replicas: 1
  version: v1.34.2+k0s.0
  updateStrategy: Recreate
  k0sConfigSpec:
    args:
      - --enable-worker
    k0s:
      apiVersion: k0s.k0sproject.io/v1beta1
      kind: ClusterConfig
      metadata:
        name: k0s
      spec:
        network:
          provider: calico   # required for Windows nodes
  machineTemplate:
    infrastructureRef:
      # This ref is a classic ObjectReference -> use apiVersion, not apiGroup.
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: RemoteMachineTemplate
      name: win-remote-cp
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteMachineTemplate
metadata:
  name: win-remote-cp
  namespace: default
spec:
  template:
    spec:
      pool: cp
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: PooledRemoteMachine
metadata:
  name: win-remote-cp-0
  namespace: default
spec:
  pool: cp
  machine:
    address: <linux-ip>
    port: 22
    user: ubuntu
    useSudo: true            # non-root SSH user needs sudo
    sshKeyRef:
      name: linux-ssh-key
```

## Windows worker

The Windows worker is a plain `Machine` backed by a `RemoteMachine`. Key
differences from Linux:

- `platform: windows` and provisioner `type: cloud-config` on the
  `K0sWorkerConfig`.
- `k0sInstallDir` set to a Windows path — the default `/usr/local/bin` is invalid
  on Windows.
- `useSudo: false` and `user: Administrator` on the `RemoteMachine`.

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Machine
metadata:
  name: win-remote-worker-0
  namespace: default
spec:
  clusterName: win-remote
  version: v1.34.2+k0s.0
  bootstrap:
    configRef:
      apiGroup: bootstrap.cluster.x-k8s.io
      kind: K0sWorkerConfig
      name: win-remote-worker-0
  infrastructureRef:
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: RemoteMachine
    name: win-remote-worker-0
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: K0sWorkerConfig
metadata:
  name: win-remote-worker-0
  namespace: default
spec:
  version: v1.34.2+k0s.0
  k0sInstallDir: 'C:\k0s'    # must be a Windows path
  provisioner:
    platform: windows
    type: cloud-config       # Remote Machine (SSH) path; not powershell-xml
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: RemoteMachine
metadata:
  name: win-remote-worker-0
  namespace: default
spec:
  address: <windows-ip>
  port: 22
  user: Administrator
  sshKeyRef:
    name: windows-ssh-key
```

## Apply and verify

```shell
kubectl apply -f cluster.yaml
kubectl get cluster,k0scontrolplane,machine -w
```

The Linux control plane provisions first. Once its API answers on
`<linux-ip>:6443`, the Windows worker bootstraps: k0smotron uploads
`C:\bootstrap\k0s_install.ps1`, enables the Windows `Containers` feature
(which may reboot — a scheduled task resumes bootstrap on startup), downloads
`k0s.exe` and joins the cluster.

```shell
clusterctl get kubeconfig win-remote > win.kubeconfig
KUBECONFIG=win.kubeconfig kubectl get nodes -o wide
```

You should see the Linux control-plane node and a `Ready` Windows node
(`OS-IMAGE` reporting Windows). Schedule Windows workloads with a node selector:

```yaml
nodeSelector:
  kubernetes.io/os: windows
```
