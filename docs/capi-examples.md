# Cluster API examples - software prerequisites

This section presents a collection of examples showcasing the use of k0smotron
as a Cluster API provider across various cloud platforms.

## Prerequisites

Before proceeding with the Cluster API examples, make sure that you have met
[common prerequisites](install.md#software-prerequisites) and installed
[k0smotron](install.md).

## Provider versions

Each Cluster API example uses specific provider releases as a reference for its
manifest and installation instructions. The versions below record the releases
used when the examples were prepared; they are not a promise that every example
is tested with every provider environment. Provider APIs change over time, so
using another release may require adapting the example.

| Provider | Example reference version |
| --- | --- |
| Cluster API core | {{{ extra.capi_versions.core }}} |
| AWS | {{{ extra.capi_versions.aws }}} |
| Docker | {{{ extra.capi_versions.docker }}} |
| Hetzner | {{{ extra.capi_versions.hetzner }}} |
| KubeVirt | {{{ extra.capi_versions.kubevirt }}} |
| OpenStack | {{{ extra.capi_versions.openstack }}} |
| vSphere | {{{ extra.capi_versions.vsphere }}} |
| In-cluster IPAM | {{{ extra.capi_versions.ipam }}} |

The k0smotron provider version follows the documentation release shown by the
per-module installation instructions. The provider pins are centralized in
`mkdocs.yml` so each example's reference version can be updated in one place.

These versions preserve the API schemas used by the examples. For example, the
vSphere reference release serves `VSphereCluster` and
`VSphereMachineTemplate` as `infrastructure.cluster.x-k8s.io/v1beta1`, while the
OpenStack and Hetzner examples also use API versions served by their reference
releases.
