# ci.yml schema

pici publishes a [JSON Schema](https://json-schema.org) (draft 2020-12) for `ci.yml`, so editors can validate a workflow as you type and autocomplete keys and values. The schema mirrors the **strict** parser: unknown keys are rejected, and a step needs a `name` and either a `script` or a `run`.

## Where to get it

Every pici instance serves the schema:

```sh
curl https://pici.example.com/static/ci.schema.json
```

A copy is also published alongside this documentation at [`/ci.schema.json`](/ci.schema.json). An instance URL always matches the exact version you run.

## VS Code

Install the [YAML extension](https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml), then associate the schema with `ci.yml` using one of these methods.

**Inline `$schema` key** — pici accepts and ignores this key at runtime:

```yaml
$schema: https://pici.example.com/static/ci.schema.json
name: build
steps:
  - name: test
    run: go test ./...
```

**Modeline comment** — keeps the key out of the file:

```yaml
# yaml-language-server: $schema=https://pici.example.com/static/ci.schema.json
name: build
```

**`yaml.schemas` setting** — applies to every `ci.yml` in the repository:

```json
{
  "yaml.schemas": {
    "https://pici.example.com/static/ci.schema.json": ".ci/*/ci.yml"
  }
}
```

The setting can also point at a local copy of the schema (`./ci.schema.json`), which works offline.

## JetBrains IDEs

In **Settings → Languages & Frameworks → Schemas and DTDs → JSON Schema Mappings**, add a mapping with the schema URL or file and the `.ci/*/ci.yml` pattern. GoLand, IntelliJ IDEA, WebStorm, and others support this for YAML files.

## Other editors

Any editor backed by [yaml-language-server](https://github.com/redhat-developer/yaml-language-server) understands the same inline `$schema` key, modeline, and schema settings. Neovim users can wire it up through `nvim-lspconfig`.

## Validate from the CLI

Editors only check the schema. To validate a workflow exactly as pici does (globs, durations, trigger rules), use the CLI:

```sh
pici-cli validate .ci/build/ci.yml
```

The server exposes the same check at `POST /api/validate`.
