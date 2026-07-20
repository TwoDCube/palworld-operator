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
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
	"github.com/twodcube/palworld-operator/internal/resources"
)

// PalworldBackupReconciler reconciles a PalworldBackup object.
type PalworldBackupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	DefaultServerImage string

	capOnce     sync.Once
	openShift   bool
	hasSnapshot bool
}

// +kubebuilder:rbac:groups=palworld.twodcube.io,resources=palworldbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=palworld.twodcube.io,resources=palworldbackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=palworld.twodcube.io,resources=palworldgames,verbs=get;list;watch
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives a PalworldBackup through its lifecycle.
func (r *PalworldBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var backup palworldv1alpha1.PalworldBackup
	if err := r.Get(ctx, req.NamespacedName, &backup); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	r.probe(ctx)

	// Terminal states: only TTL cleanup remains.
	if backup.Status.Phase == palworldv1alpha1.BackupPhaseCompleted ||
		backup.Status.Phase == palworldv1alpha1.BackupPhaseFailed {
		return r.handleTTL(ctx, &backup)
	}

	var game palworldv1alpha1.PalworldGame
	if err := r.Get(ctx, client.ObjectKey{Name: backup.Spec.GameRef, Namespace: backup.Namespace}, &game); err != nil {
		if apierrors.IsNotFound(err) {
			return r.fail(ctx, &backup, "GameNotFound",
				fmt.Sprintf("referenced PalworldGame %q not found", backup.Spec.GameRef))
		}
		return ctrl.Result{}, err
	}

	switch backup.Status.Phase {
	case "", palworldv1alpha1.BackupPhasePending:
		return r.start(ctx, &backup, &game)
	case palworldv1alpha1.BackupPhaseSaving, palworldv1alpha1.BackupPhaseSnapshotting:
		return r.reconcileSnapshot(ctx, &backup, &game)
	case palworldv1alpha1.BackupPhaseUploading:
		return r.reconcileUpload(ctx, &backup, &game)
	}
	return ctrl.Result{}, nil
}

func (r *PalworldBackupReconciler) probe(ctx context.Context) {
	r.capOnce.Do(func() {
		r.openShift = hasAPI(ctx, r.Client, resources.RouteGVK)
		r.hasSnapshot = hasAPI(ctx, r.Client, resources.VolumeSnapshotGVK)
	})
}

// start records timing, flushes the save, and creates the snapshot (or, when
// snapshots are unavailable but the volume is RWX, a direct upload job).
func (r *PalworldBackupReconciler) start(ctx context.Context, backup *palworldv1alpha1.PalworldBackup, game *palworldv1alpha1.PalworldGame) (ctrl.Result, error) {
	if backup.Status.StartTime == nil {
		now := metav1.Now()
		backup.Status.StartTime = &now
	}
	backup.Status.ServerVersion = game.Status.CurrentVersion
	if err := controllerutil.SetControllerReference(game, backup, r.Scheme); err != nil {
		// Non-fatal: the backup may be intentionally unowned (final backup).
		_ = err
	}

	// Flush the world to disk for an application-consistent backup.
	if backup.Spec.FlushSave {
		backup.Status.Phase = palworldv1alpha1.BackupPhaseSaving
		if rc, err := restClientFor(ctx, r.Client, game); err == nil {
			if err := rc.Save(ctx); err != nil {
				log.FromContext(ctx).Info("flush save failed; continuing with crash-consistent backup", "error", err.Error())
			} else {
				// Give the write a moment to settle before snapshotting.
				time.Sleep(2 * time.Second)
			}
		}
	}

	dtype := backup.Spec.Destination.Type
	if dtype == "" {
		dtype = palworldv1alpha1.BackupDestinationVolumeSnapshot
	}

	if r.hasSnapshot {
		snap := resources.DesiredVolumeSnapshot(game, backup)
		if err := controllerutil.SetControllerReference(backup, snap, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, snap); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		backup.Status.VolumeSnapshotName = resources.SnapshotName(backup)
		backup.Status.Phase = palworldv1alpha1.BackupPhaseSnapshotting
		r.setBackupCondition(backup, "Snapshotting", metav1.ConditionFalse, "Creating VolumeSnapshot")
		return r.requeueBackup(ctx, backup, 5*time.Second)
	}

	// No snapshot support: only tarball destinations onto an RWX volume work.
	if dtype == palworldv1alpha1.BackupDestinationVolumeSnapshot {
		return r.fail(ctx, backup, "NoSnapshotSupport",
			"cluster has no VolumeSnapshot API; use an S3 or PVC destination on a ReadWriteMany volume")
	}
	if !hasRWX(game) {
		return r.fail(ctx, backup, "NoSnapshotSupport",
			"tarball backup without VolumeSnapshot support requires a ReadWriteMany data volume")
	}
	// Directly upload from the live RWX volume.
	return r.createUploadJob(ctx, backup, game, resources.DataPVCName(game))
}

// reconcileSnapshot waits for the snapshot to be ready, then completes (for the
// VolumeSnapshot destination) or provisions a clone PVC + upload job.
func (r *PalworldBackupReconciler) reconcileSnapshot(ctx context.Context, backup *palworldv1alpha1.PalworldBackup, game *palworldv1alpha1.PalworldGame) (ctrl.Result, error) {
	snap := &unstructured.Unstructured{}
	snap.SetGroupVersionKind(resources.VolumeSnapshotGVK)
	if err := r.Get(ctx, client.ObjectKey{Name: resources.SnapshotName(backup), Namespace: backup.Namespace}, snap); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	ready, size := resources.SnapshotReady(snap)
	if !ready {
		return r.requeueBackup(ctx, backup, 10*time.Second)
	}
	backup.Status.SizeBytes = size

	dtype := backup.Spec.Destination.Type
	if dtype == "" || dtype == palworldv1alpha1.BackupDestinationVolumeSnapshot {
		backup.Status.Location = "volumesnapshot://" + backup.Namespace + "/" + resources.SnapshotName(backup)
		return r.complete(ctx, backup)
	}

	// Provision a clone PVC from the snapshot for a contention-free tar/upload.
	pvc := resources.DesiredRestorePVCFromSnapshot(game, resources.RestorePVCName(backup.Name), resources.SnapshotName(backup))
	if err := controllerutil.SetControllerReference(backup, pvc, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Create(ctx, pvc); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, err
	}
	return r.createUploadJob(ctx, backup, game, resources.RestorePVCName(backup.Name))
}

func (r *PalworldBackupReconciler) createUploadJob(ctx context.Context, backup *palworldv1alpha1.PalworldBackup, game *palworldv1alpha1.PalworldGame, srcPVC string) (ctrl.Result, error) {
	image := resources.ResolveServerImage(game, resources.BuildParams{DefaultServerImage: r.serverImage()})
	job := resources.DesiredBackupJob(game, backup, image, srcPVC, r.openShift)
	if err := controllerutil.SetControllerReference(backup, job, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, err
	}
	backup.Status.JobName = job.Name
	backup.Status.Phase = palworldv1alpha1.BackupPhaseUploading
	r.setBackupCondition(backup, "Uploading", metav1.ConditionFalse, "Exporting backup")
	return r.requeueBackup(ctx, backup, 10*time.Second)
}

// reconcileUpload watches the upload Job to completion.
func (r *PalworldBackupReconciler) reconcileUpload(ctx context.Context, backup *palworldv1alpha1.PalworldBackup, game *palworldv1alpha1.PalworldGame) (ctrl.Result, error) {
	var job batchv1.Job
	if err := r.Get(ctx, client.ObjectKey{Name: resources.BackupJobName(backup.Name), Namespace: backup.Namespace}, &job); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if job.Status.Succeeded >= 1 {
		backup.Status.Location = r.uploadLocation(backup, game)
		r.cleanupTransient(ctx, backup, game)
		return r.complete(ctx, backup)
	}
	if isJobFailed(&job) {
		r.cleanupTransient(ctx, backup, game)
		return r.fail(ctx, backup, "UploadFailed", "backup upload job failed")
	}
	return r.requeueBackup(ctx, backup, 10*time.Second)
}

func (r *PalworldBackupReconciler) uploadLocation(backup *palworldv1alpha1.PalworldBackup, game *palworldv1alpha1.PalworldGame) string {
	switch backup.Spec.Destination.Type {
	case palworldv1alpha1.BackupDestinationS3:
		if s3 := backup.Spec.Destination.S3; s3 != nil {
			prefix := ""
			if s3.Prefix != "" {
				prefix = s3.Prefix + "/"
			}
			return fmt.Sprintf("s3://%s/%s%s/%s.tar.gz", s3.Bucket, prefix, game.Name, backup.Name)
		}
	case palworldv1alpha1.BackupDestinationPVC:
		return fmt.Sprintf("pvc://%s/%s/%s.tar.gz", backup.Spec.Destination.PVCName, game.Name, backup.Name)
	}
	return ""
}

// cleanupTransient removes the clone PVC and (for non-snapshot destinations) the
// transient snapshot once an upload has finished.
func (r *PalworldBackupReconciler) cleanupTransient(ctx context.Context, backup *palworldv1alpha1.PalworldBackup, game *palworldv1alpha1.PalworldGame) {
	pvc := &corev1.PersistentVolumeClaim{}
	pvc.Name = resources.RestorePVCName(backup.Name)
	pvc.Namespace = backup.Namespace
	_ = client.IgnoreNotFound(r.Delete(ctx, pvc))

	// The snapshot is transient for S3/PVC destinations.
	snap := &unstructured.Unstructured{}
	snap.SetGroupVersionKind(resources.VolumeSnapshotGVK)
	snap.SetName(resources.SnapshotName(backup))
	snap.SetNamespace(backup.Namespace)
	_ = client.IgnoreNotFound(r.Delete(ctx, snap))
}

func (r *PalworldBackupReconciler) complete(ctx context.Context, backup *palworldv1alpha1.PalworldBackup) (ctrl.Result, error) {
	now := metav1.Now()
	backup.Status.CompletionTime = &now
	backup.Status.Phase = palworldv1alpha1.BackupPhaseCompleted
	backup.Status.Message = "Backup completed"
	r.setBackupCondition(backup, "Completed", metav1.ConditionTrue, "Backup completed")
	r.Recorder.Eventf(backup, corev1.EventTypeNormal, "BackupCompleted", "Backup stored at %s", backup.Status.Location)
	return ctrl.Result{}, r.Status().Update(ctx, backup)
}

func (r *PalworldBackupReconciler) fail(ctx context.Context, backup *palworldv1alpha1.PalworldBackup, reason, message string) (ctrl.Result, error) {
	now := metav1.Now()
	backup.Status.CompletionTime = &now
	backup.Status.Phase = palworldv1alpha1.BackupPhaseFailed
	backup.Status.Message = message
	r.setBackupCondition(backup, reason, metav1.ConditionFalse, message)
	r.Recorder.Event(backup, corev1.EventTypeWarning, reason, message)
	return ctrl.Result{}, r.Status().Update(ctx, backup)
}

func (r *PalworldBackupReconciler) requeueBackup(ctx context.Context, backup *palworldv1alpha1.PalworldBackup, after time.Duration) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, backup); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: after}, nil
}

func (r *PalworldBackupReconciler) handleTTL(ctx context.Context, backup *palworldv1alpha1.PalworldBackup) (ctrl.Result, error) {
	if backup.Spec.TTLSecondsAfterFinished == nil || backup.Status.CompletionTime == nil {
		return ctrl.Result{}, nil
	}
	ttl := time.Duration(*backup.Spec.TTLSecondsAfterFinished) * time.Second
	deadline := backup.Status.CompletionTime.Add(ttl)
	if time.Now().After(deadline) {
		return ctrl.Result{}, client.IgnoreNotFound(r.Delete(ctx, backup))
	}
	return ctrl.Result{RequeueAfter: time.Until(deadline)}, nil
}

func (r *PalworldBackupReconciler) setBackupCondition(backup *palworldv1alpha1.PalworldBackup, reason string, status metav1.ConditionStatus, message string) {
	setStatusCondition(&backup.Status.Conditions, "Ready", status, reason, message, backup.Generation)
}

func (r *PalworldBackupReconciler) serverImage() string {
	if r.DefaultServerImage != "" {
		return r.DefaultServerImage
	}
	return defaultServerImageFallback
}

// SetupWithManager wires the controller into the manager.
func (r *PalworldBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.DefaultServerImage == "" {
		r.DefaultServerImage = envOr("DEFAULT_SERVER_IMAGE", defaultServerImageFallback)
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&palworldv1alpha1.PalworldBackup{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

func hasRWX(game *palworldv1alpha1.PalworldGame) bool {
	for _, m := range game.Spec.Storage.AccessModes {
		if m == corev1.ReadWriteMany {
			return true
		}
	}
	return false
}

func isJobFailed(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
