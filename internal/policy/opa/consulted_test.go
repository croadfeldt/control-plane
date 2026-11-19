package opa

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseConsulted(t *testing.T) {
	const id = "6c1f5a0e-2b7d-4c3a-9e41-0d8f7a6b5c21"
	const head = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	good := map[string]any{"consulted": []any{
		map[string]any{"uuid": id, "version": "3", "head": head},
		map[string]any{"uuid": id, "version": "4"},
	}}
	got, err := ParseConsulted(good)
	if err != nil {
		t.Fatalf("a well-formed list was refused: %v", err)
	}
	if want := []Citation{{UUID: id, Version: "3", Head: head}, {UUID: id, Version: "4"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, err := ParseConsulted(map[string]any{"rejected": false}); err != nil || got != nil {
		t.Errorf("an absent list is none: got %v, %v", got, err)
	}

	// A citation is the record's uuid, version and head, and nothing else. Which
	// provider served the record, and when, are not the citation's to carry.
	for name, tc := range map[string]struct {
		consulted any
		says      string
	}{
		"extra key":     {[]any{map[string]any{"uuid": id, "version": "3", "provider": "dcm/providers/mock-cmdb"}}, `carries "provider"`},
		"not a list":    {map[string]any{"uuid": id}, "must be a list"},
		"not an object": {[]any{id}, "must be an object"},
		"no version":    {[]any{map[string]any{"uuid": id}}, "no version"},
		"bad uuid":      {[]any{map[string]any{"uuid": "app-01", "version": "3"}}, "not a v4 uuid"},
		"bad head":      {[]any{map[string]any{"uuid": id, "version": "3", "head": "abc"}}, "not sha256"},
		"number":        {[]any{map[string]any{"uuid": id, "version": 3}}, "must be a string"},
	} {
		_, err := ParseConsulted(map[string]any{"consulted": tc.consulted})
		if err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: got %v, want an error saying %q", name, err, tc.says)
		}
	}
}

func TestParsePolicyDecisionReadsReason(t *testing.T) {
	d := ParsePolicyDecision(map[string]any{"rejected": false, "reason": "impact tier high"})
	if d.Reason != "impact tier high" {
		t.Errorf("reason = %q", d.Reason)
	}
}
