# Two presentation surfaces over shared use-case modules

pici serves the same use cases twice: `internal/web` renders HTML with htmx and `internal/handlers` serves the JSON API that `client/`, `pici-cli` and `docs/public/openapi.yaml` depend on. Each surface had grown its own copy of project resolution, execution reads, artifact download and cache listing, and they had started to drift. We decided to keep both surfaces and move the use cases into per-area modules under `internal/app` (with the on-disk layout owned by `internal/workspace`), leaving each transport responsible for routing, authentication, error-to-response mapping and rendering.

The obvious alternative — make the web UI a client of the JSON API — was rejected because the API deliberately refuses browser session cookies (`internal/handlers/middlewares/auth.go`) to avoid CSRF, so it would have forced cookie auth or a token hop onto the published surface, and because rendering a page would then depend on a wire shape we do not want to change for presentation reasons.

## Considered Options

- **Web UI calls the JSON API**: one in-process path, but drags cookie authentication into the API and couples HTML rendering to the frozen wire contract.
- **Merge both surfaces into one HTTP package with two adapters**: shares routing and error plumbing, but leaves one module serving two media types and keeps the use cases entangled with HTTP.
- **Generate one surface from the other**: removes the duplication by hiding the HTML-specific behaviour (flash messages, redirects, CSRF tokens) that the JSON surface must not have.

## Consequences

- A new use case is one module plus two thin adapters; the domain logic is written once.
- The published JSON shapes stay frozen: each app module returns domain values and typed errors, and the JSON adapter alone owns its response structs.
- App modules declare the store methods they call rather than taking the whole `stores.Store` interface, so they stay testable through their own interface.
- Transport-only concerns stay duplicated on purpose: CSRF, flash messages, redirects, SSE headers and status-code mapping.
