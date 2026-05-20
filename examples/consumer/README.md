# Example gRPC consumer

A small Go CLI that demonstrates how to call machinery from another
process. It exists to answer the questions that don't fit in the proto
file: how do I dial, how do I attach a bearer token, what does a
`GetResourceDetail` response actually look like, how do I exercise the
[`GRPCRoute`](../../kcl/grpcroute.k) from outside the cluster.

`client/client.go` at the repo root is the minimal smoke test;
**this** is the copy-from-me example.

## Build

```bash
go build -o consumer ./examples/consumer
```

## Commands

| Command  | RPC                                       | Notes |
|----------|-------------------------------------------|---|
| `list`   | `ResourceService.GetResources`            | `--kind=*` returns every configured kind |
| `get`    | `ResourceService.GetResourceDetail`       | `--kind` + `--name` required; `--namespace` for namespaced CRs |
| `health` | `grpc.health.v1.Health/Check`             | non-zero exit if the server isn't `SERVING` |

Connection flags accepted on **every** subcommand:

| Flag                | Env                          | Default            | Purpose |
|---------------------|------------------------------|--------------------|---|
| `--server`          | `MACHINERY_SERVER`           | `localhost:50051`  | host:port of the gRPC endpoint |
| `--insecure`        | `MACHINERY_INSECURE`         | `true`             | plaintext gRPC; flip for TLS |
| `--ca-cert`         | `MACHINERY_CA_CERT`          | —                  | PEM CA bundle (TLS only) |
| `--tls-skip-verify` | `MACHINERY_TLS_SKIP_VERIFY`  | `false`            | dev only |
| `--token`           | `MACHINERY_AUTH_TOKEN`       | —                  | bearer token; sent as `authorization: Bearer <token>` |
| `--timeout`         | —                            | `10s`              | per-RPC deadline |
| `--json`            | —                            | `false`            | emit JSON instead of a table |

## Three ways to call machinery

### 1. In-cluster (or `kubectl port-forward`)

Inside the cluster the Service is reachable directly; from your laptop
the easiest path is port-forward. No TLS, no auth needed unless
`auth.enabled` is on in the server config.

```bash
kubectl -n machinery port-forward svc/machinery 50051:50051 &

./consumer list --kind=VsphereVMAnsible --count=10
./consumer get  --kind=VsphereVMAnsible --name=demo-vm --namespace=demo
./consumer health
```

If the server has auth enabled, add `--token=$(cat /path/to/token)` or
`export MACHINERY_AUTH_TOKEN=…` and the same commands will work.

### 2. Via the `GRPCRoute` from a LAN host

Once the [GRPCRoute is applied](../../kcl/grpcroute.k), machinery is
addressable at the hostname your gateway listener exposes — e.g.
`machinery.example.com:443` for a TLS listener. Replace
`machinery.example.com` with whatever you set as `grpcRouteHostname`.

```bash
export MACHINERY_SERVER=machinery.example.com:443
export MACHINERY_AUTH_TOKEN=$(kubectl -n machinery get secret machinery-auth -o jsonpath='{.data.token}' | base64 -d)

./consumer list --insecure=false --kind='*' --count=20
./consumer health --insecure=false
```

Use `--ca-cert=/path/to/ca.pem` if the gateway presents a private CA,
or `--tls-skip-verify` for a quick smoke test while you're chasing a
cert issue. The bearer token is sent over TLS only by default — if you
need to send it over plaintext for LAN debugging, pass `--insecure` and
the example flips `RequireTransportSecurity` for you (see
`bearerToken` in `main.go`).

### 3. `grpcurl` — "I don't want to write Go"

The proto is small enough that the no-code path is fast. Same gateway
address, same token, ad-hoc requests:

Machinery does **not** register the gRPC reflection service, so
`grpcurl` needs the proto file to know the message shapes. Point it at
the one in this repo (or a checkout of it):

```bash
PROTO=$(git rev-parse --show-toplevel)/resourceservice/resource_service.proto

# Health check — does the route reach a pod at all? (no proto needed)
grpcurl machinery.example.com:443 grpc.health.v1.Health/Check

# List five VsphereVMAnsible resources.
grpcurl \
  -proto "$PROTO" \
  -H "authorization: Bearer $MACHINERY_AUTH_TOKEN" \
  -d '{"count": 5, "kind": "VsphereVMAnsible"}' \
  machinery.example.com:443 \
  resourceservice.ResourceService/GetResources

# Get one. Drop "namespace" for cluster-scoped CRs.
grpcurl \
  -proto "$PROTO" \
  -H "authorization: Bearer $MACHINERY_AUTH_TOKEN" \
  -d '{"kind": "VsphereVMAnsible", "name": "demo-vm", "namespace": "demo"}' \
  machinery.example.com:443 \
  resourceservice.ResourceService/GetResourceDetail
```

The `grpc.health.v1.Health` service is registered without a `-proto`
because `grpcurl` ships the health proto built in. Add `-plaintext` if
you're hitting an in-cluster port-forward instead of the gateway.

## JSON output

Both `list` and `get` accept `--json`, useful for piping into `jq` or
embedding in another tool:

```bash
./consumer list --kind=VsphereVMAnsible --json | jq '.[] | select(.ready) | .name'
```

## What's intentionally *not* here

- A daemon / polling loop. The "consume machinery on a timer" use
  case (the `examples/consumer-webservice/` idea from
  [#57](https://github.com/stuttgart-things/machinery/issues/57)) is
  intentionally left as a follow-up to keep this example focused on
  the one-shot call patterns.
- Client code for other languages. `grpcurl` covers most quick cases;
  for Python/TS, generate from
  [`resourceservice/resource_service.proto`](../../resourceservice/resource_service.proto)
  with the standard tooling.
- A release artifact. This is example code — vendor it or use it as a
  reference, don't depend on a binary release.
