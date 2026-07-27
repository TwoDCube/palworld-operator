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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

// Source resolution decides the shell command that runs inside the restore Job,
// and it runs before the game is stopped (spec 05). These tests pin both halves:
// that a well-formed source produces the right object key, and that a malformed
// one is rejected up front rather than failing inside the Job after the server
// is already down.

func s3Source(mutate func(*palworldv1alpha1.S3Destination)) palworldv1alpha1.BackupDestination {
	s3 := &palworldv1alpha1.S3Destination{
		Bucket:            "backups",
		Prefix:            "seeds/world.tar.gz",
		CredentialsSecret: "s3-creds",
	}
	if mutate != nil {
		mutate(s3)
	}
	return palworldv1alpha1.BackupDestination{
		Type: palworldv1alpha1.BackupDestinationS3,
		S3:   s3,
	}
}

func TestDirectSourcePlanValidSources(t *testing.T) {
	t.Run("S3 prefix becomes the object key", func(t *testing.T) {
		plan, err := directSourcePlan(s3Source(nil))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !plan.tarball {
			t.Error("expected a tarball plan")
		}
		if plan.key != "seeds/world.tar.gz" {
			t.Errorf("key = %q, want %q", plan.key, "seeds/world.tar.gz")
		}
		// s3Env emits S3_PREFIX and S3_KEY separately and restore.sh joins them,
		// so leaving the prefix set would duplicate it in the remote path.
		if plan.dest.S3.Prefix != "" {
			t.Errorf("dest prefix = %q, want it cleared", plan.dest.S3.Prefix)
		}
	})

	t.Run("PVC path becomes the object key", func(t *testing.T) {
		plan, err := directSourcePlan(palworldv1alpha1.BackupDestination{
			Type:    palworldv1alpha1.BackupDestinationPVC,
			PVCName: "backup-pvc",
			PVCPath: "seeds/world.tar.gz",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !plan.tarball {
			t.Error("expected a tarball plan")
		}
		// The Job runs "restore.sh pvc /backup/<key>"; an empty key would name the
		// mount directory instead of an archive.
		if plan.key != "seeds/world.tar.gz" {
			t.Errorf("key = %q, want %q", plan.key, "seeds/world.tar.gz")
		}
		if plan.dest.PVCName != "backup-pvc" {
			t.Errorf("dest pvcName = %q, want %q", plan.dest.PVCName, "backup-pvc")
		}
	})
}

// The spec the reconciler hands in aliases the live PalworldRestore object, so
// clearing the prefix must not write through to it -- a mutated cache entry
// would lose the key on the next reconcile.
func TestDirectSourcePlanDoesNotMutateCallerSpec(t *testing.T) {
	src := s3Source(nil)
	if _, err := directSourcePlan(src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.S3.Prefix != "seeds/world.tar.gz" {
		t.Errorf("caller's prefix was mutated to %q", src.S3.Prefix)
	}
}

func TestDirectSourcePlanRejectsInvalidSources(t *testing.T) {
	tests := []struct {
		name    string
		src     palworldv1alpha1.BackupDestination
		wantMsg string
	}{
		{
			name:    "VolumeSnapshot needs backupRef",
			src:     palworldv1alpha1.BackupDestination{Type: palworldv1alpha1.BackupDestinationVolumeSnapshot},
			wantMsg: "requires backupRef",
		},
		{
			name:    "S3 without config",
			src:     palworldv1alpha1.BackupDestination{Type: palworldv1alpha1.BackupDestinationS3},
			wantMsg: "source.s3 is required",
		},
		{
			name:    "S3 without bucket",
			src:     s3Source(func(s *palworldv1alpha1.S3Destination) { s.Bucket = "" }),
			wantMsg: "source.s3.bucket is required",
		},
		{
			name:    "S3 without credentials",
			src:     s3Source(func(s *palworldv1alpha1.S3Destination) { s.CredentialsSecret = "" }),
			wantMsg: "source.s3.credentialsSecret is required",
		},
		{
			// Previously accepted, yielding an empty key and a restore.sh failure
			// against the bucket root after the game had been stopped.
			name:    "S3 without prefix carrying the key",
			src:     s3Source(func(s *palworldv1alpha1.S3Destination) { s.Prefix = "" }),
			wantMsg: "source.s3.prefix is required",
		},
		{
			name:    "PVC without name",
			src:     palworldv1alpha1.BackupDestination{Type: palworldv1alpha1.BackupDestinationPVC, PVCPath: "a.tar.gz"},
			wantMsg: "source.pvcName is required",
		},
		{
			// The original gap: no field carried the key, so every direct PVC
			// source resolved to "/backup/" and failed inside the Job.
			name:    "PVC without path",
			src:     palworldv1alpha1.BackupDestination{Type: palworldv1alpha1.BackupDestinationPVC, PVCName: "backup-pvc"},
			wantMsg: "source.pvcPath is required",
		},
		{
			name:    "unknown type",
			src:     palworldv1alpha1.BackupDestination{Type: palworldv1alpha1.BackupDestinationType("Glacier")},
			wantMsg: `unsupported source type "Glacier"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := directSourcePlan(tc.src)
			if err == nil {
				t.Fatalf("expected an error for %+v", tc.src)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

func completedBackup(name, ns, snapshot string, dest palworldv1alpha1.BackupDestination) *palworldv1alpha1.PalworldBackup {
	return &palworldv1alpha1.PalworldBackup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       palworldv1alpha1.PalworldBackupSpec{GameRef: "game", Destination: dest},
		Status: palworldv1alpha1.PalworldBackupStatus{
			Phase:              palworldv1alpha1.BackupPhaseCompleted,
			VolumeSnapshotName: snapshot,
		},
	}
}

func TestResolvePlanFromBackupRefDerivesKey(t *testing.T) {
	s := drainScheme(t)
	dest := palworldv1alpha1.BackupDestination{
		Type:    palworldv1alpha1.BackupDestinationPVC,
		PVCName: "backup-pvc",
	}
	// A tarball backup (no snapshot recorded) -- the key is derived, never taken
	// from pvcPath, which is why pvcPath is a restore-source-only field.
	backup := completedBackup("nightly", "ns", "", dest)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(backup).Build()
	r := &PalworldRestoreReconciler{Client: c, Scheme: s}

	restore := &palworldv1alpha1.PalworldRestore{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "ns"},
		Spec:       palworldv1alpha1.PalworldRestoreSpec{GameRef: "game", BackupRef: "nightly"},
	}
	plan, err := r.resolvePlan(context.Background(), restore)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !plan.tarball {
		t.Error("expected a tarball plan")
	}
	if plan.key != "game/nightly.tar.gz" {
		t.Errorf("key = %q, want %q", plan.key, "game/nightly.tar.gz")
	}
}

func TestResolvePlanRequiresBackupRefOrSource(t *testing.T) {
	s := drainScheme(t)
	r := &PalworldRestoreReconciler{Client: fake.NewClientBuilder().WithScheme(s).Build(), Scheme: s}
	restore := &palworldv1alpha1.PalworldRestore{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "ns"},
		Spec:       palworldv1alpha1.PalworldRestoreSpec{GameRef: "game"},
	}
	_, err := r.resolvePlan(context.Background(), restore)
	if err == nil || !strings.Contains(err.Error(), "one of backupRef or source must be set") {
		t.Fatalf("error = %v, want the backupRef/source requirement", err)
	}
}
