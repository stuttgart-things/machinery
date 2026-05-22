# machinery-client

The CLI for the machinery gRPC service: list and inspect resource
status, watch live changes, and probe health. It handles the
connection plumbing — plaintext, TLS with a custom CA, bearer-token
auth — so callers don't have to.

`client/client.go` at the repo root is the minimal in-tree smoke test;
this is the supported, released client.

## Install

Download a binary from the [releases page](https://github.com/stuttgart-things/machinery/releases)
— `machinery-client_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows),
built for linux/darwin/windows on amd64 and arm64.

Or build from source:

```bash
go build -o machinery-client ./cmd/machinery-client
```

## Commands

| Command   | RPC                                  | Notes |
|-----------|--------------------------------------|---|
| `list`    | `ResourceService.GetResources`       | `--kind=*` returns every configured kind |
| `get`     | `ResourceService.GetResourceDetail`  | `--kind` + `--name` required; `--namespace` for namespaced CRs |
| `watch`   | `ResourceService.WatchResources`     | streams live ADDED/MODIFIED/DELETED events; Ctrl-C to stop |
| `health`  | `grpc.health.v1.Health/Check`        | non-zero exit if the server isn't `SERVING` |
| `version` | —                                    | prints the build version |

Connection flags accepted on **every** subcommand:

| Flag                | Env                          | Default            | Purpose |
|---------------------|------------------------------|--------------------|---|
| `--server`          | `MACHINERY_SERVER`           | `localhost:50051`  | host:port of the gRPC endpoint |
| `--insecure`        | `MACHINERY_INSECURE`         | `true`             | plaintext gRPC; flip for TLS |
| `--ca-cert`         | `MACHINERY_CA_CERT`          | —                  | PEM CA bundle (TLS only) |
| `--tls-skip-verify` | `MACHINERY_TLS_SKIP_VERIFY`  | `false`            | dev only |
| `--token`           | `MACHINERY_AUTH_TOKEN`       | —                  | bearer token; sent as `authorization: Bearer <token>` |
| `--token-file`      | `MACHINERY_AUTH_TOKEN_FILE`  | —                  | file holding the token (mirrors the server's `auth.tokenFile`; `--token` wins) |
| `--timeout`         | —                            | `10s`              | per-RPC deadline (ignored by `watch`) |
| `--json`            | —                            | `false`            | emit JSON instead of a table |

## Calling machinery

### In-cluster (or `kubectl port-forward`)

```bash
kubectl -n machinery port-forward svc/machinery 50051:50051 &

machinery-client list  --kind=VsphereVMAnsible --count=10
machinery-client get   --kind=VsphereVMAnsible --name=demo-vm --namespace=demo
machinery-client watch --kind='*'
machinery-client health
```

If the server has auth enabled, add `--token=…`, `--token-file=/path/to/token`,
or `export MACHINERY_AUTH_TOKEN=…`.

### Via the `GRPCRoute` from outside the cluster

Once the [GRPCRoute is applied](../../kcl/grpcroute.k), machinery is
addressable at the hostname the gateway listener exposes — e.g.
`machinery.example.com:443` for a TLS listener.

```bash
export MACHINERY_SERVER=machinery.example.com:443
export MACHINERY_AUTH_TOKEN=$(kubectl -n machinery get secret machinery-auth -o jsonpath='{.data.token}' | base64 -d)

machinery-client list  --insecure=false --kind='*' --count=20
machinery-client watch --insecure=false --kind='*'
```

Off-cluster gRPC over a TLS `:443` listener needs HTTP/2 ALPN on the
Gateway — see [`kcl/README.md`](../../kcl/README.md). Use
`--ca-cert=/path/to/ca.pem` for a private CA, or `--tls-skip-verify`
for a quick smoke test. The bearer token is sent over TLS only by
default; pass `--insecure` to also send it on a plaintext LAN dial.

### `watch`

`watch` opens a server stream: the current state arrives as `ADDED`
events, then live `ADDED`/`MODIFIED`/`DELETED` deltas follow until you
Ctrl-C. `--timeout` is ignored — a watch is unbounded.

```
$ machinery-client watch --kind=Certificate
ADDED     Certificate      cert-manager/cluster-ca               Ready
ADDED     Certificate      default/homerun2-dev-gateway-tls      Ready
MODIFIED  Certificate      cert-manager/cluster-ca               Ready
```

`--json` emits one `ResourceEvent` JSON object per line instead.

### `grpcurl` — the no-code path

Machinery does not register gRPC reflection, so `grpcurl` needs an
explicit `-proto`:

```bash
REPO=$(git rev-parse --show-toplevel)
grpcurl -proto "$REPO/resourceservice/resource_service.proto" \
  -H "authorization: Bearer $MACHINERY_AUTH_TOKEN" \
  -d '{"count": 5, "kind": "VsphereVMAnsible"}' \
  machinery.example.com:443 resourceservice.ResourceService/GetResources
```

Add `-plaintext` for an in-cluster port-forward instead of the gateway.

## JSON output

`list`, `get` and `watch` accept `--json`, for piping into `jq` or
another tool:

```bash
machinery-client list --kind=VsphereVMAnsible --json | jq '.[] | select(.ready) | .name'
```
