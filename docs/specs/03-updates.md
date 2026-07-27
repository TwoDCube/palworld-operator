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
3. **Player drain** (`drainUpdate`), when `update.drainTimeoutSeconds > 0`. The
   reconciler never sleeps; the drain is a requeue loop driven by two status
   fields:
   - First entry: set `status.updateDrainStartTime = now`, broadcast
     `update.warnMessage`, set `status.updateDrainLastWarnTime = now`, emit a
     `DrainingPlayers` event, requeue after 15s.
   - Later entries: finish early when `status.playersOnline == 0` (emit
     `PlayersDrained`); finish anyway once
     `updateDrainStartTime + drainTimeoutSeconds` has passed (emit
     `DrainTimeout`). Otherwise re-broadcast when at least
     `update.warnIntervalSeconds` has elapsed since `updateDrainLastWarnTime`,
     and requeue after 15s.
   - `Progressing` reports `Draining N player(s), Ms remaining`.

   Re-broadcasts are gated on `updateDrainLastWarnTime` rather than on reconcile
   entry, because the controller is reconciled far more often than its own
   `RequeueAfter` (any owned-object write, and on a `LoadBalancer` game MetalLB's
   status rewrites — spec 02); announcing per reconcile would spam chat.

   `%d` in `warnMessage` is replaced with the seconds remaining in the drain.
   A failed REST metrics poll leaves `status.playersOnline` at its previous value
   (spec 02), so an unreachable server drains for the full timeout rather than
   restarting early.
4. `restartServerPod`: delete pod `<name>-0`. Clear both drain status fields. The
   StatefulSet recreates it and the entrypoint installs the latest build; the
   `preStop` hook warns any remaining players, then saves the world (spec 07).
5. Set `lastUpdateTime = now`, `currentVersion = available`,
   `UpdateAvailable=False/Updated`, emit an `Updated` event, requeue after 30s.

The drain state is cleared whenever no update is pending, so an update that
becomes unnecessary mid-drain (build re-pinned, manual restart) does not leave a
half-finished drain behind.

### Drain vs. the preStop countdown

These are two different mechanisms and they stack:

- The **drain** waits for players to *leave voluntarily* before the pod is
  deleted, and is update-only.
- The **preStop countdown** (`shutdown.warnSeconds`, spec 07) warns whoever is
  *still connected* at deletion, on every termination.

In the common case players log off during the drain, so preStop finds an empty
server and skips its countdown — the restart is immediate. If players ignore the
drain entirely, the worst case is `drainTimeoutSeconds + shutdown.warnSeconds`
(600s with both defaults), which fits inside the derived
`terminationGracePeriodSeconds`. Set `drainTimeoutSeconds: 0` to restart as soon
as the warning goes out and rely on the preStop countdown alone.

Build pinning to arbitrary historical ids is not supported (SteamCMD installs the
branch head).
