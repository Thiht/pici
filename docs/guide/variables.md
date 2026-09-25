# Variables & secrets

Variables and secrets are injected into every workflow execution as environment variables. Secrets are masked in API responses and redacted from logs.

## Global variables

```sh
curl -X POST localhost:8080/api/variables \
  -H 'content-type: application/json' \
  -d '{"key":"DOCKER_REGISTRY","value":"registry.example.com"}'

curl localhost:8080/api/variables
curl -X DELETE localhost:8080/api/variables/DOCKER_REGISTRY
```

## Project variables

```sh
curl -X POST localhost:8080/api/projects/demo/variables \
  -H 'content-type: application/json' \
  -d '{"key":"NPM_TOKEN","value":"secret","secret":true}'

curl localhost:8080/api/projects/demo/variables
curl -X DELETE localhost:8080/api/projects/demo/variables/NPM_TOKEN
```

Project variables override global variables with the same key. Step-level `env` overrides both.

## Secrets at rest

Set `PICI_SECRET_KEY` (or `-secret-key`) to a 32-byte key (hex or base64) to encrypt secret values at rest with AES-256-GCM:

```sh
PICI_SECRET_KEY=$(openssl rand -hex 32) go run ./cmd/pici
```

Without a key, secrets are stored in plaintext (with a warning).

## Secret masking

Values of secret variables are automatically replaced with `***` in all step and build logs.
