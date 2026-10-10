package models

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iskorotkov/avro/v2"
)

// TestEveryBodyFieldHasADefault is the guard on the outage in #583: v0.4.23
// added two fields to WarmupEmailAction, the derived schema carried no Avro
// default for them, and the registry refused the whole envelope under BACKWARD
// so nothing could be published in either direction for three hours.
//
// A field with a default lets a reader on the new schema decode data written
// before the field existed, which is the only thing that makes adding one safe.
func TestEveryBodyFieldHasADefault(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema avro.Schema
	}{
		{"WorkerEvent", WorkerEvent{}.Schema()},
		{"JobEvent", JobEvent{}.Schema()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := map[string]bool{}
			var walk func(s avro.Schema, path string)
			walk = func(s avro.Schema, path string) {
				switch v := s.(type) {
				case *avro.RecordSchema:
					if seen[v.FullName()] {
						return
					}
					seen[v.FullName()] = true
					for _, f := range v.Fields() {
						// The envelope's own two fields are the record itself,
						// not a body: neither can be "added later" without a
						// new subject, so neither needs a default.
						if v.FullName() == "warmbly.events."+tc.name {
							walk(f.Type(), path+"/"+f.Name())
							continue
						}
						if !f.HasDefault() {
							t.Errorf("%s%s/%s has no Avro default; adding a field without one is refused by the registry and stops every publish",
								path, v.FullName(), f.Name())
						}
						walk(f.Type(), path+"/"+f.Name())
					}
				case *avro.UnionSchema:
					for _, b := range v.Types() {
						walk(b, path)
					}
				case *avro.ArraySchema:
					walk(v.Items(), path)
				case *avro.MapSchema:
					walk(v.Values(), path)
				case *avro.RefSchema:
					// Already described where it was defined.
				}
			}
			walk(tc.schema, "")
		})
	}
}

// TestSchemaDocumentCarriesDefaultsAndParses guards the second half of #583
// and the fixed-default bug found in review of #586. The library's
// Schema.String() emits only a field's name and type, so registering that
// text throws every default away; its MarshalJSON keeps them but writes a
// fixed field's default as a byte array, which the spec forbids and no parser
// accepts. The registered document has to be one that carries defaults AND
// parses back.
func TestSchemaDocumentCarriesDefaultsAndParses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema avro.Schema
	}{
		{"WorkerEvent", WorkerEvent{}.Schema()},
		{"JobEvent", JobEvent{}.Schema()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := SchemaDocument(tc.schema)
			if err != nil {
				t.Fatalf("document: %v", err)
			}
			if !strings.Contains(string(doc), `"default"`) {
				t.Fatal("the document carries no defaults; the registry would refuse the next added field")
			}
			if _, err := avro.Parse(string(doc)); err != nil {
				t.Fatalf("the document we would register does not parse: %v", err)
			}
			if strings.Contains(tc.schema.String(), `"default"`) {
				t.Fatal("String() unexpectedly carries defaults now; recheck internal/infrastructure/codec/avro.go")
			}
		})
	}
}

// The uint64 sync cursor is the one fixed-typed field on the bus, so its
// default is the one that has to be the spec's string form rather than the
// byte array the library marshals.
func TestSchemaDocumentWritesFixedDefaultAsString(t *testing.T) {
	doc, err := SchemaDocument(JobEvent{}.Schema())
	if err != nil {
		t.Fatal(err)
	}
	var parsed any
	if err := json.Unmarshal(doc, &parsed); err != nil {
		t.Fatal(err)
	}
	var found bool
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case []any:
			for _, b := range v {
				walk(b)
			}
		case map[string]any:
			if v["type"] == "record" {
				for _, f := range v["fields"].([]any) {
					field := f.(map[string]any)
					if field["name"] == "mod_seq" {
						found = true
						s, ok := field["default"].(string)
						if !ok {
							t.Fatalf("mod_seq default is %T (%v), want a string of code points", field["default"], field["default"])
						}
						if s != strings.Repeat("\x00", 8) {
							t.Fatalf("mod_seq default = %q, want eight NUL code points", s)
						}
					}
					walk(field["type"])
				}
			} else {
				walk(v["type"])
			}
		}
	}
	walk(parsed)
	if !found {
		t.Fatal("mod_seq not found in the JobEvent document; the test no longer covers a fixed field")
	}

	// The raw marshal is what this exists to correct. If the library ever
	// fixes it, this stops failing and SchemaDocument can shrink.
	raw, _ := json.Marshal(JobEvent{}.Schema())
	if _, err := avro.Parse(string(raw)); err == nil {
		t.Log("note: json.Marshal(schema) now parses back on its own; the fixed-default rewrite may be removable")
	}
}

// TestZeroDefaultMatchesTheSchemaItDescribes checks the derived default is one
// the Avro library accepts for that schema, because NewField silently keeps a
// field defaultless if we hand it something invalid.
func TestZeroDefaultMatchesTheSchemaItDescribes(t *testing.T) {
	cases := []struct {
		name   string
		schema avro.Schema
		want   any
	}{
		{"string", avro.NewPrimitiveSchema(avro.String, nil), ""},
		{"bool", avro.NewPrimitiveSchema(avro.Boolean, nil), false},
		{"int", avro.NewPrimitiveSchema(avro.Int, nil), 0},
		{"long", avro.NewPrimitiveSchema(avro.Long, nil), int64(0)},
		{"double", avro.NewPrimitiveSchema(avro.Double, nil), float64(0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := zeroDefault(c.schema, 0)
			if !ok {
				t.Fatalf("no default derived for %s", c.name)
			}
			if got != c.want {
				t.Fatalf("default for %s = %#v, want %#v", c.name, got, c.want)
			}
			if _, err := avro.NewField("f", c.schema, avro.WithDefault(got)); err != nil {
				t.Fatalf("avro rejected the derived default for %s: %v", c.name, err)
			}
		})
	}

	// An optional field is a ["null", T] union, and a union default is written
	// against its first branch, so this has to be nil rather than T's zero.
	u, err := avro.NewUnionSchema([]avro.Schema{&avro.NullSchema{}, avro.NewPrimitiveSchema(avro.String, nil)})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := zeroDefault(u, 0)
	if !ok {
		t.Fatal("no default derived for an optional field")
	}
	if got != nil {
		t.Fatalf("optional default = %#v, want nil", got)
	}
	if _, err := avro.NewField("f", u, avro.WithDefault(got)); err != nil {
		t.Fatalf("avro rejected the optional default: %v", err)
	}
}
