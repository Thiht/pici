# Projects

A project is a git repository. It can be public or private, on GitHub, GitLab, or any git host.

## Register a project

```sh
pici-cli projects add demo https://github.com/acme/demo.git
```

Optional fields are set via flags on `add`/`update` (`--provider`, `--auth-type`, `--auth-user`, `--auth-secret`, `--webhook-secret`, `--default-branch`):

| Field            | Description                                                     |
| ---------------- | --------------------------------------------------------------- |
| `name`           | unique slug (required)                                          |
| `repo_url`       | git URL (required)                                              |
| `provider`       | `github`, `gitlab`, or `generic` (inferred from URL if omitted) |
| `auth_type`      | `none`, `token`, or `ssh`                                       |
| `auth_user`      | username for token auth (defaults to `oauth2`/`git`)            |
| `auth_secret`    | the token or SSH private key                                    |
| `webhook_secret` | shared secret for webhook signature verification                |
| `default_branch` | branch used when no ref is specified                            |

## Private repositories

Token (HTTPS):

```sh
pici-cli projects add --auth-type token --auth-user x-access-token --auth-secret ghp_... private https://github.com/acme/private.git
```

SSH:

```sh
pici-cli projects add --auth-type ssh --auth-secret '-----BEGIN OPENSSH PRIVATE KEY-----...' private git@github.com:acme/private.git
```

The `auth_secret` (a GitHub/GitLab PAT) is also used to report GitHub check runs.

## List / inspect / update / delete

```sh
pici-cli projects
pici-cli projects show demo
pici-cli projects update demo --default-branch develop
pici-cli projects rm demo
```

Projects are addressable by name **or** id.
