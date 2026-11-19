package records

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sync"

	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/dcm-project/control-plane/internal/udlm/schema"
)

var (
	compileOnce sync.Once
	compiled    map[string]*jsonschema.Schema
	compileErr  error
)

// compiledSchema returns the compiled schema for one vendored schema id. All of
// them are compiled once, with every sibling schema registered under its `$id`
// so relative `$ref`s resolve.
func compiledSchema(id string) (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		c := jsonschema.NewCompiler()
		c.UseRegexpEngine(patternEngine)
		// Every vendored file is registered under the path it has in the registry
		// (a facet lives under facets/ and refers up to ../common-elements).
		err := fs.WalkDir(schema.Files, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			raw, err := schema.Files.ReadFile(path)
			if err != nil {
				return err
			}
			var doc any
			if err := json.Unmarshal(raw, &doc); err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			if err := c.AddResource(schema.BaseURL+path, doc); err != nil {
				return fmt.Errorf("add %s: %w", path, err)
			}
			return nil
		})
		if err != nil {
			compileErr = err
			return
		}
		compiled = map[string]*jsonschema.Schema{}
		for _, id := range []string{schema.StateRecord, schema.EntityView, schema.ContextFacet} {
			sch, err := c.Compile(id)
			if err != nil {
				compileErr = fmt.Errorf("compile %s: %w", id, err)
				return
			}
			compiled[id] = sch
		}
	})
	if compileErr != nil {
		return nil, compileErr
	}
	return compiled[id], nil
}

// patternEngine compiles schema patterns with Go's RE2 engine and falls back to
// an ECMAScript-compatible engine for the few registry patterns RE2 refuses
// (the ISO 8601 duration pattern uses lookahead). JSON Schema patterns are
// ECMAScript by definition, so the fallback is the conforming path.
func patternEngine(pattern string) (jsonschema.Regexp, error) {
	if re, err := regexp.Compile(pattern); err == nil {
		return re, nil
	}
	re, err := regexp2.Compile(pattern, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	return ecmaRegexp{re: re}, nil
}

type ecmaRegexp struct{ re *regexp2.Regexp }

func (r ecmaRegexp) MatchString(s string) bool {
	ok, err := r.re.MatchString(s)
	return err == nil && ok
}

func (r ecmaRegexp) String() string { return r.re.String() }

// Validate checks a record against the registry's state-record schema. The
// record is round-tripped through JSON first so Go-typed values (ints, structs)
// validate as the JSON they will be stored as.
func Validate(record map[string]any) error {
	return validateAgainst(schema.StateRecord, record)
}

// ValidateView checks a computed entity view against the registry's
// entity-view schema.
func ValidateView(view map[string]any) error {
	return validateAgainst(schema.EntityView, view)
}

// ValidateContext checks a udlm_context facet against the registry's facet schema.
func ValidateContext(facet map[string]any) error {
	return validateAgainst(schema.ContextFacet, facet)
}

func validateAgainst(id string, doc map[string]any) error {
	sch, err := compiledSchema(id)
	if err != nil {
		return fmt.Errorf("udlm schema: %w", err)
	}
	v, err := roundTrip(doc)
	if err != nil {
		return err
	}
	return sch.Validate(v)
}

// roundTrip returns the JSON-typed form of v (map[string]any / []any / float64 /
// string / bool / nil), which is what both the validator and the canonicalizer
// operate on.
func roundTrip(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
