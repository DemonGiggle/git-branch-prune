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

Or with the Makefile:

```bash
make build
```

That creates:

- `bin/git-branch-prune`
- `bin/git-brp` as a symlink to the same binary

## Install

From a local checkout:

```bash
go install .
```

Or with the Makefile:

```bash
make install
```

`make install` installs `git-branch-prune` with `go install .` and also creates a `git-brp` symlink in the same Go bin directory.

From GitHub after the repository is published:

```bash
go install github.com/DemonGiggle/git-branch-prune@latest
```

If you install with plain `go install`, you automatically get:

- `git branch-prune` via the `git-branch-prune` executable name

If you also want:

- `git brp`

use `make install` or create a `git-brp` symlink manually in a directory on `PATH`.

## Common development commands

```bash
make help
make fmt
make test
make build
make install
make clean
```

## Usage

```bash
git branch-prune --dry-run
git brp --dry-run
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
