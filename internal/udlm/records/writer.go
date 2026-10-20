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
	// Elements are the class's top-level element names — the only heads a
	// dispatched path may have (DSP-002). Nil means the writer does not know the
	// class's elements, and then nothing may be admitted (DSP-001).
	Elements []string
}

// ClassLookup maps a UDLM class name to the class, for the companions a service
// type's spec expands into (a VM's disks are a StorageLayout).
type ClassLookup func(resourceType string) (Type, bool)

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
	store   Store
	types   TypeLookup
	classes ClassLookup
	tenant  string
	log     *slog.Logger
	now     func() time.Time
}

// NewWriter returns a Writer. defaultTenant is the v4 UUID recorded as
// tenant_uuid until the control plane carries a tenant on its own requests.
func NewWriter(store Store, types TypeLookup, classes ClassLookup, defaultTenant string, log *slog.Logger) *Writer {
	if log == nil {
		log = slog.Default()
	}
	return &Writer{store: store, types: types, classes: classes, tenant: defaultTenant, log: log, now: time.Now}
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

// Intent writes an intent_record. It is the chain root for the entity. A spec
// that expands into companions writes their intent records too: a VM's disks
// are a StorageLayout the VM references, written first; a cluster's node groups
// are KubernetesNodePools that reference the cluster, written after it.
func (w *Writer) Intent(ctx context.Context, in IntentInput) {
	if w == nil {
		return
	}
	t, fields, exp, err := w.typedFields(in.Spec)
	if err != nil {
		w.fail(ctx, "intent", in.EntityUUID, err)
		return
	}
	if exp.layout != nil {
		layoutEntity := uuid.NewString()
		if !w.writeIntent(ctx, layoutEntity, exp.layout.t, exp.layout.fields, suffixed(in.Name, "disks")) {
			w.fail(ctx, "intent", in.EntityUUID, errors.New("its StorageLayout record was not written"))
			return
		}
		fields[layoutRefElement] = entityRef(layoutEntity)
	}
	if !w.writeIntent(ctx, in.EntityUUID, t, fields, in.Name) {
		return
	}
	for _, p := range exp.pools {
		p.fields[clusterRefElement] = entityRef(in.EntityUUID)
		w.writeIntent(ctx, uuid.NewString(), p.t, p.fields, suffixed(in.Name, p.name))
	}
}

func suffixed(name, suffix string) string {
	if name == "" {
		return ""
	}
	return name + " " + suffix
}

func (w *Writer) fail(ctx context.Context, kind, entity string, err error) {
	w.log.ErrorContext(ctx, "udlm record not written", "kind", kind, "entity_uuid", entity, "error", err)
}

func (w *Writer) writeIntent(ctx context.Context, entity string, t Type, fields map[string]any, name string) bool {
	return w.write(ctx, "intent", func() (map[string]any, error) {
		rec := w.base("intent_record", "Intent", entity, t, "dcm-control-plane")
		rec["fields"] = fields
		at := stringOf(rec["at"])
		if prov := setAll(fields, "actor", actorID(ctx), at); len(prov) > 0 {
			rec["provenance"] = prov
		}
		if name != "" {
			rec["metadata"] = map[string]any{"display_name": name}
		}
		return rec, nil
	})
}

// Requested writes a requested_record pointing at the entity's latest intent
// record. The selected agent is noted on the record; the binding to a provider
// is the realized record's to carry. Each companion the intent expanded into
// gets its own requested record, a child of this order (DEP-015).
func (w *Writer) Requested(ctx context.Context, in RequestedInput) {
	if w == nil {
		return
	}
	t, fields, exp, err := w.typedFields(in.Spec)
	if err != nil {
		w.fail(ctx, "requested", in.EntityUUID, err)
		return
	}
	var layoutEntity string
	if exp.layout != nil {
		// The layout entity is the one the intent named; the evaluated spec carries
		// the disks again, not the reference.
		intent, err := w.store.Latest(ctx, in.EntityUUID, "Intent")
		if err != nil {
			w.fail(ctx, "requested", in.EntityUUID, fmt.Errorf("no intent record to reference: %w", err))
			return
		}
		intentFields, _ := intent.Body["fields"].(map[string]any)
		if layoutEntity = layoutEntityOf(intentFields); layoutEntity == "" {
			w.fail(ctx, "requested", in.EntityUUID, errors.New("the request carries disks but its intent record names no StorageLayout"))
			return
		}
		fields[layoutRefElement] = entityRef(layoutEntity)
	}
	if !w.writeRequested(ctx, in.EntityUUID, t, fields, in.AgentName, "") || (exp.layout == nil && len(exp.pools) == 0) {
		return
	}
	root, err := w.store.Latest(ctx, in.EntityUUID, "Requested")
	if err != nil {
		w.fail(ctx, "requested", in.EntityUUID, err)
		return
	}
	if exp.layout != nil {
		w.writeRequested(ctx, layoutEntity, exp.layout.t, exp.layout.fields, in.AgentName, root.RecordUUID)
	}
	if len(exp.pools) == 0 {
		return
	}
	pools, err := w.poolsOf(ctx, in.EntityUUID)
	if err != nil {
		w.fail(ctx, "requested", in.EntityUUID, err)
		return
	}
	for _, p := range exp.pools {
		entity, ok := pools[p.name]
		if !ok {
			w.fail(ctx, "requested", in.EntityUUID, fmt.Errorf("the request carries node group %q but its intent had no such pool", p.name))
			continue
		}
		p.fields[clusterRefElement] = entityRef(in.EntityUUID)
		w.writeRequested(ctx, entity, p.t, p.fields, in.AgentName, root.RecordUUID)
	}
}

// poolsOf returns a cluster's pool entities by pool name, read from their
// intent records.
func (w *Writer) poolsOf(ctx context.Context, cluster string) (map[string]string, error) {
	ids, err := w.store.Contained(ctx, cluster)
	if err != nil {
		return nil, err
	}
	pools := make(map[string]string, len(ids))
	for _, id := range ids {
		intent, err := w.store.Latest(ctx, id, "Intent")
		if err != nil {
			return nil, err
		}
		f, _ := intent.Body["fields"].(map[string]any)
		pools[stringOf(f["name"])] = id
	}
	return pools, nil
}

// writeRequested writes one requested record. root is the record_uuid of the
// requested record at the root of the order, or empty when this one is the root.
func (w *Writer) writeRequested(ctx context.Context, entity string, t Type, fields map[string]any, agent, root string) bool {
	return w.write(ctx, "requested", func() (map[string]any, error) {
		intent, err := w.store.Latest(ctx, entity, "Intent")
		if err != nil {
			return nil, fmt.Errorf("no intent record to reference: %w", err)
		}
		rec := w.base("requested_record", "Requested", entity, t, "dcm-control-plane")
		rec["intent_ref"] = intent.RecordUUID
		if intent.Head != "" {
			rec["intent_ref_head"] = intent.Head // the transition is bound to the bytes it consumed (TRN-002)
		}
		rec["generation"] = intent.Generation
		rec["fields"] = fields
		// Every requested record names the root of its order (DEP-015). A root names
		// itself; a companion expanded out of the root's spec names the root.
		// caused_by_request (DEP-018) is written only by a provider-originated
		// request, which the control plane does not take.
		if root == "" {
			root = stringOf(rec["record_uuid"])
		}
		rec["root_request_uuid"] = root
		// The receipt of what crossed to the provider (DSP-004). A reference the
		// writer filled did not cross: the provider was sent the cluster's node
		// groups or the VM's disks, and those are on the companion's own receipt.
		// Each such reference is recorded as the edge it stands for.
		sent := fields
		refs := sortedKeys(fieldsWithRefs(fields))
		if len(refs) > 0 {
			sent = cloneMap(fields)
		}
		var deps []any
		for _, el := range refs {
			delete(sent, el)
			e := refEdges[el]
			deps = append(deps, map[string]any{
				"edge_type": e.edgeType, "target_uuid": refEntity(fields[el]), "relation": e.relation,
			})
		}
		if len(deps) > 0 {
			rec["dependencies"] = deps
		}
		receipt, err := dispatchReceipt("dcm/agents/"+agent, sent, t)
		if err != nil {
			return nil, err
		}
		rec["dispatch"] = receipt
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
				"text":   "selected agent " + agent,
			}},
		}
		return rec, nil
	})
}

// Realized writes a realized_record: the latest requested record's fields, the
// agent's outputs, and the agent as provider. A later realized record for the
// same entity supersedes the earlier one. The provider realizes a VM's disks and
// a cluster's pools with it, so those companions are realized by the same report.
func (w *Writer) Realized(ctx context.Context, in RealizedInput) {
	if w == nil {
		return
	}
	if !w.writeRealized(ctx, in) {
		return
	}
	requested, err := w.store.Latest(ctx, in.EntityUUID, "Requested")
	if err != nil {
		return
	}
	// The agent reports nothing per disk or per pool, so a companion's outputs
	// are empty: they are filled only by what a provider actually reports.
	fields, _ := requested.Body["fields"].(map[string]any)
	var companions []string
	if layoutEntity := layoutEntityOf(fields); layoutEntity != "" {
		companions = append(companions, layoutEntity)
	}
	pools, err := w.store.Contained(ctx, in.EntityUUID)
	if err != nil {
		w.fail(ctx, "realized", in.EntityUUID, err)
		return
	}
	for _, entity := range append(companions, pools...) {
		w.writeRealized(ctx, RealizedInput{EntityUUID: entity, AgentName: in.AgentName, RunID: in.RunID, At: in.At})
	}
}

func (w *Writer) writeRealized(ctx context.Context, in RealizedInput) bool {
	return w.write(ctx, "realized", func() (map[string]any, error) {
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
		if requested.Head != "" {
			rec["requested_ref_head"] = requested.Head // TRN-002
		}
		// generation is the request cycle the record belongs to (four-states.md §2.7):
		// a realized record carries its requested record's generation, and a later
		// report of the same request supersedes without changing it.
		rec["generation"] = requested.Generation
		rec["fields"] = requested.Body["fields"]
		if deps, ok := requested.Body["dependencies"]; ok {
			rec["dependencies"] = deps
		}
		outputs := map[string]any{}
		if in.Outputs != nil {
			normalized, err := roundTrip(in.Outputs)
			if err != nil {
				return nil, err
			}
			outputs = mapOutputs(w.serviceTypeOf(requested.ResourceType), normalized.(map[string]any))
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

// serviceTypeOf finds the service type whose class is resourceType, among those
// with output renames. The record keeps the class, not the service type.
func (w *Writer) serviceTypeOf(resourceType string) string {
	for slug := range outputRenames {
		if t, ok := w.types(slug); ok && t.ResourceType == resourceType {
			return slug
		}
	}
	return ""
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
// the spec minus the envelope keys, under the class's names. A VM's disks come
// back separately, as the StorageLayout they expand into, and a cluster's node
// groups as KubernetesNodePools.
func (w *Writer) typedFields(spec map[string]any) (Type, map[string]any, expansion, error) {
	st := stringOf(spec["service_type"])
	if st == "" {
		return Type{}, nil, expansion{}, errors.New("spec carries no service_type")
	}
	t, ok := w.types(st)
	if !ok {
		return Type{}, nil, expansion{}, fmt.Errorf("service type %q has no UDLM class", st)
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
		return Type{}, nil, expansion{}, err
	}
	fields = normalized.(map[string]any)
	layout, err := w.splitLayout(st, fields)
	if err != nil {
		return Type{}, nil, expansion{}, err
	}
	pools, err := w.splitPools(st, fields)
	if err != nil {
		return Type{}, nil, expansion{}, err
	}
	// The service type's names are upstream's; the record carries the class's.
	mapped, err := mapFields(st, fields)
	if err != nil {
		return Type{}, nil, expansion{}, err
	}
	return t, mapped, expansion{layout: layout, pools: pools}, nil
}

// write runs build, then validates, seals onto the entity's chain, and stores.
// Every failure is logged at error level and dropped.
func (w *Writer) write(ctx context.Context, kind string, build func() (map[string]any, error)) bool {
	rec, err := build()
	if errors.Is(err, errNothingChanged) {
		w.log.DebugContext(ctx, "udlm record skipped", "kind", kind, "reason", err)
		return true
	}
	if err != nil {
		w.log.ErrorContext(ctx, "udlm record not written", "kind", kind, "error", err)
		return false
	}
	entity := stringOf(rec["entity_uuid"])
	rt, err := roundTrip(rec)
	if err != nil {
		w.log.ErrorContext(ctx, "udlm record not written", "kind", kind, "entity_uuid", entity, "error", err)
		return false
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
		return false
	}
	if err := Seal(body, previous); err != nil {
		w.log.ErrorContext(ctx, "udlm record not written", "kind", kind, "entity_uuid", entity, "error", err)
		return false
	}
	if err := Validate(body); err != nil {
		w.log.ErrorContext(ctx, "udlm record rejected by state-record schema", "kind", kind, "entity_uuid", entity, "error", err)
		return false
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
		ContainedBy:  containerOf(body),
		Body:         body,
	}
	if err := w.store.Put(ctx, row); err != nil {
		w.log.ErrorContext(ctx, "udlm record not stored", "kind", kind, "entity_uuid", entity, "error", err)
		return false
	}
	w.log.DebugContext(ctx, "udlm record written", "kind", kind, "entity_uuid", entity, "record_uuid", row.RecordUUID)
	return true
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

// fieldsWithRefs returns the reference elements in fields that the writer filled.
func fieldsWithRefs(fields map[string]any) map[string]bool {
	out := map[string]bool{}
	for el := range refEdges {
		if refEntity(fields[el]) != "" {
			out[el] = true
		}
	}
	return out
}

// containerOf is the entity a record's entity is contained by, when the writer
// made the containment (a node pool's cluster_ref).
func containerOf(body map[string]any) string {
	if stringOf(body["resource_type"]) != poolClass {
		return ""
	}
	fields, _ := body["fields"].(map[string]any)
	return refEntity(fields[clusterRefElement])
}
