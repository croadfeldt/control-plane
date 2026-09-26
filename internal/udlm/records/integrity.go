package records

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Head computes a record's `integrity.head`: SHA-256 over the canonical form of
// {"previous": <previous head or null>, "record": <record minus integrity>}
// (registry/tools/integrity_chain.py head_for). A nil previous marks a chain root.
func Head(record map[string]any, previous *string) (string, error) {
	body := make(map[string]any, len(record))
	for k, v := range record {
		if k != "integrity" {
			body[k] = v
		}
	}
	var prev any
	if previous != nil {
		prev = *previous
	}
	payload := map[string]any{"previous": prev, "record": body}
	b, err := jcsBytes(payload)
	if err != nil {
		return "", fmt.Errorf("record is not canonicalizable: %w", err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Seal sets the record's integrity block from its current bytes and the
// previous head. It mutates record.
func Seal(record map[string]any, previous *string) error {
	head, err := Head(record, previous)
	if err != nil {
		return err
	}
	var prev any
	if previous != nil {
		prev = *previous
	}
	record["integrity"] = map[string]any{
		"algorithm": Algorithm,
		"head":      head,
		"previous":  prev,
	}
	return nil
}

// Verify recomputes the head from the record's bytes and its claimed previous.
func Verify(record map[string]any) error {
	integ, ok := record["integrity"].(map[string]any)
	if !ok {
		return fmt.Errorf("record carries no integrity block")
	}
	if integ["algorithm"] != Algorithm {
		return fmt.Errorf("algorithm %v is not %s", integ["algorithm"], Algorithm)
	}
	var previous *string
	if p, ok := integ["previous"].(string); ok {
		previous = &p
	}
	expect, err := Head(record, previous)
	if err != nil {
		return err
	}
	if expect != integ["head"] {
		return fmt.Errorf("head %v does not recompute (expected %s)", integ["head"], expect)
	}
	return nil
}
