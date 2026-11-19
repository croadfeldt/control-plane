package records

import (
	"sort"

	"github.com/dcm-project/control-plane/internal/udlm/schema"
)

// Who set which field of a requested record, told policy by policy.
//
// Before this file every field that differed between the intent and the
// requested record was put down to one source, `dcm/placement`. Policy
// evaluation now reports what each policy did, so the writer walks the spec
// through the same steps the request took: the intent, the spec placement
// handed to policy, the spec as each policy left it, and the spec that was
// sent. Each step's changes belong to whoever made that step.
//
// A policy that read an outside record to decide (an information provider's
// answer) stays the source of what it set: it decided, and it wrote the patch.
// The record it read is cited in the udlm_context facet's `consulted` list.

const (
	// placementID is the source of a change no single policy made: the binding
	// placement does before policy runs, and anything left over after it.
	placementID = "dcm/placement"
	// policyRefPrefix makes a policy's id in the control plane's store a UDLM
	// reference, the same way an agent is `dcm/agents/<name>`.
	policyRefPrefix = "dcm/policies/"
	// contextProducer is the facet's `_producer`: what emitted it.
	contextProducer = "https://github.com/croadfeldt/control-plane"
)

// policyStep is one policy's part, with its spec already under the class's names.
type policyStep struct {
	id        string
	fields    map[string]any // nil when it patched nothing, or its spec could not be typed
	reason    string
	consulted []Citation
}

// policyTrail is the path a spec took through policy evaluation. A nil trail
// means the writer was told nothing about it.
type policyTrail struct {
	input map[string]any // the policy input under the class's names; nil when unknown
	steps []policyStep
}

// policyTrail types the request's policy input and each policy's spec. A spec
// that cannot be typed is left out of the walk, and its changes fall to the
// last step, placement: an attribution may be coarse, but it is never guessed.
func (w *Writer) policyTrail(in RequestedInput) *policyTrail {
	if in.PolicyInput == nil && len(in.Policies) == 0 {
		return nil
	}
	typed := func(spec map[string]any) map[string]any {
		if spec == nil {
			return nil
		}
		_, fields, _, err := w.typedFields(spec)
		if err != nil {
			return nil
		}
		return fields
	}
	trail := &policyTrail{input: typed(in.PolicyInput)}
	for _, p := range in.Policies {
		trail.steps = append(trail.steps, policyStep{id: p.ID, fields: typed(p.Spec), reason: p.Reason, consulted: p.Consulted})
	}
	return trail
}

// telling is what a trail puts on the requested record and in its context facet.
type telling struct {
	provenance map[string]any // per-field entries, each with its place in the field's chain
	applied    []any          // assembly.applied: every source that contributed
	policies   []any          // policies[]: every policy that decided, and what it changed
	decisions  []any          // the facet's policy_decisions
	consulted  []any          // the facet's consulted citations, each once
}

// tell walks from the intent's fields to the fields that were sent.
func (t *policyTrail) tell(intent, sent map[string]any, at string) telling {
	out := telling{provenance: map[string]any{}}
	current := intent
	step := func(after map[string]any, id string) []string {
		prov, changed := diffProvenance(current, after, "policy", id, at)
		for path, entries := range prov {
			earlier, _ := out.provenance[path].([]any)
			e := entries.([]any)[0].(map[string]any)
			e["sequence"] = len(earlier) + 1
			out.provenance[path] = append(earlier, e)
		}
		current = after
		return changed
	}

	var byPlacement []string
	var byPolicy []any
	if t != nil {
		if t.input != nil {
			byPlacement = step(t.input, placementID)
		}
		cited := map[Citation]bool{}
		for _, s := range t.steps {
			ref := policyRefPrefix + s.id
			var changed []string
			if s.fields != nil {
				changed = step(s.fields, ref)
			}
			entry := map[string]any{"policy": ref, "decision": "allow"}
			outcome := "permit"
			if len(changed) > 0 {
				outcome = "transform"
				entry["fields"] = anys(changed)
				byPolicy = append(byPolicy, map[string]any{
					"source": map[string]any{"kind": "policy", "id": ref},
					"fields": anys(changed),
				})
			}
			if s.reason != "" {
				entry["reason"] = s.reason
			}
			out.policies = append(out.policies, entry)
			out.decisions = append(out.decisions, map[string]any{"policy_uuid": ref, "outcome": outcome})
			for _, c := range s.consulted {
				if cited[c] {
					continue
				}
				cited[c] = true
				citation := map[string]any{"uuid": c.UUID, "version": c.Version}
				if c.Head != "" {
					citation["head"] = c.Head
				}
				out.consulted = append(out.consulted, citation)
			}
		}
	}
	byPlacement = append(byPlacement, step(sent, placementID)...)

	placement := map[string]any{"source": map[string]any{"kind": "policy", "id": placementID}}
	if paths := uniqueSorted(byPlacement); len(paths) > 0 {
		placement["fields"] = anys(paths)
	}
	out.applied = append([]any{placement}, byPolicy...)
	return out
}

// contextFacet builds the requested record's udlm_context facet. The principal
// is the actor the intent record already names, carried through and not looked
// up again; fallback stands in only when the intent names none. The pathway is
// the request, cited by the root of its order.
func contextFacet(intent *StateRecord, rootRequest, fallback string, told telling) map[string]any {
	facet := map[string]any{
		"_producer":        contextProducer,
		"_schemaURL":       schema.ContextFacet,
		"principal":        map[string]any{"id": intentActor(intent, fallback)},
		"pathway_ref":      map[string]any{"pathway": "request", "request_id": rootRequest},
		"policy_decisions": told.decisions,
	}
	if len(told.consulted) > 0 {
		facet["consulted"] = told.consulted
	}
	return facet
}

// intentActor is the actor the intent record's provenance names.
func intentActor(intent *StateRecord, fallback string) string {
	prov, _ := intent.Body["provenance"].(map[string]any)
	for _, path := range sortedKeys(prov) {
		entries, _ := prov[path].([]any)
		for _, e := range entries {
			entry, _ := e.(map[string]any)
			source, _ := entry["source"].(map[string]any)
			if stringOf(source["kind"]) == "actor" && stringOf(source["id"]) != "" {
				return stringOf(source["id"])
			}
		}
	}
	return fallback
}

func anys(paths []string) []any {
	out := make([]any, len(paths))
	for i, p := range paths {
		out[i] = p
	}
	return out
}

func uniqueSorted(paths []string) []string {
	sort.Strings(paths)
	out := paths[:0]
	for i, p := range paths {
		if i == 0 || p != paths[i-1] {
			out = append(out, p)
		}
	}
	return out
}
