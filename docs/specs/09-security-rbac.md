# 09 — Security, credentials, RBAC, webhook

Sources: `internal/resources/statefulset.go` (security contexts),
`internal/resources/secret.go` (credentials), `internal/resources/backup.go`
(job security), `api/v1alpha1/palworldgame_webhook.go`, `config/rbac/`,
`config/scc/`.

## Pod / container security contexts

Server **pod** (`podSecurityContext`): uses `spec.podSecurityContext` verbatim if
set, otherwise: `runAsNonRoot: true`, `seccompProfile: RuntimeDefault`,
`fsGroupChangePolicy: OnRootMismatch`, and:

- On **OpenShift** (`hasRoute` true): `runAsUser`/`runAsGroup`/`fsGroup` are left
  unset for the restricted-v2 SCC to inject.
- On **vanilla Kubernetes**: `runAsUser: 10000`, `runAsGroup: 0`, `fsGroup: 0`
  (so the group-root-writable volume is writable).

Server **container** (`containerSecurityContext`): `allowPrivilegeEscalation:
false`, `runAsNonRoot: true`, `readOnlyRootFilesystem: false` (SteamCMD/HOME
writes), `capabilities.drop: [ALL]`.

Backup/restore **Job** pods use the same model via `opsPodSecurityContext`
(`runAsNonRoot`, `RuntimeDefault`; UID/GID/fsGroup pinned only off-OpenShift) and
the same container context.

## Credentials

`spec.credentials.secretName` selects a user-supplied Secret; otherwise the
operator creates `<name>-credentials` **once** with `adminPassword` = a random
24-char alphanumeric (`GeneratePassword`) and `serverPassword` = `""`. It is
never overwritten on later reconciles. Keys default to `adminPassword` /
`serverPassword` (overridable). The admin password doubles as the RCON password
and the REST basic-auth password (username `admin`). Passwords reach the server
only via `secretKeyRef` env + INI placeholder substitution — never in a
ConfigMap or the CR (spec 06/07).

## Validating webhook

`PalworldGameValidator` (registered only when `--enable-webhooks` is set;
default off — CRD OpenAPI validation applies unconditionally). On create/update
it rejects:

- `serverName`/`serverDescription`/`region`/`banListURL`/`randomizerSeed`
  containing `"`, `\n`, or `\r` (would corrupt the single-line INI).
- `backup.enabled` without a `schedule`, or an invalid cron; an `S3` destination
  missing `s3.bucket` or `s3.credentialsSecret`; a `PVC` destination missing
  `pvcName`.
- `update.strategy=Scheduled` without a valid cron `schedule`.
- `networking.restAPI.route` with `tls` = `reencrypt` or `passthrough`.

It warns (non-blocking) for `restAPI.route` (Route API needed; publishes the
admin API externally) and `serviceType=LoadBalancer` (needs a UDP-capable LB).

## RBAC

The generated ClusterRole `manager-role` (`config/rbac/role.yaml`) grants:

- core: `configmaps`, `persistentvolumeclaims`, `secrets`, `serviceaccounts`,
  `services` (full CRUD); `events` (create/patch); `pods` (get/list/watch/delete).
- `apps/statefulsets`, `batch/jobs`, `policy/poddisruptionbudgets`,
  `networking.k8s.io/networkpolicies`, `monitoring.coreos.com/servicemonitors`,
  `route.openshift.io/routes`, `snapshot.storage.k8s.io/volumesnapshots` (full
  CRUD).
- `palworld.twodcube.io` `palworldgames`/`palworldbackups`/`palworldrestores`
  plus their `/status` and `/finalizers` subresources. The `/finalizers`
  permissions are required so owned children can be created under OpenShift's
  `OwnerReferencesPermissionEnforcement`.

A namespaced `leader-election-role` grants `configmaps`, `coordination/leases`,
and `events` for leader election. Metrics auth uses `metrics-auth-role`
(tokenreviews/subjectaccessreviews).

## SCC (OpenShift)

The server image runs under the default **restricted-v2** SCC unchanged (no
privileges, arbitrary UID). `config/scc/scc.yaml` provides an **optional**
`palworld-server` SCC for hardened clusters; it is not applied by any overlay and
is not required.
