# Run docker steps in a per-execution sandbox

A step with `docker: true` used to receive the socket of the daemon pici itself runs on. Everything it created — containers, networks, volumes, images, build cache — landed on pici's daemon, and pici only removed the step container: leftovers were the step's own responsibility, so a forgotten `trap` or a `kill -9` of pici left them behind for good. The same socket also let a step bind-mount any path of the pici container, including other projects' workspaces. We decided that a workflow declaring at least one `docker: true` step runs entirely inside a throwaway `docker:dind` sandbox created for the execution and removed when it ends, so Docker leftovers are contained by construction instead of being tracked and deleted. The sandbox mounts only the execution's repo at its usual path and the project's cache volumes at a fixed path; step containers run inside it, so a service started by a step stays reachable by the following steps and disappears with the execution. The workflow image is still built on pici's daemon — keeping its build cache and the cache view — then pushed to a pici-owned local registry the sandbox pulls from, and that registry also proxies Docker Hub so already-pulled images are not fetched again.

## Considered Options

- **Label-injecting proxy in front of the daemon**: keeps one daemon and every cache, and cleans containers, networks, volumes and labelled images by ownership, but the build cache cannot be attributed to a step and a step can still mount pici's filesystem.
- **Diff sweep before and after each step**: no proxy, but parallel steps and concurrent instances make the delta ambiguous, so it either deletes another step's resources or misses its own.
- **One sandbox per step**: strongest isolation and per-step cleanup, but every step pays a daemon start and a full image pull, and an image built by a step is no longer visible to the next.
- **Keep the shared daemon and document `trap`**: zero code, but leaves the guarantee exactly where it is today.

## Consequences

- Cleanup is structural: containers, networks, anonymous volumes, images built by steps and their build cache die with the sandbox. Only caches and artifacts outlive an execution.
- The guarantee covers step failures, timeouts, cancellations and a pici crash: sandboxes are labelled with their attempt, the periodic collector removes those whose attempt is terminal or gone in the database, and a requeued attempt destroys any sandbox still carrying its id before creating a new one.
- A step can no longer mount arbitrary pici paths: bind mounts resolve inside the sandbox, which only holds the repo and the cache volumes.
- `docker: true` is declared per step but applies per execution: one such step puts the whole workflow in the sandbox, so the other steps of that workflow run there too.
- Images built by a step stay visible to the other steps of the same execution, no longer to later executions.
- An execution whose sandbox cannot start fails instead of falling back to the shared daemon, since a degraded run would silently lose the guarantee.
- The local registry is an unauthenticated internal service reachable from the sandbox, so a step could push to another project's tag; steps are trusted code, which puts that outside the threat model.
- Only Docker Hub is cached; other registries are pulled directly by the sandbox. The registry cache is bounded by size and appears in the cache page with a purge action.
- Each execution that uses Docker costs an extra daemon start, an extra privileged container, and pulling the workflow image through the registry. Workflows without a `docker: true` step are unchanged and get no sandbox.
