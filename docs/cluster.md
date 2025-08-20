# Create a cluster

After you install k0smotron as described in the [Installation](install.md)
section, you can start creating your Kubernetes control planes.

**To create a cluster:**

1. Create the `Cluster` object:

   ```shell
   cat <<EOF | kubectl create -f-
   apiVersion: k0smotron.io/v1beta1
   kind: Cluster
   metadata:
      name: <cluster-name>
   spec: null
   EOF
   ```

   This triggers the k0smotron controller manager to set up the control plane
   in pods.

  !!! note "Use an external cluster to set up control plane in pods"
      By default, control planes are deployed in the management cluster.  If you choose to deploy them in an external cluster via [`kubeconfigRef`](/resource-reference/k0smotron.io-v1beta1/#clusterspeckubeconfigref), the identity in that kubeconfig must have sufficient RBAC permissions for k0smotron to create and manage control plane resources. You can review the roles used by k0smotron [here](https://github.com/k0sproject/k0smotron/tree/main/config/rbac).


1. Once k0smotron finishes setting up the cluster, obtain the admin access
   kubeconfig:

   ```shell
   kubectl get secret <cluster-name>-kubeconfig -o jsonpath='{.data.value}' | base64 -d > ~/.kube/child.conf
   ```

   Depending on your configuration, the admin kubeconfig may not be pointing
   to the correct address. If the kubeconfig does not work by default,
   set the correct value for `<server-URL>`:

   ```yaml
   apiVersion: v1
   clusters:
   - cluster:
       server: <server-URL>
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

Once your control plane is ready, you can start [joining worker nodes](join-nodes.md)
into the newly created control plane.
