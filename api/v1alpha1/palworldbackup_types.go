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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BackupPhase is the lifecycle phase of a PalworldBackup.
// +kubebuilder:validation:Enum=Pending;Saving;Snapshotting;Uploading;Completed;Failed
type BackupPhase string

const (
	BackupPhasePending      BackupPhase = "Pending"
	BackupPhaseSaving       BackupPhase = "Saving"
	BackupPhaseSnapshotting BackupPhase = "Snapshotting"
	BackupPhaseUploading    BackupPhase = "Uploading"
	BackupPhaseCompleted    BackupPhase = "Completed"
	BackupPhaseFailed       BackupPhase = "Failed"
)

// PalworldBackupSpec defines the desired state of a single backup.
type PalworldBackupSpec struct {
	// GameRef is the name of the PalworldGame to back up, in the same namespace.
	// +kubebuilder:validation:Required
	GameRef string `json:"gameRef"`

	// Destination for this backup. Defaults to a VolumeSnapshot.
	// +optional
	Destination BackupDestination `json:"destination,omitempty"`

	// FlushSave, when true (default), issues an RCON/REST "save" to the running
	// server and waits for it to complete before snapshotting so the backup is
	// application-consistent rather than merely crash-consistent.
	// +kubebuilder:default=true
	// +optional
	FlushSave bool `json:"flushSave,omitempty"`

	// TTLSecondsAfterFinished, when set, deletes the PalworldBackup object (and
	// its VolumeSnapshot for snapshot backups if not retained) this many
	// seconds after completion.
	// +optional
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`

	// Retain, when true, prevents retention policies from garbage-collecting
	// this backup.
	// +kubebuilder:default=false
	// +optional
	Retain bool `json:"retain,omitempty"`
}

// PalworldBackupStatus captures the observed state of a backup.
type PalworldBackupStatus struct {
	// Phase is the current backup phase.
	// +optional
	Phase BackupPhase `json:"phase,omitempty"`

	// Message is a human-readable status message.
	// +optional
	Message string `json:"message,omitempty"`

	// StartTime is when the backup began.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the backup finished.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// VolumeSnapshotName is the created VolumeSnapshot (for snapshot backups).
	// +optional
	VolumeSnapshotName string `json:"volumeSnapshotName,omitempty"`

	// Location is the URI of the stored backup (e.g. s3://bucket/key or the
	// snapshot name).
	// +optional
	Location string `json:"location,omitempty"`

	// SizeBytes is the size of the backup, when known.
	// +optional
	SizeBytes int64 `json:"sizeBytes,omitempty"`

	// ServerVersion is the game build id that was backed up.
	// +optional
	ServerVersion string `json:"serverVersion,omitempty"`

	// JobName is the name of the Job performing the upload, when applicable.
	// +optional
	JobName string `json:"jobName,omitempty"`

	// Conditions represent the latest observations of the backup's state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pwbackup;pwbk,categories=palworld
// +kubebuilder:printcolumn:name="Game",type=string,JSONPath=`.spec.gameRef`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.destination.type`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Completed",type=date,JSONPath=`.status.completionTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PalworldBackup is a point-in-time backup of a PalworldGame world.
type PalworldBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PalworldBackupSpec   `json:"spec,omitempty"`
	Status PalworldBackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PalworldBackupList contains a list of PalworldBackup.
type PalworldBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PalworldBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PalworldBackup{}, &PalworldBackupList{})
}
