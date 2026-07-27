# 08 — Networking

Source: `internal/resources/service.go`, `internal/resources/misc.go`
(NetworkPolicy), `internal/resources/route.go`, `internal/controller/palworldgame_status.go`
(endpoint derivation).

## Container ports

Server container: `game` 8211/UDP, `query` 27015/UDP, `rcon` 25575/TCP, `rest`
8212/TCP (names are the `targetPort` referents). Metrics sidecar: `metrics`
9877/TCP.

## Services

| Service | Name | Type | Ports | Purpose |
| ------- | ---- | ---- | ----- | ------- |
| Headless | `<name>-headless` | ClusterIP `None` | game, query, rcon, rest | StatefulSet governing service; `publishNotReadyAddresses: true`. |
| Game | `<name>-game` | `networking.serviceType` (default ClusterIP) | game, query (UDP) | **Public** player traffic. |
| Admin | `<name>-admin` | ClusterIP | rcon, rest (TCP) | **Internal only** — the operator's RCON/REST channel; `publishNotReadyAddresses: true`. |
| Metrics | `<name>-metrics` | ClusterIP | metrics (TCP) | Scrape target; only when `monitoring.metricsExporter` and `OPERATOR_IMAGE` set. Carries `app.kubernetes.io/component=metrics`. |

The game Service uses `externalTrafficPolicy: Local` for `NodePort`/
`LoadBalancer` (client-IP preservation), pins `nodePort` when set, and applies
`loadBalancerIP` / `loadBalancerClass` / `serviceAnnotations`. The admin Service
is **never** exposed externally.

UDP cannot traverse an OpenShift Route or Kubernetes Ingress, so the game port
must be a `LoadBalancer` (e.g. MetalLB on-prem) or `NodePort`.

## `status.gameEndpoint`

Derived from the game Service: `LoadBalancer` → `<ingress ip|hostname>:<gamePort>`
(or `<pending-loadbalancer>`); `NodePort` → `<node-ip>:<nodePort>`; `ClusterIP`
→ `<clusterIP>:<gamePort>`.

## NetworkPolicy (`<name>`)

`podSelector` = `SelectorLabels`; ingress rules:

1. **game + query UDP** — open to all sources (no `from`).
2. **rcon + rest TCP** — only from same-namespace pods (`podSelector: {}`) and
   the operator namespace (`namespaceSelector` on
   `kubernetes.io/metadata.name = <OPERATOR_NAMESPACE>`).
3. **metrics TCP** — open to all sources (Prometheus usually lives in a
   different namespace whose identity is not known here; metrics carry only
   non-sensitive data). Restrict further with your own policy if required.

## Route (`<name>-rest`, OpenShift only)

Created only when `networking.restAPI.route` is true **and** the cluster has the
Route API. Built as an unstructured `route.openshift.io/v1 Route` targeting
Service `<name>-admin` port `rest`, `host` from `networking.restAPI.host`.
Termination is always `edge` with `insecureEdgeTerminationPolicy: Redirect` —
the REST backend is plain HTTP, so `reencrypt`/`passthrough` are invalid and the
webhook rejects them (spec 09). `status.routeURL` reflects the Route host.
