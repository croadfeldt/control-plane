// Command udlm-servicetype-gen translates a UDLM registry resource-type into a
// DCM control-plane service-type OpenAPI spec (spec.yaml).
//
// The UDLM registry (github.com/croadfeldt/udlm registry/resource-types/*.{json,yaml})
// is the single source of truth for the estate data model. The control-plane's
// service-types are an OpenAPI projection of that model, code-generated to Go via
// oapi-codegen. This tool keeps the projection honest: given a UDLM resource-type,
// it emits the matching servicetypes/<slug>/spec.yaml. After running it, regenerate
// the Go types with the existing oapi-codegen command (see the per-type README).
//
// It is deliberately a mechanical JSON-Schema -> OpenAPI-3.0.x translation:
//   - spec.type/required/properties           -> the service-type schema body
//   - the body is wrapped in allOf[CommonFields, {...}] so every service-type
//     inherits service_type/metadata/provider_hints
//   - nested object properties are hoisted to named component schemas (so
//     oapi-codegen emits real Go types), arrays hoist their item schema
//   - anyOf sizing constraints are folded into the schema description (kept
//     human-readable, no awkward union types) rather than emitted structurally
//   - scalar facets (pattern/enum/minimum/maximum/format/example) pass through
//
// Non-portable provider extensions are NOT promoted to portable properties; they
// belong under CommonFields.provider_hints (noted in the schema description).
//
// Usage:
//
//	go run ./cmd/udlm-servicetype-gen -udlm ../udlm -out api/catalog/v1alpha1/servicetypes -type vm
//	go run ./cmd/udlm-servicetype-gen -udlm ../udlm -out api/catalog/v1alpha1/servicetypes -type all
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// mapping ties a control-plane service-type slug to its UDLM source resource-type
// and the OpenAPI schema base name (schema is <base>Spec). Only service-types with a
// clean UDLM counterpart are listed; three_tier_app_demo is a composite with no
// single UDLM resource-type and is intentionally absent.
type mapping struct {
	slug      string // servicetypes/<slug>
	schema    string // <schema>Spec, e.g. VM -> VMSpec
	udlmFile  string // registry/resource-types/<file>
	title     string // info.title
	shortDesc string // one-line info.description lead
}

var mappings = []mapping{
	{"vm", "VM", "compute.virtual-machine.json", "DCM Virtual Machine Specification", "virtual machine"},
	{"container", "Container", "compute.container.yaml", "DCM Container Specification", "container workload"},
	{"database", "Database", "data.database.json", "DCM Database Specification", "managed database"},
	{"cluster", "Cluster", "compute.cluster.json", "DCM Cluster Specification", "Kubernetes cluster"},
	{"storage", "Storage", "storage.volume.json", "DCM Storage Specification", "storage volume"},
}

func main() {
	udlmDir := flag.String("udlm", "../udlm", "path to a croadfeldt/udlm checkout (contains registry/resource-types)")
	outDir := flag.String("out", "api/catalog/v1alpha1/servicetypes", "servicetypes output directory")
	which := flag.String("type", "all", "service-type slug to generate, or 'all'")
	flag.Parse()

	var todo []mapping
	for _, m := range mappings {
		if *which == "all" || *which == m.slug {
			todo = append(todo, m)
		}
	}
	if len(todo) == 0 {
		fmt.Fprintf(os.Stderr, "unknown -type %q; known: %s, all\n", *which, slugs())
		os.Exit(2)
	}

	for _, m := range todo {
		if err := generate(m, *udlmDir, *outDir); err != nil {
			fmt.Fprintf(os.Stderr, "generate %s: %v\n", m.slug, err)
			os.Exit(1)
		}
		fmt.Printf("generated %s from %s\n", filepath.Join(*outDir, m.slug, "spec.yaml"), m.udlmFile)
	}
	fmt.Fprintln(os.Stderr, "\nNext: regenerate Go types for each changed service-type, e.g.")
	fmt.Fprintln(os.Stderr, "  cd "+*outDir+"/<slug> && oapi-codegen -config spec.gen.cfg \\")
	fmt.Fprintln(os.Stderr, "    -import-mapping '../common.yaml:github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes' spec.yaml > types.gen.go")
}

func slugs() string {
	s := make([]string, len(mappings))
	for i, m := range mappings {
		s[i] = m.slug
	}
	return strings.Join(s, ", ")
}

// generate reads the UDLM resource-type for m and writes servicetypes/<slug>/spec.yaml.
func generate(m mapping, udlmDir, outDir string) error {
	src := filepath.Join(udlmDir, "registry", "resource-types", m.udlmFile)
	raw, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}

	// JSON is valid YAML flow syntax, so yaml.v3 parses both .json and .yaml while
	// preserving mapping key order (via yaml.Node) — which keeps the emitted spec
	// stable and human-readable.
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", src, err)
	}
	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: expected a mapping at the document root", src)
	}

	spec := mapGet(root, "spec")
	if spec == nil || spec.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: missing object 'spec'", src)
	}
	resourceType := scalarValue(mapGet(root, "resource_type"))
	udlmVersion := scalarValue(mapGet(root, "version"))
	metaDesc := scalarValue(mapGet(mapGet(root, "metadata"), "description"))

	g := &generator{schemaBase: m.schema, hoisted: map[string]*yaml.Node{}}
	rootSchema := g.buildRootSchema(spec, m, resourceType, udlmVersion, metaDesc)

	// Assemble components.schemas: the root <base>Spec first, then hoisted schemas
	// in first-seen order for a stable diff.
	schemas := mappingNode()
	appendKV(schemas, m.schema+"Spec", rootSchema)
	for _, name := range g.order {
		appendKV(schemas, name, g.hoisted[name])
	}

	out := buildOpenAPIDoc(m, schemas)
	data, err := marshalYAML(out)
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}

	dir := filepath.Join(outDir, m.slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "spec.yaml"), data, 0o644)
}

type generator struct {
	schemaBase string
	hoisted    map[string]*yaml.Node
	order      []string
}

// buildRootSchema turns the UDLM spec object into the <base>Spec schema:
// allOf[CommonFields, {required, properties, additionalProperties:true}].
func (g *generator) buildRootSchema(spec *yaml.Node, m mapping, resourceType, udlmVersion, metaDesc string) *yaml.Node {
	desc := strings.TrimSpace(metaDesc)
	prov := fmt.Sprintf("Provider-agnostic %s specification, generated from the UDLM %s (v%s) resource type — the UDLM registry is the source of truth. Non-portable provider settings go under provider_hints (CommonFields). Regenerate with cmd/udlm-servicetype-gen; do not hand-edit.",
		m.shortDesc, resourceType, udlmVersion)
	if desc != "" {
		desc = desc + "\n\n" + prov
	} else {
		desc = prov
	}
	if note := anyOfNote(spec); note != "" {
		desc = desc + "\n\n" + note
	}

	// The servicetype envelope (CommonFields) reserves these top-level keys. A UDLM
	// type that reuses one would silently override the envelope field (e.g. a UDLM
	// `metadata` shadowing the required ServiceMetadata), so we skip it here and
	// surface it — the UDLM type should rename the property upstream. This guard
	// applies only to the root body, not to nested/hoisted objects.
	props := mapGet(spec, "properties")
	topProps, skipped := g.buildTopProperties(props)
	if len(skipped) > 0 {
		desc = desc + "\n\nEnvelope-reserved collisions skipped (owned by CommonFields, rename upstream in UDLM): " + strings.Join(skipped, ", ") + "."
		fmt.Fprintf(os.Stderr, "  warning[%s]: skipped %d UDLM propert(y|ies) colliding with CommonFields: %s\n", m.slug, len(skipped), strings.Join(skipped, ", "))
	}

	inner := mappingNode()
	appendKV(inner, "type", scalarNode("object"))
	if req := filteredRequired(mapGet(spec, "required"), skipped); req != nil {
		appendKV(inner, "required", req)
	}
	appendKV(inner, "properties", topProps)
	appendKV(inner, "additionalProperties", boolNode(true))

	schema := mappingNode()
	appendKV(schema, "type", scalarNode("object"))
	appendKV(schema, "description", literalScalar(desc))
	allOf := sequenceNode()
	commonRef := mappingNode()
	appendKV(commonRef, "$ref", scalarNode("../common.yaml#/components/schemas/CommonFields"))
	allOf.Content = append(allOf.Content, commonRef, inner)
	appendKV(schema, "allOf", allOf)
	return schema
}

// reservedTopKeys are owned by CommonFields; a UDLM root property using one of
// these names would collide with the servicetype envelope.
var reservedTopKeys = map[string]bool{
	"id": true, "status": true, "status_message": true, "path": true,
	"create_time": true, "update_time": true,
	"service_type": true, "metadata": true, "provider_hints": true,
}

// buildTopProperties translates the root property map, skipping any name that
// collides with a CommonFields key. It returns the translated properties and the
// sorted list of skipped names.
func (g *generator) buildTopProperties(props *yaml.Node) (*yaml.Node, []string) {
	out := mappingNode()
	var skipped []string
	if props == nil || props.Kind != yaml.MappingNode {
		return out, nil
	}
	for i := 0; i+1 < len(props.Content); i += 2 {
		name := props.Content[i].Value
		if reservedTopKeys[name] {
			skipped = append(skipped, name)
			continue
		}
		appendKV(out, name, g.buildProperty(name, props.Content[i+1]))
	}
	sort.Strings(skipped)
	return out, skipped
}

// filteredRequired clones a required sequence minus any skipped name, returning
// nil when nothing remains.
func filteredRequired(req *yaml.Node, skipped []string) *yaml.Node {
	if req == nil || req.Kind != yaml.SequenceNode {
		return nil
	}
	skip := map[string]bool{}
	for _, s := range skipped {
		skip[s] = true
	}
	out := sequenceNode()
	for _, c := range req.Content {
		if !skip[c.Value] {
			out.Content = append(out.Content, scalarNode(c.Value))
		}
	}
	if len(out.Content) == 0 {
		return nil
	}
	return out
}

// buildProperties translates a UDLM properties map, hoisting nested object and
// array-item schemas to named component schemas.
func (g *generator) buildProperties(props *yaml.Node) *yaml.Node {
	out := mappingNode()
	if props == nil || props.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(props.Content); i += 2 {
		name := props.Content[i].Value
		appendKV(out, name, g.buildProperty(name, props.Content[i+1]))
	}
	return out
}

// buildProperty translates one property schema. Objects with their own properties
// and arrays of such objects are hoisted to named schemas and replaced with a $ref.
func (g *generator) buildProperty(name string, sch *yaml.Node) *yaml.Node {
	typ := scalarValue(mapGet(sch, "type"))

	// Nested object with properties -> hoist to a named schema.
	if typ == "object" && mapGet(sch, "properties") != nil {
		ref := g.hoist(pascal(name), sch)
		return ref
	}

	// Array whose items are an object with properties -> hoist the item schema.
	if typ == "array" {
		items := mapGet(sch, "items")
		out := mappingNode()
		appendKV(out, "type", scalarNode("array"))
		if d := scalarValue(mapGet(sch, "description")); d != "" {
			appendKV(out, "description", literalScalar(d))
		}
		if items != nil && scalarValue(mapGet(items, "type")) == "object" && mapGet(items, "properties") != nil {
			appendKV(out, "items", g.hoist(pascal(singular(name)), items))
		} else if items != nil {
			appendKV(out, "items", g.buildProperty(singular(name), items))
		}
		return out
	}

	// Scalar (or object without properties): copy the supported facets through.
	return copyScalarSchema(sch)
}

// hoist registers a named component schema for a nested object and returns a $ref
// to it. The object's own nested objects/arrays are translated recursively so
// deep structures produce a flat set of named schemas.
func (g *generator) hoist(name string, obj *yaml.Node) *yaml.Node {
	if _, ok := g.hoisted[name]; !ok {
		g.hoisted[name] = nil // reserve the name before recursing (cycle guard)
		g.order = append(g.order, name)

		schema := mappingNode()
		appendKV(schema, "type", scalarNode("object"))
		if d := scalarValue(mapGet(obj, "description")); d != "" {
			appendKV(schema, "description", literalScalar(d))
		}
		if req := mapGet(obj, "required"); req != nil && req.Kind == yaml.SequenceNode && len(req.Content) > 0 {
			appendKV(schema, "required", cloneSeqStrings(req))
		}
		appendKV(schema, "properties", g.buildProperties(mapGet(obj, "properties")))
		appendKV(schema, "additionalProperties", boolNode(true))
		g.hoisted[name] = schema
	}
	ref := mappingNode()
	appendKV(ref, "$ref", scalarNode("#/components/schemas/"+name))
	return ref
}

// anyOfNote renders a spec-level anyOf sizing constraint as a human-readable
// sentence instead of an OpenAPI union (which oapi-codegen models awkwardly).
func anyOfNote(spec *yaml.Node) string {
	any := mapGet(spec, "anyOf")
	if any == nil || any.Kind != yaml.SequenceNode || len(any.Content) == 0 {
		return ""
	}
	var groups []string
	for _, branch := range any.Content {
		req := mapGet(branch, "required")
		if req == nil {
			continue
		}
		var fields []string
		for _, f := range req.Content {
			fields = append(fields, f.Value)
		}
		if len(fields) > 0 {
			groups = append(groups, "["+strings.Join(fields, " + ")+"]")
		}
	}
	if len(groups) == 0 {
		return ""
	}
	return "Sizing constraint (UDLM anyOf): provide exactly one of " + strings.Join(groups, " OR ") + "."
}

// copyScalarSchema copies the OpenAPI-relevant facets of a scalar schema in a
// stable key order, always leading with type (inferred as string when the UDLM
// property omits it, e.g. an enum-only field).
func copyScalarSchema(sch *yaml.Node) *yaml.Node {
	out := mappingNode()
	typ := "string"
	if sch != nil {
		if t := mapGet(sch, "type"); t != nil && t.Value != "" {
			typ = t.Value
		}
	}
	appendKV(out, "type", scalarNode(typ))
	if sch == nil {
		return out
	}
	for _, key := range []string{"format", "description", "pattern", "enum", "minimum", "maximum", "minLength", "maxLength", "default", "example"} {
		if v := mapGet(sch, key); v != nil {
			if key == "description" {
				appendKV(out, key, literalScalar(v.Value))
			} else {
				appendKV(out, key, deepCopy(v))
			}
		}
	}
	return out
}

// buildOpenAPIDoc wraps the schemas in a minimal, valid OpenAPI 3.0.4 document.
func buildOpenAPIDoc(m mapping, schemas *yaml.Node) *yaml.Node {
	doc := mappingNode()
	appendKV(doc, "openapi", scalarNode("3.0.4"))

	info := mappingNode()
	appendKV(info, "title", scalarNode(m.title))
	appendKV(info, "description", literalScalar(
		"Generated from the UDLM registry by cmd/udlm-servicetype-gen — DO NOT hand-edit.\n"+
			"Provider-agnostic "+m.shortDesc+" specification; the UDLM resource type is the source of truth."))
	appendKV(info, "version", scalarNode("v1alpha1"))
	lic := mappingNode()
	appendKV(lic, "name", scalarNode("Apache 2.0"))
	appendKV(lic, "url", scalarNode("https://www.apache.org/licenses/LICENSE-2.0.html"))
	appendKV(info, "license", lic)
	appendKV(doc, "info", info)

	appendKV(doc, "paths", mappingNode())

	components := mappingNode()
	appendKV(components, "schemas", schemas)
	appendKV(doc, "components", components)
	return doc
}

// ---- yaml.Node helpers ----------------------------------------------------

func mapGet(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func scalarValue(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	return n.Value
}

func mappingNode() *yaml.Node  { return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"} }
func sequenceNode() *yaml.Node { return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"} }

func scalarNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func boolNode(b bool) *yaml.Node {
	v := "false"
	if b {
		v = "true"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: v}
}

// literalScalar renders multi-line text as a YAML literal block for readability.
func literalScalar(v string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
	if strings.Contains(v, "\n") {
		n.Style = yaml.LiteralStyle
	}
	return n
}

func appendKV(m *yaml.Node, key string, val *yaml.Node) {
	m.Content = append(m.Content, scalarNode(key), val)
}

func cloneSeqStrings(seq *yaml.Node) *yaml.Node {
	out := sequenceNode()
	for _, c := range seq.Content {
		out.Content = append(out.Content, scalarNode(c.Value))
	}
	return out
}

// deepCopy clones a node but drops the source Style so the encoder chooses clean,
// minimal quoting (JSON-sourced scalars otherwise carry a forced double-quote
// style, e.g. type: "string").
func deepCopy(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := &yaml.Node{Kind: n.Kind, Tag: n.Tag, Value: n.Value}
	for _, ch := range n.Content {
		c.Content = append(c.Content, deepCopy(ch))
	}
	return c
}

func marshalYAML(n *yaml.Node) ([]byte, error) {
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}

// ---- naming ---------------------------------------------------------------

// pascal converts snake/kebab case to PascalCase (guest_os -> GuestOs).
func pascal(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' || r == '.' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// singular is a naive de-pluralizer for array item names (disks -> disk,
// node_pools -> node_pool, networks -> network).
func singular(s string) string {
	switch {
	case strings.HasSuffix(s, "ies"):
		return strings.TrimSuffix(s, "ies") + "y"
	case strings.HasSuffix(s, "ses"):
		return strings.TrimSuffix(s, "es")
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss"):
		return strings.TrimSuffix(s, "s")
	default:
		return s
	}
}

var _ = sort.Strings // reserved for future deterministic ordering knobs
