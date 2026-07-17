package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// fixtureUDLM is a minimal UDLM resource-type exercising the transform: a scalar
// property, a nested object (hoisted), an array of objects (item hoisted), an
// anyOf sizing constraint, and a reserved-name collision (metadata).
const fixtureUDLM = `{
  "resource_type": "Test.Widget",
  "version": "1.2.3",
  "metadata": {"description": "A test widget."},
  "spec": {
    "type": "object",
    "required": ["shape", "metadata"],
    "anyOf": [{"required": ["shape"]}, {"required": ["size_class"]}],
    "properties": {
      "shape": {"type": "string", "description": "Widget shape."},
      "size_class": {"type": "string"},
      "engine": {"type": "object", "required": ["kind"], "properties": {"kind": {"type": "string"}}},
      "ports": {"type": "array", "items": {"type": "object", "required": ["num"], "properties": {"num": {"type": "integer", "minimum": 1}}}},
      "metadata": {"type": "object", "properties": {"owner": {"type": "string"}}}
    }
  }
}`

func TestGenerateTransform(t *testing.T) {
	root := t.TempDir()
	rtDir := filepath.Join(root, "registry", "resource-types")
	if err := os.MkdirAll(rtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rtDir, "test.widget.json"), []byte(fixtureUDLM), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")

	m := mapping{slug: "widget", schema: "Widget", udlmFile: "test.widget.json", title: "Test Widget", shortDesc: "widget"}
	if err := generate(m, root, out); err != nil {
		t.Fatalf("generate: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(out, "widget", "spec.yaml"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	text := string(raw)

	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("output is not valid YAML: %v", err)
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)

	// Root schema exists and wraps CommonFields via allOf.
	spec, ok := schemas["WidgetSpec"].(map[string]any)
	if !ok {
		t.Fatal("missing WidgetSpec schema")
	}
	allOf, ok := spec["allOf"].([]any)
	if !ok || len(allOf) != 2 {
		t.Fatalf("WidgetSpec should allOf[CommonFields, body], got %v", spec["allOf"])
	}
	if ref := allOf[0].(map[string]any)["$ref"]; ref != "../common.yaml#/components/schemas/CommonFields" {
		t.Errorf("first allOf branch should $ref CommonFields, got %v", ref)
	}
	body := allOf[1].(map[string]any)
	props := body["properties"].(map[string]any)

	// Reserved-name guard: metadata (owned by CommonFields) is dropped from both
	// properties and required, and surfaced in the description.
	if _, present := props["metadata"]; present {
		t.Error("reserved 'metadata' should be skipped from portable properties")
	}
	for _, r := range body["required"].([]any) {
		if r == "metadata" {
			t.Error("reserved 'metadata' should be filtered out of required")
		}
	}
	if !strings.Contains(spec["description"].(string), "Envelope-reserved collisions skipped") {
		t.Error("description should note the skipped collision")
	}

	// anyOf sizing constraint is folded into the description, not emitted structurally.
	if _, present := body["anyOf"]; present {
		t.Error("anyOf should be folded into the description, not emitted")
	}
	if !strings.Contains(spec["description"].(string), "Sizing constraint (UDLM anyOf)") {
		t.Error("description should carry the anyOf sizing note")
	}

	// Nested object hoisted to a named schema and referenced.
	if props["engine"].(map[string]any)["$ref"] != "#/components/schemas/Engine" {
		t.Errorf("engine should $ref a hoisted Engine schema, got %v", props["engine"])
	}
	if _, ok := schemas["Engine"]; !ok {
		t.Error("nested object 'engine' should be hoisted to schema 'Engine'")
	}

	// Array of objects: the item schema is hoisted (singularized) and referenced.
	ports := props["ports"].(map[string]any)
	if ports["type"] != "array" {
		t.Errorf("ports should be an array, got %v", ports["type"])
	}
	if ports["items"].(map[string]any)["$ref"] != "#/components/schemas/Port" {
		t.Errorf("ports items should $ref a hoisted Port schema, got %v", ports["items"])
	}

	// Scalar facets pass through.
	if !strings.Contains(text, "DO NOT hand-edit") {
		t.Error("generated file should carry the do-not-hand-edit banner")
	}
}

func TestPascalAndSingular(t *testing.T) {
	cases := []struct{ in, pascal, singular string }{
		{"guest_os", "GuestOs", "guest_o"}, // singular is naive; only used for array item names
		{"node_pools", "NodePools", "node_pool"},
		{"disks", "Disks", "disk"},
		{"networks", "Networks", "network"},
		{"policies", "Policies", "policy"},
	}
	for _, c := range cases {
		if got := pascal(c.in); got != c.pascal {
			t.Errorf("pascal(%q)=%q want %q", c.in, got, c.pascal)
		}
		if got := singular(c.in); got != c.singular {
			t.Errorf("singular(%q)=%q want %q", c.in, got, c.singular)
		}
	}
}
