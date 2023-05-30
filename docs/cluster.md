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
kubectl get secret kmc-admin-kubeconfig-k0smotron-test -o jsonpath='{.data.value}' | base64 -d > ~/.kube/child.conf
```

Once your control plane is ready you can start [adding worker nodes](join-nodes.md) into the newly created control plane.