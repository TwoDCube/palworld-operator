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
`/healthz` and `/readyz`. Requests cpu `10m` / mem `128Mi`, limit mem `256Mi`.

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
