# stuttgart-things/machinery

gRPC service for querying Stuttgart-Things Kubernetes custom resources. Watches any Crossplane-managed resource with configurable status field extraction. Returns resource status, readiness, and connection details across namespaces. Includes an HTMX web frontend for browsing resources.

## Architecture

```
Browser (HTMX) ──> HTTP :8080 ──┐
                                 ├──> Machinery Server ──> Kubernetes API ──> CRDs
Client (gRPC)  ──> gRPC :50051 ─┘        (dynamic client)
```

## Quick Start

### Server

```bash
export KUBECONFIG=~/.kube/config
go run main.go
```

### Client

```bash
export CLUSTERBOOK_SERVER=localhost:50051
export SECURE_CONNECTION=false
go run client/client.go
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `KUBECONFIG` | Path to kubeconfig file | required |
| `MACHINERY_CONFIG` | Path to JSON config file | built-in defaults |

### Client Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CLUSTERBOOK_SERVER` | Server address (host:port) | required |
| `SECURE_CONNECTION` | Enable TLS (`true`/`false`) | `false` |
| `TLS_CA_CERT` | Path to CA certificate | system CA pool |
| `TLS_SKIP_VERIFY` | Skip cert verification (dev only) | `false` |

### Config File

Resource types are configurable via JSON. Set `MACHINERY_CONFIG` to load a custom config. Each resource kind supports:

- **`group`/`version`/`resource`** — the GVR for the Kubernetes custom resource
- **`connectionField`** — dot-separated path to extract a primary connection value (e.g., `status.vm.name`)
- **`statusFields`** — list of dot-separated paths displayed as `label=value` pairs

Field extraction supports string, bool, and int64 types automatically.

```json
{
  "port": 50051,
  "httpPort": 8080,
  "resources": {
    "HarvesterVM": {
      "group": "resources.stuttgart-things.com",
      "version": "v1alpha1",
      "resource": "harvestervms",
      "connectionField": "status.vm.name",
      "statusFields": ["status.volume.ready", "status.cloudInit.ready", "status.vm.ready"]
    },
    "StoragePlatform": {
      "group": "resources.stuttgart-things.com",
      "version": "v1alpha1",
      "resource": "storageplatforms",
      "statusFields": ["status.installed", "status.observedVersion"]
    }
  }
}
```

## Default Resource Types

| Kind | API Group | Resource | Connection Field |
|------|-----------|----------|-----------------|
| HarvesterVM | resources.stuttgart-things.com/v1alpha1 | harvestervms | `status.vm.name` |
| StoragePlatform | resources.stuttgart-things.com/v1alpha1 | storageplatforms | — |
| NetworkIntegration | resources.stuttgart-things.com/v1alpha1 | networkintegrations | — |

## gRPC API

```protobuf
service ResourceService {
  rpc GetResources(ResourceRequest) returns (ResourceListResponse);
}

message ResourceRequest {
  string kind = 1;   // e.g. "HarvesterVM", comma-separated, or "*" for all
  int32 count = 2;   // -1 for all, max 1000
}
```

## Web Frontend

HTMX-based dashboard available at `http://localhost:8080`. Features:
- Auto-refreshing resource table (10s interval)
- Filter by resource kind
- Ready/Not Ready status badges
- Dark theme

## Deployment

KCL-based Kubernetes deployment (no Helm):

```bash
kcl run kcl/main.k -Y tests/kcl-deploy-profile.yaml
```

See `kcl/` directory for all manifests. Features: gRPC health probes, hardened security context, optional kubeconfig secret mount.

## Development

```bash
# Run tests
go test -v -race ./...

# Build
go build -o machinery .

# Generate proto
task proto
```

## License

Apache-2.0
