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
| Admin | `<name>-admin` | ClusterIP | rcon, rest (TCP) | **Internal only** — the operator's admin channel; `publishNotReadyAddresses: true`. |
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
2. **rcon TCP** (25575) — only from same-namespace pods (`podSelector: {}`) and
   the operator namespace (`namespaceSelector` on
   `kubernetes.io/metadata.name = <OPERATOR_NAMESPACE>`). RCON is a raw,
   unauthenticated-until-login admin channel and is **never** reachable from the
   ingress router, regardless of `restAPI.route`.
3. **rest TCP** (8212) — the same two peers as rcon, **plus**, when
   `networking.restAPI.route` is true, the OpenShift ingress router
   (`namespaceSelector` on `policy-group.network.openshift.io/ingress`, the label
   OpenShift maintains on router namespaces).
4. **metrics TCP** — open to all sources (Prometheus usually lives in a
   different namespace whose identity is not known here; metrics carry only
   non-sensitive data). Restrict further with your own policy if required.

Rules 2 and 3 are deliberately separate. A single rule covering both ports would
force the router peer onto RCON as well; splitting them keeps the router grant
scoped to the one port the Route actually targets.

The operator itself speaks **only REST** — every live interaction (player counts,
`info`, `save`, `announce`, `shutdown`) goes over 8212, from the controllers
(spec 02/03/04) and from `graceful-shutdown.sh` (spec 07). Rule 2 exists because
the port is enabled in the rendered INI (spec 06) and administrators use it
directly; `internal/palworld.RCONClient` implements that channel but is not wired
into any controller path.

Without rule 3's router peer a Route created by `restAPI.route: true` is
answered with **HTTP 503** — the router runs in `openshift-ingress` and matches
neither of the other two peers. The peer is added purely from the spec field, so
it is inert on clusters without the Route API (the selector matches no
namespace) and no Route is created there anyway.

## Route (`<name>-rest`, OpenShift only)

Created only when `networking.restAPI.route` is true **and** the cluster has the
Route API. Built as an unstructured `route.openshift.io/v1 Route` targeting
Service `<name>-admin` port `rest`, `host` from `networking.restAPI.host`.
Termination is always `edge` with `insecureEdgeTerminationPolicy: Redirect` —
the REST backend is plain HTTP, so `reencrypt`/`passthrough` are invalid and the
webhook rejects them (spec 09). `status.routeURL` reflects the Route host.
