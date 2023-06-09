# Creating a cluster

The following example creates a simple cluster named `k0smotron-test`:

```shell
cat <<EOF | kubectl apply -f -
apiVersion: k0smotron.io/v1beta1
kind: Cluster
metadata:
  name: k0smotron-test
spec: null
EOF
```

This triggers k0smotron controllers to setup the control plane in pods. Once k0smotron is done you can get the admin access kubeconfig:

```shell
kubectl get secret k0smotron-test-kubeconfig -o jsonpath='{.data.value}' | base64 -d > ~/.kube/child.conf
```

**Warning**: Depending on your configuration, the admin kubeconfig may not be pointing to the right address.
If the kubeconfig doesn't work by default, you'll need to set the right value in `<server URL>`.

```yaml
apiVersion: v1
clusters:
- cluster:
    server: <server URL>
    certificate-authority-data: <redacted>
  name: k0s
contexts:
- context:
    cluster: k0s
    user: admin
  name: k0s
current-context: k0s
kind: Config
preferences: {}
users:
- name: admin
  user:
    client-certificate-data: <redacted>
    client-key-data: <redacted>
```

Once your control plane is ready you can start [adding worker nodes](join-nodes.md) into the newly created control plane.