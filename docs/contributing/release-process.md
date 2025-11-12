# Release process

This document describes the steps required to make a new release of k0smotron. It covers tagging in Git, creating a GitHub Release, building and publishing assets (container images, CRD and install manifests) and publishing documentation.

## Preparation

Before starting a release, ensure that:

1. All planned changes have been merged into the `main` branch.
2. CI is passing and the `main` branch is green.
3. The [`metadata.yaml`](https://github.com/k0sproject/k0smotron/blob/main/metadata.yaml) files have been updated for the new release series if you are making a new **major** or **minor** release.

## Create a Git tag

Tag the current `main` branch following semantic versioning (for example `v1.6.0`). Then push the tag to GitHub:

```shell
git checkout main
git pull --ff-only origin main
git tag vX.Y.Z
git push origin vX.Y.Z
```

## Draft GitHub Release

Pushing the tag triggers the GitHub Actions workflow defined in [`.github/workflows/release.yml`](https://github.com/k0sproject/k0smotron/blob/main/.github/workflows/release.yml). This workflow:

- Creates a **draft** GitHub Release for the new tag.
- Builds and pushes multi-architecture container images to `quay.io/k0sproject/k0smotron` and `ghcr.io/k0sproject/k0smotron`.
- Generates and uploads release assets:
  - `metadata.yaml`
  - `bootstrap-components.yaml`
  - `control-plane-components.yaml`
  - `infrastructure-components.yaml`
  - `cluster-template.yaml`
  - `cluster-template-hcp.yaml`

!!! note

    Draft releases allow you to review and edit the release notes before publishing.

After the workflow completes, go to the draft release on GitHub, update the release notes (highlights, change log, known issues, etc.), and click **Publish release**.

## Publish documentation

Once the GitHub Release is published, the documentation workflow (`.github/workflows/publish-docs.yml`) will automatically:

1. Build and deploy the site using [mike](https://github.com/jimporter/mike).
2. Publish the newly tagged version.
3. Update version aliases (`stable`, `main`) and set `stable` as the default.
4. Update and commit the `install.yaml` in the `gh-pages` branch under the `stable/` directory.

You can verify the documentation at https://docs.k0smotron.io/ after the workflow completes.

## Post-release housekeeping

If you are making a new **major** or **minor** release, after publishing the release, update the E2E upgrade test to reflect the new release version:

- Add new release in `k0smotronMinorVersionsToCheckUpgrades` in [`e2e/k0smotron_upgrade_test.go`](https://github.com/k0sproject/k0smotron/blob/main/e2e/k0smotron_upgrade_test.go).
- Add the new release entry under the `k0sproject-k0smotron` provider in [`e2e/config.yaml`](https://github.com/k0sproject/k0smotron/blob/main/e2e/config.yaml) (including matching `control-plane-components.yaml` and `bootstrap-components.yaml` URLs).