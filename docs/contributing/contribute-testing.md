# Testing guidelines

k0smotron uses GitHub actions to run automated tests on all pull requests prior to merging.
Pull request will not be reviewed before all tests are green,
so to save time and prevent your Pull request from going stale,
it is best to test it before submitting the pull request.

## Run local verifications

Run the following style and formatting commands to fix or check-in the changes:

1. Verify code style using [`golangci-lint`](https://golangci-lint.run/). In the root directory of the repository run:

   ```shell
   make lint
   ```

   The build system automatically installs `golangci-lint`.

2. Format Go source code using the `go fmt` command:


   ```shell
   go fmt ./...
   ```

3. Check documentation.

   The Dockerized setup is used to locally perform documentation tests.
   Run `make docs-serve-dev` to build documentation on http://localhost:8000.

   If your port `8000` is busy, run `make docs-serve-dev DOCS_DEV_PORT=9999`.
   The documentation page will be available on http://localhost:9999.

4. Pre-submit flight checks.

   In the repository root directory, ensure that the following commands are successfully run:

    * `make build && git diff --exit-code`
      This command verifies that the build is working
      and that the generated source code matches the code checked into source control.
    * `make test`
      This command verifies that all the unit tests pass.

   Once all tests have passed, you can open a pull request upstream.

## Open pull request

### Draft mode

If you open a pull request in draft mode, it will only go through the automated testing.
Once you decide the PR is ready for review, transition it out of draft mode to notify the k0smotron team.

### Pre-requisites for merge

In order for a pull request to be merged, the following conditions should be met:

1. The PR has passed all the automated tests (style, build and conformance tests).
1. All the PR commits have been signed with the `--signoff` option.
1. The PR has been reviewed and approved by a code owner.
1. The PR has been rebased against upstream main branch.

## Cleanup local workspace

Run `make clean` to remove all the intermediate files and directories created
locally during the k0smotron build.
You cannot use `git clean -X` or `rm -rf`, since the Go modules
cache sets all of its subdirectories to read-only.
If you encounter problems during a deletion process,
run `chmod -R u+w /path/to/workspace && rm -rf /path/to/workspace`.