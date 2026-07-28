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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

// Pod QoS decides whether the kubelet's static CPU Manager policy will hand the
// game container exclusive cores, and it is a pod-wide property that any single
// container can veto. These tests pin the property end-to-end on the built
// StatefulSet rather than on the sidecar in isolation, because that is what the
// kubelet actually evaluates.

// qosShaped mirrors the kubelet's per-container test for Guaranteed QoS.
func qosShaped(r corev1.ResourceRequirements) bool {
	for _, n := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
		req, okReq := r.Requests[n]
		lim, okLim := r.Limits[n]
		if !okReq || !okLim || req.Cmp(lim) != 0 {
			return false
		}
	}
	return true
}

func pinnedGame() *palworldv1alpha1.PalworldGame {
	g := testGame()
	g.Spec.Monitoring.MetricsExporter = true
	g.Spec.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("16Gi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("16Gi"),
		},
	}
	return g
}

// The regression this whole change exists for: with the exporter enabled, a
// game container shaped for Guaranteed QoS must actually yield a pod where every
// container qualifies. The previous sidecar defaults (10m/32Mi requests, a
// memory-only limit) made this impossible to express through the API.
func TestStatefulSetIsGuaranteedShapedWithExporterEnabled(t *testing.T) {
	sts := DesiredStatefulSet(pinnedGame(),
		BuildParams{DefaultServerImage: "img", OperatorImage: "op"}, "h")

	containers := sts.Spec.Template.Spec.Containers
	if len(containers) < 2 {
		t.Fatalf("expected the exporter sidecar to be present, got %d container(s)", len(containers))
	}
	for _, c := range containers {
		if !qosShaped(c.Resources) {
			t.Errorf("container %q is not Guaranteed-shaped: requests=%v limits=%v",
				c.Name, c.Resources.Requests, c.Resources.Limits)
		}
	}

	// The game container must keep an integer CPU: only integer-cpu containers in
	// a Guaranteed pod are given exclusive cores.
	cpu := containers[0].Resources.Requests[corev1.ResourceCPU]
	if cpu.MilliValue() != 4000 {
		t.Errorf("game container cpu request = %s, want exactly 4 whole cores", cpu.String())
	}

	// The sidecar must stay fractional so it draws from the shared pool instead of
	// claiming an exclusive core of its own.
	side := containers[1].Resources.Requests[corev1.ResourceCPU]
	if side.MilliValue()%1000 == 0 {
		t.Errorf("exporter cpu request %s is a whole core; it should stay fractional", side.String())
	}
}

// Neutral means neutral: the sidecar must not manufacture Guaranteed QoS for a
// game container that did not ask for it.
func TestBurstableGameStaysBurstable(t *testing.T) {
	g := testGame()
	g.Spec.Monitoring.MetricsExporter = true
	g.Spec.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("6")},
	}
	sts := DesiredStatefulSet(g, BuildParams{DefaultServerImage: "img", OperatorImage: "op"}, "h")
	if qosShaped(sts.Spec.Template.Spec.Containers[0].Resources) {
		t.Errorf("game container should not have been rewritten into Guaranteed shape")
	}
}

func TestExporterResourcesOverrideIsHonoured(t *testing.T) {
	g := pinnedGame()
	g.Spec.Monitoring.ExporterResources = &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("250m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("250m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}
	sts := DesiredStatefulSet(g, BuildParams{DefaultServerImage: "img", OperatorImage: "op"}, "h")
	got := sts.Spec.Template.Spec.Containers[1].Resources.Requests[corev1.ResourceCPU]
	if got.String() != "250m" {
		t.Errorf("exporter cpu request = %s, want the override 250m", got.String())
	}
}

// An override is taken verbatim, including one that breaks Guaranteed -- the
// operator does not silently "fix" it. The webhook warns instead (spec 01).
func TestExporterResourcesOverrideIsNotCorrected(t *testing.T) {
	g := pinnedGame()
	g.Spec.Monitoring.ExporterResources = &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m")},
	}
	sts := DesiredStatefulSet(g, BuildParams{DefaultServerImage: "img", OperatorImage: "op"}, "h")
	if qosShaped(sts.Spec.Template.Spec.Containers[1].Resources) {
		t.Errorf("override was rewritten; it must be applied verbatim")
	}
}

// The override must not alias the caller's spec: DesiredStatefulSet runs on every
// reconcile against the cached object.
func TestExporterResourcesOverrideIsCopied(t *testing.T) {
	g := pinnedGame()
	g.Spec.Monitoring.ExporterResources = &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")},
	}
	sts := DesiredStatefulSet(g, BuildParams{DefaultServerImage: "img", OperatorImage: "op"}, "h")
	sts.Spec.Template.Spec.Containers[1].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("999m")
	if got := g.Spec.Monitoring.ExporterResources.Requests[corev1.ResourceCPU]; got.String() != "250m" {
		t.Errorf("mutating the built StatefulSet changed the CR: %s", got.String())
	}
}
