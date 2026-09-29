package registry

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/c0dn/ocws/internal/model"
)

// schemaProps collects every property name declared anywhere in a JSON Schema.
func schemaProps(t *testing.T, path string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if props, ok := x["properties"].(map[string]any); ok {
				for k := range props {
					out[k] = true
				}
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(doc)
	return out
}

// TestSchemasCoverStructs keeps the published JSON Schemas in step with the
// Go types that parse templates.
func TestSchemasCoverStructs(t *testing.T) {
	cases := map[string][]any{
		"../../docs/public/schema/profiles.schema.json": {Profile{}, Detect{}, Scaffold{}, ScaffoldFile{}, BasePack{}, CapabilityGroup{}, CapabilityPack{}, Recommend{}},
		"../../docs/public/schema/pack.schema.json":     {PackManifest{}, PackTarget{}, model.FilePlan{}},
	}
	undocumented := map[string]bool{"capability": true} // internal pack metadata
	for path, types := range cases {
		props := schemaProps(t, path)
		for _, v := range types {
			rt := reflect.TypeOf(v)
			for i := 0; i < rt.NumField(); i++ {
				name := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
				if name == "" || name == "-" || undocumented[name] {
					continue
				}
				if !props[name] {
					t.Errorf("%s.%s (%q) is missing from %s", rt.Name(), rt.Field(i).Name, name, path)
				}
			}
		}
	}
}
