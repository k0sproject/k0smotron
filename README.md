# k0smotron
An operator to deploy k0s clusters on top of k0s.

## Instalation
Currently k0smotron isn't hosted anywhere.
The image must be built and copied manually to the nodes where it will be
executed or uploaded to a registry.

To do it manually do:

```
make kustomize
make docker-build

# Copy the image to the nodes
docker save k0s/k0smotron -o k0smotron.tgz
for node in <node 1> [node n]... ; do
scp k0smotron.tgz <target-node>:/tmp/k0smotron.tgz
k0s ctr images import /tmp/k0smotron.tgz
done

# Create the necessary objects in the kubernetes API
for i in config/crd config/rbac config/manager; do
bin/kustomize build $i | docker exec -i TestKubeRouterHairpinSuite-controller0  k0s kc apply -n k0smotron -f-
done
```

## Testing

Once everything is deployed, you can deploy a k0smotron cluster in your desired namespace by running:
```
cat <<EOF | kubectl apply -n <namespace> -f-
apiVersion: k0smotron.io/v1beta1
kind: K0smotronCluster
metadata:
  name: my-k0smotron
spec: null
EOF
```

## At the moment there isn't a way to gather one, you may obtain one by running 

```
kubectl exec -n <K0smotronCluster namespace> <K0smotron pod> k0s token create --role=worker
```