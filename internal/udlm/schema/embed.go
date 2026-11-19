// Package schema carries the UDLM registry schemas a record must validate
// against, vendored from croadfeldt/udlm by `make generate` (see SOURCE for the
// commit). They are read at startup by internal/udlm/records; do not hand-edit.
package schema

import "embed"

// Files holds state-record.schema.json, entity-view.schema.json, the schemas they
// reference, and the udlm_context facet under facets/, at the path each has in
// the registry so relative `$ref`s resolve.
//
//go:embed *.schema.json facets/*.facet.json
var Files embed.FS

// BaseURL is the `$id` base every vendored schema declares; relative `$ref`s
// between them resolve against it.
const BaseURL = "https://udlm.dev/registry/udlm/0.1/"

// StateRecord is the schema id of the per-state record schema.
const StateRecord = BaseURL + "state-record.schema.json"

// EntityView is the schema id of the computed read model.
const EntityView = BaseURL + "entity-view.schema.json"

// ContextFacet is the schema id of the udlm_context facet: who caused a write,
// the policy decisions that permitted it, and the records those consulted
// (ADR-059). It is also the facet's required `_schemaURL`.
const ContextFacet = BaseURL + "facets/udlm-context.facet.json"
