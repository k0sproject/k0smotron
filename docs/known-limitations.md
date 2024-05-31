# Known Limitations

This pages lists the known limitations we have in k0smotron.

## Controlplane VM updates

When running CAPI managed controlplane in VMs, k0smotron currently supports only `InPlace` upgrade strategy. This means that k0smotron will actually trigger k0s [autopilot](https://docs.k0sproject.io/stable/autopilot/) to update the control plane nodes.

The reason behind is that the "traditional" CAPI way of re-creating the machines involves etcd cluster tweaking, namely removing and adding peers. This is unfortunately not possible with k0s from external toolin, k0smotron in this case, as k0s configures etcd so that it cannot be accessed externally from the nodes.

We are working on an approach to allow also `ReCreate` strategy, to comply with traditional CAPI way and to match with other providers.

## Infrastructure Controlplane LBs need extra ports

k0s uses 3 ports on controllers: 6443 for Kubernetes API, 8132 for konnectivity and 9443 for k0s internal join API. With CAPI all these three ports needs to be enabled on the control plane LBs. Most of the infrastructure providers support adding extra ports.
For example with AWS provider you'd need to specify these ports in [`AWSCluster.spec.controlPlaneLoadBalancer.additionalListeners`](https://cluster-api-aws.sigs.k8s.io/crd/#infrastructure.cluster.x-k8s.io/v1beta2.AdditionalListenerSpec).

!!! note "If you encounter an infrastructure provider which does not support adding additional ports, please let us know. We're happy to work with that upstream project to get that functionality added."
