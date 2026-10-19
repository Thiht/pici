# Authentication

pici uses **GitHub OAuth** to sign people in, and **personal API tokens** for
the CLI and scripts. There is no local password.

Access is restricted to:

- members of one or more **GitHub teams** (`PICI_GITHUB_TEAMS`), or
- an explicit **allowlist of GitHub logins** (`PICI_GITHUB_USERS`).

Set at least one of the two. A personal instance with no GitHub organization can
use the users allowlist only; a team instance can use teams, users, or both.

> Any authorized user is a full pici administrator: they can read every project
> secret and run arbitrary code in privileged containers. Treat access as root
> access to the pici host.

## 1. Create a GitHub OAuth App

In GitHub: **Settings → Developer settings → OAuth Apps → New OAuth App**.

- **Homepage URL**: your `PICI_PUBLIC_URL`, e.g. `https://pici.example.com`
- **Authorization callback URL**: `https://pici.example.com/auth/github/callback`

Copy the **Client ID** and generate a **Client secret**.

In **team mode**, the app requests the `read:org` scope to list your teams. If
your organization restricts third-party OAuth Apps, approve the app in
**Organization settings → Third-party access**. In **users-only mode**, no extra
scope is requested and no organization approval is needed.

## 2. Configure pici

| Variable                    | Description                                        |
| --------------------------- | -------------------------------------------------- |
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

## 3. Configure access

### Teams

Create a **flat** team in GitHub (for example `pici-users`) and add its members.
pici checks direct team membership only:

- a member of a child team is **not** considered a member of a configured
  parent team; list each team explicitly or use a flat team;
- outside collaborators explicitly added to the team are accepted;
- the organization and team slugs are matched case-insensitively.

Removing somebody from the team removes their pici access at the next access
check, without touching pici. Teams change without a pici restart.

### Users

List GitHub logins in `PICI_GITHUB_USERS`:

```sh
export PICI_GITHUB_USERS=Thiht,other-login
```

This is the simplest option for a personal instance: no organization, no team,
no `read:org` scope. The list is matched case-insensitively. Changing the list
requires a pici restart, and renaming a GitHub login requires updating the list
(the login is refreshed on every sign-in).

## 4. Sign in and create a token

Open `https://pici.example.com`, click **Sign in with GitHub**, and authorize the
OAuth App. In team mode the requested scope is `read:org` only, used to list your
teams; in users-only mode no extra scope is requested.

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
24 hours per user. When a user leaves all configured teams, is removed from the
users allowlist, or when GitHub rejects their stored OAuth token (for example
because they revoked the OAuth App), pici:

- marks the user disabled;
- deletes their browser sessions;
- rejects their personal API tokens.

Signing in again with valid access reactivates the account. Network errors, rate
limits and GitHub 5xx responses are treated as transient: access is kept and the
check is retried later.

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
- a `/user` call after every exchange to revalidate the GitHub identity;
- minimal scopes: `read:org` only in team mode, none in users-only mode.

There is no OIDC `nonce`: GitHub OAuth Apps do not return an ID token. If an
OIDC provider (Google, GitLab OIDC, ...) is added later, its ID token must be
validated for signature, issuer, audience and nonce.

In the GitHub OAuth App settings, keep **wildcard matching disabled** so the
callback URL must match exactly.

The stored GitHub access token is renewed with its refresh token before every
access check. Both GitHub OAuth App modes are supported, so the opt-in
**expiring user tokens** option can be left as you prefer: with it enabled,
pici refreshes the credential every time it verifies the user's access; with it
disabled, the access token never expires and no refresh happens. GitHub rotates
refresh tokens on every use, and pici persists each new pair immediately. If a
refresh is rejected (the user revoked the OAuth App, or the credential expired
for good), the account is disabled and the user must sign in again.

## Limitations

- GitHub.com only (GitHub Enterprise Server is not supported yet).
- One identity per user: there is no account linking across providers yet.
- No roles or per-project permissions: every authorized user has full access.
- The public status badge and webhooks remain unauthenticated (webhooks use
  their own secret).
