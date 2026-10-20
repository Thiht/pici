# Authentication

pici signs people in with **GitHub, GitLab, or any OpenID Connect provider**,
and uses **personal API tokens** for the CLI and scripts. There is no local
password.

An instance uses exactly one identity provider at a time, selected with
`PICI_AUTH_PROVIDER` (`github` by default, `gitlab`, or `oidc`). Access is
restricted to:

- members of one or more **teams or groups**, or
- an explicit **allowlist of logins**.

Set at least one of the two for the selected provider. A personal instance with
no organization can use the users allowlist only; an organization instance can
use teams/groups, users, or both.

> Any authorized user is a full pici administrator: they can read every project
> secret and run arbitrary code in privileged containers. Treat access as root
> access to the pici host.

## GitHub

### 1. Create a GitHub OAuth App

In GitHub: **Settings → Developer settings → OAuth Apps → New OAuth App**.

- **Homepage URL**: your `PICI_PUBLIC_URL`, e.g. `https://pici.example.com`
- **Authorization callback URL**: `https://pici.example.com/auth/github/callback`

Copy the **Client ID** and generate a **Client secret**.

In **team mode**, the app requests the `read:org` scope to list your teams. If
your organization restricts third-party OAuth Apps, approve the app in
**Organization settings → Third-party access**. In **users-only mode**, no extra
scope is requested and no organization approval is needed.

### 2. Configure pici

| Variable                    | Description                                        |
| --------------------------- | -------------------------------------------------- |
| `PICI_AUTH_PROVIDER`        | `github` (default)                                 |
| `PICI_PUBLIC_URL`           | public base URL, used to build the OAuth callback  |
| `PICI_GITHUB_CLIENT_ID`     | OAuth App client id                                |
| `PICI_GITHUB_CLIENT_SECRET` | OAuth App client secret                            |
| `PICI_GITHUB_TEAMS`         | optional, teams allowed to sign in (`org/team`)    |
| `PICI_GITHUB_USERS`         | optional, GitHub logins allowed to sign in         |
| `PICI_SECRET_KEY`           | 32-byte key; also encrypts the stored OAuth tokens |
| `PICI_SESSION_TTL`          | optional browser session lifetime (default `720h`) |

Team instance:

```sh
export PICI_PUBLIC_URL=https://pici.example.com
export PICI_GITHUB_CLIENT_ID=Iv1.abc123
export PICI_GITHUB_CLIENT_SECRET=ghs_...
export PICI_GITHUB_TEAMS=acme/pici-users
export PICI_SECRET_KEY=$(openssl rand -hex 32)
pici
```

Personal instance, with no organization:

```sh
export PICI_PUBLIC_URL=https://pici.example.com
export PICI_GITHUB_CLIENT_ID=Iv1.abc123
export PICI_GITHUB_CLIENT_SECRET=ghs_...
export PICI_GITHUB_USERS=Thiht
export PICI_SECRET_KEY=$(openssl rand -hex 32)
pici
```

For local development, `PICI_PUBLIC_URL=http://localhost:8080` is accepted and
the callback URL is `http://localhost:8080/auth/github/callback`.

### 3. Configure access

#### Teams

Create a **flat** team in GitHub (for example `pici-users`) and add its members.
pici checks direct team membership only:

- a member of a child team is **not** considered a member of a configured
  parent team; list each team explicitly or use a flat team;
- outside collaborators explicitly added to the team are accepted;
- the organization and team slugs are matched case-insensitively.

Removing somebody from the team removes their pici access at the next access
check, without touching pici. Teams change without a pici restart.

#### Users

List GitHub logins in `PICI_GITHUB_USERS`:

```sh
export PICI_GITHUB_USERS=Thiht,other-login
```

This is the simplest option for a personal instance: no organization, no team,
no `read:org` scope. The list is matched case-insensitively. Changing the list
requires a pici restart, and renaming a GitHub login requires updating the list
(the login is refreshed on every sign-in).

## GitLab

GitLab.com and self-hosted GitLab instances are supported.

### 1. Create a GitLab OAuth application

In GitLab: **User settings → Applications** (or an instance/group application
managed by an administrator).

- **Name**: pici
- **Redirect URI**: `https://pici.example.com/auth/gitlab/callback`
- **Scopes**: `read_api` and `offline_access`
- **Confidential**: yes (pici exchanges the code server-side with the secret)

Copy the **Application ID** and **Secret**. For a self-hosted instance, also note
its base URL, for example `https://gitlab.example.com` (a relative installation
path such as `https://example.com/gitlab` is supported).

### 2. Configure pici

| Variable                    | Description                                        |
| --------------------------- | -------------------------------------------------- |
| `PICI_AUTH_PROVIDER`        | `gitlab`                                           |
| `PICI_PUBLIC_URL`           | public base URL, used to build the OAuth callback  |
| `PICI_GITLAB_URL`           | base URL (default `https://gitlab.com`)            |
| `PICI_GITLAB_CLIENT_ID`     | OAuth application id                               |
| `PICI_GITLAB_CLIENT_SECRET` | OAuth application secret                           |
| `PICI_GITLAB_GROUPS`        | optional, groups allowed to sign in (`group/path`) |
| `PICI_GITLAB_USERS`         | optional, GitLab usernames allowed to sign in      |
| `PICI_SECRET_KEY`           | 32-byte key; also encrypts the stored OAuth tokens |
| `PICI_SESSION_TTL`          | optional browser session lifetime (default `720h`) |

Group instance:

```sh
export PICI_AUTH_PROVIDER=gitlab
export PICI_PUBLIC_URL=https://pici.example.com
export PICI_GITLAB_URL=https://gitlab.example.com
export PICI_GITLAB_CLIENT_ID=0123456789abcdef
export PICI_GITLAB_CLIENT_SECRET=gloas-...
export PICI_GITLAB_GROUPS=acme/pici-users
export PICI_SECRET_KEY=$(openssl rand -hex 32)
pici
```

Personal instance, with no group:

```sh
export PICI_AUTH_PROVIDER=gitlab
export PICI_PUBLIC_URL=https://pici.example.com
export PICI_GITLAB_CLIENT_ID=0123456789abcdef
export PICI_GITLAB_CLIENT_SECRET=gloas-...
export PICI_GITLAB_USERS=alice
export PICI_SECRET_KEY=$(openssl rand -hex 32)
pici
```

With `PICI_GITLAB_URL=https://gitlab.com` (the default), the callback URL is
`https://pici.example.com/auth/gitlab/callback`. A self-hosted instance uses the
same pici callback URL: only the GitLab base URL changes.

### 3. Configure access

#### Groups

List full GitLab group paths in `PICI_GITLAB_GROUPS`:

```sh
export PICI_GITLAB_GROUPS=acme/pici-users,acme/platform/subgroup
```

Subgroups are supported and matched by their complete path. GitLab applies
inherited membership: a member of a parent group is considered a member of its
subgroups, and pici sees the groups GitLab reports for the user. Paths are
matched case-insensitively.

#### Users

List GitLab usernames in `PICI_GITLAB_USERS`:

```sh
export PICI_GITLAB_USERS=alice,bob
```

The list is matched case-insensitively. Changing the list requires a pici
restart, and renaming a GitLab username requires updating the list (the
username is refreshed on every sign-in).

## OpenID Connect

Any OpenID Connect 1.0 provider works: Keycloak, Authentik, Authelia, Okta,
Microsoft Entra ID, Google, Dex, Zitadel, Forgejo/Codeberg, and so on. pici
discovers the issuer configuration at startup, so a misconfigured issuer stops
the server immediately.

### 1. Create an OIDC application

In your provider, create a **confidential** OAuth2/OIDC application:

- **Redirect URI**: `https://pici.example.com/auth/oidc/callback`
- **Scopes**: `openid profile email offline_access`
- **Client secret**: enabled (pici exchanges the code server-side)

`offline_access` lets pici refresh the access token before each access check.
If your provider rejects that scope, override the list with
`PICI_OIDC_SCOPES`, but expect users to sign in again when their token expires.

### 2. Configure pici

| Variable                  | Description                                                   |
| ------------------------- | ------------------------------------------------------------- |
| `PICI_AUTH_PROVIDER`      | `oidc`                                                        |
| `PICI_PUBLIC_URL`         | public base URL, used to build the OAuth callback             |
| `PICI_OIDC_ISSUER`        | issuer URL, exactly as returned in OIDC discovery             |
| `PICI_OIDC_CLIENT_ID`     | OIDC client id                                                |
| `PICI_OIDC_CLIENT_SECRET` | OIDC client secret                                            |
| `PICI_OIDC_GROUPS`        | optional, groups allowed to sign in (from the `groups` claim) |
| `PICI_OIDC_USERS`         | optional, usernames or emails allowed to sign in              |
| `PICI_OIDC_SCOPES`        | optional, default `openid profile email offline_access`       |
| `PICI_OIDC_NAME`          | optional display name, e.g. `Keycloak` (default `SSO`)        |
| `PICI_SECRET_KEY`         | 32-byte key; also encrypts the stored OAuth tokens            |
| `PICI_SESSION_TTL`        | optional browser session lifetime (default `720h`)            |

Group instance:

```sh
export PICI_AUTH_PROVIDER=oidc
export PICI_PUBLIC_URL=https://pici.example.com
export PICI_OIDC_ISSUER=https://keycloak.example.com/realms/pici
export PICI_OIDC_CLIENT_ID=pici
export PICI_OIDC_CLIENT_SECRET=...
export PICI_OIDC_GROUPS=acme/pici-users
export PICI_OIDC_NAME=Keycloak
export PICI_SECRET_KEY=$(openssl rand -hex 32)
pici
```

Personal instance, with no group:

```sh
export PICI_AUTH_PROVIDER=oidc
export PICI_PUBLIC_URL=https://pici.example.com
export PICI_OIDC_ISSUER=https://authentik.example.com/application/o/pici/
export PICI_OIDC_CLIENT_ID=...
export PICI_OIDC_CLIENT_SECRET=...
export PICI_OIDC_USERS=alice@example.com
export PICI_SECRET_KEY=$(openssl rand -hex 32)
pici
```

The issuer must match the provider's discovery document exactly, including any
trailing slash. For Google, the issuer is `https://accounts.google.com`; for
Entra ID, use your tenant-specific issuer. pici sends `access_type=offline` so
Google can return a refresh token.

### 3. Configure access

pici reads the `groups` claim from the userinfo endpoint. Configure the groups
with `PICI_OIDC_GROUPS`:

```sh
export PICI_OIDC_GROUPS=acme/pici-users,platform
```

Group names are matched case-insensitively. Make sure your provider actually
includes the `groups` claim (some require an extra scope or claim mapper).

Otherwise, list usernames or emails in `PICI_OIDC_USERS`:

```sh
export PICI_OIDC_USERS=alice,bob@example.com
```

pici matches the `preferred_username` claim, or the `email` claim when the
provider does not return a username. The list is matched case-insensitively and
requires a pici restart to change.

Forgejo and Codeberg work as OIDC providers, but do not expose organization
teams in their userinfo response. Use `PICI_OIDC_USERS` with them, or add a
Forgejo-specific provider that queries its teams API.

## Sign in and create a token

Open your `PICI_PUBLIC_URL`, click **Sign in with GitHub** or **Sign in with
GitLab**, and authorize the OAuth application. In team/group mode the requested
scope is only what is needed to list memberships; in users-only mode no
membership call is made beyond verifying the token.

Once signed in, open **Account**:

- create a personal API token for the CLI, name it, and choose an expiration
  (90 days by default, or never);
- copy the token immediately: it is shown once and only its hash is stored;
- rename or revoke tokens, and revoke browser sessions from the same page.

```sh
export PICI_TOKEN=pici_pat_...
pici-cli projects
```

The CLI and API accept the token as `Authorization: Bearer <token>` or
`X-API-Token: <token>`. The JSON API never accepts the browser session cookie.

## Access checks and revocation

Access is checked when you sign in, then by a background job at most once every
24 hours per user. When a user leaves all configured teams/groups, is removed
from the users allowlist, or when the provider rejects their stored OAuth token
(for example because they revoked the application), pici:

- marks the user disabled;
- deletes their browser sessions;
- rejects their personal API tokens.

Signing in again with valid access reactivates the account. Network errors, rate
limits and provider 5xx responses are treated as transient: access is kept and
the check is retried later.

## Switching providers

Only one provider is active per instance. After switching `PICI_AUTH_PROVIDER`,
accounts and browser sessions created with the previous provider are rejected:
the background check disables them, and their sessions and API tokens stop
working immediately. Users must sign in again with the new provider.

## Sessions

Browser sessions are stored in the database and are valid for 30 days, sliding
on activity. Every mutation is protected by a CSRF token bound to the session.
You can revoke a single session or sign out everywhere from the Account page.

## OAuth security

pici uses the OAuth 2.0 authorization-code flow with:

- an unguessable `state`, stored in an `HttpOnly` cookie and compared in
  constant time;
- **PKCE (`S256`)**; the verifier is stored in an `HttpOnly`, single-use cookie
  and sent only to the token endpoint;
- a static redirect URI built from `PICI_PUBLIC_URL`, exchanged server-side
  with the client secret;
- a `/user` call after every exchange to revalidate the identity;
- **OIDC-specific**: a `nonce`, sent in the authorization request and compared
  in the ID token; the ID token is validated for signature, issuer, audience,
  expiry and `sub`; the userinfo `sub` must match the ID token;
- minimal scopes: `read:org` only in GitHub team mode, `read_api` for GitLab
  (plus `offline_access` so the token can be refreshed), and `openid profile
email offline_access` by default for OIDC.

GitHub OAuth Apps and GitLab OAuth applications do not return an ID token, so
the nonce and ID token checks only apply to the OIDC provider.

In the OAuth application settings, keep **wildcard matching disabled** so the
callback URL must match exactly.

Stored access tokens are renewed with their refresh token before every access
check. GitHub rotates refresh tokens on every use and pici persists each new
pair immediately; GitLab access tokens expire after two hours by default, so
`offline_access` is requested. OIDC providers that support `offline_access`
issue a refresh token; pici keeps the previous one when a later sign-in does not
return a new one. If a refresh is rejected (the user revoked the application, or
the credential expired for good), the account is disabled and the user must sign
in again. A provider that never issues refresh tokens cannot be revalidated once
its access token expires, so users must sign in again.

## Limitations

- One identity per user: there is no account linking across providers. Switching
  providers requires users to sign in again.
- No roles or per-project permissions: every authorized user has full access.
- The public status badge and webhooks remain unauthenticated (webhooks use
  their own secret).
