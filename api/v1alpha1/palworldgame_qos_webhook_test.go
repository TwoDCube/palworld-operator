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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// A pod that misses Guaranteed QoS still schedules and runs perfectly; it just
// never receives exclusive CPUs. That silence is why this is worth warning
// about, and why the warning has to name the container responsible.

const qosWarnSubstr = "Guaranteed QoS"

func matched(cpu, mem string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(mem),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(mem),
		},
	}
}

func pinnedSpecGame() *PalworldGame {
	g := game()
	g.Spec.Resources = matched("4", "16Gi")
	return g
}

func TestGuaranteedIntentWithNothingBreakingItDoesNotWarn(t *testing.T) {
	if w := validateWarnings(t, pinnedSpecGame()); hasWarningContaining(w, qosWarnSubstr) {
		t.Errorf("unexpected QoS warning: %v", w)
	}
}

func TestUnmatchedSidecarBreaksGuaranteedWarns(t *testing.T) {
	g := pinnedSpecGame()
	g.Spec.Sidecars = []corev1.Container{{
		Name: "log-shipper",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m")},
		},
	}}
	w := validateWarnings(t, g)
	if !hasWarningContaining(w, qosWarnSubstr) {
		t.Fatalf("expected a QoS warning, got %v", w)
	}
	// The operator has to say which container did it, or the user cannot act.
	if !hasWarningContaining(w, "log-shipper") {
		t.Errorf("warning does not name the offending sidecar: %v", w)
	}
}

func TestUnmatchedExporterResourcesBreakGuaranteedWarns(t *testing.T) {
	g := pinnedSpecGame()
	g.Spec.Monitoring.ExporterResources = &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m")},
		Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")},
	}
	w := validateWarnings(t, g)
	if !hasWarningContaining(w, "exporterResources") {
		t.Errorf("expected a warning naming exporterResources, got %v", w)
	}
}

func TestMatchedSidecarDoesNotWarn(t *testing.T) {
	g := pinnedSpecGame()
	g.Spec.Sidecars = []corev1.Container{{Name: "ok", Resources: matched("100m", "64Mi")}}
	if w := validateWarnings(t, g); hasWarningContaining(w, qosWarnSubstr) {
		t.Errorf("unexpected QoS warning for a matched sidecar: %v", w)
	}
}

// Without Guaranteed intent on the game container there is nothing to break, so
// an unmatched sidecar is unremarkable and must not generate noise.
func TestBurstableGameWithUnmatchedSidecarDoesNotWarn(t *testing.T) {
	g := game()
	g.Spec.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("6")},
	}
	g.Spec.Sidecars = []corev1.Container{{Name: "whatever"}}
	if w := validateWarnings(t, g); hasWarningContaining(w, qosWarnSubstr) {
		t.Errorf("unexpected QoS warning on a Burstable game: %v", w)
	}
}

func TestGuaranteedShapedRejectsPartialSpecs(t *testing.T) {
	cases := map[string]corev1.ResourceRequirements{
		"cpu only":      matchedCPUOnly(),
		"requests only": {Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}},
		"limits above requests": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		"empty": {},
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			if guaranteedShaped(r) {
				t.Errorf("%s should not count as Guaranteed-shaped", name)
			}
		})
	}
	if !guaranteedShaped(matched("1", "1Gi")) {
		t.Errorf("matched cpu+memory should count as Guaranteed-shaped")
	}
}

func matchedCPUOnly() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
	}
}
