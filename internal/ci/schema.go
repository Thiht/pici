package ci

import _ "embed"

// Schema is the JSON Schema describing the `ci.yml` format. It is written and
// maintained by hand, embedded so the server can serve it (see
// GET /static/ci.schema.json), and guarded against drift from the structs by
// a test.
//
//go:embed ci.schema.json
var Schema []byte
