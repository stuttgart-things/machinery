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
| gRPC | 50051 | `ResourceService.GetResources`, `.GetResourceDetail`, `.WatchResources` |
| HTTP | 8080 | `/` (dashboard), `/resources` (table), `/detail` (detail view), `/health` |

## Web Frontend

HTMX-based dashboard at `http://localhost:8080`:

- Auto-refreshing resource table (5s interval, stale-response guarded)
- Multi-select filters by resource kind and namespace
- Clickable rows for detail view with info fields
- Ready/Not Ready status badges
- Build info footer (version, commit, date)
- Dark theme

## gRPC API

```protobuf
service ResourceService {
  rpc GetResources(ResourceRequest) returns (ResourceListResponse);
  rpc GetResourceDetail(ResourceDetailRequest) returns (ResourceStatus);
  rpc WatchResources(ResourceRequest) returns (stream ResourceEvent);
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

enum EventType { ADDED = 0; MODIFIED = 1; DELETED = 2; }

message ResourceEvent {
  EventType type = 1;          // ADDED on subscribe (cache replay), then live
  ResourceStatus resource = 2;
}
```

`GetResources` / `GetResourceDetail` answer from a shared informer cache — one watch per configured kind, no per-request API-server calls. `WatchResources` is a server stream: the current state replays as `ADDED` events on subscribe, then live `ADDED` / `MODIFIED` / `DELETED` deltas follow.

## CLI client

[`machinery-client`](cmd/machinery-client/README.md) is the gRPC client for the service. Pre-built binaries for linux/darwin/windows (amd64/arm64) are attached to every [release](https://github.com/stuttgart-things/machinery/releases); or build from source with `go build -o machinery-client ./cmd/machinery-client`.

```bash
machinery-client list  --kind='*'                       # GetResources
machinery-client get   --kind=VsphereVM --name=demo-vm  # GetResourceDetail
machinery-client watch --kind='*'                       # WatchResources (live stream)
machinery-client health                                 # gRPC health probe
machinery-client version
```

Connection flags — `--server`, `--insecure`, `--ca-cert`, `--tls-skip-verify`, `--token`, `--token-file`, `--timeout`, `--json` — and their `MACHINERY_*` env-var equivalents are accepted on every subcommand. See [`cmd/machinery-client/README.md`](cmd/machinery-client/README.md) for the full reference, TLS/auth setup, and the `grpcurl` no-code path.

## Configuration

Resource types are configurable via JSON. Set `MACHINERY_CONFIG` to load a custom config file. Drop-in examples for common watch sets (VsphereVM, platform XRs, AnsibleRun, …) live in [`examples/configs/`](examples/configs/).

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `KUBECONFIG` | Path to kubeconfig file | in-cluster config |
| `MACHINERY_CONFIG` | Path to JSON config file | built-in defaults |
| `MACHINERY_AUTH_TOKEN` | Bearer token for the gRPC `auth` gate (see below); also picked up by `client/client.go` | unset |

### gRPC auth (opt-in)

The gRPC `ResourceService` runs anonymous by default — appropriate for in-cluster usage where access is gated at the network/Gateway layer. To require a bearer token on every network-side RPC, set:

```json
{
  "auth": {
    "enabled": true,
    "tokenFile": "/etc/machinery/auth-token"
  }
}
```

Token resolution order: `auth.token` (inline) → `auth.tokenFile` → `$auth.tokenEnvVar` → `$MACHINERY_AUTH_TOKEN`. Startup fails fast if `enabled: true` but no token resolves. `/grpc.health.v1.Health/*` stays anonymous so liveness/readiness probes keep working. In-process calls from the HTMX frontend (`web.go`) bypass the interceptor by construction. Pair this with the GRPCRoute (see [`kcl/README.md`](kcl/README.md)) when exposing the service beyond the pod network.

Caller side:

```bash
grpcurl -H 'authorization: Bearer <token>' \
  -authority machinery-grpc.example.com \
  machinery-grpc.example.com:443 resourceservice.ResourceService/GetResources
```

or via `client/client.go`: `MACHINERY_AUTH_TOKEN=<token> go run client/client.go`.

### Config File

Each resource kind supports:

- **`group`/`version`/`resource`** — GVR for the Kubernetes custom resource
- **`connectionField`** — dot-separated path to extract a primary connection value (e.g., `status.share.ip`)
- **`statusFields`** — dot-separated paths displayed as status indicators
- **`infoFields`** — labeled fields for the detail view, each with a `label` and `path`

Field extraction supports string, bool, and int64 scalars, plus slices: `[]string` joins comma-separated, `[]map` collapses to `namespace/name` pairs (useful for Gateway API's `spec.hostnames` and `spec.parentRefs`). For Gateway API kinds, readiness falls back from `status.conditions` to `status.parents[*].conditions[*]` automatically.

See [`examples/configs/`](examples/configs/) for drop-in JSON files, including a Gateway API example.

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

Features: gRPC/HTTP health probes, hardened security context (non-root, read-only rootfs, drop all capabilities), pod anti-affinity, optional kubeconfig secret mount, Gateway API HTTPRoute and GRPCRoute (opt-in, see [`kcl/README.md`](kcl/README.md) for the gRPC-via-gateway profile).

See [`kcl/README.md`](kcl/README.md) for all configuration options and manifest structure.

### Flux CD

Machinery can be deployed via [Flux CD](https://fluxcd.io/) using OCI-based Kustomizations.

| Resource | Description |
|----------|-------------|
| [Flux app manifests](https://github.com/stuttgart-things/flux/tree/main/apps/machinery) | Upstream Flux Kustomization, OCIRepository, HTTPRoute, and Namespace |
| [Cluster config example](https://github.com/stuttgart-things/stuttgart-things/blob/main/clusters/labul/vsphere/cd-mgmt-1/apps/machinery.yaml) | Per-cluster Flux Kustomization with `postBuild` variable substitution and config injection patches |

The upstream app manifests define the base deployment, while per-cluster configs customize version, hostname, gateway, and resource configuration via `postBuild.substitute` and strategic merge patches.

### PR preview environments

Labelling a pull request with `preview` triggers `push-kustomize-pr.yaml` to publish `pr-<n>-<sha>`-tagged image and kustomize artifacts. An Argo `ApplicationSet` ([`stuttgart-things/argocd@platforms/machinery-pr-preview`](https://github.com/stuttgart-things/argocd/tree/main/platforms/machinery-pr-preview)) then deploys them onto opt-in clusters and posts the preview URL back on the PR. Closing the PR tears the environment down and `cleanup-pr-artifacts.yaml` removes the OCI tags.

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

# Client — the machinery-client CLI (see "CLI client" above)
go run ./cmd/machinery-client list  --kind='*'
go run ./cmd/machinery-client watch --kind='*'
```

`client/client.go` is a separate single-file smoke-test client driven by the
environment variables below; `task client` runs it.

### `client/client.go` environment variables

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
