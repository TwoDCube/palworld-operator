# CLAUDE.md

## What this project is

`palworld-operator` is a Kubernetes operator (Kubebuilder v4 / controller-runtime)
that hosts **Palworld dedicated servers** on OpenShift and vanilla Kubernetes. A
single `PalworldGame` custom resource declares a server and all of its settings;
the operator provisions and runs it and performs day-2 operations (updates,
backups, restores, graceful lifecycle, monitoring). Two companion resources,
`PalworldBackup` and `PalworldRestore`, drive backup and restore. A fleet is many
independent `PalworldGame` resources.

API group: `palworld.twodcube.io/v1alpha1`. Go module:
`github.com/twodcube/palworld-operator`.

## Spec-driven development

The authoritative description of **how the system currently behaves** lives in
[`docs/specs/`](docs/specs/). These specs state exactly what the code does today
and are the source of truth for changes: update the relevant spec first, then
make the code match it. Do not let code and spec diverge.

- [`docs/specs/README.md`](docs/specs/README.md) — spec index and conventions
- [`docs/architecture.md`](docs/architecture.md) — high-level design overview

## Repository layout

- `api/v1alpha1/` — CRD types, deepcopy, validating webhook. `palworldsettings_types.go` is generated (see spec 06).
- `internal/controller/` — the three reconcilers and their helpers.
- `internal/resources/` — builders for the child Kubernetes/OpenShift objects.
- `internal/settings/` — the `PalWorldSettings.ini` renderer.
- `internal/palworld/` — REST, RCON, and Steam-version clients.
- `internal/exporter/` — the metrics-exporter subcommand.
- `cmd/main.go` — manager entrypoint (and `exporter` subcommand dispatch).
- `build/palworld-server/` — the game-server container image and its scripts.
- `config/` — kustomize manifests (`default`, `openshift`, `webhook`, `certmanager`, `scc`, `prometheus`, `samples`).

## Common commands

```sh
make manifests generate   # regenerate CRDs, RBAC, webhook config, and deepcopy
make build                # go build
make test                 # unit tests
make run                  # run the manager against ~/.kube/config
make docker-build IMG=... # build the operator image
```

CRD generation uses `crd:allowDangerousTypes=true` because `spec.serverSettings`
uses `float` fields for the game multipliers (the only consumer is the operator's
own INI renderer). After changing any `api/` type or `+kubebuilder` marker, run
`make manifests generate` and commit the regenerated files (CI enforces this).
