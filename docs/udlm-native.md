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
| 2 | **Per-state records, shadow-written.** On `catalogItemInstanceService.Create` write one `intent_record` per resource. When placement selects an agent and dispatches (`PlacementService.CreateRun`), write a `requested_record` that carries `intent_ref`, `provider`, and `assembly`. When the status consumer applies a `RUNNING` event (`PlacementService.OnResourceRunning`), write a `realized_record` that carries `requested_ref`, `provider`, `fields`, and `outputs`. Records share `entity_uuid`; each has its own v7 `record_uuid`; a later realized record `supersedes` the earlier one. Stored in a new `udlm_records` table as JSON, validated against the schema before insert. Existing rows are untouched, so nothing upstream breaks. | new package `internal/udlm/records` and a store; hooks at the three call sites | 1, 5, 10, 13 | next |
| 3 | **Entity view read API.** `GET /udlm/v1alpha1/entities/{entity_uuid}` returns the computed view (latest record per state), and `.../records` lists the chain. Read only. | `api/udlm/v1alpha1`, `internal/udlm/handlers` | 1, 5 | after 2 |
| 4 | **Provenance on realized fields.** Each field in a realized record's `fields` and `outputs` carries the agent, the run id, and the event time from the status event that produced it. A changed value on a later event supersedes the record and advances that field's provenance. | increment 2's realized writer | 5, 14 | after 2 |
| 5 | **Typed output binding.** A CEL reference `${name.output}` is checked against the registry's declared `outputs` for the source type, including its type. A reference spliced into a larger string is reported, since the model binds by typed reference, not by string assembly. | `internal/catalog/service/cel_validation.go`, reading the generated specs' read-only fields | 2, 7 | independent of 2 |
| 6 | **Agent registration as capability advertisement.** The agent registration payload gains the fields provider-contract §8.1a names (resource types with versions, capacity), mapped from `service_types`. | `api/agent/v1alpha1`, `internal/agent` | 17 | after upstream's environment-agent settles |
| 7 | **Policy verdict three-state and override.** The policy response distinguishes refused (permanent) from pending (transient), and an `override` policy type is honored per policy-contract §18. | `internal/policy`, `internal/placement/policy` | 11, 16, 19 | after 2 |
| 8 | **Identity reconciliation** with UDLM `Identity.*` (upstream enhancement #91). | `internal/auth` | none in the release set | when #91 moves |

Audit chain (rows 15, 21) and graph queries over stored edges (rows 7, 8, 9) are not scheduled here. The
audit chain needs a writer that does not exist in either tree; graph ordering exists upstream inside a
run and should be lifted to stored edges there, not re-implemented here.

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
