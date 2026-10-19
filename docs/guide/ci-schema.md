# ci.yml schema

pici publishes a [JSON Schema](https://json-schema.org) (draft 2020-12) for
`ci.yml`. Editors use it to validate a workflow as you type and to autocomplete
keys and values. The schema mirrors the **strict** parser: unknown keys are
rejected, a step needs a `name` and either a `script` or a `run`.

## Where to get it

Every pici instance serves the schema as a static asset:

```sh
curl https://pici.example.com/static/ci.schema.json
```

A copy is also published alongside this documentation at
[`/ci.schema.json`](/ci.schema.json). Pick whichever URL you prefer — an
instance URL always matches the exact version you run.

## VS Code

Install the [YAML extension](https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml),
then associate the schema with `ci.yml` in one of three ways.

**Inline `$schema` key** — the schema also allows this key, so you can put it at
the top of the file (pici ignores it at runtime):

```yaml
$schema: https://pici.example.com/static/ci.schema.json
name: build
steps:
  - name: test
    run: go test ./...
```

**Modeline comment** — same idea, in a comment, if you would rather not add a
key to the file:

```yaml
# yaml-language-server: $schema=https://pici.example.com/static/ci.schema.json
name: build
```

**`yaml.schemas` setting** — associate the schema with every `ci.yml` of the
repository, without touching the files. In `settings.json`:

```json
{
  "yaml.schemas": {
    "https://pici.example.com/static/ci.schema.json": ".ci/*/ci.yml"
  }
}
```

The setting can also point at a local copy of the schema
(`./ci.schema.json`), which works offline.

## JetBrains IDEs

In **Settings → Languages & Frameworks → Schemas and DTDs → JSON Schema
Mappings**, add a new mapping with the schema URL or file, and the
`.ci/*/ci.yml` file pattern. GoLand, IntelliJ IDEA, WebStorm, … all support this
for YAML files.

## Other editors

Any editor backed by
[yaml-language-server](https://github.com/redhat-developer/yaml-language-server)
(the engine behind the VS Code extension) understands the same inline `$schema`
key, modeline and schema-settings mechanisms. Neovim users can wire it up
through `nvim-lspconfig`.

## Validate from the CLI

Editors only check the schema; to validate a workflow exactly as pici does
(matching globs, duration parsing, trigger rules), use the CLI:

```sh
pici-cli validate .ci/build/ci.yml
```

The server exposes the same check at `POST /api/validate`.
