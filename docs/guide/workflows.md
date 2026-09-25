# Workflows (.ci)

A workflow is a folder under `.ci/`. One folder = one workflow.

```
.ci/
  build/
    Dockerfile
    ci.yml
    install.sh
    test.py
  deploy/
    Dockerfile
    ci.yml
    deploy.sh
```

## How a run works

1. pici clones the repo (branch, tag, or SHA).
2. It builds the workflow's `Dockerfile` (pre-built and cached by tag).
3. Each step runs as a container from that image, with the repo bind-mounted at `PICI_REPO_DIR`.
4. Variables & secrets are injected as environment variables.

## ci.yml

```yaml
name: build                # optional (defaults to folder name)
image: node:20             # optional: use this image instead of the Dockerfile

env:                       # workflow-level environment variables
  NODE_ENV: test

schedule: "0 4 * * *"      # optional: cron schedule (5-field)

concurrency: deploy        # optional: cancel running builds in this group when a new one starts

paths:                     # optional: only run when these files change
  - src/**
paths_ignore:
  - src/generated/**

cache:                     # optional: paths (relative to repo root) persisted between runs
  - node_modules
  - .cache

steps:
  - name: install
    script: install.sh     # path relative to .ci/<workflow>/
    timeout: 5m
    retry: 2

  - name: build
    run: npm run build
    depends_on: [install]
    artifacts:             # optional: files to collect (glob, relative to repo root)
      - dist/**
    env:
      FOO: bar
```

### Steps

- A step must define either `script` or `run`.
- `script` runs via its shebang (`#!...`) if present, else its extension picks the interpreter (`.sh`, `.py`, `.js`, `.rb`, `.go`, ...).
- `run` is executed with `sh -c`.
- `timeout` is per-step (default `PICI_STEP_TIMEOUT`, 30m).
- `retry` is the number of additional attempts after the first failure.
- `depends_on` controls ordering. Independent steps run **in parallel**. A failed step skips its dependents.
- `artifacts` collects matching files after a successful step; download them via the [API](/guide/api#artifacts).

### Cache

`cache` mounts a persistent Docker volume at each path (under `PICI_REPO_DIR`), so dependencies survive between runs. The volume is shared across executions of the same project.

### Concurrency groups

`concurrency` cancels any currently-running execution in the same group (same project) when a new one starts, so only the latest build of a branch/deployment keeps running.

## Built-in environment variables

| Variable | Description |
|---|---|
| `CI` | always `true` |
| `PICI_PROJECT` | project name |
| `PICI_PROJECT_ID` | project id |
| `PICI_WORKFLOW` | workflow name |
| `PICI_EXECUTION_ID` | execution id |
| `PICI_REF` | the ref being built |
| `PICI_COMMIT_SHA` | resolved commit SHA |
| `PICI_REPO_DIR` | mount path of the repo (default `/workspace`) |
| `PICI_WORKFLOW_DIR` | mount path of the workflow folder |

## Validate a ci.yml

```sh
curl -X POST localhost:8080/api/validate --data-binary @.ci/build/ci.yml
```
