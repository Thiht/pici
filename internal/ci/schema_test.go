package ci

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSchemaMatchesStructs(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(Schema, &doc); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	defs, ok := doc["$defs"].(map[string]any)
	if !ok {
		t.Fatal("schema has no $defs object")
	}
	step, ok := defs["step"].(map[string]any)
	if !ok {
		t.Fatal("schema has no $defs.step object")
	}

	cases := []struct {
		name string
		obj  map[string]any
		typ  reflect.Type
	}{
		{"Config", doc, reflect.TypeFor[Config]()},
		{"Step", step, reflect.TypeFor[Step]()},
	}

	for _, c := range cases {
		props, ok := c.obj["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: schema has no properties", c.name)
		}

		fields := map[string]bool{}
		for field := range c.typ.Fields() {
			name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
			if name == "" || name == "-" {
				continue
			}
			fields[name] = true
		}

		for field := range fields {
			if _, ok := props[field]; !ok {
				t.Errorf("%s: yaml field %q is missing from the schema", c.name, field)
			}
		}
		for prop := range props {
			if !fields[prop] {
				t.Errorf("%s: schema property %q has no yaml field", c.name, prop)
			}
		}
	}
}
