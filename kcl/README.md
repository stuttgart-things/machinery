# machinery/kcl

KCL (KusionLang Configuration Language) manifests for Kubernetes deployment. Used as the source for OCI kustomize artifacts pushed by the CI release workflow.

## Render Manifests

```bash
kcl run kcl/main.k -Y tests/kcl-deploy-profile.yaml
```

## Apply Directly

```bash
kcl run kcl/main.k -Y tests/kcl-deploy-profile.yaml | yq eval '.manifests[] | splitDoc' | kubectl apply -f -
```

## Structure

| File | Description |
|---|---|
| `schema.k` | Configuration schema with defaults and validation |
| `labels.k` | Common/selector labels and option() wiring |
| `main.k` | Entry point — imports and exports all manifests |
| `deploy.k` | Deployment with health probes, security context |
| `service.k` | ClusterIP service (gRPC + optional HTTP) |
| `httproute.k` | Gateway API HTTPRoute (optional) |
| `grpcroute.k` | Gateway API GRPCRoute (optional) |
| `configmap.k` | ConfigMap for environment variables |
| `namespace.k` | Namespace |
| `serviceaccount.k` | ServiceAccount |

## Configuration

Settings are passed via `-Y` profile file using `kcl_options` format:

```yaml
kcl_options:
  - key: config.image
    value: ghcr.io/stuttgart-things/machinery:latest
  - key: config.namespace
    value: machinery
  - key: config.httpEnabled
    value: True
  - key: config.httpRouteEnabled
    value: True
  - key: config.httpRouteParentRefName
    value: my-gateway
  - key: config.httpRouteHostname
    value: machinery.example.com
```

See `schema.k` for all available options and defaults.

## gRPC via the Gateway (in-cluster / local-network)

Expose the gRPC `ResourceService` through the same Gateway as the HTMX UI by
enabling a `GRPCRoute`. `parentRefName`/`parentRefNamespace` default to the
HTTPRoute values when unset, so most profiles only need the enable flag:

```yaml
kcl_options:
  - key: config.grpcRouteEnabled
    value: True
  # optional — defaults to httpRouteParentRefName / httpRouteParentRefNamespace
  - key: config.grpcRouteParentRefName
    value: movie-scripts2-gateway
  - key: config.grpcRouteParentRefNamespace
    value: default
  - key: config.grpcRouteHostname
    value: machinery-grpc.example.com
```

Then dial through the Gateway:

```bash
# health check
grpcurl -plaintext machinery-grpc.example.com:80 grpc.health.v1.Health/Check

# via the bundled client
CLUSTERBOOK_SERVER=machinery-grpc.example.com:80 SECURE_CONNECTION=false \
  go run client/client.go
```

> Scoped to local-network access; no TLS or auth is added here — track those
> separately.
