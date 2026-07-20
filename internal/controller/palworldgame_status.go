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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
	"github.com/twodcube/palworld-operator/internal/resources"
)

// updateStatus persists the status subresource.
func (r *PalworldGameReconciler) updateStatus(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	game.Status.ObservedGeneration = game.Generation
	return r.Status().Update(ctx, game)
}

// reconcileObservedStatus refreshes the observed runtime state: pod readiness,
// endpoints, and live player/version data from the REST API. Best-effort: it
// never returns an error, only records what it can observe.
func (r *PalworldGameReconciler) reconcileObservedStatus(ctx context.Context, game *palworldv1alpha1.PalworldGame) {
	game.Status.Selector = gameSelector(game).String()
	game.Status.PersistentVolumeClaim = resources.DataPVCName(game)
	game.Status.RESTEndpoint = fmt.Sprintf("%s:%d", adminHost(game), resources.RESTPort)
	game.Status.MaxPlayers = maxPlayers(game)
	game.Status.GameEndpoint = r.gameEndpoint(ctx, game)
	if url := r.routeURL(ctx, game); url != "" {
		game.Status.RouteURL = url
	}
	r.observeBackups(ctx, game)

	desired := resources.DesiredReplicas(game)

	var sts appsv1.StatefulSet
	err := r.Get(ctx, client.ObjectKey{Name: resources.StatefulSetName(game), Namespace: game.Namespace}, &sts)
	if apierrors.IsNotFound(err) {
		game.Status.Replicas = 0
		game.Status.Phase = palworldv1alpha1.PhasePending
		r.setReady(game, false, "Pending", "StatefulSet not yet created")
		return
	}
	if err != nil {
		return
	}
	game.Status.Replicas = sts.Status.ReadyReplicas

	if desired == 0 {
		game.Status.Phase = palworldv1alpha1.PhaseStopped
		game.Status.PlayersOnline = 0
		r.setReady(game, false, "Stopped", "Server is stopped (replicas=0)")
		r.clearProgressing(game, "Stopped", "Server is stopped")
		return
	}

	if sts.Status.ReadyReplicas < 1 {
		phase := palworldv1alpha1.PhaseInstalling
		msg := "Server pod is installing/starting"
		if game.Status.CurrentVersion != "" {
			phase = palworldv1alpha1.PhaseUpdating
			msg = "Server pod is restarting"
		}
		game.Status.Phase = phase
		r.setReady(game, false, "Starting", msg)
		r.setProgressing(game, "Starting", msg)
		return
	}

	// Pod is Ready; query the live REST API for authoritative runtime info.
	r.observeLive(ctx, game)
}

// observeLive queries the REST API for players/version and finalizes phase.
func (r *PalworldGameReconciler) observeLive(ctx context.Context, game *palworldv1alpha1.PalworldGame) {
	rc, err := restClientFor(ctx, r.Client, game)
	if err != nil {
		game.Status.Phase = palworldv1alpha1.PhaseRunning
		r.setReady(game, true, "Running", "Server pod is ready")
		return
	}

	if info, err := rc.Info(ctx); err == nil {
		game.Status.ServerName = info.ServerName
		// The in-game version string lives in its own field so it never
		// pollutes CurrentVersion, which the update controller manages as a
		// Steam build id.
		game.Status.ServerVersion = info.Version
	}
	if metrics, err := rc.Metrics(ctx); err == nil {
		game.Status.PlayersOnline = metrics.CurrentPlayerNum
		if metrics.MaxPlayerNum > 0 {
			game.Status.MaxPlayers = metrics.MaxPlayerNum
		}
	}

	game.Status.Phase = palworldv1alpha1.PhaseRunning
	r.setReady(game, true, "Running", "Server is running and reachable")
	r.clearProgressing(game, "Running", "Reconcile complete")
	r.setDegraded(game, false, "Running", "Server healthy")
}

func maxPlayers(game *palworldv1alpha1.PalworldGame) int32 {
	if game.Spec.ServerSettings.ServerPlayerMaxNum > 0 {
		return game.Spec.ServerSettings.ServerPlayerMaxNum
	}
	return 32
}

// gameEndpoint derives a human-readable address for the public game port.
func (r *PalworldGameReconciler) gameEndpoint(ctx context.Context, game *palworldv1alpha1.PalworldGame) string {
	var svc corev1.Service
	if err := r.Get(ctx, client.ObjectKey{Name: resources.GameServiceName(game), Namespace: game.Namespace}, &svc); err != nil {
		return ""
	}
	port := resources.GamePort(game)
	switch svc.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		for _, ing := range svc.Status.LoadBalancer.Ingress {
			if ing.IP != "" {
				return fmt.Sprintf("%s:%d", ing.IP, port)
			}
			if ing.Hostname != "" {
				return fmt.Sprintf("%s:%d", ing.Hostname, port)
			}
		}
		return "<pending-loadbalancer>"
	case corev1.ServiceTypeNodePort:
		for _, p := range svc.Spec.Ports {
			if p.Name == "game" && p.NodePort != 0 {
				return fmt.Sprintf("<node-ip>:%d", p.NodePort)
			}
		}
	default:
		if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != corev1.ClusterIPNone {
			return fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, port)
		}
	}
	return ""
}

func (r *PalworldGameReconciler) routeURL(ctx context.Context, game *palworldv1alpha1.PalworldGame) string {
	if !game.Spec.Networking.RESTAPI.Route || !r.hasRoute {
		return ""
	}
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(resources.RouteGVK)
	if err := r.Get(ctx, client.ObjectKey{Name: resources.RouteName(game), Namespace: game.Namespace}, route); err != nil {
		return ""
	}
	if host, found, _ := unstructured.NestedString(route.Object, "spec", "host"); found && host != "" {
		return "https://" + host
	}
	ingress, found, _ := unstructured.NestedSlice(route.Object, "status", "ingress")
	if found {
		for _, ing := range ingress {
			if m, ok := ing.(map[string]any); ok {
				if host, ok := m["host"].(string); ok && host != "" {
					return "https://" + host
				}
			}
		}
	}
	return ""
}

// observeBackups records the most recent successful backup on the game status.
func (r *PalworldGameReconciler) observeBackups(ctx context.Context, game *palworldv1alpha1.PalworldGame) {
	var list palworldv1alpha1.PalworldBackupList
	if err := r.List(ctx, &list, client.InNamespace(game.Namespace)); err != nil {
		return
	}
	for i := range list.Items {
		b := &list.Items[i]
		if b.Spec.GameRef != game.Name {
			continue
		}
		if b.Status.Phase != palworldv1alpha1.BackupPhaseCompleted || b.Status.CompletionTime == nil {
			continue
		}
		if game.Status.LastBackupTime == nil || b.Status.CompletionTime.After(game.Status.LastBackupTime.Time) {
			game.Status.LastBackupTime = b.Status.CompletionTime
			game.Status.LastBackupName = b.Name
		}
	}
}
