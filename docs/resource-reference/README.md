# Resource reference

The `.md` files in this directory are generated from the CRDs via [crdoc](https://github.com/fybrik.io/crdoc) and are not tracked in git
(see `.gitignore`). Only the `*-toc.yaml` files, which control the table of contents for each generated page, are checked in.

To (re)generate them locally:

```sh
make docs-generate-reference
```

This runs automatically as part of `make -C docs docs` and `make docs-serve-dev`.
