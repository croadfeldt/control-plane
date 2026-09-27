// Package records writes UDLM per-state records — one intent, requested, or
// realized record per event, never edited — beside the control plane's own
// rows. Every record validates against the vendored registry schema
// (internal/udlm/schema) before it is stored and is sealed into the entity's
// integrity chain. Writes are shadow writes: a failure is logged and dropped,
// and never blocks the path that triggered it.
package records

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"github.com/google/uuid"

	"github.com/dcm-project/control-plane/internal/auth"
)

// ConformsTo is the UDLM spec line the records declare.
const ConformsTo = "udlm/0.1"

// Type names the UDLM class a service type projects.
type Type struct {
	ResourceType string
	Version      string
}

// TypeLookup maps a control-plane service type (the `service_type` value in a
// resource spec) to its UDLM class; the generator emits the table.
type TypeLookup func(serviceType string) (Type, bool)

// envelopeKeys are the service-type envelope fields (CommonFields) that are
// not part of the entity's typed `fields`.
var envelopeKeys = map[string]bool{
	"service_type": true, "metadata": true, "id": true, "status": true,
	"status_message": true, "path": true, "create_time": true, "update_time": true,
}

// Writer builds, validates, seals, and stores records.
type Writer struct {
	store  Store
	types  TypeLookup
	tenant string
	log    *slog.Logger
	now    func() time.Time
}

// NewWriter returns a Writer. defaultTenant is the v4 UUID recorded as
// tenant_uuid until the control plane carries a tenant on its own requests.
func NewWriter(store Store, types TypeLookup, defaultTenant string, log *slog.Logger) *Writer {
	if log == nil {
		log = slog.Default()
	}
	return &Writer{store: store, types: types, tenant: defaultTenant, log: log, now: time.Now}
}

// IntentInput is what the consumer asked for, as the catalog resolved it.
type IntentInput struct {
	EntityUUID string
	Spec       map[string]any // the resource spec, envelope keys included
	Name       string         // display name, advisory
}

// RequestedInput is what the control plane dispatched, after policy and binding.
type RequestedInput struct {
	EntityUUID string
	Spec       map[string]any // the evaluated spec as sent to the agent
	AgentName  string         // the agent placement selected
}

// RealizedInput is what the agent reported when the resource reached RUNNING.
type RealizedInput struct {
	EntityUUID string
	Outputs    map[string]any // the status event's output_spec
	AgentName  string
	RunID      string    // the placement run, noted on the record
	At         time.Time // the status event's timestamp; zero means now
}

// errNothingChanged is returned by a builder when the record would repeat the
// latest one of its state (a retried status event); the write is skipped.
var errNothingChanged = errors.New("nothing changed since the latest record")

// anonymousActor is the provenance source when no actor is on the context.
const anonymousActor = "dcm/actors/anonymous"

// actorID names the authenticated identity on the context, or the anonymous
// placeholder. The auth middleware sets one on every request, even when auth
// is disabled.
func actorID(ctx context.Context) string {
	if info, ok := auth.ActorInfoFromContext(ctx); ok && info.ActorID != "" {
		return "dcm/actors/" + info.ActorID
	}
	return anonymousActor
}

// Intent writes an intent_record. It is the chain root for the entity.
func (w *Writer) Intent(ctx context.Context, in IntentInput) {
	if w == nil {
		return
	}
	w.write(ctx, "intent", func() (map[string]any, error) {
		t, fields, err := w.typedFields(in.Spec)
		if err != nil {
			return nil, err
		}
		rec := w.base("intent_record", "Intent", in.EntityUUID, t, "dcm-control-plane")
		rec["fields"] = fields
		at := stringOf(rec["at"])
		if prov := setAll(fields, "actor", actorID(ctx), at); len(prov) > 0 {
			rec["provenance"] = prov
		}
		if in.Name != "" {
			rec["metadata"] = map[string]any{"display_name": in.Name}
		}
		return rec, nil
	})
}

// Requested writes a requested_record pointing at the entity's latest intent
// record. The selected agent is noted on the record; the binding to a provider
// is the realized record's to carry.
func (w *Writer) Requested(ctx context.Context, in RequestedInput) {
	if w == nil {
		return
	}
	w.write(ctx, "requested", func() (map[string]any, error) {
		intent, err := w.store.Latest(ctx, in.EntityUUID, "Intent")
		if err != nil {
			return nil, fmt.Errorf("no intent record to reference: %w", err)
		}
		t, fields, err := w.typedFields(in.Spec)
		if err != nil {
			return nil, err
		}
		rec := w.base("requested_record", "Requested", in.EntityUUID, t, "dcm-control-plane")
		rec["intent_ref"] = intent.RecordUUID
		rec["fields"] = fields
		intentFields, _ := intent.Body["fields"].(map[string]any)
		prov, changed := diffProvenance(intentFields, fields, "policy", "dcm/placement", stringOf(rec["at"]))
		if len(prov) > 0 {
			rec["provenance"] = prov
		}
		applied := map[string]any{"source": map[string]any{"kind": "policy", "id": "dcm/placement"}}
		if len(changed) > 0 {
			paths := make([]any, len(changed))
			for i, c := range changed {
				paths[i] = c
			}
			applied["fields"] = paths
		}
		rec["assembly"] = map[string]any{"applied": []any{applied}}
		rec["metadata"] = map[string]any{
			"notes": []any{map[string]any{
				"at":     rec["at"],
				"author": "dcm/placement",
				"text":   "selected agent " + in.AgentName,
			}},
		}
		return rec, nil
	})
}

// Realized writes a realized_record: the latest requested record's fields, the
// agent's outputs, and the agent as provider. A later realized record for the
// same entity supersedes the earlier one.
func (w *Writer) Realized(ctx context.Context, in RealizedInput) {
	if w == nil {
		return
	}
	w.write(ctx, "realized", func() (map[string]any, error) {
		requested, err := w.store.Latest(ctx, in.EntityUUID, "Requested")
		if err != nil {
			return nil, fmt.Errorf("no requested record to reference: %w", err)
		}
		t := Type{ResourceType: requested.ResourceType, Version: stringOf(requested.Body["type_version"])}
		rec := w.base("realized_record", "Realized", in.EntityUUID, t, "agent-status-event")
		if !in.At.IsZero() {
			rec["at"] = in.At.UTC().Format("2006-01-02T15:04:05Z")
		}
		rec["requested_ref"] = requested.RecordUUID
		rec["fields"] = requested.Body["fields"]
		outputs := map[string]any{}
		if in.Outputs != nil {
			normalized, err := roundTrip(in.Outputs)
			if err != nil {
				return nil, err
			}
			outputs = normalized.(map[string]any)
		}
		rec["outputs"] = outputs
		providerID := "dcm/agents/" + in.AgentName
		rec["provider"] = providerID
		var prevOutputs, prevProv map[string]any
		if prev, err := w.store.Latest(ctx, in.EntityUUID, "Realized"); err == nil {
			prevOutputs, _ = prev.Body["outputs"].(map[string]any)
			prevProv, _ = prev.Body["provenance"].(map[string]any)
			if stringOf(prev.Body["provider"]) == providerID && reflect.DeepEqual(prevOutputs, outputs) {
				return nil, errNothingChanged
			}
			rec["supersedes"] = []any{prev.RecordUUID}
			rec["generation"] = prev.Generation + 1
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if prov := outputsProvenance(prevOutputs, outputs, prevProv, providerID, stringOf(rec["at"])); len(prov) > 0 {
			rec["provenance"] = prov
		}
		if in.RunID != "" {
			rec["metadata"] = map[string]any{
				"notes": []any{map[string]any{
					"at":     rec["at"],
					"author": "dcm/placement",
					"text":   "placement run " + in.RunID,
				}},
			}
		}
		return rec, nil
	})
}

// base fills the envelope every record carries.
func (w *Writer) base(recordType, state, entityUUID string, t Type, timeSource string) map[string]any {
	return map[string]any{
		"record_type":   recordType,
		"state":         state,
		"entity_uuid":   entityUUID,
		"record_uuid":   uuid.Must(uuid.NewV7()).String(),
		"tenant_uuid":   w.tenant,
		"conforms_to":   ConformsTo,
		"resource_type": t.ResourceType,
		"type_version":  t.Version,
		"generation":    1,
		"origin":        "declared",
		"at":            w.now().UTC().Format("2006-01-02T15:04:05Z"),
		"time_source":   timeSource,
	}
}

// typedFields resolves the UDLM type from the spec's service_type and returns
// the spec minus the envelope keys.
func (w *Writer) typedFields(spec map[string]any) (Type, map[string]any, error) {
	st := stringOf(spec["service_type"])
	if st == "" {
		return Type{}, nil, errors.New("spec carries no service_type")
	}
	t, ok := w.types(st)
	if !ok {
		return Type{}, nil, fmt.Errorf("service type %q has no UDLM class", st)
	}
	fields := make(map[string]any, len(spec))
	for k, v := range spec {
		if !envelopeKeys[k] {
			fields[k] = v
		}
	}
	// JSON-typed from here on, so a diff against a stored record (which is
	// JSON-typed) compares like with like.
	normalized, err := roundTrip(fields)
	if err != nil {
		return Type{}, nil, err
	}
	return t, normalized.(map[string]any), nil
}

// write runs build, then validates, seals onto the entity's chain, and stores.
// Every failure is logged at error level and dropped.
func (w *Writer) write(ctx context.Context, kind string, build func() (map[string]any, error)) {
	rec, err := build()
	if errors.Is(err, errNothingChanged) {
		w.log.DebugContext(ctx, "udlm record skipped", "kind", kind, "reason", err)
		return
	}
	if err != nil {
		w.log.ErrorContext(ctx, "udlm record not written", "kind", kind, "error", err)
		return
	}
	entity := stringOf(rec["entity_uuid"])
	rt, err := roundTrip(rec)
	if err != nil {
		w.log.ErrorContext(ctx, "udlm record not written", "kind", kind, "entity_uuid", entity, "error", err)
		return
	}
	body, _ := rt.(map[string]any)
	// The integrity chain is per state stream (state-record.schema.json: `previous`
	// is "the prior version's head, or null at the first version"; `supersedes` is
	// "within the same state's stream"). The first record of a state is a chain
	// root; a superseding record's previous is the head of the record it replaces.
	// Cross-state linkage is intent_ref / requested_ref, never the chain.
	var previous *string
	if prev, err := w.store.Latest(ctx, entity, stringOf(rec["state"])); err == nil {
		previous = &prev.Head
	} else if !errors.Is(err, ErrNotFound) {
		w.log.ErrorContext(ctx, "udlm record not written", "kind", kind, "entity_uuid", entity, "error", err)
		return
	}
	if err := Seal(body, previous); err != nil {
		w.log.ErrorContext(ctx, "udlm record not written", "kind", kind, "entity_uuid", entity, "error", err)
		return
	}
	if err := Validate(body); err != nil {
		w.log.ErrorContext(ctx, "udlm record rejected by state-record schema", "kind", kind, "entity_uuid", entity, "error", err)
		return
	}
	generation, _ := body["generation"].(float64)
	row := &StateRecord{
		RecordUUID:   stringOf(body["record_uuid"]),
		EntityUUID:   entity,
		TenantUUID:   stringOf(body["tenant_uuid"]),
		RecordType:   stringOf(body["record_type"]),
		State:        stringOf(body["state"]),
		ResourceType: stringOf(body["resource_type"]),
		Generation:   int(generation),
		Head:         stringOf(body["integrity"].(map[string]any)["head"]),
		Body:         body,
	}
	if err := w.store.Put(ctx, row); err != nil {
		w.log.ErrorContext(ctx, "udlm record not stored", "kind", kind, "entity_uuid", entity, "error", err)
		return
	}
	w.log.DebugContext(ctx, "udlm record written", "kind", kind, "entity_uuid", entity, "record_uuid", row.RecordUUID)
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}
