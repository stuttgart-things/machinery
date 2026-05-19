# CI/CD

This page documents the GitHub Actions pipelines that run on `machinery`,
with a focus on the **preview app** workflow that spins up a live
per-PR environment.

## Preview app workflow

Every pull request against `main` can be promoted to a live preview
environment by attaching the `preview` label. The deployment is driven
by an ArgoCD `ApplicationSet` living in
[`stuttgart-things/argocd`](https://github.com/stuttgart-things/argocd)
under `platforms/machinery-pr-preview/`, which uses a `pullRequest`
generator to pick up labeled PRs and render them against the per-PR
artifacts pushed from this repo.

### End-to-end flow

```
                     ┌──────────────────────────────────────────────┐
                     │ 1. PR opened against main                    │
                     └────────────────────┬─────────────────────────┘
                                          │
                ┌─────────────────────────┴───────────────────────────┐
                ▼                                                     ▼
   build-scan-image.yaml                                  push-kustomize-pr.yaml
   pushes ghcr.io/stuttgart-things/                       pushes ghcr.io/stuttgart-things/
   machinery:pr-<num>-<sha>                               machinery-kustomize:pr-<num>-<sha>

                                          │
                     ┌────────────────────┴─────────────────────────┐
                     │ 2. Maintainer adds the `preview` label       │
                     └────────────────────┬─────────────────────────┘
                                          │
                ┌─────────────────────────┴───────────────────────────┐
                ▼                                                     ▼
   comment-preview-url.yaml                                ArgoCD ApplicationSet
   posts the preview URL as a                              (stuttgart-things/argocd)
   PR comment                                              renders the kustomize OCI
                                                           artifact at the matching
                                                           pr-<num>-<sha> tag and
                                                           deploys it into the
                                                           machinery-pr-<num> namespace

                                          │
                     ┌────────────────────┴─────────────────────────┐
                     │ 3. PR closed (merged or not)                 │
                     └────────────────────┬─────────────────────────┘
                                          ▼
                              cleanup-pr-artifacts.yaml
                              deletes the pr-<num>-* tags
                              from both GHCR packages
```

### The artifacts

The AppSet keys on **two** OCI artifacts that share the same tag:

| Artifact | Workflow | Tag |
|---|---|---|
| Runtime image | `build-scan-image.yaml` (ko build) | `pr-<num>-<sha>` (and `pr-<num>`) |
| Kustomize manifests | `push-kustomize-pr.yaml` | `pr-<num>-<sha>` |

The `<sha>` part of the tag matches `.head_sha` from Argo's
`pullRequest` generator, so the AppSet substitutes it verbatim — no
massaging needed.

### The preview URL

When the `preview` label is attached (or a PR is reopened while still
carrying the label), `comment-preview-url.yaml` posts a comment of the
form:

```
machinery-pr-<num>.homerun2-dev.sthings-vsphere.labul.sva.de
```

The hostname prefix (`machinery-pr`), domain
(`homerun2-dev.sthings-vsphere.labul.sva.de`), and namespace prefix
(`machinery-pr`) are configured in
`.github/workflows/comment-preview-url.yaml` and **must** match the
AppSet template in `stuttgart-things/argocd`.

### Trigger semantics

`comment-preview-url.yaml` listens to `reopened` and `labeled` —
**not** `opened`. The reason: when a PR is created with the `preview`
label already attached, GitHub fires `opened` and `labeled`
back-to-back and the URL comment ends up duplicated. The `labeled`
event covers the at-creation case on its own.

### Cleanup

`cleanup-pr-artifacts.yaml` fires on `pull_request: closed` and
removes the `pr-<num>-*` tags from both `machinery` and
`machinery-kustomize` on GHCR. ArgoCD tears down the namespace on its
own once the PR drops out of the generator's result set.

## Other pipelines

| Workflow | Trigger | Purpose |
|---|---|---|
| `build-test.yaml` | push to `main`, PR | `go build` + `go test -race` + `go vet` |
| `e2e.yaml` | push to `main`, PR | kind-based end-to-end (`task e2e`) on self-hosted runner |
| `lint-repo.yaml` | push to `main`, PR | Repository linting via Dagger |
| `build-scan-image.yaml` | push to `main`, PR | ko build + scan; PR builds get `pr-*` tags |
| `release.yaml` | after successful main image build | semantic-release + tagged kustomize OCI push |
| `pages.yaml` | after successful release | Publish MkDocs site to GitHub Pages |

## Trying it out

1. Open a PR against `main`.
2. Wait for `build-scan-image` and `push-kustomize-pr` to go green —
   the artifacts have to exist before Argo can pull them.
3. Apply the `preview` label.
4. A PR comment with the live URL shows up shortly after Argo syncs.
5. Close (or merge) the PR to tear everything down.

## Troubleshooting

If the preview URL responds `HTTP 500` from `server: envoy` with
no inbound request lines on the pod, the HTTPRoute is the suspect
rather than machinery itself. Check `ResolvedRefs`:

```
kubectl -n machinery-pr-<num> get httproute machinery -o yaml \
  | yq '.status.parents[].conditions[] | select(.type=="ResolvedRefs")'
```

A `BackendNotFound` here with a stale `lastTransitionTime` means
Cilium's gateway-controller latched the verdict before the backend
Service made it into its informer cache. The
`apps/machinery/httproute` chart in `stuttgart-things/argocd` ships
a PostSync `Job` that re-annotates the HTTPRoute on every sync to
force Cilium to re-reconcile — if that Job ran successfully, the
race is already self-healed. Set
`httpRoute.nudgeAfterSync: false` on the install chart's values to
opt out of the Job (sensible only for long-lived deployments where
the gateway-controller's cache is steady-state).

If the dashboard renders but the resource table stays empty for
**every** kind, `kubectl logs deploy/machinery` will usually show
the cause directly — typically `the server could not find the
requested resource` against a CRD that's been removed or an API
version that has been retired (e.g. ESO `v1beta1` → `v1`).
`GetResources` skips any kind that 404s and continues with the
others, so a single broken kind only loses its own rows. RBAC
gaps surface the same way but as `forbidden`; the
`apps/machinery/install` chart's `rbac.rules` block controls the
ClusterRole the machinery SA gets.
