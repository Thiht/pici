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

# list projects
pici-cli projects

# list a project's executions
pici-cli executions demo --limit 10

# validate a ci.yml
pici-cli validate .ci/build/ci.yml
```
