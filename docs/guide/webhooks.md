# Webhooks & schedules

## GitHub webhooks

Configure a webhook in your GitHub repository pointing to:

```
http://<your-host>:8080/api/webhooks/github/<project-name>
```

Content type: `application/json`. Optionally set the shared secret — it must match the project's `webhook_secret`.

Supported events:

- `push` — triggers workflows whose `paths`/`paths_ignore` match the changed files.
- `pull_request` (`opened` / `synchronize` / `reopened`) — triggers workflows and reports a GitHub **check run**. Pull requests opened from a fork are ignored (see [Pull requests from forks](#pull-requests-from-forks)).
- `ping` — healthcheck.

On pull requests, pici creates a check run (`pici/<workflow>`) using the project's `auth_secret` (a GitHub PAT). The run appears in the PR's **Checks** tab with its status, a summary, and the full logs — no PR comment needed.

## GitLab webhooks

Configure a webhook in your GitLab project pointing to:

```
http://<your-host>:8080/api/webhooks/gitlab/<project-name>
```

Set the **Secret token** to the project's `webhook_secret`.

Supported events:

- **Push events** (`object_kind: push` / `tag_push`) — triggers workflows whose `paths`/`paths_ignore` match the changed files.
- **Merge request events** (`open` / `reopen` / `update`) — triggers workflows on the source branch, filtered by the MR's changed files. Merge requests opened from another project (a fork) are ignored.

On merge requests, pici posts a **commit status** (`pici/<workflow>`) using the project's `auth_secret` (a GitLab token with the `api` scope), so the result shows on the MR. Changed files are read from the MR's diffs, so `paths`/`paths_ignore` apply.

GitLab authenticates webhooks via the `X-Gitlab-Token` header (unlike GitHub's HMAC signature).

## Pull requests from forks

A pull request opened from a fork runs an untrusted contributor's code, and every step receives the project's variables and secrets. pici therefore never builds one automatically: the webhook is acknowledged, and no execution is created. The same goes for GitLab merge requests opened from another project.

The repository owner starts those builds by hand:

```sh
pici-cli run demo build --ref refs/pull/42/head           # GitHub
pici-cli run demo build --ref refs/merge-requests/7/head  # GitLab
```

The ref is fetched from the project's own repository, where the provider publishes pull request refs — the fork is never contacted directly, and the fetch authenticates like any other clone, so it also works on private repositories. The run reports its usual check run (GitHub) or commit status (GitLab) when the project has an auth token. If the workflow restricts manual runs with `on.manual.branches`, the pull request ref has to be allowed there too, e.g. `branches: ["refs/pull/*"]`.

## Scheduled builds

Add a 5-field cron expression to a workflow:

```yaml
on:
  schedule: "0 4 * * *"
```

The schedule is registered whenever the workflow is discovered or run (e.g. via `GET /api/projects/{id}/configs` or any execution). The scheduler polls every `PICI_SCHEDULER_INTERVAL` (default 1m) and enqueues due builds on the default branch.

### Dependency updates

A cron workflow can propose dependency updates (à la Renovate/Dependabot). See `.ci/deps/` in the pici repository: a weekly workflow that runs `go list -m -u`, applies `go get -u ./...`, then pushes a branch and opens a PR.

It needs a project secret named `GH_TOKEN` — a GitHub PAT with `contents: write` and `pull_requests: write`:

```sh
pici-cli vars set GH_TOKEN <pat> --project pici --secret
```

The `PICI_REPO_URL` and `PICI_REF` built-in environment variables give the script the repository and base branch.
