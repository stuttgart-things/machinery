# Example machinery configs

Drop-in `config.json` files for the `MACHINERY_CONFIG` env var. Pick the
one closest to your needs and edit — `group`/`version`/`resource` map
1:1 to a Kubernetes GVR, dot-paths into the CR object pluck values for
the dashboard.

| File | Watches | Useful when |
|---|---|---|
| [`default.json`](default.json) | `HarvesterVM`, `StoragePlatform`, `NetworkIntegration` | starter — same as the binary's built-in `defaultConfig()` |
| [`vsphere-vms.json`](vsphere-vms.json) | `VsphereVM`, `HarvesterVM` | VM-provisioning view; includes `infoFields` for the detail pane |
| [`platforms.json`](platforms.json) | `StoragePlatform`, `NetworkIntegration`, `SecurityPlatform`, `AnsibleRun` | platform/automation rollout view across XRs |
| [`gateway-api.json`](gateway-api.json) | cert-manager `Certificate`, ESO `ExternalSecret`, Gateway API `HTTPRoute` | platform-services view; exercises the Gateway API per-parent status path and slice-valued info fields |

## Schema (per kind)

```jsonc
"<DisplayName>": {
  "group":           "resources.stuttgart-things.com",   // GVR.group
  "version":         "v1alpha1",                          // GVR.version
  "resource":        "vspherevms",                        // GVR.resource (plural, lowercase)
  "connectionField": "status.share.ip",                   // optional — primary value shown in the table
  "statusFields":    ["status.share.ip"],                 // optional — extra columns appended to connection details
  "infoFields": [                                         // optional — rows in the row-click detail panel
    { "label": "Datacenter", "path": "spec.vm.datacenter" }
  ]
}
```

Dot-paths support `string`, `bool`, and `int64` scalars, plus slices:
`[]string` joins comma-separated (e.g. `spec.hostnames`), `[]map`
collapses to `namespace/name` pairs when the items carry those keys
(e.g. `spec.parentRefs`). Missing paths render as empty. Array
indexing (`spec.parentRefs[0].name`) is not supported — point at the
parent path and let the renderer flatten it.

## Readiness

The dashboard's `Ready` badge comes from `status.conditions[*]` —
specifically the `type: Ready` entry (standard kubernetes /
crossplane convention). For Gateway API kinds (`HTTPRoute`,
`GRPCRoute`, …) there is no `Ready` type; conditions live per parent
at `status.parents[*].conditions[*]` instead. machinery falls back to
that path automatically and reports Ready when every parent condition
is `True`, otherwise surfaces the first non-`True` condition as
`<Type>: <Reason>`. No config knob — the fallback engages whenever
flat `status.conditions` is absent.

## Deploying a config

### Option A — flux / kustomize (any cluster)

1. Materialise the JSON as a ConfigMap in the destination namespace:

   ```bash
   kubectl -n machinery create configmap machinery-config \
     --from-file=config.json=examples/configs/platforms.json
   ```

2. Patch the Deployment to mount it (see the example below).

### Option B — Argo CD via `apps/machinery/install` chart

Pass the new `config.fromConfigMap` value (added in
[stuttgart-things/argocd#139](https://github.com/stuttgart-things/argocd/pull/139)):

```yaml
helm:
  values: |
    config:
      fromConfigMap: machinery-config
```

The chart mounts the named ConfigMap at `/etc/machinery` and sets
`MACHINERY_CONFIG=/etc/machinery/config.json`. The ConfigMap itself
still has to be provisioned out-of-band (Kyverno generate / sealed
secret / ESO / `kubectl apply`).

## Pod patch reference

If you wire the ConfigMap up by hand (Option A) rather than through
the chart, this is the Deployment overlay:

```yaml
spec:
  template:
    spec:
      containers:
        - name: machinery
          env:
            - name: MACHINERY_CONFIG
              value: /etc/machinery/config.json
          volumeMounts:
            - name: machinery-config
              mountPath: /etc/machinery
              readOnly: true
      volumes:
        - name: machinery-config
          configMap:
            name: machinery-config
```
