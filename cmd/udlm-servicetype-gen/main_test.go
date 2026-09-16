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
      "metadata": {"type": "object", "properties": {"owner": {"type": "string"}}},
      "layout_ref": {"$ref": "../common-elements.schema.json#/$defs/Reference", "description": "own wording wins"},
      "zone": {"allOf": [{"$ref": "../data-reference.schema.json#/$defs/data_reference"}, {"required": ["reference_data_type"], "properties": {"reference_data_type": {"const": "network_zone"}}}]},
      "guest_os": {"description": "inline or reference", "oneOf": [{"type": "object", "required": ["type"], "properties": {"type": {"type": "string"}}}, {"$ref": "../common-elements.schema.json#/$defs/Reference"}]}
    },
    "oneOf": [{"required": ["size_class"]}, {"required": ["engine"]}]
  },
  "outputs": {
    "handle": {"type": "string", "description": "provider handle"},
    "addresses": {"type": "array", "description": "untyped list"},
    "secret": {"type": "string", "sensitive": true},
    "metadata": {"type": "string"},
    "engine": {"type": "string"}
  }
}`

const (
	fixtureCommonElements = `{"$defs": {"Reference": {"type": "string", "format": "udlm-ref-url", "description": "shared wording"}}}`
	fixtureDataReference  = `{"$defs": {"data_reference": {"type": "string", "format": "udlm-ref-url", "description": "a reference-data pointer"}}}`
)

func TestGenerateTransform(t *testing.T) {
	root := t.TempDir()
	rtDir := filepath.Join(root, "registry", "generated")
	if err := os.MkdirAll(rtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		filepath.Join(rtDir, "test.widget.json"):                       fixtureUDLM,
		filepath.Join(root, "registry", "common-elements.schema.json"): fixtureCommonElements,
		filepath.Join(root, "registry", "data-reference.schema.json"):  fixtureDataReference,
	} {
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
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

	// Spec-level oneOf sizing constraint is folded like anyOf.
	if !strings.Contains(spec["description"].(string), "Sizing constraint (UDLM oneOf): provide exactly one of [size_class] OR [engine]") {
		t.Error("description should carry the oneOf sizing note")
	}

	// $ref into a sibling registry schema resolves to its scalar form; the
	// property's own description wins over the shared definition's.
	layout := props["layout_ref"].(map[string]any)
	if layout["type"] != "string" || layout["format"] != "udlm-ref-url" {
		t.Errorf("layout_ref should resolve to string/udlm-ref-url, got %v", layout)
	}
	if layout["description"] != "own wording wins" {
		t.Errorf("layout_ref should keep its own description, got %v", layout["description"])
	}

	// allOf[$ref data_reference, {const}] -> the typed schema with the constraint noted.
	zone := props["zone"].(map[string]any)
	if zone["type"] != "string" || zone["format"] != "udlm-ref-url" {
		t.Errorf("zone should resolve to string/udlm-ref-url, got %v", zone)
	}
	if !strings.Contains(zone["description"].(string), "reference_data_type = network_zone") {
		t.Errorf("zone should note its const constraint, got %v", zone["description"])
	}
	if _, present := zone["allOf"]; present {
		t.Error("zone should not emit allOf")
	}

	// oneOf[inline object, $ref] -> OpenAPI oneOf with the object hoisted to <Name>Inline.
	guest := props["guest_os"].(map[string]any)
	branches, ok := guest["oneOf"].([]any)
	if !ok || len(branches) != 2 {
		t.Fatalf("guest_os should be a two-branch oneOf, got %v", guest)
	}
	if branches[0].(map[string]any)["$ref"] != "#/components/schemas/GuestOsInline" {
		t.Errorf("first branch should $ref GuestOsInline, got %v", branches[0])
	}
	if _, ok := schemas["GuestOsInline"]; !ok {
		t.Error("inline object branch should be hoisted to GuestOsInline")
	}
	if branches[1].(map[string]any)["type"] != "string" {
		t.Errorf("second branch should resolve to string, got %v", branches[1])
	}

	// Typed realized outputs land beside the spec fields, readOnly, never required.
	handle := props["handle"].(map[string]any)
	if handle["readOnly"] != true || handle["type"] != "string" {
		t.Errorf("output handle should be a readOnly string, got %v", handle)
	}
	if !strings.Contains(handle["description"].(string), "UDLM realized output") {
		t.Errorf("output description should say it is a realized output, got %v", handle["description"])
	}
	addresses := props["addresses"].(map[string]any)
	if addresses["items"].(map[string]any)["type"] != "string" {
		t.Errorf("untyped output array should default items to string, got %v", addresses)
	}
	if !strings.Contains(props["secret"].(map[string]any)["description"].(string), "Sensitive") {
		t.Error("sensitive output should say so in its description")
	}
	for _, r := range body["required"].([]any) {
		if r == "handle" || r == "addresses" || r == "secret" {
			t.Errorf("outputs must never be required, found %v", r)
		}
	}
	// Collisions: an output named like a spec field or an envelope key is skipped and noted.
	if !strings.Contains(spec["description"].(string), "Outputs skipped because a spec field or envelope key already uses the name (rename upstream in UDLM): engine, metadata.") {
		t.Errorf("description should list skipped outputs, got %q", spec["description"])
	}

	// Scalar facets pass through.
	if !strings.Contains(text, "DO NOT hand-edit") {
		t.Error("generated file should carry the do-not-hand-edit banner")
	}
}

func TestResolveRefRejectsUnknownShapes(t *testing.T) {
	g := &generator{registry: t.TempDir(), files: map[string]*yaml.Node{}}
	for _, ref := range []string{"#/components/schemas/X", "common.yaml#/x", "../missing.json#/$defs/Y", "../a.json"} {
		if _, err := g.resolveRef(ref); err == nil {
			t.Errorf("resolveRef(%q) should fail", ref)
		}
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
