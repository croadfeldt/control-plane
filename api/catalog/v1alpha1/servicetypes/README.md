# Service Types

This directory contains Go type definitions generated 
from OpenAPI specifications for DCM service types.

## Structure

```
servicetypes/
├── common.yaml             # Common fields shared by all service types
├── types.gen.cfg           # oapi-codegen config for common types
├── types.gen.go            # Generated common types (ServiceMetadata, ProviderHints, etc.)
├── README.md               # This file
├── vm/
│   ├── spec.yaml           # VM OpenAPI specification
│   ├── spec.gen.cfg        # oapi-codegen config for VM types
│   └── types.gen.go        # Generated VM types
├── container/
│   ├── spec.yaml           # Container OpenAPI specification
│   ├── spec.gen.cfg        # oapi-codegen config for Container types
│   └── types.gen.go        # Generated Container types
├── database/
│   ├── spec.yaml           # Database OpenAPI specification
│   ├── spec.gen.cfg        # oapi-codegen config for Database types
│   └── types.gen.go        # Generated Database types
├── cluster/
│   ├── spec.yaml           # Cluster OpenAPI specification
│   ├── spec.gen.cfg        # oapi-codegen config for Cluster types
│   └── types.gen.go        # Generated Cluster types
├── storage/
│   ├── spec.yaml           # Storage OpenAPI specification
│   ├── spec.gen.cfg        # oapi-codegen config for Storage types
│   └── types.gen.go        # Generated Storage types
├── network/
│   ├── spec.yaml           # Network OpenAPI specification
│   ├── spec.gen.cfg        # oapi-codegen config for Network types
│   └── types.gen.go        # Generated Network types
└── three_tier_app_demo/
    ├── spec.yaml           # Three-Tier App Demo OpenAPI specification
    ├── spec.gen.cfg        # oapi-codegen config for Three-Tier App Demo types
    └── types.gen.go        # Generated Three-Tier App Demo types
```

Each service type folder is self-contained with:
- **spec.yaml**: OpenAPI specification defining the service type schema
- **spec.gen.cfg**: oapi-codegen configuration for code generation
- **types.gen.go**: Generated Go types (auto-generated, do not edit)

## Packages

- **`servicetypes`**: Common types shared across all service specifications
  - `CommonFields`
  - `ServiceMetadata`
  - `ProviderHints`
  - `ServiceType` enum

- **`servicetypes/vm`**: Virtual Machine specification types
  - `VMSpec`
  - `Vcpu`, `Memory`, `Storage`, `Disk`
  - `GuestOS`, `Access`

- **`servicetypes/container`**: Container specification types
  - `ContainerSpec`
  - `Image`, `ContainerResources`, `Process`, `Network`

- **`servicetypes/database`**: Database specification types
  - `DatabaseSpec`
  - `DatabaseResources`

- **`servicetypes/cluster`**: Kubernetes Cluster specification types
  - `ClusterSpec`
  - `Nodes`, `ControlPlaneNodes`, `WorkerNodes`

- **`servicetypes/storage`**: Standalone storage volume specification types
  - `StorageSpec`
  - `KubernetesProviderHints` (documented PVC settings for `provider_hints.kubernetes`)

- **`servicetypes/network`**: Network service specification types
  - `NetworkSpec`
  - `NetworkPort`
  - `KubernetesProviderHints` (selector, cluster_ip, node_ports for `provider_hints.kubernetes`)

- **`servicetypes/three_tier_app_demo`**: Three-tier demo specification types (exactly 3 components: database, app, web/nginx)
  - `ThreeTierAppDemoSpec`
  - `DatabaseTier` (engine, version, optional image)
  - `AppTier` (image)
  - `WebTier` (version, optional image)

## Usage

Import the types you need:

```go
import (
    "github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes"
    "github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes/vm"
)

// Create a VM specification
vmSpec := vm.VMSpec{
    // ServiceType constants are in the servicetypes package
    ServiceType: servicetypes.Vm,
    
    Metadata: servicetypes.ServiceMetadata{
        Name: "my-vm",
        Labels: &map[string]string{
            "environment": "production",
        },
    },
    
    Vcpu: vm.Vcpu{Count: 4},
    Memory: vm.Memory{Size: "16GB"},
    Storage: vm.Storage{
        Disks: []vm.Disk{
            {Name: "boot", Capacity: "100GB"},
        },
    },
    GuestOS: vm.GuestOS{Type: "rhel-9"},
}
```

**Important**: The `ServiceType` enum constants (`Vm`, `Container`, `Database`, `Cluster`, `Storage`, `Network`, `ThreeTierAppDemo`)
are defined in the `servicetypes` package.

## Regenerating Types

After modifying any OpenAPI specification files (`spec.yaml` in any service 
type folder or `common.yaml`), regenerate all Go types with a single command:

```bash
make generate-service-types
```

This command will:
1. Generate common types from `common.yaml`
2. Generate VM types from `vm/spec.yaml` with proper imports to common types
3. Generate Container types from `container/spec.yaml` with proper imports
4. Generate Database types from `database/spec.yaml` with proper imports
5. Generate Cluster types from `cluster/spec.yaml` with proper imports
6. Generate Storage types from `storage/spec.yaml` with proper imports
7. Generate Network types from `network/spec.yaml` with proper imports
8. Generate Three-Tier App Demo types from `three_tier_app_demo/spec.yaml` with proper imports

The command runs sequentially and provides progress feedback for each step.

### Modifying Service Type Specifications

To modify a service type:
1. Edit the `spec.yaml` file in the corresponding folder (e.g., `vm/spec.yaml`)
2. Run `make generate-service-types`
3. The Go types in `types.gen.go` will automatically be regenerated

## Import Mapping

The `--import-mapping` flag tells oapi-codegen how to resolve external references:

```
--import-mapping=../common.yaml:github.com/dcm-project/control-plane/api/catalog/v1alpha1/servicetypes
```

This ensures that references to `../common.yaml#/components/schemas/ServiceMetadata` 
in the service type specs are correctly resolved to `servicetypes.ServiceMetadata` 
rather than being inlined or causing import cycles.
