# 04 — Backups

Sources: `internal/controller/palworldgame_backup.go` (scheduling, run from the
game reconcile), `internal/controller/palworldbackup_controller.go` (the
`PalworldBackup` state machine), `internal/resources/backup.go` (objects).

## Scheduled backups (game controller)

`reconcileScheduledBackups`:

- No-op (and clears `status.nextScheduledBackup`) when `backup` is nil,
  `enabled=false`, `suspend=true`, or `schedule==""`.
- Parses `schedule` with `cron.ParseStandard` (5-field cron); an invalid
  expression is a reconcile error.
- If `status.nextScheduledBackup` is nil, set it to `sched.Next(now)` and run
  retention.
- If `now` is past `nextScheduledBackup`: create a `PalworldBackup` named
  `<game>-scheduled-<unixtime>` labeled `palworld.twodcube.io/scheduled-backup=true`,
  owner-refed to the game, `destination` from the policy, `flushSave: true`;
  then set `nextScheduledBackup = sched.Next(now)`.

`enforceBackupRetention`: `retention` default `7`; `≤0` keeps all. Lists backups
labeled `scheduled-backup=true` for the game, keeps `Completed` non-`retain`
ones, and if more than `retention` exist, deletes the oldest beyond the count
(sorted by `completionTime` desc).

`observeBackups` (spec 02) copies the newest `Completed` backup's time/name into
`status.lastBackupTime` / `status.lastBackupName`.

## PalworldBackup state machine

Once per process the controller probes `openShift` (Route API) and `hasSnapshot`
(VolumeSnapshot API). Terminal phases (`Completed`, `Failed`) only run
`handleTTL`: if `ttlSecondsAfterFinished` is set, the object is deleted that long
after `completionTime` (otherwise requeued until the deadline). A missing
`gameRef` fails the backup (`GameNotFound`).

Phase dispatch: `""`/`Pending` → `start`; `Saving`/`Snapshotting` →
`reconcileSnapshot`; `Uploading` → `reconcileUpload`.

### `start`

1. Set `startTime` (once); `serverVersion = game.status.currentVersion`.
2. If `flushSave`: phase `Saving`; issue REST `POST /v1/api/save` (best-effort);
   on success `sleep 2s` to let the write settle.
3. If `hasSnapshot`: create a `VolumeSnapshot` named `<backup>` of PVC
   `data-<game>-0` (owner-refed to the backup; `volumeSnapshotClassName` from the
   game's storage spec if set); set `status.volumeSnapshotName`; phase
   `Snapshotting`; requeue 5s.
4. Else (no snapshot API): if destination is `VolumeSnapshot` → fail
   (`NoSnapshotSupport`); if the data volume is not `ReadWriteMany` → fail; else
   `createUploadJob` directly against the live PVC `data-<game>-0`.

### `reconcileSnapshot`

- Get the snapshot. NotFound → `fail(SnapshotMissing)` if `timedOut`, else
  requeue 5s (the snapshot is not watched, so NotFound must self-requeue). Other
  error → return it.
- `SnapshotReady` reads `status.readyToUse` and `status.restoreSize`. Not ready →
  requeue 10s. Ready → `status.sizeBytes = restoreSize`.
- Destination `VolumeSnapshot` → `location = volumesnapshot://<ns>/<name>`,
  `complete`.
- Destination `S3`/`PVC` → create a clone PVC `<backup>-src` from the snapshot
  (owner-refed), then `createUploadJob` against it.

### `createUploadJob` / `reconcileUpload`

`createUploadJob` builds `DesiredBackupJob` (name `<backup>-upload`, image =
resolved server image), owner-refs it to the backup, creates it, sets
`status.jobName`, phase `Uploading`, requeue 10s.

`reconcileUpload`: Get the Job. NotFound → `fail(UploadJobMissing)` if
`timedOut`, else requeue 10s. `succeeded>=1` → set `location` (see below),
`cleanupTransient`, `complete`. Failed → `cleanupTransient`, `fail(UploadFailed)`.
Else requeue 10s.

`location`: `s3://<bucket>/<prefix>/<game>/<backup>.tar.gz` or
`pvc://<pvcName>/<game>/<backup>.tar.gz`.

`cleanupTransient` deletes the clone PVC `<backup>-src` and the (transient)
snapshot — only relevant for the S3/PVC paths; a `VolumeSnapshot`-destination
backup keeps its snapshot as the artifact.

`timedOut` = `startTime` older than `backupPhaseTimeout` (45m).

## Backup Job

`DesiredBackupJob` (`backup.go`): `restartPolicy: Never`, `backoffLimit: 3`,
`ttlSecondsAfterFinished: 3600`, `activeDeadlineSeconds: 10800` (3h). Mounts the
source PVC at `/palworld` (and, for `PVC` destination, the backup PVC at
`/backup`). Runs `/usr/local/bin/backup.sh s3` (env from the S3 destination +
`AWS_*` from `credentialsSecret`) or `/usr/local/bin/backup.sh pvc
/backup/<game>/<backup>.tar.gz`. Backs up `${STEAMAPPDIR}/Pal/Saved`.

`SetupWithManager` `Owns` Jobs. `DEFAULT_SERVER_IMAGE` env selects the image when
the game does not set one.
