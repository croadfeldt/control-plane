# udlm-native — the fork's UDLM integration branch

**What this is:** the branch on croadfeldt/control-plane where the control plane learns to write and
read UDLM records. **What it settles:** what goes on the branch, in what order, and how it relates to
upstream dcm-project/control-plane while upstream decides how it will adopt UDLM.

Upstream has no UDLM code today (2026-09-16: the word appears nowhere in its tree). Its enhancements
that touch the same ground are `state-management` (provider status events over CloudEvents),
`sp-resource-status-reader` (the consumer that applies them), `declarative-api` and
`catalog-item-schema` (DAG runs and CEL cross-resource wiring), `rehydration-flow`, and
`service-type-definitions` (the hand-authored service-type schemas). None of them defines a record
model. Enhancement #91 asks to reconcile the auth actor model with UDLM `Identity.*`.

## Rules for the branch

1. `udlm-native` is the integration branch. Work lands on it by pull request from a topic branch;
   merging is Chris's.
2. Upstream is merged into `main` as a merge commit (never rebased or squashed), then `main` is merged
   into `udlm-native`. After every sync, `make generate UDLM_DIR=../udlm` is re-run and the five
   service types are recommitted.
3. Nothing on the branch invents a record shape. Every record validates against the schema in
   croadfeldt/udlm `registry/state-record.schema.json`, and the merged view is computed by
   `registry/tools/entity_view.py`, never stored.
4. Each increment names the enablement-map row it moves (croadfeldt/udlm
   `docs/uc-enablement-map.md`) and updates that row when it merges.
5. An increment that upstream ships on its own is dropped here and taken from upstream.
6. The fork's CI workflows list `udlm-native` beside `main` in their `pull_request` branch filters, so
   PRs into the branch get the same checks. That is a three-line fork-only difference in
   `.github/workflows/`; expect to re-apply it after a sync if upstream edits those lines.

## Increments, in blocking order

| # | Increment | Where it lands | Rows moved | Status |
|---|---|---|---|---|
| 1 | Service types generated from the registry's served flat specs, with UDLM `outputs` as read-only fields | `cmd/udlm-servicetype-gen`, `api/catalog/v1alpha1/servicetypes/` | 1, 6 | merged to main (#4) |
| 2 | **Per-state records, shadow-written.** On `catalogItemInstanceService.Create` write one `intent_record` per resource. When placement selects an agent and dispatches (`PlacementService.CreateRun`), write a `requested_record` that carries `intent_ref`, `provider`, and `assembly`. When the status consumer applies a `RUNNING` event (`PlacementService.OnResourceRunning`), write a `realized_record` that carries `requested_ref`, `provider`, `fields`, and `outputs`. Records share `entity_uuid`; each has its own v7 `record_uuid`; a later realized record `supersedes` the earlier one. Stored in a new `udlm_records` table as JSON, validated against the schema before insert. Existing rows are untouched, so nothing upstream breaks. | new package `internal/udlm/records` and a store; hooks at the three call sites | 1, 5, 10, 13 | in review |
| 3 | **Entity view read API.** `GET /api/v1alpha1/udlm/entities/{entity_uuid}` returns the computed view (latest record per state), `.../records` lists the chain, `GET /api/v1alpha1/udlm/entities` pages through entities. Read only. | `api/udlm/v1alpha1`, `internal/udlm/handlers` | 1, 5 | in review |
| 4 | **Field-level provenance.** Intent fields attributed to the actor, requested fields that placement changed attributed to `dcm/placement` with the previous value, realized outputs attributed to the agent with the status event's time; a changed output on a later event supersedes the record and appends to that output's provenance, an unchanged one keeps its origin entry. The placement run is noted on the realized record. | increment 2's writer | 5, 14 | in review |
| 5 | **Typed output binding.** A CEL reference `${name.output}` is checked against the outputs the source's UDLM class declares (a table the generator emits from the registry), including its type against the consumer field, and a path into an output must fit the output's type. A reference spliced into a larger string was already refused. | `internal/catalog/service/cel_validation.go`, `servicetypes/udlm_types.gen.go` | 2, 7 | in review |
| 6 | **Agent registration as capability advertisement.** The agent registration payload gains the fields provider-contract §8.1a names (resource types with versions, capacity), mapped from `service_types`. | `api/agent/v1alpha1`, `internal/agent` | 17 | after upstream's environment-agent settles |
| 7 | **Policy verdict three-state and override.** The policy response distinguishes refused (permanent) from pending (transient), and an `override` policy type is honored per policy-contract §18. | `internal/policy`, `internal/placement/policy` | 11, 16, 19 | after 2 |
| 8 | **Identity reconciliation** with UDLM `Identity.*` (upstream enhancement #91). | `internal/auth` | none in the release set | when #91 moves |

Audit chain (rows 15, 21) and graph queries over stored edges (rows 7, 8, 9) are not scheduled here. The
audit chain needs a writer that does not exist in either tree; graph ordering exists upstream inside a
run and should be lifted to stored edges there, not re-implemented here.

## Increment 2 as built

Differences from the design notes below, all forced by where the data actually is:

- **All three records are written from placement**, not the catalog. Resource ids are minted in
  `PlacementService.CreateRun`, so the intent record is written there once the rows exist, from the
  spec the catalog handed over. Requested records are written at each dispatch (level 0 in `CreateRun`,
  later levels in `OnResourceRunning`), realized records when the status consumer reports `RUNNING`.
- **One chain per entity, across states.** Intent is the root; each later record's `integrity.previous`
  is the newest record's head, whatever its state. A second realized record supersedes the first and
  bumps `generation`.
- **The selected agent is noted, not bound, on the requested record.** The schema forbids `provider`
  there; the requested record carries `assembly.applied` with a `dcm/placement` policy source and an
  attributed note naming the agent. The realized record carries `provider: dcm/agents/<name>`.
- **Schemas are vendored by `make generate`** into `internal/udlm/schema/` (three files plus `SOURCE`
  with the registry commit) and compiled once at startup. One registry pattern uses lookahead, which
  Go's RE2 refuses, so the compiler falls back to an ECMAScript engine for patterns RE2 cannot take.
- **The generator now emits `servicetypes/udlm_types.gen.go`**, the slug-to-class table the writer
  uses to type a record from a spec's `service_type`. A service type with no UDLM class (`network`)
  writes nothing and logs why.
- **Parity with the registry's checker is tested**: the Go canonicalizer reproduces heads computed by
  `registry/tools/integrity_chain.py` on fixed vectors, and every record the writer produces validates
  against the vendored schema and verifies.
- Config: `UDLM_RECORDS_DISABLED` (default false), `UDLM_DEFAULT_TENANT_UUID` (a v4 placeholder
  until requests carry a tenant).

## Increment 5 as built

- The generator emits `UDLMOutputs` (slug to output name to type and sensitivity) beside the type
  table, read from each flat spec's `outputs`.
- For a source whose service type has a UDLM class, the registry is the authority: a reference must
  name a declared output (a source *input* such as `${db.engine}` is refused as not an output; an
  unknown name stays "not found"), an index or key path into an output must fit the output's type
  (indexing a string is refused; indexing a declared array is accepted even though the seeded
  template's array is empty), and a plain reference's type must bind to the consumer field's type
  when the consumer's template declares that field (integer and number bind to a numeric field). A
  sensitive output being bound is logged for audit.
- Service types with no UDLM class (the hand-authored `network`, custom types) keep the
  template-only check. Splicing a reference into a larger string was already refused by the
  reference grammar.

## Increment 4 as built

Provenance follows the registry's worked example (`example-vm-app01-*`) rather than the design note's
"every field on realized": the example attributes the intent's fields to the actor, the requested
record's *changed* fields to the policy that changed them, and the realized record's *outputs* to the
provider. So:

- **Intent**: every leaf path of `fields` gets one entry, `source.kind: actor`, `source.id:
  dcm/actors/<actor id>` from the request context (the auth middleware sets an actor even when auth is
  disabled; `dcm/actors/anonymous` otherwise).
- **Requested**: every leaf path whose value differs from the latest intent record gets an entry from
  `dcm/placement` (`set` with `previous_value` when the intent had one, `remove` when the path
  disappeared). `assembly.applied[0].fields` lists the same paths.
- **Realized**: every `outputs.<path>` gets an entry from `dcm/agents/<name>` with the status event's
  timestamp (the consumer now passes it through; `at` on the record is the event time). When a later
  event supersedes the record, an unchanged output keeps its earlier entries, a changed one appends an
  entry with `previous_value` and the next `sequence`, a vanished one appends a `remove`. The placement
  run id is an attributed note on the record.
- Leaf paths are dot-paths into objects; arrays and scalars are leaves, as in the example
  (`outputs.ip_addresses`, `cpu.count`). Fields and outputs are JSON-normalized before diffing so a
  stored record and a fresh spec compare like with like.

## Increment 3 as built

- **Three read endpoints** under `/api/v1alpha1/udlm/`: `entities` (paged summaries: uuid, tenant, class,
  version, lifecycle state, record count, newest `at`), `entities/{uuid}` (the view), and
  `entities/{uuid}/records` (every record, oldest first, as stored). Same bearer auth and OpenAPI
  request validation as the other domains; `make generate-udlm-api` regenerates the code.
- **The view is the registry's view.** `records.Fold` is a port of the registry's
  `entity_view.py` fold, line for line: latest record per state by record uuid, envelope from the
  most-realized record, snapshot keys per state, carried blocks from the state the registry takes them
  from, provenance merged, `observed_generation` once a realized record exists. A test folds the
  registry's worked example (`example-vm-app01-*`) and compares the result key by key with the
  registry tool's output, then validates it against the vendored `entity-view.schema.json`.
- **The API does not restate the shapes.** The OpenAPI schemas for the view and the record name only
  the keys the API itself depends on and allow everything else; the registry schemas stay the
  authority. The view is written through the generated type's own marshaller so no key is dropped.
- `make generate` now vendors `entity-view.schema.json` too.

## Increment 2 design notes

- **Where records come from.** The intent record's `fields` are the resolved resource spec from the
  catalog item instance (after field configuration, before placement). The requested record's `fields`
  are the placement payload as dispatched to the agent. The realized record's `fields` are the spec
  the agent acknowledged; its `outputs` are the output fields the status event carried.
- **Identity.** `entity_uuid` is the placement resource id when it is a v4 UUID; it is, since
  placement mints ids with `uuid.New()`. `record_uuid` is v7. `tenant_uuid` comes from the actor's
  tenant when auth is on, otherwise a configured default tenant recorded as such in `origin`.
- **Type.** `resource_type` and `type_version` come from the generated service type's description
  line, which names the UDLM type and version it was generated from; increment 1 makes that
  available. `conforms_to` is `udlm/0.1`.
- **Validation.** The schema and its `$ref` targets are vendored from the udlm checkout at build time
  (`make generate` copies them under `internal/udlm/schema/`) and compiled once at startup. A record
  that fails validation is logged and dropped; it never blocks the existing path. That is what
  "shadow" means here.
- **Integrity.** Realized records are sealed with the same algorithm as
  `registry/tools/integrity_chain.py` (sha256 over the JCS form of `{previous, record}`), so a record
  written here verifies with the registry's checker.
- **Not in scope.** No read path other than the table itself; no change to what the UI or agents see.
