# Embedded Web UI — Design

## Context

pici is a minimal self-hosted CI system. It exposes a JSON HTTP API (`internal/handlers`),
a Go API client (`client/`) and a Cobra CLI (`cmd/pici-cli`), but no UI (README lists
`UI` under "Not yet implemented"). This spec adds a web UI embedded in the server binary.

The design decisions below were validated during brainstorming:

- **Scope (v1):** full management — projects, variables/secrets, executions, trigger/cancel/rebuild,
  per-step and full logs, artifacts. No real-time streaming in v1.
- **Auth:** a login form POSTs the API token; the server sets an `HttpOnly`, `SameSite=Lax`
  cookie. `middlewares.Auth` accepts either the cookie or the existing header, so native
  forms/links work (and without JS).
- **Build:** no Node. Server-rendered Go templates + htmx + Tailwind (standalone CLI),
  assets committed and embedded with `go:embed`.
- **Design system:** daisyUI (Tailwind v4 plugin) via npm, build-time only.
- **Accessibility:** prefer native HTML elements; enhance with htmx; forms work without JS.

## Goals

- Serve a functional management UI from the existing `pici` binary at `/`.
- Keep the JSON API and CLI behavior **unchanged**.
- Keep the binary and build free of Node/npm.
- Provide an accessible, progressively-enhanced UI (native forms, tables, dialogs).

## Non-Goals (v1)

- SSE log streaming (API already has `/api/executions/{id}/logs/stream`; a later iteration may use it).
- Multi-user auth, roles, server-side sessions (the cookie only carries the shared API token).
- Fine-grained pagination (keep the API's `?limit=`), i18n, theming beyond light/dark.

## Architecture

A new package `internal/web`, mirroring `internal/handlers`:

- Owns its own HTTP handlers that use `stores.Store` and `*ci.Runner` directly. It does **not**
  round-trip through the JSON API.
- Renders `html/template` templates.
- Reuses existing helpers where applicable (`resolveProject` from `internal/handlers` will be
  duplicated/moved as needed; enums; `stores` types).
- Exposes `func (h *Handler) Routes() http.Handler`.

`cmd/pici/main.go` mounts the web routes and applies the existing auth middleware.
The API mux and CLI are untouched.

### Rendering model

- Go `html/template`, split into `layout`, `pages`, and `partials` (htmx fragments).
- All templates embedded via `go:embed` (`internal/web/templates/`).
- Static assets (`app.css`, `htmx.min.js`) embedded via `go:embed` (`internal/web/static/`)
  and served under `/static/`.
- Progressive enhancement: every form is a real `<form method="post" action="...">`.
  Without JS it performs POST → 303 redirect → full page + flash message (PRG).
  htmx (`hx-post`, `hx-target`, etc.) replaces only the targeted fragment.
- Native elements first: `<table>`, `<form>`, `<button>`, `<dialog>`, `<details>`,
  `<input>`, `<select>`, `<label>`, `<fieldset>`, `<progress>`, `<time>`. ARIA only when
  the native element is insufficient.

### Routing

UI served at `/`, assets at `/static/`. The existing `middlewares.Auth` covers all routes.

| Route | Purpose |
|---|---|
| `GET /` | project list (`<table>`) + "New project" (`<dialog>`) |
| `GET /projects/new` | create form |
| `POST /projects` | create |
| `GET /projects/{id}` | detail: info, `.ci` workflows, project variables/secrets, executions |
| `GET /projects/{id}/edit` | edit form |
| `POST /projects/{id}` | update |
| `POST /projects/{id}/delete` | delete |
| `GET /projects/{id}/variables` | variables fragment/page |
| `POST /projects/{id}/variables` | set variable |
| `POST /projects/{id}/variables/{key}/delete` | delete variable |
| `GET /variables` | global variables |
| `POST /variables` | set global variable |
| `POST /variables/{key}/delete` | delete global variable |
| `POST /projects/{id}/executions` | trigger run (workflow + ref) |
| `GET /executions/{id}` | execution detail: status, steps table, per-step logs, full logs, artifacts |
| `POST /executions/{id}/cancel` | cancel |
| `POST /executions/{id}/rebuild` | rebuild |
| `GET /login` | token entry form |
| `POST /login` | validate token, set cookie, redirect to `/` |
| `POST /logout` | clear cookie |

Lists (projects, variables, executions) refresh via htmx after mutations. Logs are fetched
in full on open with a manual refresh button (no streaming in v1).

### Auth

- No API token configured → UI is open.
- Token configured → `GET /login` renders a native `<form>`; `POST /login` validates the token
  against `cfg.APIToken`, sets an `HttpOnly`, `SameSite=Lax`, `Path=/` cookie, and redirects to `/`.
  `POST /logout` clears the cookie.
- `middlewares.Auth` is extended (minimally) to accept the cookie **in addition to** the existing
  `X-API-Token` / `Authorization: Bearer` header. The `/health`, webhook and `/static/` allowlist
  is unchanged; `/login` is added to it (the login form must be reachable without a cookie).
- CSRF: state-changing web forms include a CSRF token derived from the session; `SameSite=Lax`
  blocks cross-site POSTs as a baseline. The JSON API (header-only) is unaffected.
- On a 401 for an htmx request, render a redirect to `/login`; for a full navigation, `/login`
  is served.

## Design system

daisyUI components, with the light/dark themes (dark via `prefers-color-scheme`).
A limited `include` list keeps the generated CSS small. Only a couple of custom
classes live in `@layer components` (the terminal-style `.logs` block and the
skip link).

Accessibility rules: visible focus, associated labels, native `required`/`minlength`
before server validation, status/errors announced with `role="status"`/`aria-live`.

Status → badge mapping: `success` green, `failed` red, `running` blue,
`pending` neutral, `canceled`/`skipped` muted.

## Build & tooling

- Generated `app.css` and htmx are **committed** to `internal/web/static/` and embedded.
- `internal/web/package.json` (Tailwind, `@tailwindcss/cli`, daisyUI, htmx) manages the
  build-time dependencies; `package-lock.json` is committed.
- Taskfile additions:
  - `ui:install` — `npm install`.
  - `ui:build` — copies htmx from `node_modules` and builds `app.css` with the Tailwind CLI.
- No Node at runtime; Node/npm is only needed to rebuild the assets. `task check`
  (fmt/lint/test) is unchanged.

## Data flow

1. Browser requests a page → web handler reads `stores.Store` / `*ci.Runner` → renders template.
2. Mutation (form submit) → web handler validates → store/runner call → 303 redirect (full page)
   or fragment + `HX-Redirect`/`HX-Trigger` (htmx) → list/row refreshes.
3. Errors render an `alert` fragment (htmx) or a flash on the redirected page (no JS).

## Error handling

- Validation errors: 400 with the form re-rendered and field errors rendered inline.
- Not found: 404 page (web) / `render.Error` (API, unchanged).
- Store errors: 500 page/fragment with a generic message; details logged server-side.
- 401: login redirect (htmx) or login page (full navigation).

## Testing

- `httptest`-based tests for web handlers: status codes, 303 redirects, expected elements
  (via `data-testid`), htmx fragment responses.
- A test asserting all embedded templates parse.
- A test for the status → badge class mapping.
- Standard `go test ./...`; no new test framework.

## Open questions

None. Scope is fixed for v1; streaming logs, sessions, and pagination are explicitly deferred.
