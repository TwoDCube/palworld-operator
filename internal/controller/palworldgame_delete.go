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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
	"github.com/twodcube/palworld-operator/internal/resources"
)

// finalBackupMaxWait bounds how long deletion waits on a final backup so a
// broken backup path never wedges deletion forever.
const finalBackupMaxWait = 15 * time.Minute

// reconcileDelete runs finalizer cleanup: an optional final backup, PVC
// retention handling, then finalizer removal.
func (r *PalworldGameReconciler) reconcileDelete(ctx context.Context, game *palworldv1alpha1.PalworldGame) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if !containsString(game.Finalizers, palworldv1alpha1.GameFinalizer) {
		return ctrl.Result{}, nil
	}

	game.Status.Phase = palworldv1alpha1.PhaseTerminating
	_ = r.Status().Update(ctx, game)

	// Optional final backup before teardown (server pod is still running).
	if game.Spec.Backup != nil && game.Spec.Backup.OnDelete {
		done, err := r.ensureFinalBackup(ctx, game)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !done {
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
	}

	// Storage retention: the data PVC (from volumeClaimTemplates) is not garbage
	// collected with the StatefulSet, so remove it explicitly unless retained.
	if !game.Spec.Storage.Retain {
		if err := r.deletePVC(ctx, game); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		logger.Info("retaining world-data PVC", "pvc", resources.DataPVCName(game))
	}

	// Remove the finalizer to allow deletion (owned resources GC via owner refs).
	game.Finalizers = removeString(game.Finalizers, palworldv1alpha1.GameFinalizer)
	if err := r.Update(ctx, game); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// ensureFinalBackup creates (once) and waits for a final backup. The backup is
// intentionally not owner-referenced to the game so it survives deletion. It is
// bounded by finalBackupMaxWait so a broken backup never blocks deletion.
func (r *PalworldGameReconciler) ensureFinalBackup(ctx context.Context, game *palworldv1alpha1.PalworldGame) (bool, error) {
	if game.DeletionTimestamp != nil && time.Since(game.DeletionTimestamp.Time) > finalBackupMaxWait {
		r.Recorder.Event(game, corev1.EventTypeWarning, "FinalBackupTimeout",
			"Final backup did not complete within the deadline; proceeding with deletion")
		return true, nil
	}

	name := fmt.Sprintf("%s-final", game.Name)
	var backup palworldv1alpha1.PalworldBackup
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: game.Namespace}, &backup)
	if apierrors.IsNotFound(err) {
		dest := game.Spec.Backup.Destination
		newBackup := &palworldv1alpha1.PalworldBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: game.Namespace,
				Labels:    resources.CommonLabels(game),
			},
			Spec: palworldv1alpha1.PalworldBackupSpec{
				GameRef:     game.Name,
				Destination: dest,
				FlushSave:   true,
				Retain:      true,
			},
		}
		if err := r.Create(ctx, newBackup); err != nil && !apierrors.IsAlreadyExists(err) {
			return false, err
		}
		r.Recorder.Eventf(game, corev1.EventTypeNormal, "FinalBackup", "Created final backup %s", name)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	switch backup.Status.Phase {
	case palworldv1alpha1.BackupPhaseCompleted:
		return true, nil
	case palworldv1alpha1.BackupPhaseFailed:
		r.Recorder.Eventf(game, corev1.EventTypeWarning, "FinalBackupFailed",
			"Final backup %s failed; proceeding with deletion", name)
		return true, nil
	default:
		return false, nil
	}
}

func (r *PalworldGameReconciler) deletePVC(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	pvc := &corev1.PersistentVolumeClaim{}
	pvc.Name = resources.DataPVCName(game)
	pvc.Namespace = game.Namespace
	if err := r.Delete(ctx, pvc); err != nil {
		return client.IgnoreNotFound(err)
	}
	return nil
}
