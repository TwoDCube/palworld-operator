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
	"time"

	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
	"github.com/twodcube/palworld-operator/internal/palworld"
	"github.com/twodcube/palworld-operator/internal/resources"
)

// reconcileUpdates polls Steam for new builds and applies them per UpdatePolicy.
func (r *PalworldGameReconciler) reconcileUpdates(ctx context.Context, game *palworldv1alpha1.PalworldGame) (ctrl.Result, error) {
	pol := game.Spec.Update
	if pol == nil {
		return ctrl.Result{}, nil
	}
	logger := log.FromContext(ctx)
	now := time.Now()

	// Poll gate: only hit Steam every PollIntervalMinutes.
	interval := time.Duration(pol.PollIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	if game.Status.NextScheduledUpdateCheck == nil || now.After(game.Status.NextScheduledUpdateCheck.Time) {
		poller := palworld.NewSteamPoller(r.SteamInfoEndpoint)
		latest, err := poller.LatestBuildID(ctx, "public")
		if err != nil {
			logger.Info("steam version poll failed", "error", err.Error())
		} else {
			game.Status.AvailableVersion = latest
		}
		next := metav1.NewTime(now.Add(interval))
		game.Status.NextScheduledUpdateCheck = &next
	}

	available := game.Status.AvailableVersion

	// Establish the current build on first successful boot: a fresh install
	// always pulls the latest public build.
	if game.Status.CurrentVersion == "" {
		if game.Status.Phase == palworldv1alpha1.PhaseRunning && available != "" {
			game.Status.CurrentVersion = available
		}
		r.setCondition(game, palworldv1alpha1.ConditionUpdateAvailable, metav1.ConditionFalse, "UpToDate", "No update pending")
		return ctrl.Result{}, nil
	}

	updateAvailable := available != "" && available != game.Status.CurrentVersion
	if !updateAvailable {
		r.setCondition(game, palworldv1alpha1.ConditionUpdateAvailable, metav1.ConditionFalse, "UpToDate",
			fmt.Sprintf("Running build %s", game.Status.CurrentVersion))
		return ctrl.Result{}, nil
	}

	r.setCondition(game, palworldv1alpha1.ConditionUpdateAvailable, metav1.ConditionTrue, "UpdateAvailable",
		fmt.Sprintf("Build %s available (running %s)", available, game.Status.CurrentVersion))

	switch pol.Strategy {
	case palworldv1alpha1.UpdateAutomatic:
		return r.performUpdate(ctx, game, available)
	case palworldv1alpha1.UpdateScheduled:
		if r.inMaintenanceWindow(game, pol, now) {
			return r.performUpdate(ctx, game, available)
		}
		r.Recorder.Eventf(game, corev1.EventTypeNormal, "UpdateDeferred",
			"Build %s available; deferring to maintenance window %q", available, pol.Schedule)
	default: // Manual
		r.Recorder.Eventf(game, corev1.EventTypeNormal, "UpdateAvailable",
			"Build %s available; manual update strategy, not applying automatically", available)
	}
	return ctrl.Result{}, nil
}

// inMaintenanceWindow reports whether now is at/after the next scheduled tick
// since the last update (or creation).
func (r *PalworldGameReconciler) inMaintenanceWindow(game *palworldv1alpha1.PalworldGame, pol *palworldv1alpha1.UpdatePolicy, now time.Time) bool {
	if pol.Schedule == "" {
		return false
	}
	sched, err := cron.ParseStandard(pol.Schedule)
	if err != nil {
		return false
	}
	from := game.CreationTimestamp.Time
	if game.Status.LastUpdateTime != nil && game.Status.LastUpdateTime.Time.After(from) {
		from = game.Status.LastUpdateTime.Time
	}
	next := sched.Next(from)
	return !next.After(now)
}

// performUpdate rolls the server to a new build. If BackupBeforeUpdate is set it
// first ensures a completed pre-update backup, then broadcasts a warning and
// deletes the pod (the StatefulSet recreates it and the entrypoint installs the
// latest build; the preStop hook saves the world first).
func (r *PalworldGameReconciler) performUpdate(ctx context.Context, game *palworldv1alpha1.PalworldGame, available string) (ctrl.Result, error) {
	game.Status.Phase = palworldv1alpha1.PhaseUpdating
	r.setProgressing(game, "Updating", fmt.Sprintf("Applying build %s", available))

	if game.Spec.Update.BackupBeforeUpdate {
		done, err := r.ensurePreUpdateBackup(ctx, game, available)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !done {
			// Wait for the pre-update backup to complete.
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
	}

	// Warn players (best-effort).
	if rc, err := restClientFor(ctx, r.Client, game); err == nil {
		msg := game.Spec.Update.WarnMessage
		if msg == "" {
			msg = "Server will restart for updates shortly"
		}
		_ = rc.Announce(ctx, msg)
	}

	// Trigger a graceful restart by deleting the pod; preStop saves the world.
	if err := r.restartServerPod(ctx, game); err != nil {
		return ctrl.Result{}, err
	}

	now := metav1.Now()
	game.Status.LastUpdateTime = &now
	game.Status.CurrentVersion = available
	r.setCondition(game, palworldv1alpha1.ConditionUpdateAvailable, metav1.ConditionFalse, "Updated",
		fmt.Sprintf("Updated to build %s", available))
	r.Recorder.Eventf(game, corev1.EventTypeNormal, "Updated", "Applied build %s", available)
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// restartServerPod deletes the server pod so the StatefulSet recreates it.
func (r *PalworldGameReconciler) restartServerPod(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	podName := fmt.Sprintf("%s-0", resources.StatefulSetName(game))
	pod := &corev1.Pod{}
	pod.Name = podName
	pod.Namespace = game.Namespace
	if err := r.Delete(ctx, pod); err != nil {
		return client.IgnoreNotFound(err)
	}
	return nil
}

// ensurePreUpdateBackup creates (once) and waits for a pre-update backup.
func (r *PalworldGameReconciler) ensurePreUpdateBackup(ctx context.Context, game *palworldv1alpha1.PalworldGame, available string) (bool, error) {
	name := fmt.Sprintf("%s-preupdate-%s", game.Name, sanitizeName(available))
	var backup palworldv1alpha1.PalworldBackup
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: game.Namespace}, &backup)
	if apierrors.IsNotFound(err) {
		newBackup := preUpdateBackup(game, name)
		if err := controllerutil.SetControllerReference(game, newBackup, r.Scheme); err != nil {
			return false, err
		}
		if err := r.Create(ctx, newBackup); err != nil && !apierrors.IsAlreadyExists(err) {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	switch backup.Status.Phase {
	case palworldv1alpha1.BackupPhaseCompleted:
		return true, nil
	case palworldv1alpha1.BackupPhaseFailed:
		// Don't block updates forever on a failed backup; proceed with a warning.
		r.Recorder.Eventf(game, corev1.EventTypeWarning, "PreUpdateBackupFailed",
			"Pre-update backup %s failed; proceeding with update", name)
		return true, nil
	default:
		return false, nil
	}
}

// sanitizeName lowercases and trims a value to a DNS-1123-safe fragment.
func sanitizeName(s string) string {
	if s == "" {
		return "latest"
	}
	if len(s) > 20 {
		s = s[:20]
	}
	return s
}

func preUpdateBackup(game *palworldv1alpha1.PalworldGame, name string) *palworldv1alpha1.PalworldBackup {
	dest := palworldv1alpha1.BackupDestination{Type: palworldv1alpha1.BackupDestinationVolumeSnapshot}
	if game.Spec.Backup != nil {
		dest = game.Spec.Backup.Destination
	}
	return &palworldv1alpha1.PalworldBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: game.Namespace,
			Labels:    resources.CommonLabels(game),
		},
		Spec: palworldv1alpha1.PalworldBackupSpec{
			GameRef:     game.Name,
			Destination: dest,
			FlushSave:   true,
		},
	}
}
