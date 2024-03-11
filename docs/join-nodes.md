# Join a worker node

Joining worker nodes to your control plane is similar to the process of
[joining worker nodes with k0s](https://docs.k0sproject.io/stable/k0s-multi-node/#4-add-workers-to-the-cluster).
You need to obtain a join token that establishes mutual trust between the worker node
and control plane, allowing the node to join the cluster as worker.

**To join a worker node to the cluster:**

1. Obtain a join token:

   1. Create a `JoinTokenRequest` resource to generate a join token for the
      worker node. For example:

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

    !!! tip Token expiry configuration

        The `expiry` field defines the expiration time of the token.
        Refer to [API reference: JoinTokenRequest.spec](resource-reference.md#JoinTokenRequest.spec)
        for the configuration details.

       k0smotron processes the `JoinTokenRequest` resource and creates
       a `Secret` resource:

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

      The `token` field contains the base64-encoded token that you can use
      to join a worker node to the cluster.

   2. Retrieve the decoded join token from the generated `Secret`:

      ```shell
      kubectl get secret my-token -o jsonpath='{.data.token}' | base64 -d
      ```

2. Install `k0s` on the worker node:

   1. Ensure that the `k0s` binary is installed on the worker node:

      ```shell
      curl -sSLf https://get.k0s.sh | sudo sh
      ```

   2. Verify that the k0s version in the worker node matches the k0s version
      installed on the control plane. If needed, you can adjust the k0s version
      using the `K0S_VERSION` environment variable.

3. Join the worker node specifying the join token created earlier:

   ```shell
   sudo k0s install worker --token-file /path/to/token/file
   sudo k0s start
   ```

4. Delete the `JoinTokenRequest` resource to invalidate the issued token:

     ```shell
     kubectl delete jointokenrequest my-token
     ```

!!! note See also

    * [Configure join tokens](configuration/join-tokens.md)
    * [API reference: JoinTokenRequest.spec](resource-reference.md#JoinTokenRequest.spec)
