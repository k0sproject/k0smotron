
# Troubleshooting

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

## MachineDeployment with Docker Provider does not function

Docker Provider uses the version field to determine the docker image version
for the worker nodes. If you are using k0smotron as a Cluster API
provider, check whether the MachineDeployment `spec.template.spec.version`
field is present. If it is present, check that the version is supported by your
infrastructure provider.
