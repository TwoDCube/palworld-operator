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

package resources

import (
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
	"github.com/twodcube/palworld-operator/internal/settings"
)

// BuildParams carries cross-cutting information the builders need that is not on
// the PalworldGame itself.
type BuildParams struct {
	// OpenShift indicates the cluster exposes the OpenShift Route/SCC APIs. When
	// true the pod securityContext omits explicit UID/GID/fsGroup so the
	// restricted-v2 SCC can inject them.
	OpenShift bool
	// OperatorImage is the operator's own image, reused for the metrics-exporter
	// sidecar (which runs `/manager exporter`).
	OperatorImage string
	// DefaultServerImage is used when the game does not set an explicit image.
	DefaultServerImage string
}

// Annotation keys used on the pod template.
const (
	AnnotationSettingsHash = "palworld.twodcube.io/settings-hash"
	AnnotationConfigHash   = "palworld.twodcube.io/config-hash"
)

func serverImage(g *palworldv1alpha1.PalworldGame, p BuildParams) string {
	if g.Spec.Image.Server != "" {
		return g.Spec.Image.Server
	}
	return p.DefaultServerImage
}

// ResolveServerImage returns the server image for a game, used by backup/restore
// Jobs (which run the ops scripts bundled in the server image).
func ResolveServerImage(g *palworldv1alpha1.PalworldGame, p BuildParams) string {
	return serverImage(g, p)
}

func pullPolicy(g *palworldv1alpha1.PalworldGame) corev1.PullPolicy {
	if g.Spec.Image.PullPolicy != "" {
		return g.Spec.Image.PullPolicy
	}
	return corev1.PullIfNotPresent
}

func defaultResources(g *palworldv1alpha1.PalworldGame) corev1.ResourceRequirements {
	r := *g.Spec.Resources.DeepCopy()
	if r.Requests == nil {
		r.Requests = corev1.ResourceList{}
	}
	if _, ok := r.Requests[corev1.ResourceCPU]; !ok {
		r.Requests[corev1.ResourceCPU] = resource.MustParse("2")
	}
	if _, ok := r.Requests[corev1.ResourceMemory]; !ok {
		// Palworld needs ~8Gi base; grows with players and world age.
		r.Requests[corev1.ResourceMemory] = resource.MustParse("8Gi")
	}
	return r
}

// podSecurityContext returns a restricted-v2 / PSA-restricted compliant pod
// security context. On OpenShift the UID/GID/fsGroup are left for the SCC to
// inject; on plain Kubernetes we pin them so the group-root-writable volume is
// usable.
func podSecurityContext(g *palworldv1alpha1.PalworldGame, p BuildParams) *corev1.PodSecurityContext {
	if g.Spec.PodSecurityContext != nil {
		return g.Spec.PodSecurityContext
	}
	runAsNonRoot := true
	changePolicy := corev1.FSGroupChangeOnRootMismatch
	sc := &corev1.PodSecurityContext{
		RunAsNonRoot:        &runAsNonRoot,
		SeccompProfile:      &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		FSGroupChangePolicy: &changePolicy,
	}
	if !p.OpenShift {
		uid := int64(10000)
		gid := int64(0)
		fsGroup := int64(0)
		sc.RunAsUser = &uid
		sc.RunAsGroup = &gid
		sc.FSGroup = &fsGroup
	}
	return sc
}

func containerSecurityContext() *corev1.SecurityContext {
	allowPriv := false
	runAsNonRoot := true
	readOnlyRoot := false
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &allowPriv,
		RunAsNonRoot:             &runAsNonRoot,
		ReadOnlyRootFilesystem:   &readOnlyRoot,
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

func secretEnv(name, secretName, key string, optional bool) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  key,
				Optional:             &optional,
			},
		},
	}
}

func serverEnv(g *palworldv1alpha1.PalworldGame) []corev1.EnvVar {
	secretName := CredentialsSecretName(g)
	optional := true
	maxPlayers := g.Spec.ServerSettings.ServerPlayerMaxNum
	if maxPlayers <= 0 {
		maxPlayers = 32
	}
	env := []corev1.EnvVar{
		{Name: "STEAMAPPDIR", Value: DataMountPath},
		{Name: "GAME_PORT", Value: strconv.Itoa(int(GamePort(g)))},
		{Name: "QUERY_PORT", Value: strconv.Itoa(int(QueryPort(g)))},
		{Name: "RCON_ENABLED", Value: "true"},
		{Name: "RCON_PORT", Value: strconv.Itoa(RCONPort)},
		{Name: "REST_ENABLED", Value: "true"},
		{Name: "REST_PORT", Value: strconv.Itoa(RESTPort)},
		{Name: "MAX_PLAYERS", Value: strconv.Itoa(int(maxPlayers))},
		{Name: "MULTITHREAD_ENABLED", Value: "true"},
		{Name: "SETTINGS_SOURCE", Value: ConfigMountPath + "/PalWorldSettings.ini"},
		{Name: "STEAM_BRANCH", Value: "public"},
		// Shutdown countdown, consumed by graceful-shutdown.sh in the preStop hook
		// (spec 07). SHUTDOWN_GRACE_SECONDS lets the script clamp its own countdown
		// to the pod's real kubelet budget rather than being SIGKILLed mid-save.
		{Name: "SHUTDOWN_WARN_SECONDS", Value: strconv.Itoa(int(g.ShutdownWarnSeconds()))},
		{Name: "SHUTDOWN_WARN_INTERVAL_SECONDS", Value: strconv.Itoa(int(g.ShutdownWarnIntervalSeconds()))},
		{Name: "SHUTDOWN_WARN_MESSAGE", Value: g.ShutdownWarnMessage()},
		{Name: "SHUTDOWN_GRACE_SECONDS", Value: strconv.FormatInt(g.EffectiveTerminationGracePeriodSeconds(), 10)},
		secretEnv("ADMIN_PASSWORD", secretName, AdminPasswordKey(g), optional),
		secretEnv("SERVER_PASSWORD", secretName, ServerPasswordKey(g), optional),
	}
	if engine := renderEngineINI(g.Spec.EngineSettings); engine != "" {
		env = append(env, corev1.EnvVar{Name: "ENGINE_SOURCE", Value: ConfigMountPath + "/Engine.ini"})
	}
	if g.Spec.Networking.PublicIP != "" {
		env = append(env, corev1.EnvVar{Name: "PUBLIC_IP", Value: g.Spec.Networking.PublicIP})
	}
	if pp := g.Spec.Networking.PublicPort; pp > 0 {
		env = append(env, corev1.EnvVar{Name: "PUBLIC_PORT", Value: strconv.Itoa(int(pp))})
	}
	env = append(env, g.Spec.ExtraEnv...)
	return env
}

func healthExec(mode string) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{Command: []string{"/usr/local/bin/healthcheck.sh", mode}},
		},
	}
}

func serverContainer(g *palworldv1alpha1.PalworldGame, p BuildParams) corev1.Container {
	gamePort := GamePort(g)
	queryPort := QueryPort(g)

	startup := healthExec("startup")
	startup.PeriodSeconds = 15
	startup.TimeoutSeconds = 5
	startup.FailureThreshold = 80 // ~20 min for the first SteamCMD install

	liveness := healthExec("liveness")
	liveness.PeriodSeconds = 30
	liveness.TimeoutSeconds = 5
	liveness.FailureThreshold = 5

	readiness := healthExec("readiness")
	readiness.PeriodSeconds = 15
	readiness.TimeoutSeconds = 5
	readiness.FailureThreshold = 3

	return corev1.Container{
		Name:            "palworld",
		Image:           serverImage(g, p),
		ImagePullPolicy: pullPolicy(g),
		Env:             serverEnv(g),
		Ports: []corev1.ContainerPort{
			{Name: "game", ContainerPort: gamePort, Protocol: corev1.ProtocolUDP},
			{Name: "query", ContainerPort: queryPort, Protocol: corev1.ProtocolUDP},
			{Name: "rcon", ContainerPort: RCONPort, Protocol: corev1.ProtocolTCP},
			{Name: "rest", ContainerPort: RESTPort, Protocol: corev1.ProtocolTCP},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "data", MountPath: DataMountPath},
			{Name: "config", MountPath: ConfigMountPath, ReadOnly: true},
		},
		Resources:       defaultResources(g),
		SecurityContext: containerSecurityContext(),
		StartupProbe:    startup,
		LivenessProbe:   liveness,
		ReadinessProbe:  readiness,
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{Command: []string{"/usr/local/bin/graceful-shutdown.sh"}},
			},
		},
	}
}

func metricsSidecar(g *palworldv1alpha1.PalworldGame, p BuildParams) corev1.Container {
	secretName := CredentialsSecretName(g)
	optional := true
	return corev1.Container{
		Name:            "metrics-exporter",
		Image:           p.OperatorImage,
		ImagePullPolicy: pullPolicy(g),
		Args:            []string{"exporter"},
		Env: []corev1.EnvVar{
			{Name: "EXPORTER_ADDR", Value: ":" + strconv.Itoa(MetricsPort)},
			{Name: "REST_ENDPOINT", Value: "http://127.0.0.1:" + strconv.Itoa(RESTPort)},
			secretEnv("ADMIN_PASSWORD", secretName, AdminPasswordKey(g), optional),
		},
		Ports: []corev1.ContainerPort{
			{Name: "metrics", ContainerPort: MetricsPort, Protocol: corev1.ProtocolTCP},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		},
		SecurityContext: containerSecurityContext(),
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt32(MetricsPort),
				},
			},
			PeriodSeconds: 20,
		},
	}
}

// DesiredStatefulSet builds the StatefulSet for a PalworldGame. settingsHash is
// stamped on the pod template so a settings change rolls the pod.
func DesiredStatefulSet(g *palworldv1alpha1.PalworldGame, p BuildParams, settingsHash string) *appsv1.StatefulSet {
	replicas := DesiredReplicas(g)
	labels := CommonLabels(g)
	selector := SelectorLabels(g)

	// Sized to outlast the preStop player countdown (spec 02/07): the kubelet's
	// grace clock covers preStop, so a budget shorter than the countdown means the
	// pod is SIGKILLed before the world is saved.
	grace := g.EffectiveTerminationGracePeriodSeconds()

	podAnnotations := map[string]string{
		AnnotationSettingsHash: settingsHash,
	}
	for k, v := range g.Spec.PodAnnotations {
		podAnnotations[k] = v
	}

	containers := []corev1.Container{serverContainer(g, p)}
	if g.Spec.Monitoring.MetricsExporter && p.OperatorImage != "" {
		containers = append(containers, metricsSidecar(g, p))
	}
	containers = append(containers, g.Spec.Sidecars...)

	sa := ServiceAccountName(g)
	if g.Spec.ServiceAccountName != "" {
		sa = g.Spec.ServiceAccountName
	}

	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: ConfigMapName(g)},
				},
			},
		},
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      StatefulSetName(g),
			Namespace: g.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName:         HeadlessServiceName(g),
			Replicas:            &replicas,
			PodManagementPolicy: appsv1.OrderedReadyPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:            sa,
					TerminationGracePeriodSeconds: &grace,
					SecurityContext:               podSecurityContext(g, p),
					ImagePullSecrets:              g.Spec.Image.PullSecrets,
					NodeSelector:                  g.Spec.Scheduling.NodeSelector,
					Affinity:                      g.Spec.Scheduling.Affinity,
					Tolerations:                   g.Spec.Scheduling.Tolerations,
					TopologySpreadConstraints:     g.Spec.Scheduling.TopologySpreadConstraints,
					PriorityClassName:             g.Spec.Scheduling.PriorityClassName,
					Containers:                    containers,
					Volumes:                       volumes,
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{dataVolumeClaim(g)},
		},
	}
	return sts
}

func dataVolumeClaim(g *palworldv1alpha1.PalworldGame) corev1.PersistentVolumeClaim {
	size := g.Spec.Storage.Size
	if size.IsZero() {
		size = resource.MustParse("20Gi")
	}
	accessModes := g.Spec.Storage.AccessModes
	if len(accessModes) == 0 {
		accessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	}
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "data",
			Labels: CommonLabels(g),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: size},
			},
			StorageClassName: g.Spec.Storage.StorageClassName,
		},
	}
	return pvc
}

// Ensure the settings package is referenced (placeholder tokens live there and
// are documented alongside the renderer).
var _ = settings.AdminPasswordPlaceholder
