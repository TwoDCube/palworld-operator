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

When `REST_ENABLED=true` and `ADMIN_PASSWORD` is set: REST `announce` → `save`
→ `shutdown` (5s wait). On any failure, or if REST is disabled, fall back to
`pkill -INT -f PalServer-Linux-Shipping` (the real binary, since `PalServer.sh`
does not `exec`), else `SIGINT` to the passed wrapper PID. Invoked both by the
entrypoint trap and by the pod's `preStop` hook (spec 02); the operator sets
`terminationGracePeriodSeconds` (default 120) to give it time.

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
