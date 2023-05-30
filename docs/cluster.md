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
