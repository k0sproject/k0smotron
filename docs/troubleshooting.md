
# Troubleshooting

## New hosted control plane pods are in CrashLoopBackOff 

k0s uses inotify to watch for changes in manifest files. If the inotify limits are too low on the nodes of a management cluster, 
the control plane pods may crash since they cannot set up the cluster configuration. 
To increase the limit run the following command on your control plane node:

```bash
$ sysctl -w fs.inotify.max_user_watches=524288
$ sysctl -w fs.inotify.max_user_instances=512
```

## Worker nodes cannot be joined

If you are using k0smotron in standalone mode, check whether the join
token has expired. If this is the case, attempt to create a new one using
[JoinTokenRequest](https://docs.k0smotron.io/stable/join-nodes/#join-tokens).

If you are using k0smotron as a Cluster API provider, use the following
procedure to check the logs of your infrastructure provider controller:

1. Decode the token:

    ```bash
    echo "<token>" | base64 -d | gunzip
    ```

2. Take note of the `users.user.token` field. For purposes of example, the
   field will be `gb823t.b8ftcytc4ktmvkjz`.

3. Run the following command, using the KUBECONFIG of the child cluster, which
   is the first part of the `users.user.token` field:

    ```bash
    kubectl -n kube-system get secret bootstrap-token-gb823t --template='{{.data.expiration}}' | base64 -d
    2024-03-14T11:08:13Z
    ```

Furthermore, check whether different Kubernetes minor versions were used in the
initial cluster creation for the control plane and the worker nodes, as k0s
requires that the controllers and workers were created using the same minor
version. For more information, refer to the Kubernetes [Version Skew
Policy](https://kubernetes.io/releases/version-skew-policy/).

## Cloud Controller Manager fails to start with `--cloud-provider=external` when using Ingress support

When using [Ingress support](ingress-support.md) together with an external
Cloud Controller Manager (CCM), CCM pods may fail to start with an error like:
```
unable to load configmap based request-header-client-ca-file: Get
"https://10.96.0.1:443/...": dial tcp 10.96.0.1:443: connect: connection refused
```

### Root cause

The ingress support architecture relies on a local HAProxy sidecar running on
each worker node to proxy pod-to-API traffic to the ingress controller. As part
of this, k0smotron reconfigures the `kubernetes.default` Service in the child cluster to
point to the HAProxy sidecar.

With `--cloud-provider=external`, CCM (and not kubelet) is responsible for reporting the worker
node's external addresses back to the cluster. Until CCM does this, k0smotron
cannot fully configure the HAProxy sidecar, so the `kubernetes` Service is not
yet reachable. Meanwhile, CCM itself may use in-cluster config to reach the API
server — which reads `KUBERNETES_SERVICE_HOST` and `KUBERNETES_SERVICE_PORT`
env vars injected by kubelet, pointing at the `kubernetes` Service ClusterIP
(`10.96.0.1` by default). The result is a deadlock:

- CCM cannot reach the API server because the HAProxy sidecar is not yet configured
- The HAProxy sidecar is not configured because CCM has not yet reported node addresses

### Solution

Override the in-cluster config by explicitly setting `KUBERNETES_SERVICE_HOST`
and `KUBERNETES_SERVICE_PORT` in the CCM pod spec, pointing directly to the
ingress hostname:
```yaml
env:
  - name: KUBERNETES_SERVICE_HOST
    value: "https://my-cluster-api.example.com"
  - name: KUBERNETES_SERVICE_PORT
    value: "443"
```

Another option is to set `--kubeconfig` flag in the CCM deployment to point to a kubeconfig file that uses the API hostname.

This makes the CCM pod to use the API hostname directly for InCluster config instead of default `kubernetes` Service address.

## MachineDeployment with Docker Provider does not function

Docker Provider uses the version field to determine the docker image version
for the worker nodes. If you are using k0smotron as a Cluster API
provider, check whether the MachineDeployment `spec.template.spec.version`
field is present. If it is present, check that the version is supported by your
infrastructure provider.
