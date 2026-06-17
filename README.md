Git who-reviewed
================

A git extension that checks what PR(s) touched a specific file, then checks who
reviewed that PR. It returns a list of reviewers, weighted by both the number
of reviews done and how recently, giving suggestions for who to add as a
reviewer for new changes.

Written in golang.


Installation
------------

Build and place the binary on your `PATH` so git can invoke it as a subcommand:

```sh
make build
mv git-who-reviewed /usr/local/bin/   # or anywhere on your PATH
```

You can then run it as `git who-reviewed <file>`.


Usage
-----

```sh
git who-reviewed path/to/file.go
```

You can pass multiple files; reviewers are collected and scored in aggregate
across all of them (a PR touching several of the files is counted once):

```sh
git who-reviewed path/to/file.go path/to/other.go
```

With no file arguments, the files changed on the current branch (relative to the
default branch) are used. This errors if run on the default branch:

```sh
git checkout my-feature-branch
git who-reviewed          # suggests reviewers based on the branch's changed files
```

Flags:

| Flag          | Default              | Description                                            |
| ------------- | -------------------- | ------------------------------------------------------ |
| `--limit`     | `5`                  | Maximum number of reviewers to suggest.                |
| `--half-life` | `2160h` (90 days)    | Recency half-life for weighting (e.g. `720h`).         |
| `--token`     | (see Auth)           | GitHub token.                                          |
| `--host`      | host of `origin`     | GitHub host (set for GitHub Enterprise).               |
| `--remote`    | `origin`             | Git remote used to resolve owner/repo.                 |
| `--max-prs`   | `50`                 | Maximum number of PRs to inspect (`0` = no limit).     |
| `--concurrency` | `8`                | Number of concurrent GitHub API requests.              |
| `--add`       | `false`              | Add the suggested reviewers to the current branch's PR via `gh`. |

Example output:

```
Suggested reviewers for "internal/auth/token.go" (from 7 PR(s)):

REVIEWER  SCORE  REVIEWS
alice     3.42   5
bob       1.88   3
carol     0.51   1
```


Authentication
--------------

A GitHub token is required to read pull requests and reviews. The token is
resolved in this order:

1. `--token` flag
2. `$GITHUB_TOKEN` / `$GH_TOKEN`
3. `gh auth token` (using `--host` for GitHub Enterprise)

GitHub Enterprise is supported via `--host` (or automatically from the `origin`
remote's host).


How weighting works
-------------------

Each review counts toward its reviewer's score using exponential decay based on
how long ago it was submitted:

```
weight = exp(-ln(2) * age / half-life)
score  = sum of weights across all of a reviewer's reviews
```

A review exactly one half-life old contributes `0.5`; two half-lives old, `0.25`.
This means recent reviews matter more, and reviewers who review often accumulate
higher scores. All review states (APPROVED, CHANGES_REQUESTED, COMMENTED) count.
Reviewers are sorted by descending score. The GitHub Copilot review bot is
excluded from suggestions.
