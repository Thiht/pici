# Projects

A project is a git repository: public or private, on GitHub, GitLab, or any git host. pici clones it, discovers its workflows, and uses its provider for webhooks and status reporting.

## Register a project

```sh
pici-cli projects add demo https://github.com/acme/demo.git
```

The name is a unique slug. Other fields are optional and can be set with flags on `add` and `update`:

| Field              | Description                                                     |
| ------------------ | --------------------------------------------------------------- |
| `--provider`       | `github`, `gitlab`, or `generic` (inferred from URL if omitted) |
| `--auth-type`      | `none`, `token`, or `ssh`                                       |
| `--auth-user`      | username for token auth (defaults to `oauth2`/`git`)            |
| `--auth-secret`    | the token or SSH private key                                    |
| `--webhook-secret` | shared secret for webhook signature verification                |
| `--default-branch` | branch used when no ref is specified                            |

## Private repositories

With a token over HTTPS:

```sh
pici-cli projects add --auth-type token --auth-user x-access-token --auth-secret ghp_... private https://github.com/acme/private.git
```

With an SSH key:

```sh
pici-cli projects add --auth-type ssh --auth-secret '-----BEGIN OPENSSH PRIVATE KEY-----...' private git@github.com:acme/private.git
```

`auth_secret` is also used to report GitHub check runs and GitLab commit statuses (the GitLab token needs the `api` scope).

## Manage projects

```sh
pici-cli projects                  # list
pici-cli projects show demo        # inspect
pici-cli projects update demo --default-branch develop
pici-cli projects rm demo          # delete
```

Projects are addressable by name **or** id.
