# 01 — CRD API

Source: `api/v1alpha1/*_types.go` (+ generated `config/crd/bases/*.yaml`). Group
`palworld.twodcube.io`, version `v1alpha1`. All three kinds are namespaced.

## PalworldGame

`shortNames: pwgame, pwg`; `categories: palworld`. Subresources: `status` and
`scale` (`specpath=.spec.replicas`, `statuspath=.status.replicas`,
`selectorpath=.status.selector`). Finalizer `palworld.twodcube.io/finalizer`.

Printer columns: `Phase` ← `.status.phase`, `Version` ← `.status.serverVersion`,
`Players` ← `.status.playersOnline`, `Ready` ← `.status.conditions[type=Ready].status`,
`Age`.

### `spec`

| Field | Type | Default | Notes |
| ----- | ---- | ------- | ----- |
| `version` | string | `"latest"` | Steam build id or `latest`. Rollout governed by `update`. |
| `replicas` | `*int32` | `1` | Enum by range: min `0`, max `1`. 0 = stopped, 1 = running. Backs the scale subresource. |
| `image.server` | string | `""` | Server image; empty → operator default (`DEFAULT_SERVER_IMAGE`, fallback `quay.io/twodcube/palworld-server:latest`). |
| `image.pullPolicy` | enum `Always\|IfNotPresent\|Never` | `IfNotPresent` | |
| `image.pullSecrets` | `[]LocalObjectReference` | — | |
| `serverSettings` | `PalworldServerSettings` | per-field | 119 keys → `PalWorldSettings.ini` (spec 06). |
| `engineSettings` | `map[string]string` | — | Raw `Engine.ini` overrides keyed `Section/Key` (spec 06). |
| `credentials.secretName` | string | `""` | User secret; empty → operator generates `<name>-credentials`. |
| `credentials.adminPasswordKey` | string | `"adminPassword"` | |
| `credentials.serverPasswordKey` | string | `"serverPassword"` | |
| `storage.size` | `resource.Quantity` | `"20Gi"` | |
| `storage.storageClassName` | `*string` | cluster default | |
| `storage.accessModes` | `[]PersistentVolumeAccessMode` | `[ReadWriteOnce]` | |
| `storage.retain` | bool | `false` | Keep the data PVC on game deletion. |
| `storage.volumeSnapshotClassName` | `*string` | cluster default | |
| `networking.gamePort` | int32 | `8211` | 1–65535. |
| `networking.queryPort` | int32 | `27015` | 1–65535. |
| `networking.serviceType` | enum `ClusterIP\|NodePort\|LoadBalancer` | `ClusterIP` | For the public game UDP service. |
| `networking.loadBalancerIP` | string | `""` | |
| `networking.loadBalancerClass` | `*string` | — | |
| `networking.nodePort` | int32 | `0` (auto) | |
| `networking.serviceAnnotations` | `map[string]string` | — | Applied to the game Service. |
| `networking.publicIP` | string | `""` | Advertised to the community list. |
| `networking.publicPort` | int32 | `0` → `gamePort` | |
| `networking.restAPI.route` | bool | `false` | Create an OpenShift Route for the REST API. |
| `networking.restAPI.host` | string | `""` | Desired Route host. |
| `networking.restAPI.tls` | enum `edge\|reencrypt\|passthrough` | `edge` | Only `edge` is valid (spec 08/09); webhook rejects the others. |
| `resources` | `ResourceRequirements` | see 02 | Empty requests defaulted to cpu `2`, memory `8Gi`. |
| `scheduling.nodeSelector` | `map[string]string` | — | |
| `scheduling.affinity` | `*Affinity` | — | |
| `scheduling.tolerations` | `[]Toleration` | — | |
| `scheduling.topologySpreadConstraints` | `[]TopologySpreadConstraint` | — | |
| `scheduling.priorityClassName` | string | `""` | |
| `backup` | `*BackupPolicy` | nil | See below and spec 04. |
| `update` | `*UpdatePolicy` | nil | See below and spec 03. |
| `podDisruptionBudget.enabled` | bool | `true` | |
| `podDisruptionBudget.minAvailable` | `*int32` | `1` | |
| `nodeDrain` | `*NodeDrainPolicy` | nil (= enabled) | Graceful migration off draining nodes (spec 11). |
| `nodeDrain.disabled` | bool | `false` | |
| `nodeDrain.gracePeriodSeconds` | int32 (≥0) | `30` | Warn-to-migrate delay. |
| `nodeDrain.warnMessage` | string | `"Server node maintenance: migrating in %d seconds, please reach a safe spot"` | `%d` → grace seconds. |
| `monitoring.serviceMonitor` | bool | `false` | |
| `monitoring.metricsExporter` | bool | `true` | Requires `OPERATOR_IMAGE` set for the sidecar to be added. |
| `serviceAccountName` | string | `""` | Empty → operator manages `<name>`. |
| `shutdown` | `*ShutdownPolicy` | nil (= defaults) | Player countdown before the server stops (spec 07). |
| `shutdown.warnSeconds` | int32 (≥0) | `300` | 0 stops immediately. |
| `shutdown.warnIntervalSeconds` | int32 (≥1) | `60` | Re-broadcast cadence. |
| `shutdown.warnMessage` | string | `"Server is shutting down for maintenance in %s"` | `%s` → human-readable remaining time, `%d` → remaining seconds. |
| `terminationGracePeriodSeconds` | `*int64` | nil → `shutdown.warnSeconds + 300` (`600`) | Must outlast the countdown, which runs in `preStop`. An explicit value is honoured verbatim; if it leaves under 30s of headroom the webhook warns and the container clamps the countdown (spec 07). |
| `podAnnotations` | `map[string]string` | — | On the pod template. |
| `podLabels` | `map[string]string` | — | Merged into labels; reserved identity labels always win (spec 02). |
| `extraEnv` | `[]EnvVar` | — | Appended to the server container. |
| `sidecars` | `[]Container` | — | Extra containers. |
| `podSecurityContext` | `*PodSecurityContext` | see 09 | Overrides the computed default. |

### `BackupPolicy`

| Field | Type | Default |
| ----- | ---- | ------- |
| `enabled` | bool | `false` |
| `schedule` | string (cron) | `""` |
| `destination` | `BackupDestination` | `{type: VolumeSnapshot}` |
| `retention` | int32 (≥0) | `7` (0 = keep all) |
| `suspend` | bool | `false` |
| `onDelete` | bool | `false` |

### `UpdatePolicy`

| Field | Type | Default |
| ----- | ---- | ------- |
| `strategy` | enum `Manual\|Automatic\|Scheduled` | `Manual` |
| `schedule` | string (cron) | `""` (required for `Scheduled`) |
| `drainTimeoutSeconds` | int32 (≥0) | `300` (0 = no drain wait) |
| `warnMessage` | string | `"Server will restart for updates in %d seconds"` |
| `warnIntervalSeconds` | int32 (≥1) | `60` |
| `backupBeforeUpdate` | bool | `true` |
| `pollIntervalMinutes` | int32 (≥1) | `30` |

### `ShutdownPolicy`

| Field | Type | Default |
| ----- | ---- | ------- |
| `warnSeconds` | int32 (≥0) | `300` |
| `warnIntervalSeconds` | int32 (≥1) | `60` |
| `warnMessage` | string | `"Server is shutting down for maintenance in %s"` |

Applies to **every** pod termination, because it is enforced by the container's
`preStop` hook rather than by a controller code path (spec 07).
`update.drainTimeoutSeconds` is a separate, update-only wait for players to leave
*before* the pod is deleted; the two stack (spec 03).

### `BackupDestination` / `S3Destination`

| Field | Type | Default |
| ----- | ---- | ------- |
| `type` | enum `VolumeSnapshot\|S3\|PVC` | `VolumeSnapshot` |
| `s3` | `*S3Destination` | — (required for `S3`) |
| `pvcName` | string | — (required for `PVC`) |
| `s3.bucket` | string | — (required) |
| `s3.prefix` | string | `""` |
| `s3.endpoint` | string | `""` |
| `s3.region` | string | `""` |
| `s3.credentialsSecret` | string | — (required) |
| `s3.accessKeyIDKey` | string | `"AWS_ACCESS_KEY_ID"` |
| `s3.secretAccessKeyKey` | string | `"AWS_SECRET_ACCESS_KEY"` |
| `s3.insecureTLS` | bool | `false` |

### `status`

`phase` is a `PhaseType` enum: `Pending`, `Installing`, `Running`, `Updating`,
`BackingUp`, `Restoring`, `Stopped`, `Degraded`, `Terminating`.

Condition types (`metav1.Condition`): `Ready`, `Progressing`, `Degraded`,
`BackupReady`, `UpdateAvailable`.

| Field | Type | Set by |
| ----- | ---- | ------ |
| `phase` | `PhaseType` | 02 |
| `observedGeneration` | int64 | `updateStatus` |
| `conditions` | `[]Condition` | 02/03 |
| `currentVersion` | string | **update controller only** — Steam build id (03) |
| `serverVersion` | string | `observeLive` — in-game version string (display) |
| `availableVersion` | string | update poller — Steam build id (03) |
| `replicas` | int32 | StatefulSet `readyReplicas` |
| `selector` | string | serialized selector labels |
| `playersOnline` | int32 | REST metrics |
| `maxPlayers` | int32 | `serverSettings.serverPlayerMaxNum` or REST |
| `serverName` | string | REST info |
| `gameEndpoint` | string | derived from the game Service (08) |
| `restEndpoint` | string | `<name>-admin.<ns>.svc:8212` |
| `routeURL` | string | Route host, if created |
| `persistentVolumeClaim` | string | `data-<name>-0` |
| `currentNode` | string | the node the server pod runs on (spec 11) |
| `credentialsSecret` | string | user or generated secret name |
| `lastBackupTime` / `lastBackupName` | `*Time` / string | newest completed backup (04) |
| `nextScheduledBackup` | `*Time` | scheduled-backup planner (04) |
| `lastUpdateTime` | `*Time` | update rollout (03) |
| `nextScheduledUpdateCheck` | `*Time` | update poller (03) |
| `updateDrainStartTime` | `*Time` | player drain in progress; cleared on restart (03) |
| `updateDrainLastWarnTime` | `*Time` | rate-limits drain re-broadcasts (03) |

## PalworldBackup

`shortNames: pwbackup, pwbk`. Subresource: `status`. Printer columns: `Game` ←
`.spec.gameRef`, `Type` ← `.spec.destination.type`, `Phase`, `Completed` ←
`.status.completionTime`, `Age`.

### `spec`

| Field | Type | Default | Notes |
| ----- | ---- | ------- | ----- |
| `gameRef` | string | — | **Required.** Same-namespace `PalworldGame`. |
| `destination` | `BackupDestination` | `{type: VolumeSnapshot}` | |
| `flushSave` | bool | `true` | Issue REST `save` before snapshot. |
| `ttlSecondsAfterFinished` | `*int32` | — | Delete the object this long after completion. |
| `retain` | bool | `false` | Exempt from retention GC. |

### `status`

`phase` (`BackupPhase`): `Pending`, `Saving`, `Snapshotting`, `Uploading`,
`Completed`, `Failed`. Fields: `message`, `startTime`, `completionTime`,
`volumeSnapshotName`, `location`, `sizeBytes`, `serverVersion`, `jobName`,
`conditions`. State machine: spec 04.

## PalworldRestore

`shortNames: pwrestore, pwrs`. Subresource: `status`. Printer columns: `Game` ←
`.spec.gameRef`, `Backup` ← `.spec.backupRef`, `Phase`, `Age`.

### `spec`

| Field | Type | Default | Notes |
| ----- | ---- | ------- | ----- |
| `gameRef` | string | — | **Required.** |
| `backupRef` | string | `""` | A completed `PalworldBackup` (same namespace). |
| `source` | `*BackupDestination` | — | Restore directly from an external location instead of `backupRef`. |
| `force` | bool | `true` | Stop a running game to restore it. |

Exactly one of `backupRef` / `source` should be set; `resolvePlan` errors if
neither is (spec 05).

### `status`

`phase` (`RestorePhase`): `Pending`, `Stopping`, `Restoring`, `Starting`,
`Completed`, `Failed`. Fields: `message`, `startTime`, `completionTime`,
`jobName`, `originalReplicas` (`*int32`, the pre-restore replica count),
`conditions`. State machine: spec 05.
