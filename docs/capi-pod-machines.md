# Cluster API - Pod machine provider

k0smotron can run cluster nodes as plain Pods in the management cluster. Each `Machine` is backed by a `PodMachine`,
which creates a single Pod running k0s. This is handy for development, CI, scale testing, or using custom container
runtime like kata-containers, gVisor, etc.

!!! warning "Experimental"
The Pod machine provider is **experimental**. The API (`PodCluster`, `PodMachine`, `PodMachineTemplate`) may change or
be removed without following the usual deprecation policy, and it is not intended for production use.
See [Known limitations](#known-limitations) before using it.

## How it works

PodMachine provider is very simple and just creates a pod from given template. It doesn't do much and gives full control to the cluster operator. 

Bootstrap data is delivered to the Pod the same way as to a VM, through the cloud-init
[NoCloud](https://cloudinit.readthedocs.io/en/latest/reference/datasources/nocloud.html) datasource. For every
`PodMachine`, k0smotron mounts into all containers of the Pod:

- `/var/lib/cloud/seed/nocloud/meta-data` — a ConfigMap holding the hostname,
- `/var/lib/cloud/seed/nocloud/user-data` — the bootstrap Secret produced by the bootstrap provider (`K0sWorkerConfig`,
  `K0sControllerConfig`).

The Pod image is therefore responsible for running cloud-init on startup. This means the image must contain both cloud-init and k0s. 

## Requirements

- k0smotron installed with the infrastructure provider enabled (this is the default for the Cluster API deployment).
- A container image with cloud-init and a pre-installed k0s binary (see [Building the image](#building-the-image)).
- When running with default container runtime such as runc or CRI-O, the Pod needs elevated privileges to run k0s and containerd: `privileged: true`, 
  the capabilities listed in the example below, and host access to `/lib/modules` and `/sys/fs/cgroup`.
- When running with default container runtime, the bootstrap config must use `preInstalledK0s: true`, since k0s is baked into the image and cannot be downloaded and
  installed as a service the usual way.
- The bootstrap config should pass the machine's provider ID to the kubelet
  (`--kubelet-extra-args=--provider-id=pod-machine://<namespace>/$(hostname)`), otherwise Cluster API cannot match the
  `Machine` to its `Node`. `PodMachine` reports `spec.providerID` in the same format.

!!! warning "Privileged workloads"
Nodes running as Pods are privileged containers with host mounts. Only use this provider on clusters where running such
workloads is acceptable.

## Building the image

The image needs cloud-init on top of a k0s image, plus an entrypoint that runs the cloud-init stages and then keeps the
container alive:

```dockerfile
FROM quay.io/k0sproject/k0s:v1.34.2-k0s.0

RUN apk update && \
    apk add --no-cache cloud-init openrc

RUN mkdir -p \
    /run/cloud-init \
    /var/lib/cloud \
    /etc/cloud/cloud.cfg.d \
    /run/openrc/ \
    /var/lib/cloud/seed/nocloud && \
    touch /run/openrc/softlevel

# Disable cloud-init networking management, it would break the CNI setup
RUN printf "network:\n  config: disabled\n" \
    > /etc/cloud/cloud.cfg.d/99-disable-networking.cfg

# Disable SSH entirely, the Pod is reachable via kubectl
RUN printf "disable_root: true\nssh_pwauth: false\nssh_deletekeys: true\nssh_genkeytypes: []\n\nsystem_info:\n  default_user:\n    name: alpine\n  ssh_svcname: none\n" \
    > /etc/cloud/cloud.cfg.d/99-disable-ssh.cfg

RUN sed -i 's/ssh_svcname: sshd/ssh_svcname: none/' /etc/cloud/cloud.cfg || true
RUN sed -i 's/^datasource_list: .*/datasource_list: [ NoCloud ]/' /etc/cloud/cloud.cfg || true

COPY entrypoint.sh /entrypoint.sh

CMD ["/entrypoint.sh"]
```

`entrypoint.sh`:

```sh
#!/bin/sh

cloud-init clean
cloud-init init
cloud-init modules --mode=config
cloud-init modules --mode=final

sleep infinity
```

## Creating a cluster with workers in Pods

The example below creates a hosted control plane with `K0smotronControlPlane` and a `MachineDeployment` of two workers,
each running as a Pod in the management cluster.

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: pod-test
  namespace: default
spec:
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
    kind: K0smotronControlPlane
    name: pod-test-cp
  infrastructureRef:
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: PodCluster
    name: pod-test-infra
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: K0smotronControlPlane
metadata:
  name: pod-test-cp
  namespace: default
spec:
  version: v1.34.2-k0s.0
  service:
    type: NodePort
  k0sConfig:
    apiVersion: k0s.k0sproject.io/v1beta1
    kind: ClusterConfig
    spec:
      telemetry:
        enabled: false
      network:
        provider: calico
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: PodCluster
metadata:
  name: pod-test-infra
  namespace: default
spec: {}
```

The `PodCluster` is a placeholder: it has nothing to provision and is marked as provisioned as soon as it is reconciled.

Next, the worker `MachineDeployment` with its bootstrap and infrastructure templates:

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: MachineDeployment
metadata:
  name: pod-test-workers
  namespace: default
spec:
  clusterName: pod-test
  replicas: 2
  selector:
    matchLabels:
      cluster.x-k8s.io/cluster-name: pod-test
      pool: worker-pool-1
  template:
    metadata:
      labels:
        cluster.x-k8s.io/cluster-name: pod-test
        pool: worker-pool-1
    spec:
      clusterName: pod-test
      version: v1.34.2
      bootstrap:
        configRef:
          apiGroup: bootstrap.cluster.x-k8s.io
          kind: K0sWorkerConfigTemplate
          name: pod-test-worker-config
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: PodMachineTemplate
        name: pod-test-worker-pod
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: K0sWorkerConfigTemplate
metadata:
  name: pod-test-worker-config
  namespace: default
spec:
  template:
    spec:
      version: v1.34.2
      preInstalledK0s: true # k0s comes with the image
      args:
        # Let the kubelet report the same provider ID as the PodMachine, so Cluster API can
        # link the Machine to its Node. The namespace is the one of the PodMachine, and
        # $(hostname) resolves to the PodMachine (and Machine) name at bootstrap time.
        - --kubelet-extra-args=--provider-id=pod-machine://default/$(hostname)
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: PodMachineTemplate
metadata:
  name: pod-test-worker-pod
  namespace: default
spec:
  template:
    spec:
      podTemplate:
        spec:
          containers:
          - name: k0s-worker
            image: my-registry/k0s-cloud-init:v1.34.2-k0s.0 # image built above
            resources:
              requests:
                memory: "256Mi"
                cpu: "250m"
              limits:
                memory: "512Mi"
                cpu: "500m"
            securityContext:
              privileged: true
              capabilities:
                add:
                  - SYS_ADMIN
                  - SYS_RESOURCE
                  - NET_ADMIN
                  - SYS_CHROOT
                  - SYS_PTRACE
                  - DAC_OVERRIDE
            volumeMounts:
              - name: containerd
                mountPath: /var/lib/k0s/containerd/
              - name: cgroup
                mountPath: /sys/fs/cgroup
              - name: lib-modules
                mountPath: /lib/modules
                readOnly: true
          restartPolicy: Always
          volumes:
            - name: containerd
              emptyDir: {}
            - name: lib-modules
              hostPath:
                path: /lib/modules
                type: Directory
            - name: cgroup
              hostPath:
                path: /sys/fs/cgroup
                type: Directory
```

k0smotron creates one Pod per `PodMachine`, in the same namespace and with the same name as the `PodMachine`. The Pod is
labeled with `cluster.x-k8s.io/cluster-name` and annotated with `cluster.x-k8s.io/machine`, so you can find the Pods of
a cluster with:

```bash
kubectl get pods -l cluster.x-k8s.io/cluster-name=pod-test
kubectl get podmachines
```

Once cloud-init has run in the Pod and k0s has joined, the node shows up in the child cluster:

```bash
kubectl get secret pod-test-kubeconfig -o jsonpath='{.data.value}' | base64 -d > pod-test.kubeconfig
kubectl --kubeconfig pod-test.kubeconfig get nodes
```

## Using `PodMachine` directly

For a single machine you can skip the template and reference a `PodMachine` from a `Machine`. In this case the
`PodMachine` and the bootstrap config **must have the same name**, see [Known limitations](#known-limitations).

```yaml
apiVersion: cluster.x-k8s.io/v1beta2
kind: Machine
metadata:
  name: pod-test-0
  namespace: default
spec:
  clusterName: pod-test
  version: v1.34.2
  bootstrap:
    configRef:
      apiGroup: bootstrap.cluster.x-k8s.io
      kind: K0sWorkerConfig
      name: pod-test-0
  infrastructureRef:
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: PodMachine
    name: pod-test-0
---
apiVersion: bootstrap.cluster.x-k8s.io/v1beta2
kind: K0sWorkerConfig
metadata:
  name: pod-test-0
  namespace: default
spec:
  version: v1.34.2
  preInstalledK0s: true
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: PodMachine
metadata:
  name: pod-test-0
  namespace: default
spec:
  podTemplate:
    spec:
      containers:
      - name: k0s-worker
        image: my-registry/k0s-cloud-init:v1.34.2-k0s.0
        securityContext:
          privileged: true
      restartPolicy: Always
```

## Cleanup

Deleting a `PodMachine` deletes its Pod. The cloud-init ConfigMap is owned by the `PodMachine` and is garbage collected
with it. Scaling down a `MachineDeployment` or deleting the `Cluster` removes the Pods as usual.

Unlike `RemoteMachine`, no `k0s reset` is performed: the Pod and its ephemeral storage are simply deleted.

## Known limitations

- **Experimental API.** `PodCluster`, `PodMachine` and `PodMachineTemplate` are served in `v1beta2` only and may change
  without a deprecation period.
- **Node linkage depends on the kubelet provider ID.** `PodMachine` reports
  `spec.providerID: pod-machine://<namespace>/<name>`, but Cluster API links a `Machine` to its `Node` by matching that
  value against `Node.spec.providerID`. If the bootstrap config does not pass `--provider-id` to the kubelet as shown
  above, the `Machine` becomes `Provisioned` but never gets a `nodeRef`.
- **Bootstrap Secret name.** The user-data Secret mounted into the Pod is looked up by the `PodMachine` name rather than
  by `Machine.spec.bootstrap.dataSecretName`. This holds automatically when the objects are created from templates
  (`MachineDeployment`, `K0sControlPlane`), but when writing a `Machine` by hand the bootstrap config and the
  `PodMachine` must share a name, otherwise the Pod hangs waiting for the Secret.
- **Pod specs are immutable.** Changes to `spec.podTemplate` of an existing `PodMachine` are not applied to the running
  Pod. Roll the `MachineDeployment` to pick up template changes.
- **Node identity on restart.** The Pod has no persistent storage by default, so a restarted Pod re-runs cloud-init and
  rejoins as a fresh node.
- **Not a substitute for real nodes.** Kernel modules, cgroup access and CNI behaviour depend on the host and on the
  privileges given to the Pod, so workloads may behave differently from a VM or bare metal node.
