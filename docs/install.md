# Installation

To install k0smotron, run the following command:

```bash
kubectl apply -f https://docs.k0smotron.io/{{{ extra.k0smotron_version }}}/install.yaml
```

This install the k0smotron controller manager, all the related CRD definitions and needed RBAC rules.

Once the installation is completed you are ready to [create your first control planes](cluster.md).

