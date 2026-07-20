# Specifications

These documents describe **exactly how `palworld-operator` behaves today**. They
are the source of truth for spec-driven development: when changing behavior,
update the relevant spec first and then make the code conform.

## Conventions

- "MUST", "always", "never" describe invariants the current code enforces.
- Constants, field names, defaults, ports, and timings are quoted verbatim from
  the code; each spec cites the file(s) that implement it.
- Where a value has a Go constant or a `+kubebuilder` marker, that marker is the
  authority and is reproduced here.

## Index

| # | Spec | Scope |
| - | ---- | ----- |
| 01 | [crd-api.md](01-crd-api.md) | The three CRDs — every spec/status field, type, default, validation, subresource, and printer column. |
| 02 | [palworldgame-controller.md](02-palworldgame-controller.md) | The `PalworldGame` reconcile loop, owned resources, naming, ordering, status derivation, and deletion. |
| 03 | [updates.md](03-updates.md) | Steam build-version detection and the update rollout state machine. |
| 04 | [backups.md](04-backups.md) | The `PalworldBackup` state machine, scheduled backups, and retention. |
| 05 | [restores.md](05-restores.md) | The `PalworldRestore` state machine. |
| 06 | [settings-rendering.md](06-settings-rendering.md) | The `PalWorldSettings.ini` renderer and the generated settings type. |
| 07 | [server-image.md](07-server-image.md) | The game-server container image, its runtime env contract, ports, probes, and scripts. |
| 08 | [networking.md](08-networking.md) | Services, ports, NetworkPolicy, and the OpenShift Route. |
| 09 | [security-rbac.md](09-security-rbac.md) | Security contexts, SCC, credentials, RBAC, and the validating webhook. |
| 10 | [deployment.md](10-deployment.md) | The manager deployment, flags/env, and kustomize overlays. |

## Global constants

Reproduced from `internal/resources/meta.go` unless noted; these are referenced
throughout the specs.

| Constant | Value | Meaning |
| -------- | ----- | ------- |
| `DefaultGamePort` | `8211` | Game UDP port default |
| `DefaultQueryPort` | `27015` | Steam query UDP port default |
| `RCONPort` | `25575` | RCON TCP port (fixed) |
| `RESTPort` | `8212` | REST API TCP port (fixed) |
| `MetricsPort` | `9877` | Exporter sidecar TCP port |
| `DataMountPath` | `/palworld` | World-data volume mount (also `STEAMAPPDIR`) |
| `ConfigMountPath` | `/config` | Rendered-config mount |
| `SaveSubPath` | `Pal/Saved` | Backup target under the data volume |
| `GameFinalizer` | `palworld.twodcube.io/finalizer` | On `PalworldGame` (`api/v1alpha1/palworldgame_types.go`) |
| `requeueInterval` | `60s` | Periodic `PalworldGame` requeue (`internal/controller/palworldgame_controller.go`) |
