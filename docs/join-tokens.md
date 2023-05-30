# Join Tokens

You need a token to join workers to the cluster. The token embeds information that enables mutual trust between the worker and controller(s) and which allows the node to join the cluster as worker.

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
