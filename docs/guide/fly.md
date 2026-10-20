# Deploying on Fly.io

[Fly.io](https://fly.io) is the recommended hosted target for pici. Each Fly
Machine is a Firecracker VM, so pici can run its own Docker daemon inside the
instance.

## What you get

- one Fly Machine with a persistent volume for the SQLite database, workspaces,
  and the Docker image and build cache;
- HTTPS on `<app>.fly.dev`;
- GitHub, GitLab or OIDC sign-in;
- automatic restarts and a health check on `/health`.

The deployment uses a single Machine and a single volume. It is not highly
available, and deployments or host failures cause downtime. Fly takes daily
volume snapshots (kept for five days by default) for backups.

## Prerequisites

- a Fly.io account with a payment method (new organizations get a limited
  trial, not a free tier);
- [`flyctl`](https://fly.io/docs/flyctl/install/);
- `git` and `openssl`;
- a GitHub or GitLab OAuth app (you can create it while the script runs).

```sh
fly auth signup   # or: fly auth login
```

## One-command deploy

From anywhere:

```sh
sh -c "$(curl -fsSL https://raw.githubusercontent.com/Thiht/pici/main/scripts/deploy-fly.sh)"
```

Or from a clone, which also deploys local changes:

```sh
git clone https://github.com/Thiht/pici
cd pici
./scripts/deploy-fly.sh
```

The script:

1. asks for a Fly app name and region;
2. creates the app and prints the OAuth callback URL;
3. asks for the sign-in application credentials and the allowed users, teams
   or groups;
4. generates `PICI_SECRET_KEY` on the first deploy if you did not provide one
   and prints it once (save it for backups and restores);
5. stores the configuration as Fly secrets and deploys the app.

It is safe to run again to update the deployment: an existing app is reused, the
existing `PICI_SECRET_KEY` is kept, and the other secrets are replaced. Pass
`--dry-run` to see the commands without creating anything, and `--help` for all
options.

The script never replaces an existing `PICI_SECRET_KEY`: rotating it without
re-encrypting the stored variables and secrets would make them unreadable. To
rotate it deliberately, unset the secret first with `fly secrets unset
PICI_SECRET_KEY -a <app>` and expect the existing secrets to be lost.

Fly app names are global. If the generated name is taken, rerun the script with
`--name <name>`. The default region is `cdg`; see `fly platform regions` for the
full list.

### Sign-in application

For GitHub, create an OAuth App at
<https://github.com/settings/applications/new> with:

- **Homepage URL**: `https://<app>.fly.dev`
- **Authorization callback URL**: `https://<app>.fly.dev/auth/github/callback`

In team mode, the app requests the `read:org` scope and your organization may
need to approve it. For a personal instance, allowlist your login instead of a
team.

For GitLab, create an application at
`https://gitlab.com/-/user_settings/applications` (or the equivalent page on
your instance) with:

- **Redirect URI**: `https://<app>.fly.dev/auth/gitlab/callback`
- **Scopes**: `read_api` and `offline_access`

For OIDC, create a confidential application in your provider with redirect URI
`https://<app>.fly.dev/auth/oidc/callback`. The default scopes are `openid`,
`profile`, `email` and `offline_access`; if you restrict access with groups, the
provider must return a `groups` claim in the ID token or userinfo response.

See [Authentication](/guide/authentication) for the details and all access
models.

## Non-interactive deploy

Provide the values in the environment instead of answering prompts:

```sh
export PICI_FLY_APP=my-pici
export PICI_FLY_REGION=cdg
export PICI_AUTH_PROVIDER=github
export PICI_GITHUB_CLIENT_ID=Iv1.abc123
export PICI_GITHUB_CLIENT_SECRET=...
export PICI_GITHUB_USERS=your-login

./scripts/deploy-fly.sh
```

`PICI_SECRET_KEY` is generated and printed on the first deploy when omitted;
later runs reuse the existing Fly secret. In CI, set it explicitly. Use
`PICI_GITHUB_TEAMS` for team mode, the `PICI_GITLAB_*` variables with
`PICI_AUTH_PROVIDER=gitlab`, or the `PICI_OIDC_*` variables with
`PICI_AUTH_PROVIDER=oidc`. `PICI_PUBLIC_URL` defaults to
`https://$PICI_FLY_APP.fly.dev`; set it when you use a custom domain.

| Variable                    | Description                                                              |
| --------------------------- | ------------------------------------------------------------------------ |
| `PICI_FLY_APP`              | Fly app name                                                             |
| `PICI_FLY_REGION`           | Fly region code (default `cdg`)                                          |
| `PICI_FLY_ORG`              | Fly organization (default: your default org)                             |
| `PICI_FLY_REF`              | Git branch or tag to deploy (default `main`)                             |
| `PICI_PUBLIC_URL`           | Public base URL (default `https://<app>.fly.dev`)                        |
| `PICI_SECRET_KEY`           | 32-byte key for secrets at rest (generated on first deploy, then reused) |
| `PICI_AUTH_PROVIDER`        | `github`, `gitlab` or `oidc` (default `github`)                          |
| `PICI_GITHUB_CLIENT_ID`     | GitHub OAuth App client id                                               |
| `PICI_GITHUB_CLIENT_SECRET` | GitHub OAuth App client secret                                           |
| `PICI_GITHUB_USERS`         | Allowed GitHub logins, comma-separated                                   |
| `PICI_GITHUB_TEAMS`         | Allowed GitHub teams (`org/team`), comma-separated                       |
| `PICI_GITLAB_URL`           | GitLab base URL (default `https://gitlab.com`)                           |
| `PICI_GITLAB_CLIENT_ID`     | GitLab OAuth application id                                              |
| `PICI_GITLAB_CLIENT_SECRET` | GitLab OAuth application secret                                          |
| `PICI_GITLAB_GROUPS`        | Allowed GitLab groups, comma-separated                                   |
| `PICI_GITLAB_USERS`         | Allowed GitLab usernames, comma-separated                                |
| `PICI_OIDC_ISSUER`          | OIDC issuer URL                                                          |
| `PICI_OIDC_CLIENT_ID`       | OIDC client id                                                           |
| `PICI_OIDC_CLIENT_SECRET`   | OIDC client secret                                                       |
| `PICI_OIDC_GROUPS`          | Allowed OIDC groups, comma-separated                                     |
| `PICI_OIDC_USERS`           | Allowed OIDC usernames or emails, comma-separated                        |
| `PICI_OIDC_SCOPES`          | OIDC scopes (default `openid profile email offline_access`)              |
| `PICI_OIDC_NAME`            | OIDC provider display name (default `SSO`)                               |

## Costs and sizing

The included configuration uses a `shared-cpu-2x` Machine with 2 GB of RAM and a
30 GB volume. At Fly's list prices that is roughly $20 USD/month, depending on
the region, egress and snapshots. Check the
[pricing page](https://fly.io/docs/about/pricing/) for current rates.

Docker builds are I/O-heavy. For larger projects, increase the Machine size and
the volume:

```sh
fly scale vm shared-cpu-4x --vm-memory 4096 -a my-pici
fly volumes list -a my-pici
fly volumes extend <volume-id> --size 50 -a my-pici
```

## Updating

Re-run the deploy script. From a clone you can also deploy directly:

```sh
fly deploy -a my-pici --primary-region cdg --ha=false
```

To pin a release with the one-liner, pass the tag:

```sh
PICI_FLY_REF=v0.1.0 sh -c "$(curl -fsSL https://raw.githubusercontent.com/Thiht/pici/main/scripts/deploy-fly.sh)"
```

## Custom domain

Point the domain at the app with a Fly certificate:

```sh
fly certs add pici.example.com -a my-pici
```

Fly prints the DNS records to create at your registrar. Check the certificate
status with `fly certs show pici.example.com -a my-pici`, then update the public
URL and the OAuth callback:

```sh
fly secrets set PICI_PUBLIC_URL=https://pici.example.com -a my-pici
```

Finally, update the OAuth app callback to
`https://pici.example.com/auth/<provider>/callback`.

## Backups

Fly snapshots the volume daily and keeps snapshots for five days by default:

```sh
fly volumes list -a my-pici
fly volumes snapshots list <volume-id> -a my-pici
```

Snapshots include the Docker cache under `/data/docker`, so they are not a
substitute for an off-site copy of the database. The SQLite database lives at
`/data/pici.db` on the volume; for an important instance, copy it somewhere safe
regularly:

```sh
fly ssh sftp get /data/pici.db -a my-pici
```

## Troubleshooting

```sh
fly logs -a my-pici
fly status -a my-pici
fly ssh console -a my-pici
```

- **The app starts but workflows fail**: check that Docker is running with
  `docker info` inside `fly ssh console`. The entrypoint starts `dockerd` before
  pici.
- **The health check fails**: the first boot waits for `dockerd` and the
  database; `fly logs` shows the startup sequence. The check has a 60-second
  grace period.
- **The volume fills up**: images and build cache live under `/data/docker`.
  Remove unused cache entries from the project cache page or with
  `pici-cli cache`, then extend the volume if needed.
- **OAuth redirect mismatch**: `PICI_PUBLIC_URL` and the OAuth app callback must
  match the URL you open, including the scheme and host.
- **OIDC discovery fails on startup**: the issuer URL must be reachable from the
  Fly Machine and match the `issuer` value in its discovery document.
- **The app name is taken**: Fly app names are global; rerun with
  `--name <name>`.
