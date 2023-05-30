# Highly available controlplanes

As the nature of Kubernetes workloads, in this case the cluster control planes, is quite dynamic it poses a challenge to setup highly available Etcd cluster for the control plane. In k0smotron we're solving the challenge by "externalizing" the control plane data storage HA setup.

The control planes managed by `k0smotron` are k0s control planes. As k0s comes with support for using SQL DBs as data store (via Kine) you can use HA databases instead of Etcd. This enables you to use e.g. Postgres operator, MySQL operator or cloud provider managed databases as the data store for the control planes.

## Using Postgres operator

In this example we show how to use [Postgres operator](https://postgres-operator.readthedocs.io/en/latest/) to manage the control plane data store.

Install the operator following the [quicstart guide](https://postgres-operator.readthedocs.io/en/latest/quickstart/).

Create the database with a custom resource:
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

Once the database has been setup properly, you can instruct k0smotron to create a control plane using it:

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

**Note:** We know the DB URL exposes the database password now in a plain Kubernetes resource. We're working on a solution to be able to refer it from a secret.