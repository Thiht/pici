# Variables & secrets

Variables and secrets are injected into every execution as environment variables. Secrets are masked in API responses and redacted from logs.

## Global variables

```sh
pici-cli vars set DOCKER_REGISTRY registry.example.com

pici-cli vars
pici-cli vars rm DOCKER_REGISTRY
```

## Project variables

```sh
pici-cli vars set NPM_TOKEN secret --project demo --secret

pici-cli vars --project demo
pici-cli vars rm NPM_TOKEN --project demo
```

Variables are merged in this order, and a later value wins:

1. global variables
2. project variables
3. workflow-level `env` in `ci.yml`
4. step-level `env`

Project variables therefore override global variables with the same key.

## Secrets at rest

`PICI_SECRET_KEY` is required. It encrypts secret values at rest with AES-256-GCM:

```sh
export PICI_SECRET_KEY=$(openssl rand -hex 32)
```

Changing the key makes existing secrets unreadable, so keep it safe and back it up with the database.

## Secret masking

Secret values are omitted from API responses and replaced with `***` in all step and execution logs.
