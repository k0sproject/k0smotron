# k0smotron GitHub workflow

Create pull requests to contribute to [k0smotron GitHub repository](http://github.com/k0sproject/k0smotron).

## Fork the project

1. Go to [k0smotron GitHub repository](http://github.com/k0sproject/k0smotron).
2. In the top right-hand corner, click "Fork" and select your username for the fork destination.

## Configure remote repository

1. Add k0smotron as a remote branch:

    ``` shell
    cd $WORKDIR/k0smotron
    git remote add my_fork git@github.com:${GITHUB_USER}/k0smotron.git
    ```

2. Prevent push to upstream branch:

    ``` shell
    git remote set-url --push origin no_push
    ```

3. Set your fork remote as a default push target:

    ``` shell
    git push --set-upstream my_fork main
    ```

4. Check the remote branches with the following command:

    ```shell
    git remote -v
    ```
    The origin branch should have `no_push` next to it:

    ```shell
    origin  https://github.com/k0sproject/k0smotron (fetch)
    origin  no_push (push)
    my_fork git@github.com:{ github_username }/k0smotron.git (fetch)
    my_fork git@github.com:{ github_username }/k0smotron.git (push)
    ```

## Create and rebase feature branch

1. Create and switch to a feature branch:

   ```shell
   git checkout -b my_feature_branch
   ```

2. Rebase your branch:

   ```shell
   git fetch origin && \
     git rebase origin/main
   ```

!!! note

    Use `git fetch` or `git rebase` instead of `git pull` to keep the commit history linear.
    The `git pull` command performs a merge, which leaves merge commits.
    Too many commits make the history messy and violate the principle
    that commits ought to be individually understandable and useful.

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

## Commit and push

Commit and sign your changes:

```shell
git commit --signoff
```

The commit message should have:

- title
- description
- link to the GitHub issue
- sign-off

For example:

```text
Title that summarizes changes in 50 characters or less

Description that briefly explains the problem your commit is solving.
Focus on why you are making this change as opposed to how.
Are there any consequences of this change? Here you can include them.

Fixes: https://github.com/k0sproject/k0smotron/issues/373

Signed-off-by: Name Lastname <user@example.com>
```

To add some additional changes or tests use `commit --amend` command.
Push your changes to your fork's repository:

```shell
git push --set-upstream my_fork my_feature_branch
```

## Open pull request

Refer to the official GitHub documentation [Creating a pull request from a fork](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/proposing-changes-to-your-work-with-pull-requests/creating-a-pull-request-from-a-fork).

### Draft mode

If you open a pull request in draft mode, it will only go through the automated testing.
Once you decide the PR is ready for review, transition it out of draft mode to notify the k0smotron team.

### Pre-requisites for merge

In order for a pull request to be merged, the following conditions should be met:

1. The PR has passed all the automated tests (style, build and conformance tests).
1. All the PR commits have been signed with the `--signoff` option.
1. The PR has been reviewed and approved by a code owner.
1. The PR has been rebased against upstream main branch.

### Wait for code review

The k0smotron team will review your pull request and leave comments.
Commit changes made in response to review comments should be added to the same branch on your fork.

Keep the PRs small to speed up the review process.

### Squash small commits

Commits on your branch should represent meaningful milestones or units of work.
Squash together small commits that contain typo fixes, rebases, review feedbacks,
and so on.
To do that, perform an interactive rebase.

### Push final changes

Once done, you can push the final commits to your branch:

```shell
git push --force
```

If necessary, you can run multiple iteration of `rebase`/`push -f`.

## Cleanup local workspace

Run `make clean` to remove all the intermediate files and directories created
locally during the k0smotron build.
You cannot use `git clean -X` or `rm -rf`, since the Go modules
cache sets all of its subdirectories to read-only.
If you encounter problems during a deletion process,
run `chmod -R u+w /path/to/workspace && rm -rf /path/to/workspace`.
