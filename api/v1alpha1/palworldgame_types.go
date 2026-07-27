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

// Shutdown defaults. These mirror the +kubebuilder:default markers on
// ShutdownPolicy. The markers cover objects that went through the API server;
// these constants keep the operator correct for objects persisted before the
// field existed (their spec.shutdown stays nil, since CRD defaulting only fills
// in a parent that is present) and for unit tests that build a PalworldGame
// value directly.
const (
	// DefaultShutdownWarnSeconds is the player countdown before a stop.
	DefaultShutdownWarnSeconds int32 = 300
	// DefaultShutdownWarnIntervalSeconds re-broadcasts the countdown each minute.
	DefaultShutdownWarnIntervalSeconds int32 = 60
	// DefaultShutdownWarnMessage is the broadcast template. "%s" renders the
	// remaining time in words, "%d" the remaining seconds.
	DefaultShutdownWarnMessage = "Server is shutting down for maintenance in %s"

	// ShutdownGraceHeadroomSeconds is added to the countdown to derive
	// TerminationGracePeriodSeconds. It is the time left *after* the countdown
	// for the REST save to flush a large world to the PVC and for the server to
	// exit cleanly. Generous on purpose: overshooting costs nothing (the pod exits
	// as soon as the process does), while undershooting means a SIGKILL mid-save.
	ShutdownGraceHeadroomSeconds int64 = 300

	// DefaultUpdateWarnIntervalSeconds re-broadcasts the pre-update drain warning
	// once a minute. Mirrors the +kubebuilder:default on
	// UpdatePolicy.WarnIntervalSeconds, for objects persisted before that field
	// existed.
	DefaultUpdateWarnIntervalSeconds int32 = 60

	// ShutdownReserveSeconds is the minimum time the container keeps in reserve
	// for the save and clean shutdown. When an explicit
	// TerminationGracePeriodSeconds leaves less than this after the countdown, the
	// webhook warns and graceful-shutdown.sh clamps the countdown to fit. Keep in
	// step with SHUTDOWN_RESERVE_SECONDS in graceful-shutdown.sh.
	ShutdownReserveSeconds int64 = 30
)

// ShutdownWarnSeconds returns the configured player countdown in seconds,
// falling back to DefaultShutdownWarnSeconds when spec.shutdown is omitted. A
// configured 0 is honoured (stop immediately).
func (g *PalworldGame) ShutdownWarnSeconds() int32 {
	if g.Spec.Shutdown == nil {
		return DefaultShutdownWarnSeconds
	}
	if g.Spec.Shutdown.WarnSeconds < 0 {
		return 0
	}
	return g.Spec.Shutdown.WarnSeconds
}

// ShutdownWarnIntervalSeconds returns how often the countdown is re-broadcast,
// falling back to DefaultShutdownWarnIntervalSeconds. Never returns less than 1:
// a 0 interval would spin the announce loop without advancing the countdown.
func (g *PalworldGame) ShutdownWarnIntervalSeconds() int32 {
	if g.Spec.Shutdown == nil || g.Spec.Shutdown.WarnIntervalSeconds < 1 {
		return DefaultShutdownWarnIntervalSeconds
	}
	return g.Spec.Shutdown.WarnIntervalSeconds
}

// ShutdownWarnMessage returns the broadcast template, falling back to
// DefaultShutdownWarnMessage when spec.shutdown or the message is omitted.
func (g *PalworldGame) ShutdownWarnMessage() string {
	if g.Spec.Shutdown == nil || g.Spec.Shutdown.WarnMessage == "" {
		return DefaultShutdownWarnMessage
	}
	return g.Spec.Shutdown.WarnMessage
}

// EffectiveTerminationGracePeriodSeconds returns the pod's termination grace
// period: the explicit spec value when set, otherwise the countdown plus
// ShutdownGraceHeadroomSeconds so the preStop countdown cannot be cut off by the
// kubelet.
func (g *PalworldGame) EffectiveTerminationGracePeriodSeconds() int64 {
	if g.Spec.TerminationGracePeriodSeconds != nil {
		return *g.Spec.TerminationGracePeriodSeconds
	}
	return int64(g.ShutdownWarnSeconds()) + ShutdownGraceHeadroomSeconds
}

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

	// NodeDrain configures graceful migration when the server's node is
	// cordoned/drained. Enabled by default when omitted.
	// +optional
	NodeDrain *NodeDrainPolicy `json:"nodeDrain,omitempty"`

	// Shutdown configures the countdown players are given before the server
	// stops. Applies to every termination, not just updates.
	// +optional
	Shutdown *ShutdownPolicy `json:"shutdown,omitempty"`

	// Monitoring configures metrics and Prometheus integration.
	// +optional
	Monitoring MonitoringSpec `json:"monitoring,omitempty"`

	// ServiceAccountName is the ServiceAccount for the server pod. When empty
	// the operator creates and manages a dedicated ServiceAccount.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// TerminationGracePeriodSeconds is how long the pod is given to warn players,
	// save, and shut down cleanly.
	//
	// Leave unset: the operator derives Shutdown.WarnSeconds +
	// ShutdownGraceHeadroomSeconds (600s with defaults), because the player
	// countdown runs inside the preStop hook and the kubelet's grace clock covers
	// preStop -- a budget shorter than the countdown means a SIGKILL mid-save.
	// There is deliberately no CRD-level default: a fixed number here would
	// silently win over the derivation and truncate the countdown.
	//
	// An explicit value is honoured verbatim. If it leaves less than
	// ShutdownReserveSeconds of headroom the webhook warns and the container
	// clamps the countdown to fit (spec 07).
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

	// CurrentVersion is the Steam build id currently installed/running. It is
	// managed solely by the update controller so it stays comparable with
	// AvailableVersion (both are Steam build ids).
	// +optional
	CurrentVersion string `json:"currentVersion,omitempty"`

	// ServerVersion is the human-readable in-game version reported by the REST
	// API (e.g. "v0.3.5"). Display only; not used for update comparisons.
	// +optional
	ServerVersion string `json:"serverVersion,omitempty"`

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

	// CurrentNode is the node the server pod is currently scheduled on. It is
	// used to react to that node being cordoned/drained.
	// +optional
	CurrentNode string `json:"currentNode,omitempty"`

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

	// UpdateDrainStartTime is when the pre-update player drain began, and doubles
	// as the "a drain is in progress" flag. The reconciler cannot block, so the
	// drain is a requeue loop and its deadline has to survive in status. Cleared
	// once the pod is restarted or the pending update goes away.
	// +optional
	UpdateDrainStartTime *metav1.Time `json:"updateDrainStartTime,omitempty"`

	// UpdateDrainLastWarnTime is when the drain warning was last broadcast. The
	// controller is reconciled far more often than its own RequeueAfter, so
	// re-broadcasts are gated on this timestamp instead of on reconcile entry --
	// otherwise every reconcile would spam another chat message.
	// +optional
	UpdateDrainLastWarnTime *metav1.Time `json:"updateDrainLastWarnTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:shortName=pwgame;pwg,categories=palworld
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.serverVersion`
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
