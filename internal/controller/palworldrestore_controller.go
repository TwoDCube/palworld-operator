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

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
	"github.com/twodcube/palworld-operator/internal/resources"
)

// PalworldRestoreReconciler reconciles a PalworldRestore object.
type PalworldRestoreReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	DefaultServerImage string
	openShift          bool
	probed             bool
}

type restorePlan struct {
	tarball  bool
	snapshot string
	dest     palworldv1alpha1.BackupDestination
	key      string
}

// +kubebuilder:rbac:groups=palworld.twodcube.io,resources=palworldrestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=palworld.twodcube.io,resources=palworldrestores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=palworld.twodcube.io,resources=palworldgames,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=palworld.twodcube.io,resources=palworldbackups,verbs=get;list;watch

// Reconcile drives a PalworldRestore through its lifecycle.
func (r *PalworldRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var restore palworldv1alpha1.PalworldRestore
	if err := r.Get(ctx, req.NamespacedName, &restore); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !r.probed {
		r.openShift = hasAPI(ctx, r.Client, resources.RouteGVK)
		r.probed = true
	}

	if restore.Status.Phase == palworldv1alpha1.RestorePhaseCompleted ||
		restore.Status.Phase == palworldv1alpha1.RestorePhaseFailed {
		return ctrl.Result{}, nil
	}

	var game palworldv1alpha1.PalworldGame
	if err := r.Get(ctx, client.ObjectKey{Name: restore.Spec.GameRef, Namespace: restore.Namespace}, &game); err != nil {
		if apierrors.IsNotFound(err) {
			return r.failRestore(ctx, &restore, "GameNotFound", "referenced PalworldGame not found")
		}
		return ctrl.Result{}, err
	}

	plan, err := r.resolvePlan(ctx, &restore)
	if err != nil {
		return r.failRestore(ctx, &restore, "InvalidSource", err.Error())
	}

	switch restore.Status.Phase {
	case "", palworldv1alpha1.RestorePhasePending:
		return r.stopGame(ctx, &restore, &game)
	case palworldv1alpha1.RestorePhaseStopping:
		return r.awaitStopped(ctx, &restore, &game, plan)
	case palworldv1alpha1.RestorePhaseRestoring:
		return r.awaitRestore(ctx, &restore, &game, plan)
	case palworldv1alpha1.RestorePhaseStarting:
		return r.startGame(ctx, &restore, &game)
	}
	return ctrl.Result{}, nil
}

// resolvePlan determines how to restore: from a snapshot or a tarball, and from
// where.
func (r *PalworldRestoreReconciler) resolvePlan(ctx context.Context, restore *palworldv1alpha1.PalworldRestore) (restorePlan, error) {
	if restore.Spec.BackupRef != "" {
		var backup palworldv1alpha1.PalworldBackup
		if err := r.Get(ctx, client.ObjectKey{Name: restore.Spec.BackupRef, Namespace: restore.Namespace}, &backup); err != nil {
			return restorePlan{}, fmt.Errorf("backup %q: %w", restore.Spec.BackupRef, err)
		}
		if backup.Status.Phase != palworldv1alpha1.BackupPhaseCompleted {
			return restorePlan{}, fmt.Errorf("backup %q is not completed", restore.Spec.BackupRef)
		}
		if backup.Status.VolumeSnapshotName != "" {
			return restorePlan{snapshot: backup.Status.VolumeSnapshotName}, nil
		}
		return restorePlan{
			tarball: true,
			dest:    backup.Spec.Destination,
			key:     restore.Spec.GameRef + "/" + backup.Name + ".tar.gz",
		}, nil
	}
	if restore.Spec.Source != nil {
		src := *restore.Spec.Source
		if src.Type == palworldv1alpha1.BackupDestinationVolumeSnapshot {
			return restorePlan{}, fmt.Errorf("direct VolumeSnapshot source requires backupRef")
		}
		// For a direct Source the S3 Prefix (or PVC subpath) is the full object key.
		key := ""
		if src.S3 != nil {
			key = src.S3.Prefix
			src.S3.Prefix = ""
		}
		return restorePlan{tarball: true, dest: src, key: key}, nil
	}
	return restorePlan{}, fmt.Errorf("one of backupRef or source must be set")
}

func (r *PalworldRestoreReconciler) stopGame(ctx context.Context, restore *palworldv1alpha1.PalworldRestore, game *palworldv1alpha1.PalworldGame) (ctrl.Result, error) {
	if resources.DesiredReplicas(game) != 0 && !restore.Spec.Force {
		return r.failRestore(ctx, restore, "GameRunning", "game is running; set spec.force=true to stop it for restore")
	}
	if restore.Status.StartTime == nil {
		now := metav1.Now()
		restore.Status.StartTime = &now
	}
	if err := r.scaleGame(ctx, game, 0); err != nil {
		return ctrl.Result{}, err
	}
	restore.Status.Phase = palworldv1alpha1.RestorePhaseStopping
	restore.Status.Message = "Stopping server for restore"
	r.Recorder.Event(restore, corev1.EventTypeNormal, "Stopping", "Scaling game to 0 for restore")
	return r.requeueRestore(ctx, restore, 5*time.Second)
}

func (r *PalworldRestoreReconciler) awaitStopped(ctx context.Context, restore *palworldv1alpha1.PalworldRestore, game *palworldv1alpha1.PalworldGame, plan restorePlan) (ctrl.Result, error) {
	// Wait until the StatefulSet has no running pods before touching the volume.
	var sts appsv1.StatefulSet
	err := r.Get(ctx, client.ObjectKey{Name: resources.StatefulSetName(game), Namespace: game.Namespace}, &sts)
	if err == nil && (sts.Status.Replicas > 0 || sts.Status.ReadyReplicas > 0) {
		return r.requeueRestore(ctx, restore, 5*time.Second)
	}
	// The actual restore work (job for tarballs, PVC swap for snapshots) is
	// driven by awaitRestore so each step gets its own reconcile and can be
	// retried idempotently.
	restore.Status.Phase = palworldv1alpha1.RestorePhaseRestoring
	restore.Status.Message = "Restoring world data"
	return r.requeueRestore(ctx, restore, 2*time.Second)
}

// restoredAnnotation marks a data PVC that has been recreated from a snapshot so
// the restore machine can tell the restored volume from the pre-existing one.
const restoredAnnotation = "palworld.twodcube.io/restored-from"

func (r *PalworldRestoreReconciler) awaitRestore(ctx context.Context, restore *palworldv1alpha1.PalworldRestore, game *palworldv1alpha1.PalworldGame, plan restorePlan) (ctrl.Result, error) {
	if plan.tarball {
		return r.awaitTarballRestore(ctx, restore, game, plan)
	}
	return r.awaitSnapshotRestore(ctx, restore, game, plan)
}

// awaitTarballRestore ensures the restore Job exists and watches it to
// completion. The Job mounts the (now-idle) data PVC and extracts the tarball.
func (r *PalworldRestoreReconciler) awaitTarballRestore(ctx context.Context, restore *palworldv1alpha1.PalworldRestore, game *palworldv1alpha1.PalworldGame, plan restorePlan) (ctrl.Result, error) {
	var job batchv1.Job
	err := r.Get(ctx, client.ObjectKey{Name: resources.RestoreJobName(restore.Name), Namespace: restore.Namespace}, &job)
	if apierrors.IsNotFound(err) {
		image := resources.ResolveServerImage(game, resources.BuildParams{DefaultServerImage: r.serverImage()})
		newJob := resources.DesiredRestoreJob(game, restore.Name, image, plan.key, plan.dest, r.openShift)
		if err := controllerutil.SetControllerReference(restore, newJob, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, newJob); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		restore.Status.JobName = newJob.Name
		return r.requeueRestore(ctx, restore, 10*time.Second)
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if job.Status.Succeeded >= 1 {
		restore.Status.Phase = palworldv1alpha1.RestorePhaseStarting
		return r.requeueRestore(ctx, restore, 2*time.Second)
	}
	if isJobFailed(&job) {
		return r.failRestore(ctx, restore, "RestoreFailed", "restore job failed")
	}
	return r.requeueRestore(ctx, restore, 10*time.Second)
}

// awaitSnapshotRestore swaps the data PVC for one cloned from the snapshot. It
// only advances to Starting once the RESTORED PVC actually exists, so the
// StatefulSet never provisions an empty volume from its template.
func (r *PalworldRestoreReconciler) awaitSnapshotRestore(ctx context.Context, restore *palworldv1alpha1.PalworldRestore, game *palworldv1alpha1.PalworldGame, plan restorePlan) (ctrl.Result, error) {
	name := resources.DataPVCName(game)
	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: game.Namespace}, pvc)
	if err == nil {
		// A PVC exists. If it is the one we already restored from this
		// snapshot, we're done; otherwise it is the old volume and must go.
		if pvc.Annotations[restoredAnnotation] == plan.snapshot {
			restore.Status.Phase = palworldv1alpha1.RestorePhaseStarting
			return r.requeueRestore(ctx, restore, 2*time.Second)
		}
		if pvc.DeletionTimestamp == nil {
			if err := r.Delete(ctx, pvc); err != nil {
				return ctrl.Result{}, client.IgnoreNotFound(err)
			}
		}
		// Wait for the old PVC to disappear before recreating.
		return r.requeueRestore(ctx, restore, 3*time.Second)
	}
	if !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	// Old PVC gone: create the restored one from the snapshot.
	newPVC := resources.DesiredRestorePVCFromSnapshot(game, name, plan.snapshot)
	if newPVC.Annotations == nil {
		newPVC.Annotations = map[string]string{}
	}
	newPVC.Annotations[restoredAnnotation] = plan.snapshot
	// Not owner-referenced to the restore: the restored data must outlive it.
	if err := r.Create(ctx, newPVC); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, err
	}
	return r.requeueRestore(ctx, restore, 3*time.Second)
}

func (r *PalworldRestoreReconciler) startGame(ctx context.Context, restore *palworldv1alpha1.PalworldRestore, game *palworldv1alpha1.PalworldGame) (ctrl.Result, error) {
	if err := r.scaleGame(ctx, game, 1); err != nil {
		return ctrl.Result{}, err
	}
	now := metav1.Now()
	restore.Status.CompletionTime = &now
	restore.Status.Phase = palworldv1alpha1.RestorePhaseCompleted
	restore.Status.Message = "Restore completed; server restarting"
	setStatusCondition(&restore.Status.Conditions, "Ready", metav1.ConditionTrue, "Completed", "Restore completed", restore.Generation)
	r.Recorder.Event(restore, corev1.EventTypeNormal, "Restored", "World restored; game scaled back up")
	return ctrl.Result{}, r.Status().Update(ctx, restore)
}

func (r *PalworldRestoreReconciler) scaleGame(ctx context.Context, game *palworldv1alpha1.PalworldGame, replicas int32) error {
	var fresh palworldv1alpha1.PalworldGame
	if err := r.Get(ctx, client.ObjectKeyFromObject(game), &fresh); err != nil {
		return err
	}
	if fresh.Spec.Replicas != nil && *fresh.Spec.Replicas == replicas {
		return nil
	}
	fresh.Spec.Replicas = &replicas
	return r.Update(ctx, &fresh)
}

func (r *PalworldRestoreReconciler) failRestore(ctx context.Context, restore *palworldv1alpha1.PalworldRestore, reason, message string) (ctrl.Result, error) {
	now := metav1.Now()
	restore.Status.CompletionTime = &now
	restore.Status.Phase = palworldv1alpha1.RestorePhaseFailed
	restore.Status.Message = message
	setStatusCondition(&restore.Status.Conditions, "Ready", metav1.ConditionFalse, reason, message, restore.Generation)
	r.Recorder.Event(restore, corev1.EventTypeWarning, reason, message)
	return ctrl.Result{}, r.Status().Update(ctx, restore)
}

func (r *PalworldRestoreReconciler) requeueRestore(ctx context.Context, restore *palworldv1alpha1.PalworldRestore, after time.Duration) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, restore); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: after}, nil
}

func (r *PalworldRestoreReconciler) serverImage() string {
	if r.DefaultServerImage != "" {
		return r.DefaultServerImage
	}
	return defaultServerImageFallback
}

// SetupWithManager wires the controller into the manager.
func (r *PalworldRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.DefaultServerImage == "" {
		r.DefaultServerImage = envOr("DEFAULT_SERVER_IMAGE", defaultServerImageFallback)
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&palworldv1alpha1.PalworldRestore{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
