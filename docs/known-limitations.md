# Known Limitations

This pages lists the known limitations we have in k0smotron.

## Worker VM in-place updates require the Cluster API in-place updates extension

When running CAPI managed clusters on VMs, k0smotron can update control plane nodes in-place (via k0s [autopilot](https://docs.k0sproject.io/stable/autopilot/)) without any extra components. Worker nodes, however, can only be updated in-place automatically when the [Cluster API in-place updates extension](update/update-capi-cluster.md#cluster-api-in-place-updates-experimental) is installed and active. Without it, updating a `MachineDeployment`'s version follows Cluster API's default machine-replacement lifecycle instead, and an in-place update of workers requires manually creating the autopilot `Plan`.

See [Update worker nodes in Cluster API clusters (VMs)](update/update-capi-cluster-workers.md) for details.

## Infrastructure Controlplane LBs need extra ports

k0s uses 3 ports on controllers: 6443 for Kubernetes API, 8132 for konnectivity and 9443 for k0s internal join API. With CAPI all these three ports needs to be enabled on the control plane LBs. Most of the infrastructure providers support adding extra ports.
For example with AWS provider you'd need to specify these ports in [`AWSCluster.spec.controlPlaneLoadBalancer.additionalListeners`](https://cluster-api-aws.sigs.k8s.io/crd/#infrastructure.cluster.x-k8s.io/v1beta2.AdditionalListenerSpec).

!!! note "If you encounter an infrastructure provider which does not support adding additional ports, please let us know. We're happy to work with that upstream project to get that functionality added."
