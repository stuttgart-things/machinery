# stuttgart-things/machinery

Kubernetes resource dashboard and gRPC service for monitoring [Crossplane](https://www.crossplane.io/)-managed custom resources. Watches any CR with configurable status/info field extraction and exposes results via gRPC API and an HTMX web frontend.

## Architecture

```
Browser (HTMX) ──> HTTP :8080 ──┐
                                 ├──> Machinery Server ──> Kubernetes API ──> CRDs
Client (gRPC)  ──> gRPC :50051 ─┘        (dynamic client)
```

**Endpoints:**

| Protocol | Port | Paths |
|----------|------|-------|
| gRPC | 50051 | `ResourceService.GetResources`, `ResourceService.GetResourceDetail` |
| HTTP | 8080 | `/` (dashboard), `/resources` (table), `/detail` (detail view), `/health` |

## Web Frontend

HTMX-based dashboard at `http://localhost:8080`:

- Auto-refreshing resource table (5s interval)
- Filter by resource kind
- Clickable rows for detail view with info fields
- Ready/Not Ready status badges
- Build info footer (version, commit, date)
- Dark theme

## gRPC API

```protobuf
service ResourceService {
  rpc GetResources(ResourceRequest) returns (ResourceListResponse);
  rpc GetResourceDetail(ResourceDetailRequest) returns (ResourceStatus);
}

message ResourceRequest {
  int32 count = 1;    // -1 for all, max 1000
  string kind = 2;    // e.g. "VsphereVM", comma-separated, or "*" for all
}

message ResourceDetailRequest {
  string kind = 1;
  string name = 2;
  string namespace = 3;
}

message ResourceStatus {
  string name = 1;
  string kind = 2;
  bool ready = 3;
  string status_message = 4;
  string connection_details = 5;
  string namespace = 6;
  map<string, string> info_fields = 7;
}
```

## Configuration

Resource types are configurable via JSON. Set `MACHINERY_CONFIG` to load a custom config file.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `KUBECONFIG` | Path to kubeconfig file | in-cluster config |
| `MACHINERY_CONFIG` | Path to JSON config file | built-in defaults |

### Config File

Each resource kind supports:

- **`group`/`version`/`resource`** — GVR for the Kubernetes custom resource
- **`connectionField`** — dot-separated path to extract a primary connection value (e.g., `status.share.ip`)
- **`statusFields`** — dot-separated paths displayed as status indicators
- **`infoFields`** — labeled fields for the detail view, each with a `label` and `path`

Field extraction supports string, bool, and int64 types automatically.

```json
{
  "port": 50051,
  "httpPort": 8080,
  "resources": {
    "VsphereVM": {
      "group": "resources.stuttgart-things.com",
      "version": "v1alpha1",
      "resource": "vspherevms",
      "connectionField": "status.share.ip",
      "statusFields": ["status.share.ip"],
      "infoFields": [
        {"label": "Datacenter", "path": "spec.vm.datacenter"},
        {"label": "Template", "path": "spec.vm.template"},
        {"label": "Network", "path": "spec.vm.network"}
      ]
    }
  }
}
```

## Deployment

### KCL Manifests

Machinery uses [KCL](https://www.kcl-lang.io/) for Kubernetes manifests (no Helm). The KCL manifests are also the source for OCI kustomize artifacts pushed by the CI release workflow to `ghcr.io/stuttgart-things/machinery-kustomize`.

```bash
# Render manifests
kcl run kcl/main.k -Y tests/kcl-deploy-profile.yaml

# Apply directly
kcl run kcl/main.k -Y tests/kcl-deploy-profile.yaml | kubectl apply -f -
```

Features: gRPC/HTTP health probes, hardened security context (non-root, read-only rootfs, drop all capabilities), pod anti-affinity, optional kubeconfig secret mount, Gateway API HTTPRoute.

See [`kcl/README.md`](kcl/README.md) for all configuration options and manifest structure.

### Flux CD

Machinery can be deployed via [Flux CD](https://fluxcd.io/) using OCI-based Kustomizations.

| Resource | Description |
|----------|-------------|
| [Flux app manifests](https://github.com/stuttgart-things/flux/tree/main/apps/machinery) | Upstream Flux Kustomization, OCIRepository, HTTPRoute, and Namespace |
| [Cluster config example](https://github.com/stuttgart-things/stuttgart-things/blob/main/clusters/labul/vsphere/cd-mgmt-1/apps/machinery.yaml) | Per-cluster Flux Kustomization with `postBuild` variable substitution and config injection patches |

The upstream app manifests define the base deployment, while per-cluster configs customize version, hostname, gateway, and resource configuration via `postBuild.substitute` and strategic merge patches.

### Container Image

Built with [ko](https://ko.build/) on `cgr.dev/chainguard/static:latest` (distroless). Version, commit, and date are injected via ldflags at build time.

```
ghcr.io/stuttgart-things/machinery:<tag>
```

## Development

### Prerequisites

- Go 1.25+
- [Task](https://taskfile.dev/) (optional, for task runner)
- [KCL](https://www.kcl-lang.io/) (for rendering deployment manifests)
- [protoc](https://grpc.io/docs/protoc-installation/) + Go plugins (for proto generation)

### Run Locally

```bash
# Server
export KUBECONFIG=~/.kube/config
go run main.go

# Client
export CLUSTERBOOK_SERVER=localhost:50051
export SECURE_CONNECTION=false
go run client/client.go
```

### Client Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CLUSTERBOOK_SERVER` | Server address (host:port) | required |
| `SECURE_CONNECTION` | Enable TLS (`true`/`false`) | `false` |
| `TLS_CA_CERT` | Path to CA certificate | system CA pool |
| `TLS_SKIP_VERIFY` | Skip cert verification (dev only) | `false` |

### Tests

```bash
go test -v -race ./...
```

### Proto Generation

```bash
task proto
```

### Task Runner

```bash
task --list   # list available tasks
task server   # run server
task client   # run client
```

## Links

| Resource | URL |
|----------|-----|
| Documentation (GitHub Pages) | <https://stuttgart-things.github.io/machinery> |
| Container Image | `ghcr.io/stuttgart-things/machinery` |
| OCI Kustomize Artifacts | `ghcr.io/stuttgart-things/machinery-kustomize` |
| Changelog | [CHANGELOG.md](CHANGELOG.md) |

## License

Apache-2.0
