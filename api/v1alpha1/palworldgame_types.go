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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// GameFinalizer ensures cleanup (optional final backup, PVC retention
	// handling) runs before a PalworldGame is removed.
	GameFinalizer = "palworld.twodcube.io/finalizer"
)

// PalworldGameSpec defines the desired state of a Palworld dedicated server.
type PalworldGameSpec struct {
	// Version pins the Steam build id of the Palworld dedicated server (Steam
	// app 2394010). Use "latest" to always track the newest public build; the
	// exact rollout behavior is governed by UpdatePolicy.
	// +kubebuilder:default="latest"
	// +optional
	Version string `json:"version,omitempty"`

	// Replicas is the desired number of running server pods. Because a Palworld
	// world is a single authoritative instance, only 0 (stopped) and 1
	// (running) are valid. Use this (or `kubectl scale`) to stop/start a server
	// without losing its data.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Image configures the container images used by the server.
	// +optional
	Image ImageSpec `json:"image,omitempty"`

	// ServerSettings holds the full set of Palworld dedicated server options
	// rendered into PalWorldSettings.ini.
	// +optional
	ServerSettings PalworldServerSettings `json:"serverSettings,omitempty"`

	// EngineSettings are raw Unreal Engine.ini overrides applied on top of the
	// tuned defaults, keyed by "Section/Key". Advanced use only.
	// +optional
	EngineSettings map[string]string `json:"engineSettings,omitempty"`

	// Credentials references the Secret holding server passwords.
	// +optional
	Credentials CredentialsSpec `json:"credentials,omitempty"`

	// Storage configures the persistent world data volume.
	// +optional
	Storage StorageSpec `json:"storage,omitempty"`

	// Networking configures how the server is exposed to players.
	// +optional
	Networking NetworkingSpec `json:"networking,omitempty"`

	// Resources are the compute resource requirements for the server container.
	// Palworld is memory- and single-thread-CPU-sensitive; requests should be
	// generous (a full 32-player server can use 16Gi+).
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Scheduling controls placement of the server pod.
	// +optional
	Scheduling SchedulingSpec `json:"scheduling,omitempty"`

	// Backup configures automatic scheduled backups.
	// +optional
	Backup *BackupPolicy `json:"backup,omitempty"`

	// Update configures how server version updates are applied.
	// +optional
	Update *UpdatePolicy `json:"update,omitempty"`

	// PodDisruptionBudget protects the server pod from voluntary disruptions.
	// +optional
	PodDisruptionBudget *PodDisruptionSpec `json:"podDisruptionBudget,omitempty"`

	// Monitoring configures metrics and Prometheus integration.
	// +optional
	Monitoring MonitoringSpec `json:"monitoring,omitempty"`

	// ServiceAccountName is the ServiceAccount for the server pod. When empty
	// the operator creates and manages a dedicated ServiceAccount.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// TerminationGracePeriodSeconds is how long the pod is given to perform a
	// graceful save-and-shutdown on stop. Defaults to 120s.
	// +kubebuilder:default=120
	// +optional
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`

	// PodAnnotations are added to the server pod template.
	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`

	// PodLabels are added to the server pod template.
	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`

	// ExtraEnv are additional environment variables for the server container.
	// +optional
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`

	// Sidecars are additional containers to run alongside the server (e.g. a
	// custom exporter or log shipper).
	// +optional
	Sidecars []corev1.Container `json:"sidecars,omitempty"`

	// PodSecurityContext overrides the default pod security context. By default
	// the operator emits a restricted-v2-compatible context (no fixed
	// runAsUser, fsGroup left to the platform) so it works with OpenShift's
	// arbitrary UID assignment.
	// +optional
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
}

// PalworldGameStatus defines the observed state of a Palworld dedicated server.
type PalworldGameStatus struct {
	// Phase is a coarse, human-friendly lifecycle indicator.
	// +optional
	Phase PhaseType `json:"phase,omitempty"`

	// ObservedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// CurrentVersion is the Steam build id currently installed/running.
	// +optional
	CurrentVersion string `json:"currentVersion,omitempty"`

	// AvailableVersion is the newest Steam build id detected by the update
	// poller, when known.
	// +optional
	AvailableVersion string `json:"availableVersion,omitempty"`

	// Replicas is the number of running server pods (0 or 1).
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Selector is the serialized label selector for the scale subresource.
	// +optional
	Selector string `json:"selector,omitempty"`

	// PlayersOnline is the number of connected players (from the REST API).
	// +optional
	PlayersOnline int32 `json:"playersOnline,omitempty"`

	// MaxPlayers is the configured player capacity.
	// +optional
	MaxPlayers int32 `json:"maxPlayers,omitempty"`

	// ServerName is the in-game server name currently applied.
	// +optional
	ServerName string `json:"serverName,omitempty"`

	// GameEndpoint is the in-cluster/external address for the game UDP port.
	// +optional
	GameEndpoint string `json:"gameEndpoint,omitempty"`

	// RESTEndpoint is the in-cluster address of the REST admin API.
	// +optional
	RESTEndpoint string `json:"restEndpoint,omitempty"`

	// RouteURL is the external URL of the REST API Route, when created.
	// +optional
	RouteURL string `json:"routeURL,omitempty"`

	// PersistentVolumeClaim is the name of the bound world-data PVC.
	// +optional
	PersistentVolumeClaim string `json:"persistentVolumeClaim,omitempty"`

	// CredentialsSecret is the Secret holding the (possibly generated) server
	// passwords.
	// +optional
	CredentialsSecret string `json:"credentialsSecret,omitempty"`

	// LastBackupTime is the completion time of the most recent successful
	// backup.
	// +optional
	LastBackupTime *metav1.Time `json:"lastBackupTime,omitempty"`

	// LastBackupName is the name of the most recent successful PalworldBackup.
	// +optional
	LastBackupName string `json:"lastBackupName,omitempty"`

	// NextScheduledBackup is when the next scheduled backup will run.
	// +optional
	NextScheduledBackup *metav1.Time `json:"nextScheduledBackup,omitempty"`

	// LastUpdateTime is when the server was last updated to a new build.
	// +optional
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`

	// NextScheduledUpdateCheck is when the operator will next poll Steam for a
	// new build.
	// +optional
	NextScheduledUpdateCheck *metav1.Time `json:"nextScheduledUpdateCheck,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:shortName=pwgame;pwg,categories=palworld
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.currentVersion`
// +kubebuilder:printcolumn:name="Players",type=string,JSONPath=`.status.playersOnline`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PalworldGame is the Schema for hosting a Palworld dedicated server.
type PalworldGame struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PalworldGameSpec   `json:"spec,omitempty"`
	Status PalworldGameStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PalworldGameList contains a list of PalworldGame.
type PalworldGameList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PalworldGame `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PalworldGame{}, &PalworldGameList{})
}
