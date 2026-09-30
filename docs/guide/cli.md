# CLI

A small client that talks to the server. Set `PICI_ADDR` to the server URL (default `http://localhost:8080`) and, if the server requires auth, `PICI_TOKEN`.

Build it:

```sh
go build -o pici-cli ./cmd/pici-cli
```

## Shell completion

`pici-cli` ships command, flag, and dynamic value completion (project names, workflows, variable keys, cache images/volumes). Enable it for your shell:

```sh
# bash
source <(pici-cli completion bash)

# zsh
source <(pici-cli completion zsh)

# fish
pici-cli completion fish | source

# powershell
pici-cli completion powershell | Out-String | Invoke-Expression
```

Add the `source` line to your shell rc file to make it permanent. Completion queries the server (`PICI_ADDR`) for dynamic values and silently returns nothing if it is unreachable.

## Commands

```sh
# trigger a run, prints the execution id
pici-cli run demo build --ref main

# watch an execution status
pici-cli status demo 42

# stream logs (--follow keeps streaming)
pici-cli logs demo 42 --follow

# list a project's executions
pici-cli executions demo --limit 10
```

## Local runs

Run a workflow on the current worktree — uncommitted and untracked (non-ignored)
files included — without pushing:

```sh
pici-cli run demo build --local
pici-cli run demo build --local --dir path/to/checkout
```

The CLI archives the worktree plus `.git/` (never `.git/config`, which may
contain credentials) and the server extracts it instead of cloning. Snapshot
executions are marked `local`, cannot be rebuilt, and do not post check
runs/commit statuses. Uploads are capped by the server's `-max-snapshot-size`.

## Projects

```sh
# list projects
pici-cli projects

# register a project (flags for auth come before the name/url)
pici-cli projects add demo https://github.com/acme/demo.git
pici-cli projects add --auth-type token --auth-secret ghp_... private https://github.com/acme/private.git

# inspect / update / delete
pici-cli projects show demo
pici-cli projects update demo --default-branch develop
pici-cli projects rm demo
```

## Variables & secrets

```sh
# global variables
pici-cli vars
pici-cli vars set DOCKER_REGISTRY registry.example.com
pici-cli vars rm DOCKER_REGISTRY

# project variables (scoped with --project)
pici-cli vars --project demo
pici-cli vars set NPM_TOKEN secret --project demo --secret
pici-cli vars rm NPM_TOKEN --project demo
```

## Executions

```sh
# cancel or re-run an execution
pici-cli cancel demo 42
pici-cli rebuild demo 42
pici-cli retry demo 42   # retry the failed steps, reusing the workspace
```

## Artifacts

```sh
# list artifacts of an execution
pici-cli artifacts demo 42

# download one (writes to the basename of the path)
pici-cli artifacts get demo 42 build/dist/app.tar.gz
```

## Cache

```sh
# list the project's Docker cache: workflow images and cross-run cache volumes
pici-cli cache demo

# remove one image (by reference) or cache volume (by name)
pici-cli cache rm-image demo pici/<project-id>-build
pici-cli cache rm-volume demo pici-cache-<project-id>-node_modules
```

## Validate

```sh
# validate a ci.yml
pici-cli validate .ci/build/ci.yml
```

## Health

```sh
# check server health (exits non-zero if the server is unreachable or unhealthy)
pici-cli health
```

## Version

```sh
# local build version and VCS revision/time/modified
pici-cli version

# the server's version instead
pici-cli version --server
```
