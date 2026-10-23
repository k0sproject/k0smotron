# Cluster API examples - software prerequisites

This section presents a collection of examples showcasing the use of k0smotron
as a Cluster API provider across various cloud platforms.

## Prerequisites

Before proceeding with the Cluster API examples, make sure that you have met
[common prerequisites](install.md#software-prerequisites) and installed
[k0smotron](install.md).

## Provider versions

The examples pin Cluster API and infrastructure provider releases so that a new
provider release cannot silently change the CRDs installed by `clusterctl init`.
Use `clusterctl` {{{ extra.capi_versions.core }}} with the following provider versions:

| Provider | Version |
| --- | --- |
| Cluster API core | {{{ extra.capi_versions.core }}} |
| AWS | {{{ extra.capi_versions.aws }}} |
| Docker | {{{ extra.capi_versions.docker }}} |
| Hetzner | {{{ extra.capi_versions.hetzner }}} |
| KubeVirt | {{{ extra.capi_versions.kubevirt }}} |
| OpenStack | {{{ extra.capi_versions.openstack }}} |
| vSphere | {{{ extra.capi_versions.vsphere }}} |
| In-cluster IPAM | {{{ extra.capi_versions.ipam }}} |

The k0smotron providers use the release associated with this documentation:
`{{{ extra.k0smotron_version }}}`. Install them using the
[per-module installation instructions](install.md#per-module-installation-for-cluster-api)
before initializing the infrastructure provider for your example.

The core and Docker pins follow the CAPI smoke-test configuration, and the AWS
pin follows the AWS e2e configuration. Other provider pins retain the resource
schemas used by their examples. Infrastructure provider API versions are
independent of the Cluster API core API version: a `cluster.x-k8s.io/v1beta2`
Cluster can reference, for example, an OpenStack resource served at
`infrastructure.cluster.x-k8s.io/v1beta1`.

When updating these pins in `mkdocs.yml`, check the provider's compatibility with
Cluster API and validate the accompanying manifests against that release's CRDs.
Do not substitute `latest` without checking the examples. On an existing
management cluster, use the provider's documented upgrade procedure; `clusterctl
init` does not upgrade installed providers.
