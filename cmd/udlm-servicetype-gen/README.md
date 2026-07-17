# udlm-servicetype-gen

Generates the control-plane service-type OpenAPI specs (`api/catalog/v1alpha1/servicetypes/<slug>/spec.yaml`)
from the [UDLM registry](https://github.com/croadfeldt/udlm) resource types, which are the single source of
truth for the estate data model. This keeps the control-plane's model from drifting away from UDLM: change a
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

For each mapped service-type it reads the UDLM resource type and emits a service-type `spec.yaml`:

- The UDLM `spec` (a JSON-Schema object) becomes the service-type schema `<Base>Spec`, wrapped in
  `allOf: [CommonFields, { ... }]` so every service-type inherits `service_type` / `metadata` /
  `provider_hints` from `common.yaml`.
- Nested object properties are hoisted to named component schemas (so `oapi-codegen` emits real Go types);
  arrays of objects hoist their item schema.
- `anyOf` sizing constraints (e.g. `instance_size` OR `vcpu`+`memory`) are folded into the schema description
  rather than emitted as an OpenAPI union, which `oapi-codegen` models awkwardly.
- Scalar facets (`pattern`, `enum`, `minimum`, `format`, …) pass through.
- **Reserved-name guard:** a UDLM property whose name collides with a `CommonFields` key
  (`metadata`, `id`, `status`, `service_type`, `provider_hints`, `path`, `create_time`, `update_time`,
  `status_message`) is skipped from the portable body and noted in the schema description — it would otherwise
  silently override the envelope field. Rename the property upstream in UDLM to surface it.

## Service-type ↔ UDLM mapping

| service-type | UDLM resource type          |
|--------------|-----------------------------|
| `vm`         | `compute.virtual-machine`   |
| `container`  | `compute.container`         |
| `database`   | `data.database`             |
| `cluster`    | `compute.cluster`           |
| `storage`    | `storage.volume`            |

`three_tier_app_demo` is a composite with no single UDLM resource type and is intentionally not generated.

## Do not hand-edit generated specs

`spec.yaml` and `types.gen.go` under `servicetypes/<slug>/` are generated. Edit the UDLM resource type and
re-run `make generate`. Non-portable, provider-specific configuration belongs under `provider_hints`, not new
top-level properties.
