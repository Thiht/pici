# Webhooks & schedules

## GitHub webhooks

Configure a webhook in your GitHub repository pointing to:

```
http://<your-host>:8080/api/webhooks/github/<project-name>
```

Content type: `application/json`. Optionally set the shared secret — it must match the project's `webhook_secret`.

Supported events:

- `push` — triggers workflows whose `paths`/`paths_ignore` match the changed files.
- `pull_request` (`opened` / `synchronize` / `reopened`) — triggers workflows and reports a GitHub **check run**.
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
- **Merge request events** (`open` / `reopen` / `update`) — triggers workflows on the source branch.

GitLab authenticates webhooks via the `X-Gitlab-Token` header (unlike GitHub's HMAC signature).

## Scheduled builds

Add a 5-field cron expression to a workflow:

```yaml
schedule: "0 4 * * *"
```

The schedule is registered whenever the workflow is discovered or run (e.g. via `GET /api/projects/{id}/configs` or any execution). The scheduler polls every `PICI_SCHEDULER_INTERVAL` (default 1m) and enqueues due builds on the default branch.

### Dependency updates

A cron workflow can propose dependency updates (à la Renovate/Dependabot). See `.ci/deps/` in the pici repository: a weekly workflow that runs `go list -m -u`, applies `go get -u ./...`, then pushes a branch and opens a PR.

It needs a project secret named `GH_TOKEN` — a GitHub PAT with `contents: write` and `pull_requests: write`:

```sh
curl -X POST localhost:8080/api/projects/pici/variables \
  -H "Authorization: Bearer $PICI_API_TOKEN" \
  -H 'content-type: application/json' \
  -d '{"key":"GH_TOKEN","value":"<pat>","secret":true}'
```

The `PICI_REPO_URL` and `PICI_REF` built-in environment variables give the script the repository and base branch.
