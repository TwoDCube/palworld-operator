# Palworld Operator — Architecture

This document describes the design of the Palworld operator: its API, the
controllers, the reconcile flows for day-2 operations, and the OpenShift
integration.

## Goals

- Run **hundreds of independent** Palworld dedicated servers on one cluster.
- Expose **every** Palworld server setting declaratively.
- Provide **production day-2 operations**: backups, restores, updates, graceful
  lifecycle, monitoring — without manual intervention.
- Be a **first-class OpenShift citizen**: restricted-v2 SCC, Routes, CSI
  snapshots, MetalLB, ODF/NooBaa, Prometheus.

## API (`palworld.twodcube.io/v1alpha1`)

### `PalworldGame`

The primary resource. One `PalworldGame` maps to exactly one authoritative
world backed by one `StatefulSet` (0 or 1 replicas) and one PVC.

Key spec groups:

- `version`, `replicas`, `image` — what to run and how many (0=stopped, 1=running).
- `serverSettings` — the full `PalWorldSettings.ini` surface (see below).
- `credentials` — a Secret reference (or auto-generated) for admin/join passwords.
- `storage` — size, storage class, access modes, retention, snapshot class.
- `networking` — game/query ports, service type (ClusterIP/NodePort/LoadBalancer),
  REST API Route, public IP/port.
- `resources`, `scheduling` — compute and placement.
- `backup`, `update` — day-2 policy.
- `monitoring`, `podDisruptionBudget` — observability and availability.

Status surfaces `phase`, standard `conditions` (`Ready`, `Progressing`,
`Degraded`, `UpdateAvailable`, `BackupReady`), live `playersOnline`,
`currentVersion`/`availableVersion`, endpoints, and backup/update timing. A
`scale` subresource maps `spec.replicas` so `kubectl scale` works.

### `PalworldBackup`

A single point-in-time backup referencing a `PalworldGame`. Destinations:
`VolumeSnapshot` (default), `S3`, or `PVC`. Supports application-consistent
flushing, retention marking, and TTL.

### `PalworldRestore`

Restores a `PalworldBackup` (or an external source) into a `PalworldGame`.

## Controllers

All three reconcilers are idempotent, use owner references for garbage
collection, and probe optional cluster APIs (Route, ServiceMonitor,
VolumeSnapshot) once so the operator degrades gracefully where they are absent.

### PalworldGame reconciler

On each reconcile it:

1. Ensures a finalizer (`palworld.twodcube.io/finalizer`).
2. Ensures owned resources, in order: credentials Secret (generated once),
   ServiceAccount, ConfigMap (rendered settings), Services (headless / public
   game / internal admin / metrics), PodDisruptionBudget, NetworkPolicy,
   ServiceMonitor (if enabled), Route (if enabled), and the StatefulSet.
3. Refreshes observed status by reading the StatefulSet and querying the live
   REST API for players/version.
4. Runs the **update** state machine and the **scheduled backup** scheduler.
5. Requeues (default 60s) for periodic status/version/backup checks.

The ConfigMap carries a stable settings hash that is stamped as a pod-template
annotation, so any settings change triggers a rolling restart. Secret values are
injected at runtime via placeholder tokens (`__PALWORLD_ADMIN_PASSWORD__`) — no
plaintext password ever lands in the ConfigMap.

Immutable fields are respected: the StatefulSet's selector, service name, and
volume claim templates are only set on creation; Services preserve their
server-assigned `ClusterIP` and auto-allocated `NodePort`s across updates.

### Update state machine

```
poll Steam (steamcmd.net) every pollIntervalMinutes -> status.availableVersion
first Running boot                                   -> status.currentVersion := available
available != current                                 -> UpdateAvailable condition
  Manual     -> surface only (event)
  Automatic  -> performUpdate now
  Scheduled  -> performUpdate at next cron tick
performUpdate: [backupBeforeUpdate] -> announce -> delete pod (preStop saves)
               -> StatefulSet recreates -> entrypoint installs latest build
               -> currentVersion := available
```

`currentVersion`/`availableVersion` are Steam **build ids**, so the comparison
converges rather than oscillating.

### Backup state machine

```
Pending  -> flush save (REST) -> create VolumeSnapshot          -> Snapshotting
Snapshotting -> snapshot ready?
   VolumeSnapshot dest -> Completed (artifact = the snapshot)
   S3 / PVC dest       -> clone PVC from snapshot + upload Job   -> Uploading
Uploading -> Job succeeded -> Completed (+ delete transient snapshot/clone PVC)
          -> Job failed    -> Failed
```

Using a snapshot as the source for S3/PVC exports avoids read contention with
the live `ReadWriteOnce` volume. When the cluster has no snapshot API, tarball
exports fall back to mounting a `ReadWriteMany` data volume directly; snapshot
backups require CSI snapshots.

### Restore state machine

```
Pending  -> scale game to 0 (requires force if running)   -> Stopping
Stopping -> StatefulSet drained?
   snapshot source -> delete data PVC, recreate from snapshot -> Starting
   tarball source  -> restore Job onto the (freed) data PVC   -> Restoring
Restoring -> Job succeeded -> Starting
Starting -> scale game to 1 -> Completed
```

Deletion (finalizer): sets `Terminating`, takes a final backup if
`backup.onDelete` (bounded by a deadline so it never wedges), deletes the data
PVC unless `storage.retain`, then removes the finalizer.

## The game server image

`build/palworld-server` builds an OpenShift-friendly image on UBI9
(`ubi9/ubi`):

- **Arbitrary UID**: every writable path (SteamCMD install, save dir, config,
  HOME) is group-root (GID 0) writable with the setgid bit; the entrypoint
  appends a `passwd` entry for the random UID (via a group-writable
  `/etc/passwd`) so SteamCMD works.
- **Entrypoint**: installs/updates via SteamCMD onto the PVC, renders
  `PalWorldSettings.ini` (injecting secret passwords from env), launches
  `PalServer.sh` with the performance flag trio, and traps `SIGTERM` for a
  graceful `save`+`shutdown`.
- **Ops scripts**: `backup.sh`/`restore.sh` (tar + rclone to S3/PVC) run inside
  operator-scheduled Jobs; `healthcheck.sh` powers startup/liveness/readiness
  probes (the startup probe tolerates the multi-minute first install).

The operator connects to each server's **internal** admin Service for RCON/REST
to read players/metrics, flush saves, broadcast, and drive graceful restarts.

## Settings generation

The `PalworldServerSettings` type (`api/v1alpha1/palworldsettings_types.go`) is
generated from an authoritative catalog of the 119 `OptionSettings` keys shipped
in Palworld 1.0's `DefaultPalWorldSettings.ini`. Each field carries a
`pal:"<IniKey>,<kind>,<quote>"` tag that is the **single source of truth** for
the INI renderer (`internal/settings`), which reflects over it to emit the
canonical single-line `OptionSettings=(...)` tuple with correct per-type quoting
(strings quoted, enums/bools/numbers bare, `CrossplayPlatforms` as a
parenthesized list). Operator-managed keys (passwords, public address, RCON/REST
enablement and ports) are excluded from the user surface and injected at render
time.

## Security

- Pod security: `runAsNonRoot`, `seccompProfile: RuntimeDefault`, drop `ALL`
  capabilities, no privilege escalation. On OpenShift, UID/GID/fsGroup are left
  to the SCC; on vanilla Kubernetes they are pinned (UID 10000, GID/fsGroup 0)
  so the group-root-writable volume is usable.
- Network: a `NetworkPolicy` opens only the game/query UDP ports to the world
  and restricts RCON/REST/metrics to the same namespace and the operator.
- Credentials: generated randomly, stored only in a Secret, injected into the
  server at runtime — never written to a ConfigMap or the CR.
- Admission: a validating webhook (optional, cert-manager-backed) rejects
  INI-breaking values and invalid cron/backup configuration; CRD OpenAPI
  validation (enums, ranges, required fields) applies unconditionally.
