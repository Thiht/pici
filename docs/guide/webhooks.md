# Webhooks & schedules

In the web UI, the project form's **Webhook secret** field has a **Generate** button and a **How to set this up** guide. The guide links to the provider's webhook settings page and lists the exact payload URL, content type, secret and events to configure.

## GitHub webhooks

In your GitHub repository, add a webhook pointing to:

```
https://pici.example.com/api/webhooks/github/<project-name>
```

Content type: `application/json`. Set a secret if you want — it must match the project's `webhook_secret`.

Supported events:

- `push` — triggers workflows whose `paths`/`paths_ignore` match the changed files.
- `pull_request` (`opened`, `synchronize`, `reopened`) — triggers workflows and reports a GitHub **check run**. Pull requests from a fork are ignored (see [Pull requests from forks](#pull-requests-from-forks)).
- `ping` — healthcheck.

On pull requests, pici creates a check run (`pici/<workflow>`) using the project's `auth_secret` (a GitHub PAT). It appears in the PR's **Checks** tab with its status, a summary, and the full logs.

## GitLab webhooks

In your GitLab project, add a webhook pointing to:

```
https://pici.example.com/api/webhooks/gitlab/<project-name>
```

Set the **Secret token** to the project's `webhook_secret`.

Supported events:

- **Push events** (`push`, `tag_push`) — triggers workflows whose `paths`/`paths_ignore` match the changed files.
- **Merge request events** (`open`, `reopen`, `update`) — triggers workflows on the source branch, filtered by the MR's changed files. Merge requests from another project (a fork) are ignored.

On merge requests, pici posts a **commit status** (`pici/<workflow>`) using the project's `auth_secret` (a GitLab token with the `api` scope). Changed files are read from the MR's diffs, so `paths`/`paths_ignore` apply.

GitLab authenticates webhooks with the `X-Gitlab-Token` header, unlike GitHub's HMAC signature.

## Pull requests from forks

A pull request from a fork runs an untrusted contributor's code, and every step receives the project's variables and secrets. pici therefore never runs one automatically: the webhook is acknowledged, and no execution is created. The same goes for a GitLab merge request opened from another project.

The repository owner starts those runs by hand:

```sh
pici-cli run demo build --ref refs/pull/42/head           # GitHub
pici-cli run demo build --ref refs/merge-requests/7/head  # GitLab
```

The ref is fetched from the project's own repository, where the provider publishes pull request refs. The fork is never contacted directly, and the fetch authenticates like any other clone, so it also works on private repositories. The run reports its usual check run or commit status when the project has an auth token. If the workflow restricts manual runs with `on.manual.branches`, allow the pull request ref there too, e.g. `branches: ["refs/pull/*"]`.

## Scheduled runs

Add a 5-field cron expression to a workflow:

```yaml
on:
  schedule: "0 4 * * *"
```

Scheduled runs use the default branch. The schedule is registered whenever the workflow is discovered or run (for example by `GET /api/projects/{id}/configs` or any execution). The scheduler polls every `PICI_SCHEDULER_INTERVAL` (default `1m`) and enqueues due runs. See [Triggers](/guide/workflows#triggers) for the other events.

### Example: dependency updates

A cron workflow can propose dependency updates (à la Renovate/Dependabot). See [`.ci/deps`](https://github.com/Thiht/pici/tree/main/.ci/deps) in the pici repository: a weekly workflow that runs `go list -m -u`, applies `go get -u ./...`, then pushes a branch and opens a PR.

It needs a project secret named `GH_TOKEN` — a GitHub PAT with `contents: write` and `pull_requests: write`:

```sh
pici-cli vars set GH_TOKEN <pat> --project pici --secret
```

`PICI_REPO_URL` and `PICI_REF` give the script the repository and base branch.
