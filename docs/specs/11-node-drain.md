# 11 — Node drain handling

Source: `internal/controller/palworldgame_nodedrain.go`, wired into
`internal/controller/palworldgame_controller.go`.

The operator gracefully migrates a server off a node that is being cordoned or
drained (e.g. for node maintenance) instead of letting an abrupt eviction
interrupt it: it warns players, waits a grace period, flushes a save, and deletes
the pod so it reschedules onto a healthy node. Because the operator deletes the
pod directly (not via the eviction API), it is **not** blocked by the
PodDisruptionBudget — and removing the pod lets an in-progress `kubectl drain`
complete.

## Trigger

- A **Node watch** (`SetupWithManager`) with the `gamesOnNode` map function:
  on a Node event where the node is `spec.unschedulable`, it lists PalworldGames
  and enqueues those whose `status.currentNode` equals the node name. Schedulable
  nodes enqueue nothing.
- The normal 60s requeue also runs the drain step, so a cordon is acted on within
  60s even without the watch.

`status.currentNode` is maintained by `reconcileNodeDrain` on every reconcile
from the pod's `spec.nodeName`, so the map function can locate the game without a
cluster-wide pod informer.

## `reconcileNodeDrain`

Runs after `reconcileObservedStatus`, before `reconcileUpdates`.

1. Get pod `<name>-0`. NotFound → clear `status.currentNode`, return.
2. Set `status.currentNode = pod.spec.nodeName`.
3. If `nodeDrain.disabled` → return.
4. If the pod is terminating, not `Running`, or unscheduled → return (nothing to
   migrate; this also makes the flow self-limiting).
5. Get the pod's Node. If it is **not** `unschedulable` → drop any stale
   `palworld.twodcube.io/drain-warned-at` pod annotation and return.
6. The node is draining:
   - **First detection** (no `drain-warned-at` annotation): broadcast the warning
     (`nodeDrain.warnMessage`, with `%d` replaced by the grace seconds) via REST;
     stamp the pod annotation `drain-warned-at = now` (RFC3339); set phase
     `Updating` + `Progressing/NodeDraining`; emit a `NodeDraining` event;
     requeue after the grace period.
   - **Warned, grace not elapsed**: requeue for the remaining time.
   - **Grace elapsed**: REST `announce` ("migrating…") + `save`; delete pod
     `<name>-0`; set phase `Updating`; emit `NodeDrainMigrated`; requeue 15s. The
     pod's `preStop` hook performs the final save+shutdown (spec 07), and the
     StatefulSet reschedules it onto a schedulable node.

The `drain-warned-at` marker lives on the pod, so the grace period is measured
per pod instance and resets when the pod is recreated.

## Self-limiting behavior

- After the pod is deleted it is terminating → step 4 returns → no repeat delete.
- If the replacement pod lands on a healthy node → step 5 returns → done.
- If it lands on another cordoned node → it migrates again (keeps moving off
  draining nodes).
- If **every** node is cordoned → the replacement stays `Pending` (never
  `Running`) → step 4 returns → no delete loop.

## Configuration (`spec.nodeDrain`)

| Field | Type | Default | Notes |
| ----- | ---- | ------- | ----- |
| `disabled` | bool | `false` | A nil `nodeDrain` means enabled. |
| `gracePeriodSeconds` | int32 (≥0) | `30` | Warning-to-migration delay; `0` migrates immediately. |
| `warnMessage` | string | `"Server node maintenance: migrating in %d seconds, please reach a safe spot"` | `%d` → grace seconds. |

## RBAC

Requires `nodes` `get;list;watch` and `pods` `patch` (for the annotation) in
addition to the existing `pods` `get;list;watch;delete` (spec 09).

## Relationship to the PodDisruptionBudget

The PDB (spec 02, `minAvailable: 1`) still blocks *involuntary/random* eviction
of the single pod. Node-drain handling is the sanctioned path for planned
maintenance: the operator observes the cordon and voluntarily migrates the pod,
so players are warned and the world is saved, and the drain is not left blocked
by the PDB.
