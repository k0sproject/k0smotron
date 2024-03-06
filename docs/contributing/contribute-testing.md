# Testing guidelines

k0smotron uses GitHub actions to run automated tests on all pull requests, before merging.
Pull request will not be reviewed before all tests are green,
so to save time and prevent your Pull request from going stale,
it is best to test it before submitting the pull request.

## Run local verifications

Run the following style and formatting commands to fix or check-in the changes:

1. Linting

   k0smotron uses [`golangci-lint`](https://golangci-lint.run/) for style verification.
   In the root directory of the repository run:

   ```shell
   make lint
   ```

   The build system installs `golangci-lint` automatically.

2. Go fmt

   Format Go source code.

   ```shell
   go fmt ./...
   ```

3. Check documentation

   The Dockerized setup is used to perform documentation tests locally.
   Run `make docs-serve-dev` to build documentation on http://localhost:8000.

   If your port `8000` is busy, run `make docs-serve-dev DOCS_DEV_PORT=9999`.
   The documentation page will be available on http://localhost:9999.

4. Pre-submit flight checks

   In the repository root directory, make sure that:

    * `make build && git diff --exit-code` runs successfully.  
      Verifies that the build is working and that the generated source code
      matches the one that's checked into source control.
    * `make test` runs successfully.  
      Verifies that all the unit tests pass.

   Note that the last test may produce a false failing result, so it might fail on
   occasion. If it fails constantly, take a deeper look at your code to find the
   source of the problem.

   If you find that all tests passed, you may open a pull request upstream.

## Open pull request

### Draft mode

You may open pull request in [a draft mode](https://github.blog/2019-02-14-introducing-draft-pull-requests).
It will go through the automated testing, but the pull request will not be assigned for a review.
Once a pull request is ready for review, transition it from the draft mode to notify the k0smotron team.

### Conformance testing

Once a pull request has been reviewed and all other tests have passed,
a code owner runs an end-to-end conformance test against the pull request.

### Pre-requisites for merge

In order for a pull request to be merged, the following conditions should exist:

1. The pull request has passed all the automated tests (style, build and conformance tests).
2. Pull request commits have been signed with the `--signoff` option.
3. Pull request was reviewed and approved by a code owner.
4. Pull request is rebased against upstream main branch.

## Cleanup local workspace

To clean up the local workspace, run `make clean`.
It cleans up all the intermediate files and directories created during the k0smotron build.
You cannot use `git clean -X` or `rm -rf`, since the Go modules
cache sets all of its subdirectories to read-only.
If you encounter problems during a deletion process,
run `chmod -R u+w /path/to/workspace && rm -rf /path/to/workspace`.