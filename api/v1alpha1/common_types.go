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
	"k8s.io/apimachinery/pkg/api/resource"
)

// Common condition types used across PalworldGame status.
const (
	// ConditionReady indicates the server is running and reachable.
	ConditionReady = "Ready"
	// ConditionProgressing indicates the operator is actively reconciling
	// toward the desired state (installing, updating, restarting).
	ConditionProgressing = "Progressing"
	// ConditionDegraded indicates the server is in an error state that needs
	// attention.
	ConditionDegraded = "Degraded"
	// ConditionBackupReady indicates the most recent backup succeeded.
	ConditionBackupReady = "BackupReady"
	// ConditionUpdateAvailable indicates a newer server build is available.
	ConditionUpdateAvailable = "UpdateAvailable"
)

// ImageSpec configures the container images used by a Palworld server.
type ImageSpec struct {
	// Server is the Palworld dedicated server image. It must be compatible with
	// OpenShift's arbitrary UID model (group-root writable). Defaults to the
	// operator's bundled image if omitted.
	// +optional
	Server string `json:"server,omitempty"`

	// PullPolicy is the imagePullPolicy for the server and ops containers.
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +kubebuilder:default=IfNotPresent
	// +optional
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`

	// PullSecrets is a list of image pull secret names in the same namespace.
	// +optional
	PullSecrets []corev1.LocalObjectReference `json:"pullSecrets,omitempty"`
}

// StorageSpec configures persistent storage for the world save data.
type StorageSpec struct {
	// Size is the requested size of the game data volume.
	// +kubebuilder:default="20Gi"
	// +optional
	Size resource.Quantity `json:"size,omitempty"`

	// StorageClassName is the StorageClass for the game data volume. When empty
	// the cluster default StorageClass is used. Snapshot backups require the
	// class to have an associated VolumeSnapshotClass.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`

	// AccessModes for the game data volume. Defaults to ReadWriteOnce. Use
	// ReadWriteOncePod for the strongest single-writer guarantee, or
	// ReadWriteMany if you intend to run filesystem-copy backups from a
	// separate pod.
	// +optional
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`

	// Retain, when true, keeps the PersistentVolumeClaim after the PalworldGame
	// is deleted so the world can be recovered. Defaults to false.
	// +kubebuilder:default=false
	// +optional
	Retain bool `json:"retain,omitempty"`

	// VolumeSnapshotClassName is the VolumeSnapshotClass used for snapshot
	// backups. When empty the cluster default is used.
	// +optional
	VolumeSnapshotClassName *string `json:"volumeSnapshotClassName,omitempty"`
}

// ServiceType selects how the game (UDP) port is exposed.
// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
type ServiceType string

const (
	ServiceTypeClusterIP    ServiceType = "ClusterIP"
	ServiceTypeNodePort     ServiceType = "NodePort"
	ServiceTypeLoadBalancer ServiceType = "LoadBalancer"
)

// NetworkingSpec configures how the server is exposed to players.
type NetworkingSpec struct {
	// GamePort is the UDP port the server listens on for gameplay traffic.
	// +kubebuilder:default=8211
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	GamePort int32 `json:"gamePort,omitempty"`

	// QueryPort is the UDP Steam query port.
	// +kubebuilder:default=27015
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	QueryPort int32 `json:"queryPort,omitempty"`

	// ServiceType selects how the game UDP port is exposed. Because OpenShift
	// Routes only carry HTTP/TLS traffic, UDP game traffic must use a
	// LoadBalancer (e.g. MetalLB), NodePort, or ClusterIP service.
	// +kubebuilder:default=ClusterIP
	// +optional
	ServiceType ServiceType `json:"serviceType,omitempty"`

	// LoadBalancerIP requests a specific IP when ServiceType is LoadBalancer.
	// +optional
	LoadBalancerIP string `json:"loadBalancerIP,omitempty"`

	// LoadBalancerClass selects a specific load balancer implementation.
	// +optional
	LoadBalancerClass *string `json:"loadBalancerClass,omitempty"`

	// NodePort pins the game NodePort when ServiceType is NodePort. When 0 a
	// port is allocated automatically.
	// +optional
	NodePort int32 `json:"nodePort,omitempty"`

	// ServiceAnnotations are applied to the game Service. Useful for cloud
	// load-balancer configuration (e.g. AWS NLB annotations).
	// +optional
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`

	// PublicIP advertised to the Palworld community server list. When empty the
	// server auto-detects it.
	// +optional
	PublicIP string `json:"publicIP,omitempty"`

	// PublicPort advertised to the community server list. Defaults to GamePort.
	// +optional
	PublicPort int32 `json:"publicPort,omitempty"`

	// RESTAPI configures exposure of the Palworld REST admin API.
	// +optional
	RESTAPI RESTAPIExposure `json:"restAPI,omitempty"`
}

// RESTAPIExposure configures how the REST admin API is exposed. Because the
// REST API is HTTP it can be fronted by an OpenShift Route.
type RESTAPIExposure struct {
	// Route, when true, creates an OpenShift Route (edge TLS) for the REST API.
	// Ignored on clusters without the Route API.
	// +kubebuilder:default=false
	// +optional
	Route bool `json:"route,omitempty"`

	// Host is the desired Route host. When empty OpenShift assigns one.
	// +optional
	Host string `json:"host,omitempty"`

	// TLS selects the Route TLS termination policy.
	// +kubebuilder:validation:Enum=edge;reencrypt;passthrough
	// +kubebuilder:default=edge
	// +optional
	TLS string `json:"tls,omitempty"`
}

// SchedulingSpec configures where the server pod is placed.
type SchedulingSpec struct {
	// NodeSelector constrains the server to matching nodes.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Affinity for the server pod.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations for the server pod.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// TopologySpreadConstraints for the server pod.
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// PriorityClassName for the server pod. Game servers are typically
	// latency-sensitive; a high-priority class avoids preemption.
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`
}

// CredentialsSpec references the secret holding server passwords. The operator
// never stores plaintext passwords in the CR; they live in a Secret.
type CredentialsSpec struct {
	// SecretName is the name of a Secret in the same namespace holding the
	// admin, server, and RCON passwords. If omitted the operator generates a
	// Secret with random passwords.
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// AdminPasswordKey is the Secret key for the admin password (RCON/REST).
	// +kubebuilder:default="adminPassword"
	// +optional
	AdminPasswordKey string `json:"adminPasswordKey,omitempty"`

	// ServerPasswordKey is the Secret key for the optional join password.
	// +kubebuilder:default="serverPassword"
	// +optional
	ServerPasswordKey string `json:"serverPasswordKey,omitempty"`
}

// BackupDestinationType selects where backups are stored.
// +kubebuilder:validation:Enum=VolumeSnapshot;S3;PVC
type BackupDestinationType string

const (
	// BackupDestinationVolumeSnapshot creates a CSI VolumeSnapshot of the game
	// data volume. This is the default: fast, crash-consistent, no volume
	// contention.
	BackupDestinationVolumeSnapshot BackupDestinationType = "VolumeSnapshot"
	// BackupDestinationS3 exports a tarball of the SaveGames directory to an
	// S3-compatible object store (e.g. ODF/NooBaa, AWS S3, MinIO).
	BackupDestinationS3 BackupDestinationType = "S3"
	// BackupDestinationPVC writes a tarball to a separate backup PVC.
	BackupDestinationPVC BackupDestinationType = "PVC"
)

// S3Destination configures an S3-compatible backup target.
type S3Destination struct {
	// Bucket is the target bucket.
	Bucket string `json:"bucket"`

	// Prefix is an optional key prefix within the bucket.
	// +optional
	Prefix string `json:"prefix,omitempty"`

	// Endpoint is the S3 endpoint URL. For AWS this may be omitted.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// Region is the S3 region.
	// +optional
	Region string `json:"region,omitempty"`

	// CredentialsSecret references a Secret with AWS_ACCESS_KEY_ID and
	// AWS_SECRET_ACCESS_KEY keys (or the keys named below).
	CredentialsSecret string `json:"credentialsSecret"`

	// AccessKeyIDKey is the Secret key for the access key id.
	// +kubebuilder:default="AWS_ACCESS_KEY_ID"
	// +optional
	AccessKeyIDKey string `json:"accessKeyIDKey,omitempty"`

	// SecretAccessKeyKey is the Secret key for the secret access key.
	// +kubebuilder:default="AWS_SECRET_ACCESS_KEY"
	// +optional
	SecretAccessKeyKey string `json:"secretAccessKeyKey,omitempty"`

	// InsecureTLS disables TLS verification for the endpoint (test only).
	// +kubebuilder:default=false
	// +optional
	InsecureTLS bool `json:"insecureTLS,omitempty"`
}

// BackupDestination describes where a backup is stored.
type BackupDestination struct {
	// Type of destination.
	// +kubebuilder:default=VolumeSnapshot
	// +optional
	Type BackupDestinationType `json:"type,omitempty"`

	// S3 destination configuration (required when Type is S3).
	// +optional
	S3 *S3Destination `json:"s3,omitempty"`

	// PVCName is the target backup PVC (required when Type is PVC).
	// +optional
	PVCName string `json:"pvcName,omitempty"`

	// PVCPath is the path of a backup archive inside PVCName, relative to the
	// mount root (e.g. "seeds/world.tar.gz").
	//
	// It applies only to PalworldRestore.spec.source, where it carries the full
	// object key of an archive this operator did not write. As a backup
	// destination the key is always derived as "<game>/<backup>.tar.gz", so this
	// field is ignored.
	// +optional
	PVCPath string `json:"pvcPath,omitempty"`
}

// BackupPolicy configures automatic scheduled backups for a PalworldGame.
type BackupPolicy struct {
	// Enabled turns on scheduled backups.
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Schedule is a cron expression for automatic backups (e.g. "0 */6 * * *").
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// Destination for scheduled backups.
	// +optional
	Destination BackupDestination `json:"destination,omitempty"`

	// Retention is the number of successful backups to keep. Older backups are
	// garbage-collected. 0 means keep all.
	// +kubebuilder:default=7
	// +kubebuilder:validation:Minimum=0
	// +optional
	Retention int32 `json:"retention,omitempty"`

	// Suspend pauses scheduled backups without deleting the policy.
	// +kubebuilder:default=false
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// OnDelete, when true, takes a final backup before the game is deleted
	// (enforced with a finalizer).
	// +kubebuilder:default=false
	// +optional
	OnDelete bool `json:"onDelete,omitempty"`
}

// UpdateStrategyType selects how server updates are rolled out.
// +kubebuilder:validation:Enum=Manual;Automatic;Scheduled
type UpdateStrategyType string

const (
	// UpdateManual only updates when the spec version changes.
	UpdateManual UpdateStrategyType = "Manual"
	// UpdateAutomatic updates as soon as a new build is detected.
	UpdateAutomatic UpdateStrategyType = "Automatic"
	// UpdateScheduled updates during a maintenance window.
	UpdateScheduled UpdateStrategyType = "Scheduled"
)

// UpdatePolicy configures how the operator handles server version updates.
type UpdatePolicy struct {
	// Strategy for applying updates.
	// +kubebuilder:default=Manual
	// +optional
	Strategy UpdateStrategyType `json:"strategy,omitempty"`

	// Schedule is a cron maintenance window for Scheduled strategy.
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// DrainTimeoutSeconds is how long to wait for players to disconnect after
	// broadcasting an update warning before forcing a restart. The wait ends early
	// as soon as the last player leaves. 0 restarts immediately after the warning
	// and leaves warning stragglers to the preStop countdown (ShutdownPolicy).
	//
	// This stacks with ShutdownPolicy.WarnSeconds: the drain waits for players to
	// leave voluntarily, the preStop countdown warns whoever is still connected
	// when the pod is finally deleted. Players who log off during the drain leave
	// an empty server, and preStop then skips its countdown entirely.
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=0
	// +optional
	DrainTimeoutSeconds int32 `json:"drainTimeoutSeconds,omitempty"`

	// WarnMessage is broadcast to players before an update restart. It may
	// contain "%d" which is replaced with the seconds left in the drain.
	// +kubebuilder:default="Server will restart for updates in %d seconds"
	// +optional
	WarnMessage string `json:"warnMessage,omitempty"`

	// WarnIntervalSeconds is how often WarnMessage is re-broadcast while the drain
	// runs down. Kept separate from ShutdownPolicy.WarnIntervalSeconds because the
	// two cover different phases (waiting for players to leave vs. counting down a
	// termination already under way) and operators may well want different
	// cadences.
	// +kubebuilder:default=60
	// +kubebuilder:validation:Minimum=1
	// +optional
	WarnIntervalSeconds int32 `json:"warnIntervalSeconds,omitempty"`

	// BackupBeforeUpdate takes a backup before applying an update.
	// +kubebuilder:default=true
	// +optional
	BackupBeforeUpdate bool `json:"backupBeforeUpdate,omitempty"`

	// PollIntervalMinutes is how often to check Steam for new builds when using
	// Automatic or Scheduled strategies.
	// +kubebuilder:default=30
	// +kubebuilder:validation:Minimum=1
	// +optional
	PollIntervalMinutes int32 `json:"pollIntervalMinutes,omitempty"`
}

// MonitoringSpec configures observability integrations.
type MonitoringSpec struct {
	// ServiceMonitor, when true, creates a Prometheus Operator ServiceMonitor
	// scraping the metrics exporter. Ignored on clusters without the
	// monitoring.coreos.com API.
	// +kubebuilder:default=false
	// +optional
	ServiceMonitor bool `json:"serviceMonitor,omitempty"`

	// MetricsExporter, when true, runs a sidecar that translates the Palworld
	// REST metrics endpoint into Prometheus metrics.
	// +kubebuilder:default=true
	// +optional
	MetricsExporter bool `json:"metricsExporter,omitempty"`

	// ExporterResources overrides the metrics-exporter sidecar's resources.
	//
	// The default is QoS-neutral (requests == limits for both cpu and memory) so
	// the operator's own sidecar can never stop a pod reaching Guaranteed QoS,
	// which is what the kubelet's static CPU Manager policy requires before it
	// will hand the game container exclusive cores. Setting unmatched
	// requests/limits here downgrades the whole pod to Burstable.
	// +optional
	ExporterResources *corev1.ResourceRequirements `json:"exporterResources,omitempty"`
}

// NodeDrainPolicy configures how the operator reacts when the node hosting the
// server pod is cordoned/drained (e.g. for node maintenance). When enabled, the
// operator warns players, flushes a save, and gracefully migrates the pod to a
// healthy node instead of letting an abrupt eviction interrupt it.
type NodeDrainPolicy struct {
	// Disabled turns off automatic graceful migration off draining nodes. The
	// behavior is enabled by default (a nil NodeDrainPolicy means enabled).
	// +kubebuilder:default=false
	// +optional
	Disabled bool `json:"disabled,omitempty"`

	// GracePeriodSeconds is how long players are warned before the server is
	// migrated off a draining node. 0 migrates immediately.
	// +kubebuilder:default=30
	// +kubebuilder:validation:Minimum=0
	// +optional
	GracePeriodSeconds int32 `json:"gracePeriodSeconds,omitempty"`

	// WarnMessage is broadcast to players when a drain is detected. It may
	// contain "%d", which is replaced with the remaining seconds.
	// +kubebuilder:default="Server node maintenance: migrating in %d seconds, please reach a safe spot"
	// +optional
	WarnMessage string `json:"warnMessage,omitempty"`
}

// ShutdownPolicy configures the countdown players get before the server stops.
//
// This is enforced by the server container's preStop hook (spec 07), not by a
// controller code path, because preStop is the one thing that runs on *every*
// termination: a settings change rolling the StatefulSet, an update, a node
// drain, a manual pod delete, or a scale to zero. Putting the countdown in a
// reconciler would only cover the paths the operator itself initiates and would
// still let a plain `oc delete pod` cut players off without notice.
type ShutdownPolicy struct {
	// WarnSeconds is how long players are warned before the world is flushed and
	// the server stops. 0 stops immediately after a single announcement.
	//
	// Because the countdown consumes the pod's termination grace period, raising
	// this also raises the derived TerminationGracePeriodSeconds.
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=0
	// +optional
	WarnSeconds int32 `json:"warnSeconds,omitempty"`

	// WarnIntervalSeconds is how often the countdown is re-broadcast while
	// WarnSeconds runs down. The default warns once a minute, so players who
	// joined mid-countdown or missed the first message still see one.
	// +kubebuilder:default=60
	// +kubebuilder:validation:Minimum=1
	// +optional
	WarnIntervalSeconds int32 `json:"warnIntervalSeconds,omitempty"`

	// WarnMessage is broadcast on every countdown tick. "%s" is replaced with a
	// human-readable remaining time ("5 minutes", "1 minute", "30 seconds") and
	// "%d" with the remaining whole seconds. Both may be used; neither is
	// required.
	// +kubebuilder:default="Server is shutting down for maintenance in %s"
	// +optional
	WarnMessage string `json:"warnMessage,omitempty"`
}

// PodDisruptionSpec configures a PodDisruptionBudget for the server pod.
type PodDisruptionSpec struct {
	// Enabled creates a PodDisruptionBudget for the server.
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// MinAvailable for the PDB. Defaults to 1 to protect the single game pod
	// from voluntary disruptions (node drains) unless the operator initiates
	// the restart itself.
	// +optional
	MinAvailable *int32 `json:"minAvailable,omitempty"`
}

// PhaseType is a high-level lifecycle phase for a PalworldGame.
// +kubebuilder:validation:Enum=Pending;Installing;Running;Updating;BackingUp;Restoring;Stopped;Degraded;Terminating
type PhaseType string

const (
	PhasePending     PhaseType = "Pending"
	PhaseInstalling  PhaseType = "Installing"
	PhaseRunning     PhaseType = "Running"
	PhaseUpdating    PhaseType = "Updating"
	PhaseBackingUp   PhaseType = "BackingUp"
	PhaseRestoring   PhaseType = "Restoring"
	PhaseStopped     PhaseType = "Stopped"
	PhaseDegraded    PhaseType = "Degraded"
	PhaseTerminating PhaseType = "Terminating"
)

// NamespacedName is a lightweight reference used in statuses.
type NamespacedName struct {
	Name string `json:"name"`
	// +optional
	Namespace string `json:"namespace,omitempty"`
}
