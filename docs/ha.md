# Highly available control planes

!!! note

   Highly available control planes are supported for the standalone and
   Cluster API in-cluster use cases.

As the nature of Kubernetes workloads, in our case the cluster control planes,
is quite dynamic, it poses a challenge to set up highly available etcd cluster
for the control plane. In k0smotron, we are solving the challenge by
"externalizing" the HA setup of data storage for the control plane.

The control planes managed by k0smotron are k0s control planes. As k0s comes
with support for using SQL databases as data store, which uses Kine, you can
use HA databases instead of etcd. This enables you to use, for example,
Postgres operator, MySQL operator, or cloud provider managed databases as the
data store for the control planes.

## Using Postgres operator

In this example, we provide instruction on how to use
[Postgres operator](https://postgres-operator.readthedocs.io/en/latest/)
to manage the data store of a control plane. Use this instruction as an example
for the required data store resource.

1. Install the Postgres operator following the [quickstart guide](https://postgres-operator.readthedocs.io/en/latest/quickstart/).

2. Create the database using a custom resource:

   ```
   apiVersion: "acid.zalan.do/v1"
   kind: postgresql
   metadata:
     name: acid-minimal-cluster
   spec:
     teamId: "acid"
     volume:
       size: 10Gi
     numberOfInstances: 2
     users:
       # database owner
       k0smotron:
       - superuser
       - createdb

     databases:
       kine: k0smotron
     postgresql:
       version: "15"
   ```

3. Once you set up the database, configure k0smotron to create a control plane:

   ```shell
   cat <<EOF | kubectl apply -f -
   apiVersion: k0smotron.io/v1beta1
   kind: Cluster
   metadata:
     name: k0smotron-test
   spec:
     replicas: 3
     service:
       type: LoadBalancer
     kineDataSourceURL: postgres://k0smotron:<passwd>@acid-minimal-cluster.default:5432/kine?sslmode=disable
   EOF
   ```

   You can also use the reference to the secret containing the database
   credentials:

   ```shell
   cat <<EOF | kubectl apply -f -
   apiVersion: v1
   kind: Secret
   metadata:
     name: database-credentials
     namespace: k0smotron-test
   type: Opaque
   data:
     K0SMOTRON_KINE_DATASOURCE_URL: <base64-encoded-datasource>
   ---
   apiVersion: k0smotron.io/v1beta1
   kind: Cluster
   metadata:
     name: k0smotron-test
   spec:
     replicas: 3
     service:
       type: LoadBalancer
     kineDataSourceSecretName: database-credentials
   EOF
   ```

   !!! note

      The secret must be in the same namespace as the cluster and the key
      must be `K0SMOTRON_KINE_DATASOURCE_URL`.
