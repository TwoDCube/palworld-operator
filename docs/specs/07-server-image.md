# 07 — Game server image

Source: `build/palworld-server/` (`Dockerfile`, `entrypoint.sh`,
`graceful-shutdown.sh`, `healthcheck.sh`, `backup.sh`, `restore.sh`).

## Image

Base `registry.access.redhat.com/ubi9/ubi:latest`. Installs (via `dnf`)
SteamCMD's 32-bit libraries (`glibc.i686`, `libstdc++.i686`) plus
`ca-certificates tar gzip xz procps-ng findutils gawk jq unzip shadow-utils
glibc-langpack-en`. `tini` and `rclone` are not in the UBI repos and are fetched
as static binaries (`tini-static-amd64` from the tini GitHub release; the current
linux-amd64 `rclone` zip). SteamCMD is unpacked into `/steamcmd`. Baked env:
`STEAM_APP_ID=2394010`, `STEAMAPPDIR=/palworld`, `HOME=/home/steam`,
`LANG=LC_ALL=en_US.UTF-8`.

Arbitrary-UID layout: `${STEAMAPPDIR}`, `${HOME}`, `/steamcmd`, `/config` are
`chgrp -R 0` + `chmod -R g=u` and directories get the setgid bit, so any UID that
is a member of GID 0 can read/write and new files keep GID 0. `/etc/passwd` and
`/etc/group` are made group-writable (`chmod g=u`) so the entrypoint can register
the arbitrary UID.

`EXPOSE 8211/udp 27015/udp 25575/tcp 8212/tcp`. `USER 10000:0` (OpenShift
overrides with an arbitrary UID). `ENTRYPOINT ["/usr/bin/tini","-g","--",
"/usr/local/bin/entrypoint.sh"]`.

## Runtime env contract

The operator (spec 02) sets these on the server container; the entrypoint also
has defaults (shown) for standalone use:

| Env | Operator value | Entrypoint default |
| --- | -------------- | ------------------ |
| `STEAMAPPDIR` | `/palworld` | `/palworld` |
| `GAME_PORT` | `networking.gamePort` | `8211` |
| `QUERY_PORT` | `networking.queryPort` | `27015` |
| `RCON_ENABLED` | `true` | `true` |
| `RCON_PORT` | `25575` | `25575` |
| `REST_ENABLED` | `true` | `true` |
| `REST_PORT` | `8212` | `8212` |
| `MAX_PLAYERS` | `serverSettings.serverPlayerMaxNum` or `32` | `32` |
| `MULTITHREAD_ENABLED` | `true` | `true` |
| `SETTINGS_SOURCE` | `/config/PalWorldSettings.ini` | same |
| `ENGINE_SOURCE` | `/config/Engine.ini` (only when `engineSettings` set) | same |
| `STEAM_BRANCH` | `public` | `public` |
| `ADMIN_PASSWORD` | from credentials Secret (`secretKeyRef`, optional) | `""` |
| `SERVER_PASSWORD` | from credentials Secret (`secretKeyRef`, optional) | `""` |
| `PUBLIC_IP` / `PUBLIC_PORT` | from `networking` when set | `""` |
| `RCON_PASSWORD` | not set → defaults to `ADMIN_PASSWORD` | `${ADMIN_PASSWORD}` |
| `SHUTDOWN_WARN_SECONDS` | `shutdown.warnSeconds` | `300` |
| `SHUTDOWN_WARN_INTERVAL_SECONDS` | `shutdown.warnIntervalSeconds` | `60` |
| `SHUTDOWN_WARN_MESSAGE` | `shutdown.warnMessage` | `Server is shutting down for maintenance in %s` |
| `SHUTDOWN_GRACE_SECONDS` | effective `terminationGracePeriodSeconds` | `0` (= no clamp) |
| plus `spec.extraEnv` | — | — |

`PALWORLD_SERVER_NAME` is **not** set by the operator (the rendered INI drives
the name), so no `-servername` flag is appended by default. `STEAM_BETA_PASSWORD`,
`VALIDATE_ON_START` (`false`), `SKIP_UPDATE` (`false`), and `EXTRA_ARGS` are also
supported.

## entrypoint.sh

1. **Arbitrary UID**: if `whoami` fails (UID not in `/etc/passwd`), append a
   `steam` passwd entry for the current UID/GID to the group-writable
   `/etc/passwd`; fall back `HOME=/tmp` if `HOME` is not writable.
2. **Install/update**: run SteamCMD `app_update 2394010` into `${STEAMAPPDIR}`
   (branch = `STEAM_BRANCH`, `+betapassword` / `validate` as configured),
   retrying up to 5 times with linear backoff; skipped when `SKIP_UPDATE=true`
   and the binary already exists. Records the installed build id to
   `${STEAMAPPDIR}/.buildid`. Copies `steamclient.so` into `~/.steam/sdk64`.
3. **Render config**: render `${SETTINGS_SOURCE}` to
   `${STEAMAPPDIR}/Pal/Saved/Config/LinuxServer/PalWorldSettings.ini`, replacing
   the `__PALWORLD_ADMIN_PASSWORD__`, `__PALWORLD_SERVER_PASSWORD__`,
   `__PALWORLD_RCON_PASSWORD__`, and `__PALWORLD_PUBLIC_IP__` tokens with env
   values. Substitution is done by `awk` using `index()`/`substr()` with the
   secrets read from `ENVIRON`, so any character (`& \ / "`) is inserted
   literally regardless of the bash/awk version. `Engine.ini` is copied if
   present.
4. **Launch**: run `PalServer.sh` in the background with base args
   `-port -queryport -players -log`, appending `-servername` only when
   `PALWORLD_SERVER_NAME` is set, `-RCONEnabled=True -RCONPort` /
   `-RESTAPIEnabled=True -RESTAPIPort` when enabled, `-publicip`/`-publicport`
   when set, the perf trio `-useperfthreads -NoAsyncLoadingThread
   -UseMultithreadForDS=True` when `MULTITHREAD_ENABLED=true`, and `EXTRA_ARGS`.
   A `SIGTERM`/`SIGINT` trap runs `graceful-shutdown.sh` then `wait`s.

## graceful-shutdown.sh

Runs a **player countdown** before stopping, so no session is cut off by an
unannounced restart. Every termination path (settings change, update, node
drain, manual pod delete) goes through the pod's `preStop` hook, so this script
is the single place that decides how much notice players get.

Order of operations:

1. **Single-flight guard.** The script is invoked *twice* per termination — once
   by the `preStop` hook and again by the entrypoint's `SIGTERM` trap. The first
   invocation claims `/tmp/.palworld-shutdown-in-progress` with `set -o
   noclobber`; a later invocation sees the marker, logs, and exits 0
   immediately. Without this the second run would start a second countdown and
   overrun `terminationGracePeriodSeconds`, getting the pod `SIGKILL`ed
   mid-save. (`SIGTERM` reaches only PID 1, not the game binary, so the trap
   returning early leaves the first countdown running undisturbed.)
2. **Skip when empty.** `GET /v1/api/players` decides whether anyone is
   connected. With zero players the countdown is skipped entirely — there is
   nobody to warn, and a routine roll of an idle server should not stall for
   minutes. A failed/unparseable player query is treated as "players may be
   online" and still warns.
3. **Countdown.** Every `SHUTDOWN_WARN_INTERVAL_SECONDS` (default 60) it
   broadcasts `SHUTDOWN_WARN_MESSAGE` with the remaining time, until
   `SHUTDOWN_WARN_SECONDS` (default 300) is exhausted. In the message `%s` is
   replaced with a human-readable remaining time (`5 minutes`, `1 minute`,
   `30 seconds`) and `%d` with the remaining whole seconds. Defaults therefore
   broadcast at T-5m, T-4m, T-3m, T-2m and T-1m. `warnSeconds: 0` skips the
   countdown. Announce bodies are built with `jq` so quotes in an operator's
   message cannot produce invalid JSON.
4. **Clamp to the budget.** When `SHUTDOWN_GRACE_SECONDS` is > 0 the countdown
   is clamped to `SHUTDOWN_GRACE_SECONDS - 30`, reserving 30s for the save and
   clean shutdown. This bounds the countdown by the pod's real kubelet budget
   even if an operator sets a `terminationGracePeriodSeconds` too small for the
   configured warning (the webhook warns about that case — spec 01).
5. **Stop.** REST `announce` → `save` → `shutdown` (`SHUTDOWN_WAIT_SECONDS`,
   default 5).
6. **Fallback.** On any REST failure, or when REST is disabled or
   `ADMIN_PASSWORD` is empty, there is no way to broadcast, so the countdown is
   skipped and the script falls back to `pkill -INT -f PalServer-Linux-Shipping`
   (the real binary, since `PalServer.sh` does not `exec`), else `SIGINT` to the
   passed wrapper PID.

The operator sizes `terminationGracePeriodSeconds` to fit the countdown —
`shutdown.warnSeconds + 300`, so 600s with defaults (spec 02).

The countdown also depends on the game Service publishing not-ready addresses
(spec 08). The pod is `Terminating` for the entire countdown, and without that
flag the load balancer drops it seconds in — players receive the first warning
and are then disconnected, which is the exact outcome the countdown exists to
prevent.

## healthcheck.sh (probe backend)

Called as `healthcheck.sh <mode>`:

- `startup` — the server binary is installed **and** the game UDP port is bound
  (`/proc/net/udp*`).
- `liveness` — the game UDP port is bound.
- `readiness` — REST `GET /v1/api/info` returns 200 (when REST enabled and admin
  password set), else the port-bound check.

The operator wires these as exec probes: startup `period 15 / failureThreshold
80 / timeout 5` (~20m for the first install), liveness `30 / 5 / 5`, readiness
`15 / 3 / 5`.

## backup.sh / restore.sh

`backup.sh pvc <dest>` tars `${DATA_DIR}` (default `${STEAMAPPDIR}/Pal/Saved`)
to a local path; `backup.sh s3` streams the tar to an rclone S3 remote
configured from `S3_*` + `AWS_*` env. `restore.sh` extracts into
`${DATA_DIR}.restore-staging` **first** and only swaps it into `${DATA_DIR}` once
extraction fully succeeds, so a corrupt archive never destroys the existing
world. Both run inside operator-scheduled Jobs (specs 04/05).

Both scripts share `configure_rclone`, which sets
`RCLONE_CONFIG_S3_NO_CHECK_BUCKET=true`. The bucket is always a **precondition**
here — the operator is given one in `destination.s3.bucket` and has no business
provisioning it — but by default rclone probes the bucket on upload and tries to
`CreateBucket` when the probe fails. That call is not harmless: an S3 identity
provisioned per-bucket (an ODF `ObjectBucketClaim` user carries a 1-bucket quota)
answers it with `TooManyBuckets`, which fails the whole upload even though the
bucket exists and is writable. Suppressing the probe also drops `CreateBucket`
from the permissions the credentials need. Reads never probed, so this only ever
affected `backup.sh`.
