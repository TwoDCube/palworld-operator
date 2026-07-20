# Palworld Operator

A production-grade Kubernetes operator, built with [Kubebuilder](https://book.kubebuilder.io/),
for running [Palworld](https://www.pocketpair.jp/palworld) dedicated servers at
scale on **OpenShift** (and vanilla Kubernetes). Declare a server with a single
`PalworldGame` custom resource and the operator handles provisioning,
configuration, health, upgrades, backups, and restores — everything you need to
host hundreds of independent worlds.

> One `PalworldGame` = one authoritative world. A fleet is many independent
> `PalworldGame` resources, each with its own storage, credentials, and
> lifecycle.

---

## Features

- **Full configuration surface** — every one of the ~119 `PalWorldSettings.ini`
  `OptionSettings` keys is a typed, validated field on the CRD (multipliers,
  difficulty, PvP, crossplay platforms, guild/base limits, breeding, and the
  long tail of post-1.0 keys).
- **Day-2 operations, built in:**
  - **Backups** — scheduled and on-demand, via CSI `VolumeSnapshot` (default),
    S3-compatible object storage, or a backup PVC. Application-consistent
    (`save` is flushed first). Retention/GC and backup-on-delete.
  - **Restores** — restore any backup into a world with a single
    `PalworldRestore` (stop → restore → start), from a snapshot or a tarball.
  - **Updates** — Steam build detection with `Manual`, `Automatic`, or
    `Scheduled` rollout strategies, player-drain warnings, graceful
    save-and-restart, and optional pre-update backup.
- **Graceful lifecycle** — `preStop` + `SIGTERM` drive an RCON/REST
  `save`-then-`shutdown` so worlds are never corrupted by an abrupt stop.
- **OpenShift-native:**
  - Server image runs cleanly under the default **restricted-v2 SCC**
    (arbitrary non-root UID, GID 0, group-writable data — no privileges).
  - Optional **Route** for the HTTP REST admin API (edge/reencrypt/passthrough).
  - Works with **MetalLB** (UDP `LoadBalancer`), **ODF/NooBaa** (S3 backups),
    **cert-manager** (webhook certs), and the **Prometheus Operator**
    (ServiceMonitor).
- **Observability** — a bundled metrics exporter sidecar translates the Palworld
  REST metrics endpoint (fps, players, uptime, frame time) into Prometheus.
- **Safe by default** — generated random admin credentials, a `NetworkPolicy`
  that keeps RCON/REST internal while exposing only the game UDP ports, a
  `PodDisruptionBudget`, and a validating admission webhook.

## Architecture

```
                         ┌──────────────────────────┐
                         │     palworld-operator     │
                         │  (controller-manager)     │
                         └────────────┬──────────────┘
      reconciles                      │  owns / drives
      ┌──────────────┬────────────────┼───────────────────┬─────────────┐
      ▼              ▼                 ▼                   ▼             ▼
 PalworldGame   PalworldBackup   PalworldRestore     (per game) StatefulSet
      │              │                 │                   │  ConfigMap/Secret
      │              │                 │                   │  Services (game/admin/headless)
      │              │                 │                   │  PVC, PDB, NetworkPolicy
      │              │                 │                   │  Route (OpenShift), ServiceMonitor
      ▼              ▼                 ▼                   ▼
   world save    VolumeSnapshot    restore Job        Palworld server pod
                  / S3 / PVC                          (SteamCMD + PalServer +
                                                       metrics-exporter sidecar)
```

Three CRDs (API group `palworld.twodcube.io/v1alpha1`):

| Kind              | Purpose                                             |
| ----------------- | --------------------------------------------------- |
| `PalworldGame`    | A dedicated server instance and all its settings.   |
| `PalworldBackup`  | A point-in-time backup of a game's world.           |
| `PalworldRestore` | Restore a backup into a game.                        |

See [`docs/architecture.md`](docs/architecture.md) for the full design.

## Ports

| Port        | Proto | Purpose            | Exposure                                   |
| ----------- | ----- | ------------------ | ------------------------------------------ |
| `8211`      | UDP   | Game               | Public (LoadBalancer / NodePort)           |
| `27015`     | UDP   | Steam query        | Public (optional)                          |
| `25575`     | TCP   | RCON               | **Internal only** (operator)               |
| `8212`      | TCP   | REST admin API     | **Internal only** (optionally via a Route) |

> OpenShift Routes and Kubernetes Ingress only carry HTTP/TLS, **not UDP**, so
> the game port must be exposed with a `LoadBalancer` (MetalLB on-prem) or
> `NodePort`. The REST API is HTTP and *can* use a Route.

## Prerequisites

- Kubernetes 1.29+ or OpenShift 4.14+.
- A CSI `StorageClass` (with an associated `VolumeSnapshotClass` for snapshot
  backups).
- For public UDP exposure: a `LoadBalancer` provider (cloud LB, or MetalLB
  on-prem) or `NodePort` access.
- Optional: Prometheus Operator, cert-manager, ODF/NooBaa (S3), external-dns.

## Quick start

### 1. Build and push the images

```sh
# Operator image
make docker-build docker-push IMG=quay.io/youorg/palworld-operator:v0.1.0

# Game server image (OpenShift arbitrary-UID compatible)
docker build -t quay.io/youorg/palworld-server:latest build/palworld-server
docker push quay.io/youorg/palworld-server:latest
```

### 2. Install the CRDs and the operator

```sh
make install                               # CRDs
make deploy IMG=quay.io/youorg/palworld-operator:v0.1.0
# OpenShift:
make deploy-openshift IMG=quay.io/youorg/palworld-operator:v0.1.0
```

### 3. Create a server

```sh
kubectl apply -f config/samples/palworld_v1alpha1_palworldgame_minimal.yaml
kubectl get palworldgame -w
```

```
NAME               PHASE      VERSION   PLAYERS   READY   AGE
palworld-minimal   Installing           0         False   30s
palworld-minimal   Running    2394010   0         True    6m
```

The first boot installs the ~8 GB server via SteamCMD, so it can take several
minutes — the startup probe tolerates this.

### 4. Get the admin password and connect

```sh
# Generated automatically if you didn't supply your own Secret:
kubectl get secret palworld-minimal-credentials -o jsonpath='{.data.adminPassword}' | base64 -d

# The public game address:
kubectl get palworldgame palworld-minimal -o jsonpath='{.status.gameEndpoint}'
```

## Usage

### Configuring server settings

Every `PalWorldSettings.ini` option is a field under `spec.serverSettings`:

```yaml
spec:
  serverSettings:
    serverName: "My Server"
    serverPlayerMaxNum: 32
    difficulty: None          # None|Casual|Normal|Hard|Difficult
    deathPenalty: All         # None|Item|ItemAndEquipment|All
    expRate: 2.0
    palCaptureRate: 1.5
    bEnablePlayerToPlayerDamage: false
    crossplayPlatforms: [Steam, Xbox, PS5, Mac]
```

`kubectl explain palworldgame.spec.serverSettings` documents every field.
Passwords are **never** put in the CR — supply a Secret via
`spec.credentials.secretName`, or let the operator generate one.

### Stopping / starting a server

```sh
kubectl scale palworldgame/my-server --replicas=0   # stop (world preserved)
kubectl scale palworldgame/my-server --replicas=1   # start
```

### Backups

Scheduled (in the game spec):

```yaml
spec:
  backup:
    enabled: true
    schedule: "0 */6 * * *"
    retention: 8
    onDelete: true
    destination:
      type: VolumeSnapshot   # or S3 / PVC
```

On-demand:

```sh
kubectl apply -f config/samples/palworld_v1alpha1_palworldbackup.yaml
kubectl get palworldbackup -w
```

### Restores

```sh
kubectl apply -f config/samples/palworld_v1alpha1_palworldrestore.yaml
```

The operator stops the server, restores the world (from the snapshot or
tarball), and starts it back up.

### Updates

```yaml
spec:
  update:
    strategy: Automatic        # Manual | Automatic | Scheduled
    schedule: "0 4 * * *"      # for Scheduled
    drainTimeoutSeconds: 300
    backupBeforeUpdate: true
```

The operator polls Steam for new builds, surfaces an `UpdateAvailable`
condition, and (for `Automatic`/`Scheduled`) broadcasts a warning, flushes a
save, optionally backs up, and performs a graceful restart onto the new build.

## OpenShift notes

- The bundled server image is **arbitrary-UID compatible** and runs under the
  default `restricted-v2` SCC — no custom SCC or privileges required. An
  optional hardened SCC is in [`config/scc`](config/scc/scc.yaml).
- Expose the REST API via a Route with `spec.networking.restAPI.route: true`.
- For the UDP game port, install the **MetalLB Operator** and set
  `spec.networking.serviceType: LoadBalancer`.
- Store backups in **ODF/NooBaa** by pointing an S3 destination at the NooBaa S3
  endpoint.

## Monitoring

With `spec.monitoring.metricsExporter: true` (default), each server pod runs an
exporter exposing `palworld_players_online`, `palworld_server_fps`,
`palworld_uptime_seconds`, and more. Set `spec.monitoring.serviceMonitor: true`
on a Prometheus-Operator cluster to scrape it automatically.

## Development

```sh
make manifests generate   # regenerate CRDs, RBAC, deepcopy
make build test           # compile + unit tests
make run                  # run the manager against your kubeconfig
```

The full Palworld settings struct is generated from an authoritative key list;
see [`docs/architecture.md`](docs/architecture.md#settings-generation).

## Limitations

- A single world is inherently single-instance; it cannot be horizontally
  scaled. Run many `PalworldGame` resources for a fleet.
- Build pinning is limited to what SteamCMD exposes (public branch / betas);
  arbitrary historical build ids require DepotDownloader and are out of scope.
- Storage resizing after creation follows your CSI driver's PVC-expansion
  support (the `StatefulSet` volume template itself is immutable).

## License

Apache 2.0.
