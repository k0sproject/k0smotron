# Project status

To gauge the level of interest in running k0s control planes as Kubernetes
resources, we are releasing k0smotron at the earliest possible moment. Of
cousre, as with any young project just getting started, there are bound to be
some bumps, unforeseen issues, and a few surprises. In line with that, we can
offer no guarantees regarding backward or forward compatibility, and you should
expect a certain amount of breakage with each ensuing k0smotron release.

All caveats aside, though, we are truly excited about what k0smotron has to
offer, and look forward to seeing how it can transform the way you manage your
Kubernetes deployments. We are confident that the open source community will
take great interest in the project, too, and help us to smooth out the rough
edges in the days, weeks, and months ahead.

## Cluster API

One key focus of the k0smotron project is to get it to function as a Cluster
API provider for both `ControlPlane` and worker `Bootstrap` providers. Once
this goal is achieved, you will be able to use the Cluster API to provision the
control plane (within the management cluster) and the worker nodes in your
favourite infrastructure supporting cluster API.

## Known limitations

Areas in which k0smotron is currently limited include:

* Control Plane configurability

    The configurability of the [k0s](https://docs.k0sproject.io/stable/) control plane is not yet fully enabled.

* Control plane exposure

    Currently k0smotron only supports `NodePort` and `LoadBalancer` services,
    and often it is necessary to further configure created services to ease
    their interraction with cloud provider implementations.

* Updates prevalence

    Although k0smotron can easily update the cluster controlplane, such updates
    do not extend to worker nodes.
