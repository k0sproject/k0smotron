# k0smotron

The Kubernetes control plane manager. Deploy and run Kubernetes control planes on any existing cluster.

## Installation

```
kubectl apply -f https://raw.githubusercontent.com/k0sproject/k0smotron/main/install.yaml
```

## Creating a cluster

To create a cluster, you need to create a `K0smotronCluster` resource. The `spec` field is used for optional settings, so you can just pass `null` as the value.

```
cat <<EOF | kubectl apply -n <namespace> -f-
apiVersion: k0smotron.io/v1beta1
kind: K0smotronCluster
metadata:
  name: my-k0smotron
spec: null
EOF
```

## Creating cluster join tokens

At the moment there isn't an automated way to gather one, you may obtain one by running 

```
kubectl exec -n <K0smotronCluster namespace> <K0smotron pod> k0s token create --role=worker
```
