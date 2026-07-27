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
	"sort"
	"time"

	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
	"github.com/twodcube/palworld-operator/internal/resources"
)

// LabelScheduledBackup marks PalworldBackups created by the schedule (as
// opposed to on-demand or pre-update backups) so retention only prunes those.
const LabelScheduledBackup = "palworld.twodcube.io/scheduled-backup"

// reconcileScheduledBackups creates scheduled backups and enforces retention.
func (r *PalworldGameReconciler) reconcileScheduledBackups(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	pol := game.Spec.Backup
	if pol == nil || !pol.Enabled || pol.Suspend || pol.Schedule == "" {
		game.Status.NextScheduledBackup = nil
		return nil
	}
	sched, err := cron.ParseStandard(pol.Schedule)
	if err != nil {
		return fmt.Errorf("invalid backup schedule %q: %w", pol.Schedule, err)
	}
	now := time.Now()

	if game.Status.NextScheduledBackup == nil {
		next := metav1.NewTime(sched.Next(now))
		game.Status.NextScheduledBackup = &next
		return r.enforceBackupRetention(ctx, game)
	}

	if now.After(game.Status.NextScheduledBackup.Time) {
		if err := r.createScheduledBackup(ctx, game, now); err != nil {
			return err
		}
		next := metav1.NewTime(sched.Next(now))
		game.Status.NextScheduledBackup = &next
	}
	return r.enforceBackupRetention(ctx, game)
}

func (r *PalworldGameReconciler) createScheduledBackup(ctx context.Context, game *palworldv1alpha1.PalworldGame, now time.Time) error {
	name := fmt.Sprintf("%s-scheduled-%d", game.Name, now.Unix())
	labels := resources.CommonLabels(game)
	labels[LabelScheduledBackup] = "true"
	backup := &palworldv1alpha1.PalworldBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: game.Namespace,
			Labels:    labels,
		},
		Spec: palworldv1alpha1.PalworldBackupSpec{
			GameRef:     game.Name,
			Destination: game.Spec.Backup.Destination,
			FlushSave:   true,
		},
	}
	if err := controllerutil.SetControllerReference(game, backup, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, backup); err != nil {
		return err
	}
	r.Recorder.Eventf(game, corev1.EventTypeNormal, "ScheduledBackup", "Created scheduled backup %s", name)
	return nil
}

// enforceBackupRetention deletes the oldest completed scheduled backups beyond
// the retention count (Retain-marked backups are never pruned).
func (r *PalworldGameReconciler) enforceBackupRetention(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	retention := int32(7)
	if game.Spec.Backup != nil {
		retention = game.Spec.Backup.Retention
	}
	if retention <= 0 {
		return nil // keep all
	}

	var list palworldv1alpha1.PalworldBackupList
	if err := r.List(ctx, &list, client.InNamespace(game.Namespace),
		client.MatchingLabels{LabelScheduledBackup: "true", "app.kubernetes.io/instance": game.Name}); err != nil {
		return err
	}
	completed := make([]palworldv1alpha1.PalworldBackup, 0, len(list.Items))
	for _, b := range list.Items {
		if b.Spec.GameRef != game.Name || b.Spec.Retain {
			continue
		}
		if b.Status.Phase == palworldv1alpha1.BackupPhaseCompleted && b.Status.CompletionTime != nil {
			completed = append(completed, b)
		}
	}
	if int32(len(completed)) <= retention {
		return nil
	}
	sort.Slice(completed, func(i, j int) bool {
		return completed[i].Status.CompletionTime.After(completed[j].Status.CompletionTime.Time)
	})
	for _, b := range completed[retention:] {
		victim := b
		if err := r.Delete(ctx, &victim); err != nil && client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}
