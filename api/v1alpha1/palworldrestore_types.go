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

// RestorePhase is the lifecycle phase of a PalworldRestore.
// +kubebuilder:validation:Enum=Pending;Stopping;Restoring;Starting;Completed;Failed
type RestorePhase string

const (
	RestorePhasePending   RestorePhase = "Pending"
	RestorePhaseStopping  RestorePhase = "Stopping"
	RestorePhaseRestoring RestorePhase = "Restoring"
	RestorePhaseStarting  RestorePhase = "Starting"
	RestorePhaseCompleted RestorePhase = "Completed"
	RestorePhaseFailed    RestorePhase = "Failed"
)

// PalworldRestoreSpec defines a request to restore a backup into a game.
type PalworldRestoreSpec struct {
	// GameRef is the target PalworldGame to restore into (same namespace).
	// +kubebuilder:validation:Required
	GameRef string `json:"gameRef"`

	// BackupRef is the PalworldBackup to restore from (same namespace). Exactly
	// one of BackupRef or Source must be set.
	// +optional
	BackupRef string `json:"backupRef,omitempty"`

	// Source restores directly from an external location instead of a
	// PalworldBackup object.
	// +optional
	Source *BackupDestination `json:"source,omitempty"`

	// Force allows restoring into a game that is currently running by stopping
	// it first. Without Force the restore fails if the game is not already
	// stopped.
	// +kubebuilder:default=true
	// +optional
	Force bool `json:"force,omitempty"`
}

// PalworldRestoreStatus captures the observed state of a restore.
type PalworldRestoreStatus struct {
	// Phase is the current restore phase.
	// +optional
	Phase RestorePhase `json:"phase,omitempty"`

	// Message is a human-readable status message.
	// +optional
	Message string `json:"message,omitempty"`

	// StartTime is when the restore began.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the restore finished.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// JobName is the Job performing the restore copy, when applicable.
	// +optional
	JobName string `json:"jobName,omitempty"`

	// OriginalReplicas records the game's desired replica count before the
	// restore stopped it, so it can be returned to that state afterwards.
	// +optional
	OriginalReplicas *int32 `json:"originalReplicas,omitempty"`

	// Conditions represent the latest observations of the restore's state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pwrestore;pwrs,categories=palworld
// +kubebuilder:printcolumn:name="Game",type=string,JSONPath=`.spec.gameRef`
// +kubebuilder:printcolumn:name="Backup",type=string,JSONPath=`.spec.backupRef`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PalworldRestore restores a PalworldBackup into a PalworldGame.
type PalworldRestore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PalworldRestoreSpec   `json:"spec,omitempty"`
	Status PalworldRestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PalworldRestoreList contains a list of PalworldRestore.
type PalworldRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PalworldRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PalworldRestore{}, &PalworldRestoreList{})
}
