package records

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Algorithm names the integrity chain algorithm the registry's checker expects
// (registry/tools/integrity_chain.py ALGORITHM).
const Algorithm = "sha256-jcs"

// maxSafeInteger is the I-JSON safe range the registry's canonicalizer enforces.
const maxSafeInteger = 1 << 53

// jcsBytes renders a JSON value in the registry's RFC 8785 subset
// (registry/tools/generate_pin_manifest.py jcs_bytes): keys sorted, no
// whitespace, integers as integers, and the same refusals — non-string or
// non-ASCII keys, NaN/Inf, integers beyond ±2^53, floats that would need
// exponent form. Two canonicalizers that disagree about a record's bytes would
// disagree about its head, so this one refuses exactly what the Python one does.
func jcsBytes(v any) ([]byte, error) {
	var sb strings.Builder
	if err := jcsWrite(&sb, v); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}

func jcsWrite(sb *strings.Builder, v any) error {
	switch x := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		if x {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case string:
		jcsString(sb, x)
	case float64:
		return jcsNumber(sb, x)
	case int:
		return jcsNumber(sb, float64(x))
	case int64:
		return jcsNumber(sb, float64(x))
	case []any:
		sb.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				sb.WriteByte(',')
			}
			if err := jcsWrite(sb, item); err != nil {
				return err
			}
		}
		sb.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			for _, r := range k {
				if r > utf8.RuneSelf {
					return fmt.Errorf("non-ASCII key %q: this minimal JCS sorts ASCII keys only", k)
				}
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			jcsString(sb, k)
			sb.WriteByte(':')
			if err := jcsWrite(sb, x[k]); err != nil {
				return err
			}
		}
		sb.WriteByte('}')
	default:
		return fmt.Errorf("cannot canonicalize %T", v)
	}
	return nil
}

func jcsNumber(sb *strings.Builder, x float64) error {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return errors.New("NaN/Infinity cannot be canonicalized")
	}
	if x == math.Trunc(x) {
		if math.Abs(x) > maxSafeInteger {
			return fmt.Errorf("integer %v outside ±2^53 (I-JSON safe range)", x)
		}
		sb.WriteString(strconv.FormatInt(int64(x), 10))
		return nil
	}
	s := strconv.FormatFloat(x, 'g', -1, 64)
	if strings.ContainsAny(s, "eE") {
		return fmt.Errorf("unsafe float %s: exponent-form serialization diverges between canonicalizers", s)
	}
	sb.WriteString(s)
	return nil
}

// jcsString escapes the way Python's json.dumps(ensure_ascii=False) does: the
// two-character forms for \" \\ \b \f \n \r \t, \u00XX (lower-case hex) for the
// other control characters, everything else as raw UTF-8.
func jcsString(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(sb, `\u%04x`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
}
