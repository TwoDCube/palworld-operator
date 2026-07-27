# Test deployment: `palworld-temp` on OKD4 (OpenShift 4.22.6)

This document records a **complete, disposable test deployment** of the Palworld
operator into a single OpenShift project, `palworld-temp`, together with one live
`PalworldGame` instance. It exists so the whole thing can be torn down again with
confidence — every object that was created is listed, and the
[Uninstall](#uninstall) section removes all of them.

> **Scope rule for this deployment.** Everything lives in the `palworld-temp`
> project, with exactly two exceptions that are unavoidable for an operator
> install: the three CRDs, and the operator's cluster-scoped RBAC. Both are
> enumerated in [Cluster-scoped footprint](#cluster-scoped-footprint) and removed
> by the uninstall steps. No pre-existing cluster object was modified.

### Defects found while doing this

Two problems in the repository surfaced during the install. **Both are now fixed
in the repo** — this deployment runs the fixed build and needs no workarounds:

1. **The manager's 256Mi memory limit OOMKilled it on a real cluster** — the
   controller-runtime cache was cluster-wide and unfiltered. Fixed by scoping the
   cache to `app.kubernetes.io/managed-by=palworld-operator`
   (`operatorCacheOptions` in `cmd/main.go`, spec 10). Steady-state memory went
   from 290Mi to ~36Mi; the limit is now 512Mi.
   → [details](#defect-1-manager-cache-was-unscoped)
2. **`networking.restAPI.route: true` created a Route that always 503'd** — the
   operator's own NetworkPolicy blocked the ingress router. Fixed by splitting
   the RCON and REST rules and admitting the router to the REST port only
   (`DesiredNetworkPolicy` in `internal/resources/misc.go`, spec 08).
   → [details](#defect-2-the-rest-route-was-blocked-by-the-networkpolicy)

A third issue was found while verifying and is **not** an operator defect; it is
recorded in [MetalLB status churn](#open-issue-metallb-status-churn).

---

## Target environment

Recorded at deploy time (2026-07-27). Verify these before reusing the procedure —
the storage class and load-balancer names in particular are cluster-specific.

| Property | Value | How it was determined |
| -------- | ----- | --------------------- |
| API server | `https://api.okd4.home.zoltanszepesi.com:6443` | `oc whoami --show-server` |
| Product / version | **OpenShift 4.22.6** | `oc get clusterversion version -o jsonpath='{.status.desired.version}'` |
| Release payload | `quay.io/openshift-release-dev/ocp-release@sha256:4b439fab…` | `…jsonpath='{.status.desired.image}'` |
| Nodes | 3 × `control-plane,master,worker`, 31500m CPU / ~62Gi allocatable each | `oc get nodes` |
| Project | `palworld-temp` (pre-created by the user) | `oc get project palworld-temp` |
| Default StorageClass | `ocs-storagecluster-ceph-rbd` (ODF, RWO, CSI) | `oc get storageclass` |
| VolumeSnapshotClass | `ocs-storagecluster-rbdplugin-snapclass` | `oc get volumesnapshotclass` |
| Load balancer | MetalLB, pool `ip-addresspool` = `10.2.100.0/24`, `autoAssign: true` | `oc get ipaddresspools.metallb.io -A` |
| Ingress domain | `apps.okd4.home.zoltanszepesi.com` | Route created below |

> The cluster's DNS name says "okd4", but the release payload is
> `ocp-release`, so this is **OpenShift**, not OKD. The payload is authoritative.

---

## Overview of what gets created

```
cluster scope ──┬── 3 CustomResourceDefinitions   (palworld{games,backups,restores})
                ├── 3 ClusterRoles                (palworld-operator-*)
                └── 2 ClusterRoleBindings         (palworld-operator-*)

palworld-temp ──┬── build plumbing   ImageStreams + BuildConfigs + Builds (2 each)
                ├── operator         Deployment, ServiceAccount, Role, RoleBinding,
                │                    metrics Service, leader-election Lease
                └── game (operator-owned, from one PalworldGame CR)
                                     StatefulSet + Pod, PVC, ServiceAccount,
                                     Secret, ConfigMap, 4 Services, Route,
                                     NetworkPolicy, PodDisruptionBudget
```

---

## Step 1 — Build the images in-cluster

The manifests in `config/` reference `quay.io/twodcube/palworld-operator:latest`
and `quay.io/twodcube/palworld-server:latest`. **Neither image is published**, so
both were built from this repository using OpenShift binary Docker builds. The
builds run inside `palworld-temp` and push to the cluster-internal registry, so
no external registry credentials and no registry Route are needed.

```sh
oc project palworld-temp

# BuildConfig + ImageStream for each image
oc new-build --binary --name=palworld-operator --strategy=docker -n palworld-temp
oc new-build --binary --name=palworld-server   --strategy=docker -n palworld-temp

# Operator image — built from the repo root Dockerfile
oc start-build palworld-operator --from-dir=. --follow -n palworld-temp

# Game server image — built from build/palworld-server/Dockerfile
oc start-build palworld-server --from-dir=build/palworld-server --follow -n palworld-temp
```

Both builds need cluster egress to the internet (Go modules, UBI repos,
`github.com` for tini, `downloads.rclone.org`, and the Steam CDN for the SteamCMD
bootstrap). Observed durations: operator ≈ 6m50s, server ≈ 2m.

Resulting images (pinned **by digest** for the install, so a later rebuild of
`:latest` cannot silently change what is running):

```sh
oc get istag palworld-operator:latest -n palworld-temp -o jsonpath='{.image.dockerImageReference}'
oc get istag palworld-server:latest   -n palworld-temp -o jsonpath='{.image.dockerImageReference}'
```

| Image | Digest reference |
| ----- | ---------------- |
| operator | `image-registry.openshift-image-registry.svc:5000/palworld-temp/palworld-operator@sha256:f56f1b77144cdd8e149d226089ad1fd75028a4f7da26c3ed6d058b9fb2011172` |
| server | `image-registry.openshift-image-registry.svc:5000/palworld-temp/palworld-server@sha256:4962740985c9f97e00b649e192f80b3bf544b03d45fbe8dc3eb815c1c449afe6` |

Pods in `palworld-temp` can pull these without a pull secret: OpenShift grants
`system:image-puller` on the project to `system:serviceaccounts:palworld-temp`.

---

## Step 2 — Install the CRDs and the operator

`config/default` hard-codes the namespace `palworld-operator-system` and ships a
`Namespace` object for it. For this deployment the rendered manifest is
retargeted at `palworld-temp` and the `Namespace` object is **dropped**, so the
user's existing project is not modified (in particular its labels and its
pod-security level are left exactly as they were).

```sh
OP=$(oc get istag palworld-operator:latest -n palworld-temp -o jsonpath='{.image.dockerImageReference}')
SRV=$(oc get istag palworld-server:latest  -n palworld-temp -o jsonpath='{.image.dockerImageReference}')

oc kustomize config/openshift \
  | yq 'select(.kind != "Namespace")' \
  | sed -e "s|palworld-operator-system|palworld-temp|g" \
        -e "s|quay.io/twodcube/palworld-operator:latest|${OP}|g" \
        -e "s|quay.io/twodcube/palworld-server:latest|${SRV}|g" \
  > /tmp/palworld-operator-install.yaml

# Inspect before applying — this is the complete cluster footprint.
yq -r '[.kind, .metadata.name, (.metadata.namespace // "CLUSTER-SCOPED")] | @tsv' \
  /tmp/palworld-operator-install.yaml

oc apply --server-side --force-conflicts -f /tmp/palworld-operator-install.yaml
oc rollout status deploy/palworld-operator-controller-manager -n palworld-temp
```

`sed` on the literal string `palworld-operator-system` is safe here: it appears
only as a `namespace:` value and as the `ClusterRoleBinding` subject namespace.
The `palworld-operator-` **name prefix** is kept, which is what makes every
object identifiable at teardown time.

The 13 objects applied:

| Kind | Name | Scope |
| ---- | ---- | ----- |
| CustomResourceDefinition | `palworldbackups.palworld.twodcube.io` | cluster |
| CustomResourceDefinition | `palworldgames.palworld.twodcube.io` | cluster |
| CustomResourceDefinition | `palworldrestores.palworld.twodcube.io` | cluster |
| ClusterRole | `palworld-operator-manager-role` | cluster |
| ClusterRole | `palworld-operator-metrics-auth-role` | cluster |
| ClusterRole | `palworld-operator-metrics-reader` | cluster |
| ClusterRoleBinding | `palworld-operator-manager-rolebinding` | cluster |
| ClusterRoleBinding | `palworld-operator-metrics-auth-rolebinding` | cluster |
| ServiceAccount | `palworld-operator-controller-manager` | `palworld-temp` |
| Role | `palworld-operator-leader-election-role` | `palworld-temp` |
| RoleBinding | `palworld-operator-leader-election-rolebinding` | `palworld-temp` |
| Service | `palworld-operator-controller-manager-metrics-service` | `palworld-temp` |
| Deployment | `palworld-operator-controller-manager` | `palworld-temp` |

The manager additionally creates a leader-election `Lease` named
`palworld-operator.twodcube.io` in `palworld-temp` at runtime.

The validating webhook is **not** installed: it is opt-in (`--enable-webhooks`
plus the `config/webhook` + `config/certmanager` overlays). CRD OpenAPI
validation still applies. See [spec 10](specs/10-deployment.md).

### Defect 1: manager cache was unscoped

`config/manager/manager.yaml` limited the manager to `memory: 256Mi`. On this
cluster it was **OOMKilled** within three seconds of acquiring the leader lease.

Cause: `cmd/main.go` built the manager with `Cache: cache.Options{}` — the
default cluster-wide, unfiltered informer cache. It watches `Secret`,
`ConfigMap`, `Service`, `ServiceAccount`, `StatefulSet`, `PodDisruptionBudget`,
`NetworkPolicy`, `Job` and `Node` across **all** namespaces. This cluster holds
1,564 Secrets and 1,921 ConfigMaps in 120 namespaces; the initial LIST alone
exceeded 256Mi, and steady state settled at ~290Mi.

**Fixed in the repo.** `operatorCacheOptions()` now restricts the cache to
objects labelled `app.kubernetes.io/managed-by=palworld-operator` — the label
`CommonLabels` puts on everything the operator creates and which user `podLabels`
cannot override. `Node` is deliberately left unfiltered (drain detection reads
arbitrary, unlabelled nodes), and a user-supplied credentials Secret
(`spec.credentials.secretName`) carries no operator label, so it is now read
through the manager's uncached `APIReader`. See spec 10 and spec 09.

Measured on this cluster after the fix: **290Mi → ~36Mi**, zero restarts. The
manifest limit is now `512Mi` (headroom for the unfiltered `Node` informer and
for a fleet of many games — the initial LIST is the peak, not the steady state).

---

## Step 3 — Deploy the Palworld instance

One `PalworldGame` drives everything else. Written to
`/tmp/palworldgame.yaml` and applied:

```yaml
apiVersion: palworld.twodcube.io/v1alpha1
kind: PalworldGame
metadata:
  name: palworld-test
  namespace: palworld-temp
spec:
  version: latest
  replicas: 1

  serverSettings:
    serverName: "palworld-temp test server"
    serverDescription: "Palworld Operator smoke test on OpenShift 4.22"
    serverPlayerMaxNum: 8

  storage:
    size: 30Gi
    storageClassName: ocs-storagecluster-ceph-rbd
    volumeSnapshotClassName: ocs-storagecluster-rbdplugin-snapclass

  networking:
    gamePort: 8211
    queryPort: 27015
    # MetalLB (pool 10.2.100.0/24, autoAssign) serves the UDP game port.
    # UDP cannot traverse a Route/Ingress, so LoadBalancer or NodePort is required.
    serviceType: LoadBalancer
    restAPI:
      route: true

  resources:
    requests:
      cpu: "2"
      memory: 8Gi
    limits:
      memory: 12Gi

  update:
    strategy: Manual
    backupBeforeUpdate: false

  # Backups are off for this test so teardown does not have to chase
  # VolumeSnapshots. See "Optional extras" below to enable them.
  backup:
    enabled: false

  monitoring:
    metricsExporter: true
    serviceMonitor: false

  podDisruptionBudget:
    enabled: true
```

```sh
oc apply -f /tmp/palworldgame.yaml
```

Storage is sized at 30Gi because the PalServer install alone is ~5.2GB
downloaded / ~8GB on disk, and the world save grows alongside it.
`serviceMonitor` is left `false` so nothing is created that would require the
cluster monitoring stack.

### Objects the operator creates from that one CR

All in `palworld-temp`, all owned by the `PalworldGame` (garbage-collected when
it is deleted, except where noted):

| Kind | Name | Notes |
| ---- | ---- | ----- |
| StatefulSet | `palworld-test` | 1 replica; containers `palworld` + `metrics-exporter` |
| Pod | `palworld-test-0` | |
| PersistentVolumeClaim | `data-palworld-test-0` | 30Gi RWO on `ocs-storagecluster-ceph-rbd`. **Created by the StatefulSet, not garbage-collected** — see uninstall. |
| ServiceAccount | `palworld-test` | |
| Secret | `palworld-test-credentials` | operator-generated random admin/server passwords |
| ConfigMap | `palworld-test-config` | rendered `PalWorldSettings.ini` |
| Service | `palworld-test-game` | **LoadBalancer**, UDP 8211 + 27015, external IP `10.2.100.4` |
| Service | `palworld-test-admin` | ClusterIP, TCP 25575 (RCON) + 8212 (REST) — internal only |
| Service | `palworld-test-headless` | StatefulSet governing service |
| Service | `palworld-test-metrics` | ClusterIP, TCP 9877 |
| Route | `palworld-test-rest` | `palworld-test-rest-palworld-temp.apps.okd4.home.zoltanszepesi.com`, edge/Redirect → `palworld-test-admin:rest` |
| NetworkPolicy | `palworld-test` | game/query UDP open; RCON/REST restricted to this namespace + the operator namespace |
| PodDisruptionBudget | `palworld-test` | `minAvailable: 1` |

No hand-made objects: with both defects fixed in the repo, the operator creates
everything the deployment needs.

The one shared cluster resource this consumes is a **MetalLB address from the
`10.2.100.0/24` pool** (`10.2.100.4`), which is released automatically when the
`palworld-test-game` Service is deleted.

### Defect 2: the REST Route was blocked by the NetworkPolicy

With `networking.restAPI.route: true` the operator created the Route, but it
answered **HTTP 503**. The REST API itself was healthy — from inside the
namespace `http://palworld-test-admin.palworld-temp.svc:8212/v1/api/info`
returned 200.

Cause: `DesiredNetworkPolicy` in `internal/resources/misc.go` put RCON and REST
in a single ingress rule whose peers were *same-namespace pods* + *the operator
namespace* only. The OpenShift ingress router runs in `openshift-ingress`, which
matches neither peer, so its traffic to TCP 8212 was dropped. The Route feature
and the NetworkPolicy contradicted each other: `restAPI.route: true` could never
work as shipped.

**Fixed in the repo.** The rule is now split in two:

- **RCON (25575)** keeps exactly the original two peers. It is a raw admin
  channel and must never be reachable from the router.
- **REST (8212)** gets the same two peers plus, *only when
  `spec.networking.restAPI.route` is true*, a peer selecting
  `policy-group.network.openshift.io/ingress` — the label OpenShift maintains on
  router namespaces (so the fix does not hard-code `openshift-ingress`, which is
  configurable).

Splitting matters: a single rule would have forced the router peer onto RCON too.
The peer is derived purely from the spec field, so it is inert on clusters
without the Route API. Verified on this cluster: `rcon_peers=2`, `rest_peers=3`,
and the Route returns **200** with no extra policy in the namespace.

Spec 08 documents the split; `internal/resources/resources_test.go` covers
router-present/absent and asserts RCON never admits the router.

### Verification

```sh
oc get palworldgame palworld-test -n palworld-temp
oc get pod palworld-test-0 -n palworld-temp
oc logs palworld-test-0 -n palworld-temp -c palworld -f
```

First start is slow: SteamCMD downloads ~5.2GB of PalServer into the PVC before
the game process comes up, so `palworld-test-0` sits at `1/2 Ready` for several
minutes (~5 min observed). Subsequent restarts reuse the PVC and are fast.

Observed end state:

```
NAME            PHASE     VERSION           PLAYERS   READY   AGE
palworld-test   Running   v1.0.1.100619               True    5m8s
```

with `status.currentVersion: "24181105"` (Steam build), `status.gameEndpoint:
10.2.100.4:8211`, `status.currentNode: okd4-master-1`, and pod `2/2 Running`,
0 restarts.

What was actually confirmed:

| Check | Result |
| ----- | ------ |
| `PalworldGame` `Ready=True`, `Degraded=False` | pass |
| PalServer listening on UDP 8211 + 27015, TCP 8212 + 25575 (`ss` in the pod) | pass |
| REST `/v1/api/info` in-cluster via `palworld-test-admin` Service | HTTP 200 |
| REST `/v1/api/info` through the Route, no extra policy | HTTP 200, correct `version` / `servername` / `worldguid` |
| NetworkPolicy peers | `rcon_peers=2` (router excluded), `rest_peers=3` (router included) |
| Operator memory / restarts | ~36Mi against a 512Mi limit, 0 restarts |
| Prometheus sidecar `:9877/metrics` | `palworld_server_up 1`, `palworld_server_fps 59`, `palworld_players_max 8`, `palworld_build_info{version="v1.0.1.100619"}` |
| MetalLB L2 announcement | `palworld-test-game` announced from `okd4-master-1` (matches the pod's node — `externalTrafficPolicy: Local`) |
| Game Service EndpointSlice | `10.130.1.151:8211,27015` |

**Not** confirmed: an actual game-client connection over UDP. The MetalLB VIP is
only reachable from hosts on the `10.2.100.0/24` L2 segment, and the VIP does not
answer ICMP (only the declared UDP ports are forwarded), so `ping` is not a valid
test. A Steam `A2S_INFO` probe to `10.2.100.4:27015` from inside the cluster drew
no reply, which is expected here — Palworld only answers Steam queries when the
public/community listing is enabled, which this server does not have. Verifying
playability requires a Palworld client on that segment.

### Connecting

| What | Where |
| ---- | ----- |
| Game client (UDP) | `10.2.100.4:8211` — reachable from the MetalLB L2 network |
| Steam query (UDP) | `10.2.100.4:27015` |
| REST admin API | `https://palworld-test-rest-palworld-temp.apps.okd4.home.zoltanszepesi.com` |
| RCON / REST in-cluster | `palworld-test-admin.palworld-temp.svc:25575` / `:8212` |

Admin credentials (generated by the operator):

```sh
oc get secret palworld-test-credentials -n palworld-temp \
  -o jsonpath='{.data.adminPassword}' | base64 -d; echo
```

The REST API uses HTTP Basic auth with user `admin`:

```sh
PW=$(oc get secret palworld-test-credentials -n palworld-temp -o jsonpath='{.data.adminPassword}' | base64 -d)
curl -sk -u "admin:${PW}" \
  https://palworld-test-rest-palworld-temp.apps.okd4.home.zoltanszepesi.com/v1/api/info
```

### Open issue: MetalLB status churn

Found while verifying the fixes, and recorded here because it is visible on this
cluster and looks alarming. **It is not a palworld-operator defect.**

MetalLB's `controller` Deployment rewrites the `palworld-test-game` Service's
**status subresource** roughly 14 times a second with byte-identical content
(`{"ip":"10.2.100.4","ipMode":"VIP"}`). Evidence:

- `managedFields` shows two writers: `manager` (the operator) owning
  `metadata`+`spec`, whose timestamp **stayed frozen at the Service's creation
  time for over an hour**; and `controller` with `subresource: status`, whose
  timestamp equals "now" on every read.
- Consecutive full-object dumps differ *only* in that timestamp and
  `resourceVersion` — no spec or status content changes.
- MetalLB's controller Deployment is literally named `controller` in
  `metallb-system`, which is where that field-manager name comes from.

Because the game controller does `Owns(&corev1.Service{})`, each of those writes
costs one reconcile, which is why the operator shows a high reconcile rate
(~6.7/s) and ~150m CPU on an otherwise idle server. The operator's own writes are
not the cause: its field-manager entry does not advance.

What was done about it: `reconcileService` and `reconcileUnstructured` now skip
no-op writes (spec 02), so the operator no longer contributes writes of its own.
Diagnosing MetalLB itself is out of scope for this test deployment — it lives in
`metallb-system` and this exercise was scoped to `palworld-temp`. The other four
MetalLB LoadBalancer Services on the cluster (`openshift-storage`) showed **zero**
writes in the same window, so whatever triggers it is specific to this Service
rather than a general MetalLB fault. Worth investigating separately if a
`LoadBalancer` game is run long-term.

### Optional extras (not enabled here)

- **Scheduled backups** — set `spec.backup.enabled: true` with a `schedule` and
  `destination.type: VolumeSnapshot`. This creates `PalworldBackup` CRs and
  `VolumeSnapshot` objects in `palworld-temp`; delete them before the PVC at
  teardown.
- **ServiceMonitor** — `spec.monitoring.serviceMonitor: true` requires the
  Prometheus Operator and user-workload monitoring; it was left off to avoid
  touching anything outside the project.
- **Validating webhook** — needs the `config/webhook` + `config/certmanager`
  overlays and `--enable-webhooks`; adds a cluster-scoped
  `ValidatingWebhookConfiguration`, so it was deliberately skipped.

---

## Cluster-scoped footprint

These are the **only** objects created outside `palworld-temp`. Nothing
pre-existing on the cluster was modified.

| Kind | Name | Why it cannot be namespaced |
| ---- | ---- | --------------------------- |
| CustomResourceDefinition | `palworldgames.palworld.twodcube.io` | CRDs are cluster-scoped by definition. |
| CustomResourceDefinition | `palworldbackups.palworld.twodcube.io` | ” |
| CustomResourceDefinition | `palworldrestores.palworld.twodcube.io` | ” |
| ClusterRole | `palworld-operator-manager-role` | The manager watches `nodes` (cluster-scoped, for drain awareness) and its controller-runtime cache LISTs across all namespaces. |
| ClusterRoleBinding | `palworld-operator-manager-rolebinding` | Binds the above to `palworld-temp/palworld-operator-controller-manager`. |
| ClusterRole | `palworld-operator-metrics-auth-role` | `TokenReview` / `SubjectAccessReview` for the authn/authz-protected metrics endpoint. |
| ClusterRoleBinding | `palworld-operator-metrics-auth-rolebinding` | Binds the above. |
| ClusterRole | `palworld-operator-metrics-reader` | Grants `/metrics`; **no binding** — inert until someone binds it. |

Each grants access only to the operator's own ServiceAccount in `palworld-temp`.

---

## Uninstall

Run in this order. Steps 1–2 must precede step 3, because deleting the CRDs first
would strand the `PalworldGame` finalizer
(`palworld.twodcube.io/finalizer`) and leave the operator unable to clean up.

### 1. Delete the game (operator-driven graceful shutdown)

```sh
oc delete palworldgame palworld-test -n palworld-temp
```

The operator flushes a save via RCON/REST, stops the server, and garbage-collects
the StatefulSet, Services, Route, NetworkPolicy, PDB, ConfigMap, Secret and
ServiceAccount. Wait for it to finish:

```sh
oc get palworldgame -n palworld-temp          # expect: No resources found
oc get pods -n palworld-temp -l app.kubernetes.io/instance=palworld-test
```

If the CR hangs in `Terminating` because the operator is already gone, clear the
finalizer manually:

```sh
oc patch palworldgame palworld-test -n palworld-temp \
  --type=merge -p '{"metadata":{"finalizers":[]}}'
```

Then remove the two things the CR does **not** own.

**a) The PVC.** StatefulSet PVCs are never garbage-collected, and
`spec.storage.retain` was left at its default (`false`), so this is the step that
actually reclaims the 30Gi Ceph volume:

```sh
oc delete pvc data-palworld-test-0 -n palworld-temp
```

**b) Nothing else.** Earlier revisions of this document had you delete a
hand-made `palworld-test-allow-router` NetworkPolicy; that workaround is obsolete
now that the NetworkPolicy defect is fixed in the operator. If you deployed an
older build and it is still present, remove it:

```sh
oc delete networkpolicy palworld-test-allow-router -n palworld-temp --ignore-not-found
```

If backups were enabled at any point, delete those first:

```sh
oc delete palworldbackup,palworldrestore --all -n palworld-temp
oc delete volumesnapshot --all -n palworld-temp
```

### 2. Remove the operator and its cluster RBAC

```sh
oc delete deploy/palworld-operator-controller-manager -n palworld-temp
oc delete svc/palworld-operator-controller-manager-metrics-service -n palworld-temp
oc delete sa/palworld-operator-controller-manager -n palworld-temp
oc delete role/palworld-operator-leader-election-role -n palworld-temp
oc delete rolebinding/palworld-operator-leader-election-rolebinding -n palworld-temp
oc delete lease/palworld-operator.twodcube.io -n palworld-temp

# cluster-scoped RBAC
oc delete clusterrole palworld-operator-manager-role \
                      palworld-operator-metrics-auth-role \
                      palworld-operator-metrics-reader
oc delete clusterrolebinding palworld-operator-manager-rolebinding \
                             palworld-operator-metrics-auth-rolebinding
```

Equivalently, if `/tmp/palworld-operator-install.yaml` from step 2 still exists,
`oc delete -f /tmp/palworld-operator-install.yaml` removes all 13 objects
including the CRDs — but only run that **after** step 1.

### 3. Remove the CRDs

Deleting a CRD deletes every CR of that kind cluster-wide. Confirm none remain
anywhere first:

```sh
oc get palworldgames,palworldbackups,palworldrestores -A

oc delete crd palworldgames.palworld.twodcube.io \
              palworldbackups.palworld.twodcube.io \
              palworldrestores.palworld.twodcube.io
```

### 4. Remove the build plumbing and images

```sh
oc delete bc palworld-operator palworld-server -n palworld-temp
oc delete is palworld-operator palworld-server -n palworld-temp
oc delete build palworld-operator-1 palworld-server-1 -n palworld-temp 2>/dev/null
oc delete pod -l openshift.io/build.name -n palworld-temp 2>/dev/null
oc delete cm -n palworld-temp \
  palworld-operator-1-ca palworld-operator-1-global-ca palworld-operator-1-sys-config \
  palworld-server-1-ca   palworld-server-1-global-ca   palworld-server-1-sys-config 2>/dev/null
```

Deleting the ImageStreams removes the pushed layers from the internal registry
(reclaimed by the registry pruner).

### 5. Verify nothing is left

```sh
# Nothing palworld-related anywhere outside the project:
oc get crd -o name              | grep palworld            # expect: empty
oc get clusterrole -o name      | grep palworld-operator   # expect: empty
oc get clusterrolebinding -o name | grep palworld-operator # expect: empty

# Project back to just its default service accounts / configmaps.
# Expect no palworld-* rows; `builder`/`default`/`deployer`/`pipeline` service
# accounts and their dockercfg secrets are pre-existing project defaults, as are
# the kube-root-ca.crt / openshift-service-ca.crt / config-*-cabundle configmaps.
oc get all,pvc,secret,cm,sa,route,networkpolicy,pdb -n palworld-temp

# MetalLB address returned to the pool:
oc get svc -A | grep 10.2.100.4                            # expect: empty
```

### 6. Delete the project (optional)

The project was created by the user, so this is left as a deliberate final step:

```sh
oc delete project palworld-temp
```

That alone removes every namespaced object above in one shot — but it does **not**
remove the CRDs or the cluster RBAC from steps 2–3, and it skips the operator's
graceful save in step 1. Do steps 1–3 first regardless.
