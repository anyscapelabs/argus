package Shared

import (
	"encoding/json"
	"strings"
	"testing"
)

// The shape that broke a real run: an MCP tool whose schema bundles its types in
// $defs and points at them with $ref, next to a generator that adds
// x-google-enum-descriptions. Gemini answered with
// "Unknown name \"$defs\" … Cannot find field" and the request died with every
// other tool still in it.
const bundledSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "$defs": {
    "Filter": {"type": "object", "properties": {"name": {"type": "string"}}},
    "Cycle":  {"type": "object", "properties": {"back": {"$ref": "#/$defs/Cycle"}}}
  },
  "properties": {
    "where":  {"$ref": "#/$defs/Filter", "description": "what to match"},
    "loops":  {"$ref": "#/$defs/Cycle"},
    "items":  {"type": "array", "items": {"$ref": "#/$defs/Filter"}, "minItems": 1},
    "mode":   {"type": "string", "enum": ["fast", "slow"], "x-google-enum-descriptions": ["quick", "patient"]},
    "either": {"oneOf": [{"type": "string"}, {"type": "integer"}]},
    "mixed":  {"type": ["string", "null"]},
    "exact":  {"const": "only-this"},
    "extra":  {"type": "object", "additionalProperties": {"type": "string"}}
  },
  "required": ["where"],
  "additionalProperties": false
}`

func decoded(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("cleanSchema produced unreadable JSON: %v\n%s", err, raw)
	}
	return m
}

func TestCleanSchemaRemovesWhatGeminiHasNoFieldFor(t *testing.T) {
	out := CleanSchemaForGemini(json.RawMessage(bundledSchema))
	for _, forbidden := range []string{"$defs", "$ref", "$schema", "additionalProperties", "x-google-enum-descriptions", "minItems"} {
		if strings.Contains(string(out), forbidden) {
			t.Errorf("%s survived into the request:\n%s", forbidden, out)
		}
	}
}

func TestCleanSchemaKeepsWhatGeminiReads(t *testing.T) {
	m := decoded(t, CleanSchemaForGemini(json.RawMessage(bundledSchema)))
	props, ok := m["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the object lost its properties: %v", m["properties"])
	}
	if m["type"] != "object" {
		t.Errorf("type = %v, want object", m["type"])
	}
	if req, ok := m["required"].([]any); !ok || len(req) != 1 || req[0] != "where" {
		t.Errorf("required = %v, want [where]", m["required"])
	}

	// A resolved $ref must arrive as the schema it pointed at, not a pointer —
	// and the description written beside the reference must survive.
	where, ok := props["where"].(map[string]any)
	if !ok {
		t.Fatal("property where vanished")
	}
	if where["type"] != "object" || where["description"] != "what to match" {
		t.Errorf("the ref resolved to %v", where)
	}
	if _, has := where["$ref"]; has {
		t.Error("the pointer is still in the resolved slot")
	}
	filter, ok := where["properties"].(map[string]any)["name"].(map[string]any)
	if !ok || filter["type"] != "string" {
		t.Errorf("the target's own properties did not come across: %v", where["properties"])
	}

	// Nested refs (the second half of the real error) resolve too.
	items, ok := props["items"].(map[string]any)
	if !ok {
		t.Fatal("items vanished")
	}
	sub, ok := items["items"].(map[string]any)
	if !ok || sub["type"] != "object" {
		t.Fatalf("array items did not resolve: %v", items["items"])
	}
	if _, has := sub["$ref"]; has {
		t.Errorf("a nested $ref reached the wire: %v", sub)
	}

	mode, _ := props["mode"].(map[string]any)
	if e, ok := mode["enum"].([]any); !ok || len(e) != 2 {
		t.Errorf("enum lost: %v", mode)
	}
	if _, has := mode["x-google-enum-descriptions"]; has {
		t.Error("the vendor extension survived")
	}
	if s, ok := m["properties"].(map[string]any); !ok || len(s) != len(props) {
		t.Error("properties were rewritten rather than filtered")
	}
}

func TestCleanSchemaTranslatesTheKeywordsItCannotExpress(t *testing.T) {
	m := decoded(t, CleanSchemaForGemini(json.RawMessage(bundledSchema)))
	props := m["properties"].(map[string]any)

	// oneOf is not a Gemini field; anyOf is the same union.
	either, _ := props["either"].(map[string]any)
	if _, has := either["oneOf"]; has {
		t.Error("oneOf reached Gemini, which has no such field")
	}
	branches, ok := either["anyOf"].([]any)
	if !ok || len(branches) != 2 {
		t.Fatalf("oneOf did not become anyOf: %v", either)
	}
	for _, b := range branches {
		if _, has := b.(map[string]any)["type"]; !has {
			t.Errorf("a union branch lost its type: %v", b)
		}
	}

	// A type union including null becomes one type plus nullable.
	mixed, _ := props["mixed"].(map[string]any)
	if mixed["type"] != "string" || mixed["nullable"] != true {
		t.Errorf(`type:["string","null"] became %v, want string + nullable`, mixed)
	}

	// const has no field either; enum says the same thing.
	exact, _ := props["exact"].(map[string]any)
	if e, ok := exact["enum"].([]any); !ok || len(e) != 1 || e[0] != "only-this" {
		t.Errorf("const did not become enum: %v", exact)
	}
	if _, has := exact["const"]; has {
		t.Error("const is not a Gemini field")
	}

	// An object keeps a properties map even when every entry was dropped, so the
	// schema stays self-describing.
	extra, _ := props["extra"].(map[string]any)
	if _, ok := extra["properties"].(map[string]any); !ok {
		t.Errorf("an object with no usable properties: %v", extra)
	}
}

func TestCleanSchemaSurvivesARefCycle(t *testing.T) {
	// A definition that points back at itself must not hang the build or leak a
	// pointer into the request.
	out := CleanSchemaForGemini(json.RawMessage(bundledSchema))
	if strings.Count(string(out), `"$ref"`) != 0 {
		t.Errorf("a cyclic ref leaked: %s", out)
	}
	m := decoded(t, out)
	loops, _ := m["properties"].(map[string]any)["loops"].(map[string]any)
	if loops["type"] != "object" {
		t.Errorf("the cyclic slot should open as an object, got %v", loops)
	}
}

func TestCleanSchemaHandlesTheEdges(t *testing.T) {
	cases := map[string]string{
		"absent":        ``,
		"blank":         `   `,
		"not json":      `{"type": "object",`,
		"boolean":       `true`,
		"array root":    `[{"type":"string"}]`,
		"remote ref":    `{"$ref": "https://example.com/schema#/Filter"}`,
		"broken ref":    `{"$ref": "#/$defs/Missing"}`,
		"no properties": `{"type":"object"}`,
	}
	for name, in := range cases {
		out := CleanSchemaForGemini(json.RawMessage(in))
		if !json.Valid(out) {
			t.Errorf("%s produced invalid JSON: %s", name, out)
			continue
		}
		for _, forbidden := range []string{"$ref", "$defs", "definitions", "additionalProperties"} {
			if strings.Contains(string(out), forbidden) {
				t.Errorf("%s leaked %s: %s", name, forbidden, out)
			}
		}
	}
}
