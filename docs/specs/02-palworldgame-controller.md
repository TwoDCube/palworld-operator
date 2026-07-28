# 02 — PalworldGame controller

Source: `internal/controller/palworldgame_*.go`, `internal/resources/*.go`.

## Reconcile flow

`Reconcile` (`palworldgame_controller.go`) runs, in order:

1. `Get` the `PalworldGame`; NotFound → return (ignored).
2. `probeCapabilities` — once per process (`sync.Once`): sets `hasRoute` and
   `hasServiceMonitor` by asking the RESTMapper for `route.openshift.io/v1
   Route` and `monitoring.coreos.com/v1 ServiceMonitor`. `hasRoute` also serves
   as the "is OpenShift" signal for security contexts.
3. If `DeletionTimestamp` is set → `reconcileDelete` (see below).
4. If the finalizer `palworld.twodcube.io/finalizer` is absent → append it,
   `Update`, and requeue.
5. `reconcileResources` — ensure every owned object (order below). On error:
   set `Progressing=True/ReconcileError`, best-effort status update, return err.
6. `reconcileObservedStatus` — refresh observed state (best-effort, never errors).
7. `reconcileNodeDrain` — maintain `status.currentNode` and gracefully migrate
   off a cordoned/draining node (spec 11).
8. `reconcileUpdates` — version poll + rollout (spec 03).
9. `reconcileScheduledBackups` — scheduled-backup planner + retention (spec 04).
10. `updateStatus` — `Status().Update`; on conflict, requeue.
11. Requeue after `requeueInterval` (60s), or sooner if `reconcileUpdates` or
    `reconcileNodeDrain` returned a shorter `RequeueAfter`.

## Owned resources and names

`reconcileResources` calls these in this exact order:

| Step | Object | Name | Builder | Notes |
| ---- | ------ | ---- | ------- | ----- |
| `ensureCredentials` | Secret | `<name>-credentials` | `DesiredGeneratedSecret` | Only if `credentials.secretName` is empty. **Create-once**: generated with a random 24-char password only when absent; never overwritten. Sets `status.credentialsSecret`. |
| `ensureServiceAccount` | ServiceAccount | `<name>` | `DesiredServiceAccount` | Skipped if `spec.serviceAccountName` is set. |
| `ensureConfigMap` | ConfigMap | `<name>-config` | `DesiredConfigMap` | `PalWorldSettings.ini` (+ `Engine.ini` if `engineSettings`). CreateOrUpdate. |
| `ensureServices` | Service ×3–4 | see 08 | — | headless, game, admin, and metrics (if `monitoring.metricsExporter` and `OPERATOR_IMAGE` set). |
| `ensurePodDisruptionBudget` | PodDisruptionBudget | `<name>` | `DesiredPodDisruptionBudget` | Deleted if `podDisruptionBudget.enabled=false`. |
| `ensureNetworkPolicy` | NetworkPolicy | `<name>` | `DesiredNetworkPolicy` | See 08. |
| `ensureMonitoring` | ServiceMonitor | `<name>` | `DesiredServiceMonitor` | Only if `monitoring.serviceMonitor` **and** `hasServiceMonitor`. Applied as unstructured. |
| `ensureRoute` | Route | `<name>-rest` | `DesiredRoute` | Only if `networking.restAPI.route` **and** `hasRoute`. Unstructured. |
| `ensureStatefulSet` | StatefulSet | `<name>` | `DesiredStatefulSet` | See below. |

Other names (`internal/resources/meta.go`): headless service `<name>-headless`,
game service `<name>-game`, admin service `<name>-admin`, metrics service
`<name>-metrics`, data PVC `data-<name>-0`.

All owned objects carry `CommonLabels`: `app.kubernetes.io/name=palworld`,
`app.kubernetes.io/instance=<name>`, `app.kubernetes.io/managed-by=palworld-operator`,
`app.kubernetes.io/component=server`, `app.kubernetes.io/part-of=palworld`,
`palworld.twodcube.io/game=<name>`. `PodLabels` are applied first so these
reserved keys always win. `SelectorLabels` (the immutable pod selector) is
`{name, instance, component=server}`.

All owned objects get a controller owner reference to the `PalworldGame` (so
they are garbage-collected with it), **except** the data PVC (created via the
StatefulSet volume claim template — not GC'd automatically; see deletion).

## StatefulSet

`DesiredStatefulSet` (`internal/resources/statefulset.go`):

- `replicas` = `DesiredReplicas` (0 if `spec.replicas<=0`, else 1).
- `serviceName` = `<name>-headless`; `podManagementPolicy: OrderedReady`;
  `updateStrategy: RollingUpdate`.
- `selector` = `SelectorLabels` (immutable).
- Pod template annotation `palworld.twodcube.io/settings-hash` = `SettingsHash`
  (spec 06) so any settings change rolls the pod. `podAnnotations` merged in.
- `terminationGracePeriodSeconds` = `spec.terminationGracePeriodSeconds` when
  set, else **`shutdown.warnSeconds + 300`** (600s with defaults). The player
  countdown runs inside the `preStop` hook (spec 07) and the kubelet's grace
  clock covers `preStop`, so the budget must outlast the countdown or the pod is
  `SIGKILL`ed mid-save; the extra 300s covers the REST save and clean exit.
- One container `palworld` (image, env, ports, probes, lifecycle — spec 07), an
  optional `metrics-exporter` sidecar (spec 07/08) when
  `monitoring.metricsExporter` and `OPERATOR_IMAGE` set, then `spec.sidecars`.
- Volumes: `config` (the ConfigMap) mounted read-only at `/config`; `data` from
  the volume claim template mounted at `/palworld`.
- `volumeClaimTemplates`: one PVC named `data`, size (default `20Gi`),
  `accessModes` (default `[ReadWriteOnce]`), `storageClassName`.
- Compute defaults: if `resources.requests` omit cpu/memory, they default to
  `2` / `8Gi`. Only *requests* are defaulted — the operator never invents a
  limit, so the out-of-the-box pod is `Burstable`.

### Pod QoS and CPU pinning

A pod is `Guaranteed` only when **every** container sets `requests == limits` for
both cpu and memory, and only a `Guaranteed` pod's integer-cpu containers get
exclusive cores from the kubelet's `static` CPU Manager policy. QoS is therefore
a *pod-wide* property that any single container can veto.

The metrics-exporter sidecar is injected by the operator, not by the user, so its
resources are **QoS-neutral**: `requests == limits` for cpu and memory
(`exporterResources`, default `100m` / `64Mi`). Previously it requested `10m`/`32Mi`
with a memory-only limit, which downgraded every pod to `Burstable` and made CPU
pinning impossible to express through this API — the game container's own
settings could not recover it.

Neutral means the sidecar never *forces* Guaranteed either: if the game container
is Burstable-shaped, the pod is still Burstable. The sidecar's cpu stays
fractional deliberately, so it draws from the shared pool and only the game
container takes exclusive cores.

To pin cores, set matching `spec.resources` requests/limits with an integer cpu
on a node running `cpuManagerPolicy: static`:

```yaml
resources:
  requests: {cpu: "4", memory: 16Gi}
  limits:   {cpu: "4", memory: 16Gi}
```

`spec.sidecars` entries are user-supplied and left untouched; an unmatched one
downgrades the pod, so the webhook warns about it (spec 01).

`ensureStatefulSet` uses `CreateOrUpdate`. **On create** it sets the whole spec.
**On update** it mutates only `replicas`, `template`, `updateStrategy`, and
`minReadySeconds` — never `selector`, `serviceName`, `podManagementPolicy`, or
`volumeClaimTemplates` (which are immutable). Changing `storage.size` therefore
does **not** resize an existing volume.

## Service reconciliation

`reconcileService` (`helpers.go`) preserves server-assigned fields across
updates: `clusterIP`/`clusterIPs` are kept from the live object, and a port's
`nodePort` is kept when the desired value is `0`. It copies `type`, `selector`,
`ports`, `publishNotReadyAddresses`, `externalTrafficPolicy`, and load-balancer
fields from the desired object.

It then **compares the mutated object against the copy it read and issues no
`Update` at all when they are semantically equal.** Without the guard the
operator re-`PUT`s all 3–4 Services on every reconcile. Those writes are normally
no-ops server-side, but they are real API traffic, and they make the operator an
active participant in any write amplification around a `Service` that another
controller co-owns (any `LoadBalancer`). With the guard, the operator's field
manager entry on a steady-state Service stops advancing entirely — verified on a
live cluster, where the operator's `managedFields` entry stayed frozen at the
creation timestamp for over an hour while the game ran.

`reconcileUnstructured` (Route, ServiceMonitor) applies the same
compare-before-write guard. It compares only the fields this operator manages
(labels, annotations, ownerReferences, `spec`) because the live object also
carries status and defaults written by other controllers.

> The guard bounds what the operator writes; it does not stop a *third party*
> from rewriting a Service. On the OKD4 test cluster MetalLB's `controller`
> rewrites the game Service's **status subresource** ~14×/s with byte-identical
> content (`ipMode: VIP`), and because the controller `Owns(&corev1.Service{})`,
> each of those writes costs a reconcile. That is a MetalLB-side behaviour, not
> an operator defect — the operator's own entry is frozen — but it is why the
> reconcile rate on a `LoadBalancer` game can look pathological.

## Status derivation

`reconcileObservedStatus` sets `selector`, `persistentVolumeClaim` (`data-<name>-0`),
`restEndpoint` (`<name>-admin.<ns>.svc:8212`), `maxPlayers`, `gameEndpoint`
(spec 08), `routeURL`, and the newest completed backup (`observeBackups`). Then:

- StatefulSet NotFound → `replicas=0`, phase `Pending`, `Ready=False/Pending`.
- StatefulSet read error → return without changing phase.
- `replicas = sts.status.readyReplicas`.
- `DesiredReplicas==0` → phase `Stopped`, `playersOnline=0`, `Ready=False`,
  `Progressing=False`.
- `readyReplicas<1` → phase `Installing` (if `currentVersion==""`) or `Updating`;
  `Ready=False`, `Progressing=True`.
- `readyReplicas>=1` → `observeLive`: query REST `/v1/api/info`
  (`serverName`, `serverVersion`) and `/v1/api/metrics` (`playersOnline`,
  `maxPlayers`); phase `Running`; `Ready=True`; clear `Progressing`; `Degraded=False`.

Phase values `BackingUp`, `Restoring`, `Degraded` are defined in the API but are
**not** currently emitted by this controller; `Degraded` is surfaced as a
condition, not a phase.

## Deletion (`reconcileDelete`)

Runs only while the finalizer is present:

1. Set phase `Terminating`.
2. If `backup.onDelete` is true: `ensureFinalBackup` — create (once) a
   `PalworldBackup` named `<name>-final` (`retain: true`, **not** owner-refed so
   it survives), and requeue until it is `Completed` or `Failed`. Bounded by
   `finalBackupMaxWait` (15m from `DeletionTimestamp`), after which deletion
   proceeds regardless.
3. If `storage.retain` is false: delete the data PVC `data-<name>-0`.
4. Remove the finalizer and `Update`. Owned objects are then GC'd by owner refs.

## SetupWithManager

`Owns`: StatefulSet, Service, ConfigMap, Secret, ServiceAccount,
PodDisruptionBudget, NetworkPolicy, PalworldBackup. `Watches` Nodes via the
`gamesOnNode` map function to react to cordons (spec 11). Reads env once:
`OPERATOR_NAMESPACE` (default `palworld-operator-system`), `OPERATOR_IMAGE` (no
default — empty disables the metrics sidecar), `DEFAULT_SERVER_IMAGE` (default
`quay.io/twodcube/palworld-server:latest`), `STEAM_INFO_ENDPOINT` (empty → the
poller default, spec 03).
