# CLI

A small client that talks to the server. Set `PICI_ADDR` to the server URL (default `http://localhost:8080`) and, if the server requires auth, `PICI_TOKEN`.

Build it:

```sh
go build -o pici-cli ./cmd/pici-cli
```

## Commands

```sh
# trigger a run, prints the execution id
pici-cli run demo build --ref main

# watch an execution status
pici-cli status <execution-id>

# stream logs (--follow keeps streaming)
pici-cli logs <execution-id> --follow

# list a project's executions
pici-cli executions demo --limit 10
```

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
pici-cli cancel <execution-id>
pici-cli rebuild <execution-id>
```

## Artifacts

```sh
# list artifacts of an execution
pici-cli artifacts <execution-id>

# download one (writes to the basename of the path)
pici-cli artifacts get <execution-id> build/dist/app.tar.gz
```

## Validate

```sh
# validate a ci.yml
pici-cli validate .ci/build/ci.yml
```
