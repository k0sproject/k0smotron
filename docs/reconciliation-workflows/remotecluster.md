# RemoteCluster reconciliation workflow

A `RemoteCluster` has nothing to provision. Once it is owned by a `Cluster`, the controller marks it as provisioned
and sets its `Ready` condition. Its `spec.controlPlaneEndpoint` is either set by the user or, for a hosted control
plane, by the `K0smotronControlPlane` controller.
