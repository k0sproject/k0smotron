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
