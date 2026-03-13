# stuttgart-things/machinery

gRPC service for querying Stuttgart-Things Kubernetes custom resources (AnsibleRun, VsphereVMAnsible, ProxmoxVMAnsible). Returns resource status, readiness, and connection details (IPs) across namespaces.

## Architecture

```
Client (gRPC) ──> Machinery Server ──> Kubernetes API ──> CRDs
                       :50051              (dynamic client)
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

Resource types are configurable via JSON. Set `MACHINERY_CONFIG` to load:

```json
{
  "port": 50051,
  "resources": {
    "AnsibleRun": {
      "group": "resources.stuttgart-things.com",
      "version": "v1alpha1",
      "resource": "ansibleruns"
    }
  }
}
```

## Supported Resource Types

| Kind | API Group | Resource |
|------|-----------|----------|
| AnsibleRun | resources.stuttgart-things.com/v1alpha1 | ansibleruns |
| VsphereVMAnsible | resources.stuttgart-things.com/v1alpha1 | vspherevmansibles |
| ProxmoxVMAnsible | resources.stuttgart-things.com/v1alpha1 | proxmoxvmansibles |

## gRPC API

```protobuf
service ResourceService {
  rpc GetResources(ResourceRequest) returns (ResourceListResponse);
}

message ResourceRequest {
  string kind = 1;   // e.g. "AnsibleRun" or "*" for all
  int32 count = 2;   // -1 for all, max 1000
}
```

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
