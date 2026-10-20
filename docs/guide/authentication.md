# Authentication

pici signs people in with **GitHub, GitLab, or any OpenID Connect provider**, and uses **personal API tokens** for the CLI and scripts. There is no local password.

An instance uses one provider at a time, selected with `PICI_AUTH_PROVIDER` (`github` by default, `gitlab`, or `oidc`). Access is restricted to:

- members of one or more **teams or groups**, and/or
- an explicit **allowlist of logins**.

At least one is required for the selected provider. A personal instance with no organization can use the users allowlist only; an organization instance can use teams/groups, users, or both.

Common settings:

| Variable           | Description                                                       |
| ------------------ | ----------------------------------------------------------------- |
| `PICI_PUBLIC_URL`  | **required** — public base URL, used to build the OAuth callback  |
| `PICI_SECRET_KEY`  | **required** — 32-byte key; also encrypts the stored OAuth tokens |
| `PICI_SESSION_TTL` | optional browser session lifetime (default `720h`)                |

::: warning
Any authorized user is a full pici administrator: they can read every project secret and run arbitrary code in privileged containers. Treat access as root access to the pici host.
:::

## GitHub

### 1. Create a GitHub OAuth App

In GitHub: **Settings → Developer settings → OAuth Apps → New OAuth App**.

- **Homepage URL**: your `PICI_PUBLIC_URL`, e.g. `https://pici.example.com`
- **Authorization callback URL**: `https://pici.example.com/auth/github/callback`

Copy the **Client ID** and generate a **Client secret**.

In team mode, the app requests the `read:org` scope, and your organization may need to approve it in **Organization settings → Third-party access**. In users-only mode, no extra scope or approval is needed.

### 2. Configure pici

| Variable                    | Description                          |
| --------------------------- | ------------------------------------ |
| `PICI_GITHUB_CLIENT_ID`     | OAuth App client id                  |
| `PICI_GITHUB_CLIENT_SECRET` | OAuth App client secret              |
| `PICI_GITHUB_TEAMS`         | optional, allowed teams (`org/team`) |
| `PICI_GITHUB_USERS`         | optional, allowed GitHub logins      |

```sh
export PICI_PUBLIC_URL=https://pici.example.com
export PICI_GITHUB_CLIENT_ID=Iv1.abc123
export PICI_GITHUB_CLIENT_SECRET=ghs_...
export PICI_GITHUB_TEAMS=acme/pici-users
# or, for a personal instance:
# export PICI_GITHUB_USERS=Thiht
export PICI_SECRET_KEY=$(openssl rand -hex 32)
pici
```

### 3. Configure access

**Teams** — pici checks direct membership only:

- use a flat team (e.g. `pici-users`); a member of a child team is not a member of a configured parent team, so list each team explicitly;
- outside collaborators explicitly added to the team are accepted;
- names are matched case-insensitively;
- removing somebody from the team removes their access at the next access check, without a pici restart.

**Users** — list GitHub logins in `PICI_GITHUB_USERS`:

```sh
export PICI_GITHUB_USERS=Thiht,other-login
```

The list is matched case-insensitively and requires a restart to change. Renaming a GitHub login requires updating the list (the login is refreshed on every sign-in).

## GitLab

GitLab.com and self-hosted instances are supported.

### 1. Create a GitLab OAuth application

In GitLab: **User settings → Applications** (or an instance/group application managed by an administrator).

- **Name**: pici
- **Redirect URI**: `https://pici.example.com/auth/gitlab/callback`
- **Scopes**: `read_api` and `offline_access`
- **Confidential**: yes

Copy the **Application ID** and **Secret**. For a self-hosted instance, note its base URL (a relative installation path such as `https://example.com/gitlab` is supported).

### 2. Configure pici

| Variable                    | Description                             |
| --------------------------- | --------------------------------------- |
| `PICI_GITLAB_URL`           | base URL (default `https://gitlab.com`) |
| `PICI_GITLAB_CLIENT_ID`     | OAuth application id                    |
| `PICI_GITLAB_CLIENT_SECRET` | OAuth application secret                |
| `PICI_GITLAB_GROUPS`        | optional, allowed groups (`group/path`) |
| `PICI_GITLAB_USERS`         | optional, allowed usernames             |

```sh
export PICI_AUTH_PROVIDER=gitlab
export PICI_PUBLIC_URL=https://pici.example.com
export PICI_GITLAB_URL=https://gitlab.example.com
export PICI_GITLAB_CLIENT_ID=0123456789abcdef
export PICI_GITLAB_CLIENT_SECRET=gloas-...
export PICI_GITLAB_GROUPS=acme/pici-users
# or, for a personal instance:
# export PICI_GITLAB_USERS=alice
export PICI_SECRET_KEY=$(openssl rand -hex 32)
pici
```

The callback URL is always `PICI_PUBLIC_URL/auth/gitlab/callback`; only the GitLab base URL changes for a self-hosted instance.

### 3. Configure access

**Groups** — list full group paths in `PICI_GITLAB_GROUPS`:

```sh
export PICI_GITLAB_GROUPS=acme/pici-users,acme/platform/subgroup
```

Subgroups are supported and matched by their complete path. GitLab applies inherited membership, so a member of a parent group is considered a member of its subgroups. Paths are matched case-insensitively.

**Users** — list GitLab usernames in `PICI_GITLAB_USERS`:

```sh
export PICI_GITLAB_USERS=alice,bob
```

The list is matched case-insensitively and requires a restart to change.

## OpenID Connect

Any OpenID Connect 1.0 provider works: Keycloak, Authentik, Authelia, Okta, Microsoft Entra ID, Google, Dex, Zitadel, Forgejo/Codeberg, and so on. pici discovers the issuer configuration at startup, so a misconfigured issuer stops the server immediately.

### 1. Create an OIDC application

In your provider, create a **confidential** OAuth2/OIDC application:

- **Redirect URI**: `https://pici.example.com/auth/oidc/callback`
- **Scopes**: `openid profile email offline_access`

`offline_access` lets pici refresh the access token before each access check. If your provider rejects it, override the list with `PICI_OIDC_SCOPES` and expect users to sign in again when their token expires.

### 2. Configure pici

| Variable                  | Description                                             |
| ------------------------- | ------------------------------------------------------- |
| `PICI_OIDC_ISSUER`        | issuer URL, exactly as returned in OIDC discovery       |
| `PICI_OIDC_CLIENT_ID`     | client id                                               |
| `PICI_OIDC_CLIENT_SECRET` | client secret                                           |
| `PICI_OIDC_GROUPS`        | optional, allowed groups (from the `groups` claim)      |
| `PICI_OIDC_USERS`         | optional, allowed usernames or emails                   |
| `PICI_OIDC_SCOPES`        | optional, default `openid profile email offline_access` |
| `PICI_OIDC_NAME`          | optional display name, e.g. `Keycloak` (default `SSO`)  |

```sh
export PICI_AUTH_PROVIDER=oidc
export PICI_PUBLIC_URL=https://pici.example.com
export PICI_OIDC_ISSUER=https://keycloak.example.com/realms/pici
export PICI_OIDC_CLIENT_ID=pici
export PICI_OIDC_CLIENT_SECRET=...
export PICI_OIDC_GROUPS=acme/pici-users
# or, for a personal instance:
# export PICI_OIDC_USERS=alice@example.com
export PICI_OIDC_NAME=Keycloak
export PICI_SECRET_KEY=$(openssl rand -hex 32)
pici
```

The issuer must match the provider's discovery document exactly, including any trailing slash. For Google, use `https://accounts.google.com`; for Entra ID, use your tenant-specific issuer.

### 3. Configure access

pici reads the `groups` claim from the userinfo endpoint:

```sh
export PICI_OIDC_GROUPS=acme/pici-users,platform
```

Group names are matched case-insensitively. Make sure your provider actually includes the `groups` claim (some require an extra scope or claim mapper). Forgejo and Codeberg do not expose groups in userinfo; use `PICI_OIDC_USERS` with them.

Otherwise, list usernames or emails in `PICI_OIDC_USERS`:

```sh
export PICI_OIDC_USERS=alice,bob@example.com
```

pici matches the `preferred_username` claim, or the `email` claim when the provider does not return a username. The list is matched case-insensitively and requires a restart to change.

## Sign in and create a token

Open your `PICI_PUBLIC_URL` and sign in with the configured provider. Then open **Account** to:

- create a personal API token for the CLI, with an expiration (90 days by default, or never);
- rename or revoke tokens;
- revoke browser sessions.

Copy the token immediately: it is shown once and only its hash is stored.

```sh
export PICI_TOKEN=pici_pat_...
pici-cli projects
```

The CLI and API accept the token as `Authorization: Bearer <token>` or `X-API-Token: <token>`. The JSON API never accepts the browser session cookie.

## Access checks and revocation

Access is checked on sign-in, then by a background job at most once every 24 hours per user. When a user leaves all configured teams/groups, is removed from the users allowlist, or when the provider rejects their stored OAuth token, pici:

- marks the user disabled;
- deletes their browser sessions;
- rejects their personal API tokens.

Signing in again with valid access reactivates the account. Network errors, rate limits, and provider 5xx responses are treated as transient: access is kept and the check is retried later.

## Switching providers

Only one provider is active per instance. After switching `PICI_AUTH_PROVIDER`, accounts and sessions created with the previous provider stop working immediately; users must sign in again with the new provider.

## Sessions and security

Browser sessions are stored in the database, valid for `PICI_SESSION_TTL` (30 days by default), sliding on activity, and protected by a CSRF token. Revoke a single session or sign out everywhere from the Account page.

pici uses the OAuth 2.0 authorization-code flow with:

- an unguessable `state`, compared in constant time;
- **PKCE (`S256`)**, with a single-use verifier;
- a static redirect URI, and a server-side code exchange with the client secret;
- a `/user` call after every exchange to revalidate the identity;
- for OIDC, a `nonce` and a fully validated ID token (signature, issuer, audience, expiry, `sub`); the userinfo `sub` must match;
- minimal scopes: `read:org` only in GitHub team mode, `read_api` and `offline_access` for GitLab, and `openid profile email offline_access` by default for OIDC.

GitHub OAuth Apps and GitLab OAuth applications do not return an ID token, so the nonce and ID token checks only apply to OIDC. Keep **wildcard matching disabled** in the OAuth application settings so the callback URL must match exactly.

Stored access tokens are renewed with their refresh token before every access check. If a refresh is rejected (the user revoked the application, or the credential expired for good), the account is disabled and the user must sign in again. A provider that never issues refresh tokens cannot be revalidated once its access token expires.

## Limitations

- One identity per user: there is no account linking across providers. Switching providers requires users to sign in again.
- No roles or per-project permissions: every authorized user has full access.
- The public status badge and webhooks remain unauthenticated (webhooks use their own secret).
