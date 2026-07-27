# 10 — Deployment

Sources: `cmd/main.go`, `internal/exporter/exporter.go`, `config/manager/`,
`config/default/`, and the overlays under `config/`.

## Manager binary (`cmd/main.go`)

If `os.Args[1] == "exporter"`, the process runs the metrics exporter (below) and
exits. Otherwise it starts the controller manager.

Flags (defaults): `--metrics-bind-address` (`0` = off), `--metrics-secure`
(`true`), `--health-probe-bind-address` (`:8081`), `--leader-elect` (`false`,
lease id `palworld-operator.twodcube.io`), `--enable-http2` (`false`),
`--enable-webhooks` (`false`), and the `--webhook-cert-*` / `--metrics-cert-*`
paths. It registers the `PalworldGame`, `PalworldBackup`, and `PalworldRestore`
reconcilers, and — only when `--enable-webhooks` is set — the `PalworldGame`
validating webhook.

### Cache scoping (`operatorCacheOptions`)

The manager cache is **not** the controller-runtime default. An unfiltered
cluster-wide cache holds every Secret and ConfigMap on the cluster, which on a
real cluster (~1.5k Secrets / ~1.9k ConfigMaps across ~120 namespaces) exceeds
the manager's memory limit during the initial LIST and OOMKills it.

`operatorCacheOptions()` returns a `cache.Options` that:

- restricts these types to objects carrying
  `app.kubernetes.io/managed-by=palworld-operator` — the label `CommonLabels`
  puts on everything the operator creates, and which user `podLabels` cannot
  override (spec 02):

  `Secret`, `ConfigMap`, `Service`, `ServiceAccount`, `PersistentVolumeClaim`,
  `Pod`, `StatefulSet`, `Job`, `PodDisruptionBudget`, `NetworkPolicy`.

`Node` is deliberately **left unfiltered**: drain detection reads arbitrary nodes
by name (spec 11) and nodes carry no operator label. The three CR kinds are also
unfiltered — they are the operator's own API and are what it must discover.

The one object the operator reads but does not label is a user-supplied
credentials Secret (`spec.credentials.secretName`); it is read through the
uncached `APIReader` instead (spec 09).

**No cache transform.** `DefaultTransform` is deliberately nil, and no `ByObject`
entry sets `Transform`. `cache.TransformStripManagedFields()` in particular is not
used: `reconcileService` and `reconcileUnstructured` read an object from the
cached client, mutate it, and `Update` it back, so a stripped cached copy would be
written back with empty `managedFields` and force the API server to recompute
field ownership. The label filter alone accounts for the memory win (290Mi →
~35Mi observed on a 120-namespace cluster). `cmd/main_test.go` asserts the
transforms stay nil.

## Metrics exporter (`/manager exporter`)

Serves `/healthz` and `/metrics` on `EXPORTER_ADDR` (default `:9877`), scraping
`REST_ENDPOINT` (default `http://127.0.0.1:8212`) with `ADMIN_PASSWORD`. It
translates REST `/v1/api/metrics` + `/info` into Prometheus gauges/counters:
`palworld_server_up`, `palworld_server_fps`, `palworld_players_online`,
`palworld_players_max`, `palworld_server_frame_time_ms`,
`palworld_uptime_seconds`, `palworld_world_days`, and `palworld_build_info`
(labelled `version`, `servername`). It runs as the sidecar using the operator
image with args `["exporter"]` (spec 07/08).

## Manager deployment (`config/manager/manager.yaml`)

Namespace `system` (→ `palworld-operator-system` in the default overlay,
labelled `pod-security.kubernetes.io/enforce: restricted`). One replica,
ServiceAccount `controller-manager`. Pod securityContext `runAsNonRoot` +
`RuntimeDefault`; container `allowPrivilegeEscalation: false`,
`readOnlyRootFilesystem: true`, `runAsNonRoot: true`, `capabilities.drop: [ALL]`.

Image `controller:latest`; args `--leader-elect`,
`--health-probe-bind-address=:8081`, `--metrics-bind-address=:8443`,
`--metrics-secure`. Ports `metrics` 8443, `health` 8081; liveness/readiness on
`/healthz` and `/readyz`. Requests cpu `10m` / mem `128Mi`, limit mem `512Mi`.

The limit is sized against the scoped cache above, with headroom for the
unfiltered `Node` informer on large clusters. It is **not** safe to lower it
back toward the cache's steady-state footprint: the initial LIST is the peak, not
the steady state.

Env: `OPERATOR_NAMESPACE` (downward API `metadata.namespace`), `POD_NAME`
(downward API), `OPERATOR_IMAGE` (kept equal to the manager image by a kustomize
replacement in `config/default`), `DEFAULT_SERVER_IMAGE`
(`quay.io/twodcube/palworld-server:latest`).

## Kustomize overlays

| Path | Purpose |
| ---- | ------- |
| `config/default` | Base install: `crd` + `rbac` + `manager` + metrics service. `namespace: palworld-operator-system`, `namePrefix: palworld-operator-`. Replacement syncs `OPERATOR_IMAGE` to the manager image. |
| `config/openshift` | Thin overlay over `default` (`make deploy-openshift`). OpenShift specifics are auto-detected at runtime. |
| `config/webhook` | ValidatingWebhookConfiguration + `webhook-service`. |
| `config/certmanager` | Self-signed issuer + serving cert for the webhook (requires cert-manager). |
| `config/scc` | Optional hardened `palworld-server` SCC (not wired in). |
| `config/prometheus` | ServiceMonitor for the operator's own metrics (opt-in). |
| `config/samples` | Example CRs. |

Images are placeholders: `quay.io/twodcube/palworld-operator` (operator) and
`quay.io/twodcube/palworld-server` (game). The webhook is opt-in and requires the
`webhook` + `certmanager` overlays plus `--enable-webhooks`.

## Makefile

`make install`/`uninstall` (CRDs), `deploy`/`undeploy`, `deploy-openshift`,
`docker-build`/`docker-push`/`docker-buildx`, `manifests`/`generate`, `build`,
`test`, `run`, `lint`, `bundle`. `IMG=` overrides the operator image.
