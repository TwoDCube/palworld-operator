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

// Package resources builds the desired Kubernetes/OpenShift objects that back a
// PalworldGame: ConfigMap, Secret, StatefulSet, Services, Route, PDB,
// ServiceAccount, NetworkPolicy and ServiceMonitor.
package resources

import (
	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

const (
	// Fixed internal admin ports. RCON and the REST API are always enabled so
	// the operator can perform day-2 operations (save, drain, metrics); they are
	// only ever exposed on the internal admin ClusterIP service.
	RCONPort = 25575
	RESTPort = 8212

	// DefaultGamePort / DefaultQueryPort mirror the Palworld defaults.
	DefaultGamePort  = 8211
	DefaultQueryPort = 27015

	// MetricsPort is the port the bundled metrics exporter sidecar listens on.
	MetricsPort = 9877

	// DataMountPath is where the world-data PVC is mounted in the server pod
	// (the SteamCMD install dir + Pal/Saved live here).
	DataMountPath = "/palworld"
	// ConfigMountPath is where the rendered PalWorldSettings.ini is mounted.
	ConfigMountPath = "/config"
	// SaveSubPath is the directory under the data volume that holds the world
	// save and live config, i.e. the backup target.
	SaveSubPath = "Pal/Saved"
)

// Well-known label keys.
const (
	labelName      = "app.kubernetes.io/name"
	labelInstance  = "app.kubernetes.io/instance"
	labelComponent = "app.kubernetes.io/component"
	labelPartOf    = "app.kubernetes.io/part-of"
	labelGame      = "palworld.twodcube.io/game"

	appName = "palworld"
)

// LabelManagedBy / ManagedByValue mark every object the operator creates.
// CommonLabels applies them last, so user-supplied podLabels cannot override
// them. They are exported because the manager scopes its informer cache to this
// label (spec 10) and must do so from the same source of truth that writes it —
// a drift between the two would silently empty the cache.
const (
	LabelManagedBy = "app.kubernetes.io/managed-by"
	ManagedByValue = "palworld-operator"
)

// Names of the child objects for a game.
func StatefulSetName(g *palworldv1alpha1.PalworldGame) string     { return g.Name }
func HeadlessServiceName(g *palworldv1alpha1.PalworldGame) string { return g.Name + "-headless" }
func GameServiceName(g *palworldv1alpha1.PalworldGame) string     { return g.Name + "-game" }
func AdminServiceName(g *palworldv1alpha1.PalworldGame) string    { return g.Name + "-admin" }
func MetricsServiceName(g *palworldv1alpha1.PalworldGame) string  { return g.Name + "-metrics" }
func ConfigMapName(g *palworldv1alpha1.PalworldGame) string       { return g.Name + "-config" }
func GeneratedSecretName(g *palworldv1alpha1.PalworldGame) string { return g.Name + "-credentials" }
func ServiceAccountName(g *palworldv1alpha1.PalworldGame) string  { return g.Name }
func PDBName(g *palworldv1alpha1.PalworldGame) string             { return g.Name }
func RouteName(g *palworldv1alpha1.PalworldGame) string           { return g.Name + "-rest" }
func NetworkPolicyName(g *palworldv1alpha1.PalworldGame) string   { return g.Name }
func DataPVCName(g *palworldv1alpha1.PalworldGame) string         { return "data-" + g.Name + "-0" }

// SelectorLabels are the immutable labels used to select the server pod. They
// must never change for an existing StatefulSet.
func SelectorLabels(g *palworldv1alpha1.PalworldGame) map[string]string {
	return map[string]string{
		labelName:      appName,
		labelInstance:  g.Name,
		labelComponent: "server",
	}
}

// CommonLabels are applied to every child object. User-supplied PodLabels are
// applied first so the reserved identity/selector labels always win — otherwise
// a user label could break the StatefulSet selector or the ServiceMonitor
// target.
func CommonLabels(g *palworldv1alpha1.PalworldGame) map[string]string {
	l := map[string]string{}
	for k, v := range g.Spec.PodLabels {
		l[k] = v
	}
	l[labelName] = appName
	l[labelInstance] = g.Name
	l[LabelManagedBy] = ManagedByValue
	l[labelComponent] = "server"
	l[labelPartOf] = appName
	l[labelGame] = g.Name
	return l
}

// GamePort returns the configured game UDP port or the default.
func GamePort(g *palworldv1alpha1.PalworldGame) int32 {
	if g.Spec.Networking.GamePort > 0 {
		return g.Spec.Networking.GamePort
	}
	return DefaultGamePort
}

// QueryPort returns the configured Steam query UDP port or the default.
func QueryPort(g *palworldv1alpha1.PalworldGame) int32 {
	if g.Spec.Networking.QueryPort > 0 {
		return g.Spec.Networking.QueryPort
	}
	return DefaultQueryPort
}

// DesiredReplicas returns 0 or 1 based on the spec (defaulting to 1).
func DesiredReplicas(g *palworldv1alpha1.PalworldGame) int32 {
	if g.Spec.Replicas == nil {
		return 1
	}
	if *g.Spec.Replicas <= 0 {
		return 0
	}
	return 1
}
