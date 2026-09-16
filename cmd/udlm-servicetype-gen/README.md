# udlm-servicetype-gen

Generates the control-plane service-type OpenAPI specs (`api/catalog/v1alpha1/servicetypes/<slug>/spec.yaml`)
from the [UDLM registry](https://github.com/croadfeldt/udlm)'s served flat specs (`registry/generated/<type>.json`,
compiled from the authored classes under `registry/classes/`), which are the single source of truth for the estate
data model. This keeps the control-plane's model from drifting away from UDLM: change a
UDLM resource type, re-run the generator, commit the diff.

## Usage

```sh
# from the repo root, with a croadfeldt/udlm checkout at ../udlm
make generate                 # regenerate all specs + Go types
make generate UDLM_DIR=/path/to/udlm

# or the generator alone (spec.yaml only; then regenerate types.gen.go — see make generate):
go run ./cmd/udlm-servicetype-gen -udlm ../udlm -out api/catalog/v1alpha1/servicetypes -type vm
```

## What it does

For each mapped service-type it reads the UDLM flat spec and emits a service-type `spec.yaml`:

- The UDLM `spec` (a JSON-Schema object) becomes the service-type schema `<Base>Spec`, wrapped in
  `allOf: [CommonFields, { ... }]` so every service-type inherits `service_type` / `metadata` /
  `provider_hints` from `common.yaml`.
- Nested object properties are hoisted to named component schemas (so `oapi-codegen` emits real Go types);
  arrays of objects hoist their item schema.
- Spec-level `anyOf`/`oneOf` sizing constraints (e.g. `instance_size` OR `cpu`+`memory`) are folded into the schema
  description rather than emitted as an OpenAPI union, which `oapi-codegen` models awkwardly.
- A property-level `$ref` into a sibling registry schema (`../common-elements.schema.json#/$defs/Reference`,
  `../data-reference.schema.json#/$defs/data_reference`) is resolved against the registry checkout and translated in
  place (both are `string` / `format: udlm-ref-url`). Any other `$ref` shape is an error, so a new one is noticed.
- A property-level `allOf` of one typed schema plus constraint branches (the data-reference
  `reference_data_type: <kind>` pattern) becomes the typed schema with the constraint noted in its description.
- A property-level `oneOf` (inline object OR reference, e.g. `guest_os`, `image`) is emitted as an OpenAPI `oneOf`;
  object branches are hoisted to `<Name>Inline` so `oapi-codegen` emits a named type plus the union wrapper.
- The flat spec's typed realized **`outputs`** (E2) are emitted beside the spec fields as `readOnly` properties —
  the control-plane's runtime-output convention — so a CEL binding such as `${db.connection_string}` resolves against
  the outputs the registry declares. Outputs are never `required`; `sensitive` and `volatile` are noted in the
  description. An output whose name collides with a spec field or envelope key is skipped and noted.
- Scalar facets (`pattern`, `enum`, `minimum`, `format`, …) pass through.
- **Reserved-name guard:** a UDLM property whose name collides with a `CommonFields` key
  (`metadata`, `id`, `status`, `service_type`, `provider_hints`, `path`, `create_time`, `update_time`,
  `status_message`) is skipped from the portable body and noted in the schema description — it would otherwise
  silently override the envelope field. Rename the property upstream in UDLM to surface it.

## Service-type ↔ UDLM mapping

| service-type | UDLM flat spec (`registry/generated/`) | UDLM type (version at last regeneration) |
|--------------|----------------------------------------|------------------------------------------|
| `vm`         | `machine.vm.json`                      | `Machine.VM` 2.0.0                       |
| `container`  | `container.json`                       | `Container` 1.0.2                        |
| `database`   | `data.database.json`                   | `Data.Database` 0.7.7                    |
| `cluster`    | `kubernetes-cluster.json`              | `KubernetesCluster` 2.0.0                |
| `storage`    | `storage.volume.json`                  | `Storage.Volume` 0.11.6                  |

`network` and `three_tier_app_demo` are hand-authored: the network service type's shape (ports / routing_level /
endpoints) has no single UDLM class behind it, and the demo is a composite.

`three_tier_app_demo` is a composite with no single UDLM resource type and is intentionally not generated.

## Do not hand-edit generated specs

`spec.yaml` and `types.gen.go` under `servicetypes/<slug>/` are generated. Edit the UDLM resource type and
re-run `make generate`. Non-portable, provider-specific configuration belongs under `provider_hints`, not new
top-level properties.
