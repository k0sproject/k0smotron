
### I cannot join a worker node

In [standalone](usage-overview.md#standalone) mode, check whether the join
token has expired. If this is the case, attempt to create a new one using
[JoinTokenRequest](https://docs.k0smotron.io/stable/join-nodes/#join-tokens).

In [Cluster API](cluster-api.md), check the logs of your infrastructure
provider controller.

Also, check to see whether different versions were used in the initial cluster
creation for the control plane and the worker nodes, as Kubernetes is
compatible with a limited number of different versions. For more information,
refer to the Kubernetes [Version Skew
Policy](https://kubernetes.io/releases/version-skew-policy/).

### MachineDeployment with Docker Provider does not function

A [valid version](https://hub.docker.com/r/kindest/node/tags) of `spec.template.spec.version` is required for MachineDeployment.

### Overcoming issues with Cluster API Capd k0smotron child cluster deployment

In [Cluster API](https://cluster-api.sigs.k8s.io/), check whether the
MachineDeployment `spec.template.spec.version` field is present. If it is
present, check that the version is supported by your infrastructure provider.
Docker Provider relies on the version field for worker nodes, as it uses it to
determine the docker image version for the node.
