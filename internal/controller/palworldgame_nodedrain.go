/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
	"github.com/twodcube/palworld-operator/internal/resources"
)

// drainWarnedAtAnnotation records (on the pod) when players were first warned
// that the pod's node is draining, so the grace period is measured per pod
// instance and reset when the pod is recreated on a new node.
const drainWarnedAtAnnotation = "palworld.twodcube.io/drain-warned-at"

const defaultDrainGrace = 30 * time.Second

func nodeDrainDisabled(g *palworldv1alpha1.PalworldGame) bool {
	return g.Spec.NodeDrain != nil && g.Spec.NodeDrain.Disabled
}

func drainGrace(g *palworldv1alpha1.PalworldGame) time.Duration {
	if g.Spec.NodeDrain == nil {
		return defaultDrainGrace
	}
	return time.Duration(g.Spec.NodeDrain.GracePeriodSeconds) * time.Second
}

// reconcileNodeDrain keeps status.currentNode fresh and, when that node is
// cordoned/drained, gracefully migrates the server: warn players, wait a grace
// period, flush a save, then delete the pod so it reschedules onto a schedulable
// node. Deleting the pod ourselves (rather than via the eviction API) bypasses
// the PodDisruptionBudget, which also lets the in-progress `kubectl drain`
// complete.
func (r *PalworldGameReconciler) reconcileNodeDrain(ctx context.Context, game *palworldv1alpha1.PalworldGame) (ctrl.Result, error) {
	podName := fmt.Sprintf("%s-0", resources.StatefulSetName(game))
	var pod corev1.Pod
	err := r.Get(ctx, client.ObjectKey{Name: podName, Namespace: game.Namespace}, &pod)
	if apierrors.IsNotFound(err) {
		game.Status.CurrentNode = ""
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, nil // best-effort; the periodic requeue retries
	}
	game.Status.CurrentNode = pod.Spec.NodeName

	if nodeDrainDisabled(game) {
		return ctrl.Result{}, nil
	}
	// Only a live, scheduled, non-terminating pod can be migrated. This also
	// makes the flow self-limiting: once we delete the pod it stops matching.
	if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning || pod.Spec.NodeName == "" {
		return ctrl.Result{}, nil
	}

	var node corev1.Node
	if err := r.Get(ctx, client.ObjectKey{Name: pod.Spec.NodeName}, &node); err != nil {
		return ctrl.Result{}, nil // best-effort
	}
	if !node.Spec.Unschedulable {
		// Node is healthy again; drop any stale warning marker so a future drain
		// re-warns players.
		if _, ok := pod.Annotations[drainWarnedAtAnnotation]; ok {
			_ = r.patchDrainAnnotation(ctx, &pod, "")
		}
		return ctrl.Result{}, nil
	}

	grace := drainGrace(game)
	warnedAt, warned := pod.Annotations[drainWarnedAtAnnotation]

	if !warned {
		// First detection: warn players and record the time.
		r.broadcastDrainWarning(ctx, game, grace)
		if err := r.patchDrainAnnotation(ctx, &pod, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return ctrl.Result{}, err
		}
		game.Status.Phase = palworldv1alpha1.PhaseUpdating
		r.setProgressing(game, "NodeDraining",
			fmt.Sprintf("Node %s is draining; migrating in %ds", node.Name, int(grace.Seconds())))
		r.Recorder.Eventf(game, corev1.EventTypeNormal, "NodeDraining",
			"Node %s cordoned; warned players, migrating in %ds", node.Name, int(grace.Seconds()))
		if grace <= 0 {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{RequeueAfter: grace}, nil
	}

	// Already warned: wait out the remaining grace before migrating.
	if t, perr := time.Parse(time.RFC3339, warnedAt); perr == nil {
		if remaining := time.Until(t.Add(grace)); remaining > 0 {
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
	}

	// Grace elapsed: flush a save and delete the pod so it reschedules off the
	// draining node. The pod's preStop hook performs the final save+shutdown.
	if rc, err := restClientFor(ctx, r.Client, game); err == nil {
		_ = rc.Announce(ctx, "Server is migrating to another node; reconnect in a moment.")
		_ = rc.Save(ctx)
	}
	if err := r.Delete(ctx, &pod); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	game.Status.Phase = palworldv1alpha1.PhaseUpdating
	r.Recorder.Eventf(game, corev1.EventTypeNormal, "NodeDrainMigrated",
		"Saved and migrated server off draining node %s", node.Name)
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func (r *PalworldGameReconciler) broadcastDrainWarning(ctx context.Context, game *palworldv1alpha1.PalworldGame, grace time.Duration) {
	rc, err := restClientFor(ctx, r.Client, game)
	if err != nil {
		return
	}
	msg := "Server node maintenance: migrating in %d seconds, please reach a safe spot"
	if game.Spec.NodeDrain != nil && game.Spec.NodeDrain.WarnMessage != "" {
		msg = game.Spec.NodeDrain.WarnMessage
	}
	if strings.Contains(msg, "%d") {
		msg = fmt.Sprintf(msg, int(grace.Seconds()))
	}
	_ = rc.Announce(ctx, msg)
}

// patchDrainAnnotation sets (value != "") or removes (value == "") the drain
// warning marker on the pod using a merge patch.
func (r *PalworldGameReconciler) patchDrainAnnotation(ctx context.Context, pod *corev1.Pod, value string) error {
	patch := client.MergeFrom(pod.DeepCopy())
	if value == "" {
		delete(pod.Annotations, drainWarnedAtAnnotation)
	} else {
		if pod.Annotations == nil {
			pod.Annotations = map[string]string{}
		}
		pod.Annotations[drainWarnedAtAnnotation] = value
	}
	return r.Patch(ctx, pod, patch)
}

// gamesOnNode maps a Node event to the PalworldGame(s) whose pod runs on it, but
// only when the node is unschedulable (a cordon), so healthy-node churn does not
// trigger reconciles. Uses the already-cached PalworldGame list and each game's
// observed status.currentNode, avoiding a cluster-wide pod informer.
func (r *PalworldGameReconciler) gamesOnNode(ctx context.Context, obj client.Object) []reconcile.Request {
	node, ok := obj.(*corev1.Node)
	if !ok || !node.Spec.Unschedulable {
		return nil
	}
	var games palworldv1alpha1.PalworldGameList
	if err := r.List(ctx, &games); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for i := range games.Items {
		g := &games.Items[i]
		if g.Status.CurrentNode == node.Name {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{Namespace: g.Namespace, Name: g.Name},
			})
		}
	}
	return reqs
}
