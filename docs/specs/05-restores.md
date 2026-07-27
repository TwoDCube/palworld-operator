# 05 — Restores

Source: `internal/controller/palworldrestore_controller.go`,
`internal/resources/backup.go`.

The controller probes `openShift` (Route API) once. Terminal phases
(`Completed`, `Failed`) are no-ops. A missing `gameRef` →
`failRestore(GameNotFound)`.

## Source resolution (`resolvePlan`)

- `backupRef` set: Get the `PalworldBackup`; error → `InvalidSource`; if not
  `Completed` → error. If it has `status.volumeSnapshotName` → **snapshot plan**
  (`snapshot = volumeSnapshotName`). Otherwise → **tarball plan**
  (`dest = backup.spec.destination`, `key = <gameRef>/<backup>.tar.gz`).
- `source` set (no `backupRef`): a **direct source**, validated then turned into a
  **tarball plan** (see below).
- Neither set → error.

`resolvePlan` runs in `Reconcile` *before* the phase switch, so every error it
returns becomes a terminal `failRestore(InvalidSource, <message>)` on the first
reconcile — before `stopGame` touches the running server. Source misconfiguration
therefore never costs the operator an outage.

### Direct sources

A direct `source` names an archive that this operator did not write, so nothing
can be derived from a `PalworldBackup`: the object key has to come from the spec.
Unlike the `backupRef` path — where the key is always
`<gameRef>/<backup>.tar.gz` — the field that carries it holds the **full key**,
not a prefix or a parent directory.

| `source.type` | Key field | Plan |
| ------------- | --------- | ---- |
| `VolumeSnapshot` | — | error: requires `backupRef` |
| `S3` | `source.s3.prefix` | `key = prefix`, then `prefix` is cleared so `s3Env` emits `S3_PREFIX=""` + `S3_KEY=<key>` |
| `PVC` | `source.pvcPath` | `key = pvcPath`; the Job runs `restore.sh pvc /backup/<key>` |

Each is validated up front, because every one of these fields ends up in a shell
command inside the restore Job and an empty value there names a *directory*
rather than an archive — `restore.sh` would fail deep inside the Job with
`source /backup/ not found` long after the game had been stopped:

- `S3`: `s3` must be set, with non-empty `bucket`, `credentialsSecret` and
  `prefix`.
- `PVC`: non-empty `pvcName` and `pvcPath`.
- Any other type → error naming the unsupported type.

## State machine

Phase dispatch: `""`/`Pending` → `stopGame`; `Stopping` → `awaitStopped`;
`Restoring` → `awaitRestore`; `Starting` → `startGame`.

### `stopGame`

- If the game's `DesiredReplicas != 0` and `force=false` →
  `failRestore(GameRunning)`.
- Set `startTime` (once). Record `status.originalReplicas = DesiredReplicas(game)`
  (once) so the game is returned to its prior state.
- `scaleGame(0)` (patches `spec.replicas`), phase `Stopping`, requeue 5s.

### `awaitStopped`

- Get the StatefulSet. A non-NotFound read error is returned (requeued) — a
  transient error must **not** be mistaken for "stopped", which would let the
  restore wipe a mounted volume.
- If it still reports `replicas>0` or `readyReplicas>0` → requeue 5s.
- Otherwise set phase `Restoring`, requeue 2s (the actual work runs in
  `awaitRestore`).

### `awaitRestore`

**Tarball** (`awaitTarballRestore`): Get Job `<restore>-restore`. NotFound →
create `DesiredRestoreJob` (owner-refed to the restore), set `status.jobName`,
requeue 10s. `succeeded>=1` → phase `Starting`, requeue 2s. Failed →
`failRestore(RestoreFailed)`. Else requeue 10s. The Job mounts the (now idle)
data PVC `data-<game>-0` at `/palworld` and runs `restore.sh` (spec 07).

**Snapshot** (`awaitSnapshotRestore`): operate on PVC `data-<game>-0`:

- Exists with annotation `palworld.twodcube.io/restored-from == <snapshot>` →
  the restored volume is in place → phase `Starting`, requeue 2s.
- Exists otherwise (the old volume) → delete it (if not already deleting),
  requeue 3s.
- NotFound → create a PVC named `data-<game>-0` cloned from the snapshot, with
  the `restored-from=<snapshot>` annotation. It is **not** owner-refed to the
  restore, so the restored data outlives the restore object. Requeue 3s.

This guarantees the game is only started after the restored PVC exists — the
StatefulSet never provisions an empty volume from its template.

### `startGame`

`scaleGame(status.originalReplicas ?? 1)`; set `completionTime`, phase
`Completed`, condition `Ready=True`, emit a `Restored` event.

`scaleGame(n)` reads a fresh copy of the game and patches `spec.replicas` to `n`
(no-op if already `n`). Restores use no finalizer; the Job is owner-refed and
GC'd. `SetupWithManager` `Owns` Jobs and reads `DEFAULT_SERVER_IMAGE`.

## Restore Job

`DesiredRestoreJob` (`backup.go`): same shape/limits as the backup Job
(`restartPolicy: Never`, `backoffLimit: 3`, `ttl: 3600`, `activeDeadline: 10800`).
Mounts `data-<game>-0` at `/palworld` (and the backup PVC at `/backup` for the
`PVC` source). Runs `/usr/local/bin/restore.sh s3` (env from the source + `AWS_*`)
or `/usr/local/bin/restore.sh pvc /backup/<key>`, where `<key>` is
`<gameRef>/<backup>.tar.gz` for a `backupRef` restore and `source.pvcPath` for a
direct `PVC` source.
