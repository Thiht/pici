# AGENTS.md

## Code style

- No tiny helper/abstraction functions for trivial things: inline them instead (e.g. row-scanning, error-to-response mapping, resource resolution, enum/time conversions, normalizers). Only extract a helper when it does real, non-trivial work.
- Prefer direct pointer scanning (`*int64`, `*string`, `*time.Time`) over `sql.NullXxx` wrappers.
- Type everything correctly: `uuid.UUID` for ids, generated enums (go-enum) for enum fields, `time.Time`/`*time.Time` for timestamps.

## Agent skills

### Issue tracker

Issues and specs live as GitHub issues in `Thiht/pici`, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Domain docs

Single-context: `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.

### Grilling

`grilling`, `grill-me`, and `grill-with-docs` ask each round through the `question` tool — one call per round, every frontier question in it, recommendations first and labelled "(Recommended)" — so answers arrive as selections rather than prose.

The `question` tool renders its text verbatim: no Markdown, and line breaks collapse. Keep the header short and the question a single plain-text sentence; put each candidate answer in an option label of a few words, with its rationale in the option description (also plain text).
