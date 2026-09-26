# AGENTS.md

## Code style

- No tiny helper/abstraction functions for trivial things: inline them instead (e.g. row-scanning, error-to-response mapping, resource resolution, enum/time conversions, normalizers). Only extract a helper when it does real, non-trivial work.
- Prefer direct pointer scanning (`*int64`, `*string`, `*time.Time`) over `sql.NullXxx` wrappers.
- Type everything correctly: `uuid.UUID` for ids, generated enums (go-enum) for enum fields, `time.Time`/`*time.Time` for timestamps.
