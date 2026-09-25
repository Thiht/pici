# Projects

A project is a git repository. It can be public or private, on GitHub, GitLab, or any git host.

## Register a project

```sh
curl -X POST localhost:8080/api/projects \
  -H 'content-type: application/json' \
  -d '{
    "name": "demo",
    "repo_url": "https://github.com/acme/demo.git",
    "default_branch": "main"
  }'
```

Fields:

| Field | Description |
|---|---|
| `name` | unique slug (required) |
| `repo_url` | git URL (required) |
| `provider` | `github`, `gitlab`, or `generic` (inferred from URL if omitted) |
| `auth_type` | `none`, `token`, or `ssh` |
| `auth_user` | username for token auth (defaults to `oauth2`/`git`) |
| `auth_secret` | the token or SSH private key |
| `webhook_secret` | shared secret for webhook signature verification |
| `default_branch` | branch used when no ref is specified |

## Private repositories

Token (HTTPS):

```sh
curl -X POST localhost:8080/api/projects \
  -H 'content-type: application/json' \
  -d '{"name":"private","repo_url":"https://github.com/acme/private.git",
       "auth_type":"token","auth_user":"x-access-token","auth_secret":"ghp_..."}'
```

SSH:

```sh
curl -X POST localhost:8080/api/projects \
  -H 'content-type: application/json' \
  -d '{"name":"private","repo_url":"git@github.com:acme/private.git",
       "auth_type":"ssh","auth_secret":"-----BEGIN OPENSSH PRIVATE KEY-----\n..."}'
```

The `auth_secret` (a GitHub/GitLab PAT) is also used to report GitHub check runs.

## List / inspect / update / delete

```sh
curl localhost:8080/api/projects
curl localhost:8080/api/projects/demo
curl -X PUT localhost:8080/api/projects/demo -H 'content-type: application/json' -d '{"default_branch":"develop"}'
curl -X DELETE localhost:8080/api/projects/demo
```

Projects are addressable by name **or** id.
