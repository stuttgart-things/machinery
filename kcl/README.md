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

## gRPC auth (Secret-mounted bearer token)

When exposing the gRPC service beyond the pod network, gate it with the bearer-token
interceptor (machinery's `Config.Auth`). The KCL base mounts an externally-managed
`Secret` containing the token; the user's `MACHINERY_CONFIG` (passed via `configJson`)
then references the mounted file via `auth.tokenFile`.

The Secret itself is materialized out-of-band — by ESO, Kyverno generate, a sealed
secret, or plain `kubectl create secret generic`. The KCL base only mounts it.

```bash
# 1. Create the Secret out-of-band (any mechanism that lands a key called `token`):
kubectl -n machinery create secret generic machinery-auth-token \
  --from-literal=token="$(openssl rand -hex 32)"
```

```yaml
# 2. Reference it in your KCL profile. Defaults are sensible — only the Secret
#    name is required; the field defaults to mounting key `token` at
#    `/var/run/machinery-auth/token` (deliberately under /var/run so it can't
#    collide with the /etc/machinery configJson mount).
kcl_options:
  - key: config.authTokenSecret
    value: machinery-auth-token
  # optional overrides:
  # - key: config.authTokenSecretKey
  #   value: token
  # - key: config.authTokenMountPath
  #   value: /var/run/machinery-auth/token

  # 3. Enable auth in MACHINERY_CONFIG and point tokenFile at the mount path:
  - key: config.configJson
    value: |
      {
        "port": 50051,
        "auth": {
          "enabled": true,
          "tokenFile": "/var/run/machinery-auth/token"
        },
        "resources": { ... }
      }
```

Verify from outside the cluster (through the `GRPCRoute` from the previous section):

```bash
TOKEN=$(kubectl -n machinery get secret machinery-auth-token -o jsonpath='{.data.token}' | base64 -d)

# Without the token → Unauthenticated
grpcurl -authority machinery-grpc.example.com \
  machinery-grpc.example.com:443 \
  resourceservice.ResourceService/GetResources
# ERROR: Code: Unauthenticated, Message: missing authorization header

# With the token → success
grpcurl -H "authorization: Bearer ${TOKEN}" \
  -authority machinery-grpc.example.com \
  machinery-grpc.example.com:443 \
  resourceservice.ResourceService/GetResources
```

> **Off-cluster gRPC over `:443` needs HTTP/2 ALPN on the Gateway.** gRPC
> mandates HTTP/2; if the Cilium Gateway's HTTPS listener doesn't advertise
> `h2` in its TLS ALPN the commands above fail at the handshake
> (`No ALPN negotiated`) before any gRPC frames are exchanged. ALPN is not a
> Gateway API or KCL field — it's the Cilium **install** value
> `gatewayAPI.enableAlpn: true`, which also turns on `appProtocol` support so
> the gRPC Service port's `appProtocol: kubernetes.io/h2c` is honored upstream.

The HTMX dashboard is unaffected — its calls are in-process and bypass the
interceptor. `/grpc.health.v1.Health/*` also stays anonymous, so liveness/readiness
probes keep working without the token.
