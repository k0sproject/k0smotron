# Join worker nodes

Joining worker nodes is pretty much the exact same process as with [k0s](https://docs.k0sproject.io/stable/k0s-multi-node/#4-add-workers-to-the-cluster) in general. You need a join token that enables mutual trust between the worker and controller(s) and which allows the node to join the cluster as worker.

## Join Tokens

To get a token, create a `JoinTokenRequest` resource:

```yaml
apiVersion: k0smotron.io/v1beta1
kind: JoinTokenRequest
metadata:
  name: my-token
  namespace: default
spec:
  clusterRef:
    name: my-cluster
    namespace: default
```

The `JoinTokenRequest` resource will be processed by the controller and a `Secret` will be created:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-token
  namespace: default
  labels:
    k0smotron.io/cluster: my-cluster.default
    k0smotron.io/role: worker
    k0smotron.io/token-request: my-token
type: Opaque
data:
  token: <base64-encoded-token>
```

The `token` field contains the base64-encoded token that can be used to join a worker node to the cluster.

To get the decoded token you can use:

```shell
kubectl get secret my-token -o jsonpath='{.data.token}' | base64 -d
```


## Join nodes

First you need to get the `k0s` binary on the node:

```shell
curl -sSLf https://get.k0s.sh | sudo sh
```

The download script accepts the following environment variables:

| Variable                                       | Purpose                                           |
|:-----------------------------------------------|:--------------------------------------------------|
| `K0S_VERSION=v{{{ extra.k8s_version }}}+k0s.0` | Select the version of k0s to be installed         |
| `DEBUG=true`                                   | Output commands and their arguments at execution. |

**Note:** Match the k0s version to the version of the control plane you've created.

To join the worker, run k0s in the worker mode with the join token you created:

```shell
sudo k0s install worker --token-file /path/to/token/file
```

```shell
sudo k0s start
```



## Invalidating tokens

You can limit the validity period by setting the `expiry` field in the `JoinTokenRequest` resource:

```yaml
apiVersion: k0smotron.io/v1beta1
kind: JoinTokenRequest
metadata:
  name: my-token
  namespace: default
spec:
  clusterRef:
    name: my-cluster
    namespace: default
  expiry: 1h
```

To invalidate an issued token, delete the `JoinTokenRequest` resource:

```shell
kubectl delete jointokenrequest my-token
```
