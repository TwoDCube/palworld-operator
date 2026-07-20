# 03 — Updates

Source: `internal/controller/palworldgame_update.go`, `internal/palworld/steam.go`.

## Version namespaces

- `status.currentVersion` and `status.availableVersion` are **Steam build ids**
  (numeric strings). They are the only values compared for update decisions.
- `status.serverVersion` is the in-game version string (e.g. `v0.3.5`) from the
  REST API, set by `observeLive` (spec 02); it is display-only and never used in
  update logic.

`reconcileUpdates` is a no-op when `spec.update` is nil.

## Build-version poll

`SteamPoller.LatestBuildID(ctx, "public")` (`steam.go`) issues
`GET <endpoint>/2394010` and reads
`data["2394010"].depots.branches["public"].buildid`. Endpoint =
`STEAM_INFO_ENDPOINT` env, default `https://api.steamcmd.net/v1/info`
(`DefaultSteamInfoEndpoint`). App id `2394010` (`PalworldSteamAppID`).

Poll gating: interval = `update.pollIntervalMinutes` minutes (default 30 if
≤0). A poll runs when `status.nextScheduledUpdateCheck` is nil or in the past;
on success it sets `availableVersion`; either way it sets
`nextScheduledUpdateCheck = now + interval`. Poll errors are logged, not fatal.

## Decision logic

Let `available = status.availableVersion`.

1. **Establish current build**: if `currentVersion == ""`, then when
   `phase == Running` and `available != ""`, set `currentVersion = available`
   (a fresh install always pulls the latest public build). Set
   `UpdateAvailable=False/UpToDate` and return.
2. `updateAvailable = available != "" && available != currentVersion`.
3. If not available → `UpdateAvailable=False/UpToDate`, return.
4. Else → `UpdateAvailable=True/UpdateAvailable`, then dispatch by strategy:
   - `Automatic` → `performUpdate` now.
   - `Scheduled` → `performUpdate` if `inMaintenanceWindow`, else emit an
     `UpdateDeferred` event.
   - `Manual` (default) → emit an `UpdateAvailable` event only.

`inMaintenanceWindow`: parses `update.schedule` with `cron.ParseStandard`;
`from` = `status.lastUpdateTime` if later than creation, else creation time;
returns true when `sched.Next(from) <= now`.

## Rollout (`performUpdate`)

1. Set phase `Updating`, condition `Progressing=True`.
2. If `update.backupBeforeUpdate`: `ensurePreUpdateBackup` creates (once) a
   `PalworldBackup` named `<game>-preupdate-<build>` (build truncated to 20
   chars by `sanitizeName`), owner-refed to the game, destination from
   `backup.destination` (or `VolumeSnapshot`), `flushSave: true`. Requeue every
   15s until it is `Completed`; a `Failed` pre-update backup emits a warning and
   does not block the update.
3. Announce the warning to players via REST (`update.warnMessage`, or a default).
4. `restartServerPod`: delete pod `<name>-0`. The StatefulSet recreates it and
   the entrypoint installs the latest build; the `preStop` hook saves the world
   first (spec 07).
5. Set `lastUpdateTime = now`, `currentVersion = available`,
   `UpdateAvailable=False/Updated`, emit an `Updated` event, requeue after 30s.

There is no separate drain wait; `drainTimeoutSeconds` is exposed in the API but
graceful draining is performed by the pod's `preStop`/`SIGTERM` handling on
delete. Build pinning to arbitrary historical ids is not supported (SteamCMD
installs the branch head).
