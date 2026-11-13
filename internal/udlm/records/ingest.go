package records

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/google/uuid"
)

// ErrInvalidObservation marks an observation the store refuses as written: a
// part the store assigns was supplied, a required part is missing, or the
// finished record does not validate. The caller can fix it and send it again.
var ErrInvalidObservation = errors.New("invalid observation")

// ErrAmbiguousEntity marks an observation whose natural keys are bound to more
// than one entity. The store does not choose between them.
var ErrAmbiguousEntity = errors.New("natural keys name more than one entity")

// storeAssigned are the record parts only the store sets on an observed record.
var storeAssigned = []string{"entity_uuid", "record_uuid", "generation", "supersedes", "integrity"}

const (
	discoveredType  = "discovered_record"
	discoveredState = "Discovered"
)

// Ingester accepts Discovered records from outside the control plane: what a
// discovery probe or a scanner observed. The control plane writes the other
// three states itself and takes none of them here.
type Ingester struct {
	store Store
	// One at a time, so two observations of one entity cannot both chain onto
	// the same previous record. The control plane runs as a single replica.
	mu sync.Mutex
}

// NewIngester returns an Ingester over the given store.
func NewIngester(store Store) *Ingester { return &Ingester{store: store} }

// Discovered turns an observation into a stored discovered_record and returns
// it. The observation is the record without the parts the store assigns:
//
//   - the entity is found by the observation's natural keys (`correlation_ids`)
//     within its tenant and class, and a new entity uuid is issued when no key
//     is bound yet. An observer never supplies an entity uuid.
//   - the record is appended to the entity's Discovered stream: `generation`,
//     `supersedes` and `integrity` follow from the record it replaces.
//
// Sending the same observation twice stores it once: when the latest record
// already carries exactly this content, that record is returned and created is
// false. The same content with a later `at` is a new observation and is stored.
func (g *Ingester) Discovered(ctx context.Context, observation map[string]any) (rec *StateRecord, created bool, err error) {
	rt, err := roundTrip(observation)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrInvalidObservation, err)
	}
	body, _ := rt.(map[string]any)
	for _, k := range storeAssigned {
		if _, ok := body[k]; ok {
			return nil, false, fmt.Errorf("%w: `%s` is assigned by the store and must not be sent", ErrInvalidObservation, k)
		}
	}
	if v, ok := body["record_type"]; ok && v != discoveredType {
		return nil, false, fmt.Errorf("%w: record_type %v is not %s; only observed records are accepted", ErrInvalidObservation, v, discoveredType)
	}
	if v, ok := body["state"]; ok && v != discoveredState {
		return nil, false, fmt.Errorf("%w: state %v is not %s; only observed records are accepted", ErrInvalidObservation, v, discoveredState)
	}
	tenant, resourceType := stringOf(body["tenant_uuid"]), stringOf(body["resource_type"])
	if tenant == "" || resourceType == "" {
		return nil, false, fmt.Errorf("%w: tenant_uuid and resource_type are required", ErrInvalidObservation)
	}
	keys, err := keysOf(body)
	if err != nil {
		return nil, false, err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	entities, err := g.store.EntitiesByKeys(ctx, tenant, resourceType, keys)
	if err != nil {
		return nil, false, err
	}
	var entity string
	switch len(entities) {
	case 0:
		entity = uuid.NewString()
	case 1:
		entity = entities[0]
	default:
		return nil, false, fmt.Errorf("%w: %v", ErrAmbiguousEntity, entities)
	}

	body["record_type"], body["state"] = discoveredType, discoveredState
	body["entity_uuid"] = entity
	body["generation"] = float64(1)
	var previous *string
	prev, err := g.store.Latest(ctx, entity, discoveredState)
	switch {
	case err == nil:
		if sameObservation(prev.Body, body) {
			return prev, false, nil
		}
		previous = &prev.Head
		body["generation"] = float64(prev.Generation + 1)
		body["supersedes"] = []any{prev.RecordUUID}
	case !errors.Is(err, ErrNotFound):
		return nil, false, err
	}
	body["record_uuid"] = uuid.Must(uuid.NewV7()).String()
	if err := Seal(body, previous); err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrInvalidObservation, err)
	}
	if err := Validate(body); err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrInvalidObservation, err)
	}
	generation, _ := body["generation"].(float64)
	row := &StateRecord{
		RecordUUID:   stringOf(body["record_uuid"]),
		EntityUUID:   entity,
		TenantUUID:   tenant,
		RecordType:   discoveredType,
		State:        discoveredState,
		ResourceType: resourceType,
		Generation:   int(generation),
		Head:         stringOf(body["integrity"].(map[string]any)["head"]),
		Body:         body,
	}
	if err := g.store.PutDiscovered(ctx, row, keys); err != nil {
		return nil, false, err
	}
	return row, true, nil
}

// keysOf reads the observation's natural keys. At least one is required:
// without one the store cannot tell which entity was observed.
func keysOf(body map[string]any) ([]Key, error) {
	list, _ := body["correlation_ids"].([]any)
	var keys []Key
	for _, item := range list {
		m, _ := item.(map[string]any)
		k := Key{Scheme: stringOf(m["scheme"]), Value: stringOf(m["value"])}
		if k.Scheme == "" || k.Value == "" {
			return nil, fmt.Errorf("%w: each correlation_ids entry needs a scheme and a value", ErrInvalidObservation)
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: correlation_ids needs at least one natural key, so the store can tell which entity was observed", ErrInvalidObservation)
	}
	return keys, nil
}

// sameObservation reports whether a stored record carries exactly the content
// of a new observation, setting aside the parts the store assigned.
func sameObservation(stored, incoming map[string]any) bool {
	strip := func(m map[string]any) map[string]any {
		out := make(map[string]any, len(m))
		for k, v := range m {
			out[k] = v
		}
		for _, k := range storeAssigned {
			delete(out, k)
		}
		return out
	}
	return reflect.DeepEqual(strip(stored), strip(incoming))
}
