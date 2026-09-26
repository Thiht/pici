# Variables & secrets

Variables and secrets are injected into every workflow execution as environment variables. Secrets are masked in API responses and redacted from logs.

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

Project variables override global variables with the same key. Step-level `env` overrides both.

## Secrets at rest

Set `PICI_SECRET_KEY` (or `-secret-key`) to a 32-byte key (hex or base64) to encrypt secret values at rest with AES-256-GCM:

```sh
PICI_SECRET_KEY=$(openssl rand -hex 32) pici
```

Without a key, secrets are stored in plaintext (with a warning).

## Secret masking

Values of secret variables are automatically replaced with `***` in all step and build logs.
