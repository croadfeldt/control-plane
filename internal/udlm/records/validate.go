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
	compiled    *jsonschema.Schema
	compileErr  error
)

// compiledSchema compiles the vendored state-record schema once, with every
// sibling schema registered under its `$id` so relative `$ref`s resolve.
func compiledSchema() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		c := jsonschema.NewCompiler()
		c.UseRegexpEngine(patternEngine)
		entries, err := fs.ReadDir(schema.Files, ".")
		if err != nil {
			compileErr = err
			return
		}
		for _, e := range entries {
			raw, err := schema.Files.ReadFile(e.Name())
			if err != nil {
				compileErr = err
				return
			}
			var doc any
			if err := json.Unmarshal(raw, &doc); err != nil {
				compileErr = fmt.Errorf("parse %s: %w", e.Name(), err)
				return
			}
			if err := c.AddResource(schema.BaseURL+e.Name(), doc); err != nil {
				compileErr = fmt.Errorf("add %s: %w", e.Name(), err)
				return
			}
		}
		compiled, compileErr = c.Compile(schema.StateRecord)
	})
	return compiled, compileErr
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
	sch, err := compiledSchema()
	if err != nil {
		return fmt.Errorf("state-record schema: %w", err)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
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
