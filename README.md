# git-branch-prune

`git-branch-prune` is a Go CLI that deletes merged local and remote git branches after running `git fetch --prune`.

## Features

- Deletes merged local branches from `git branch --merged`
- Deletes merged remote branches from `git branch -r --merged`
- Supports `--dry-run`
- Supports `--local-only` and `--remote-only`
- Requires `--remote NAME` when multiple remotes exist
- Protects `main`, `master`, and `develop` by default
- Supports extra protected branches from CLI flags and config
- Protects the currently checked out branch
- Ignores symbolic remote refs such as `origin/HEAD -> origin/main`

## Requirements

- Go 1.24.2 or later
- Git

## Build

```bash
go build -o bin/git-branch-prune .
```

## Install

From a local checkout:

```bash
go install .
```

From GitHub after the repository is published:

```bash
go install github.com/DemonGiggle/git-branch-prune@latest
```

## Usage

```bash
git-branch-prune --dry-run
git-branch-prune --local-only
git-branch-prune --remote-only --remote origin
git-branch-prune --protect release --protect staging
```

By default, the tool:

1. Detects the current repository root
2. Runs `git fetch --prune`
3. Finds merged local and remote branches
4. Filters protected branches and remote HEAD aliases
5. Deletes the remaining merged branches

## Configuration

The default config file is:

```text
~/.config/git-branch-prune/config.toml
```

Example:

```toml
[global]
protected_branches = ["release", "staging"]

[[project]]
repo_root = "/home/gigo/src/my-repo"
protected_branches = ["demo", "production-hotfix"]
```

Project-specific protection uses the canonical repository root path from `git rev-parse --show-toplevel`.
