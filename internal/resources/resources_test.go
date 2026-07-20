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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

func testGame() *palworldv1alpha1.PalworldGame {
	return &palworldv1alpha1.PalworldGame{
		ObjectMeta: metav1.ObjectMeta{Name: "srv", Namespace: "games"},
	}
}

func TestSelectorLabelsStable(t *testing.T) {
	g := testGame()
	l := SelectorLabels(g)
	if l[labelInstance] != "srv" || l[labelName] != appName || l[labelComponent] != "server" {
		t.Errorf("unexpected selector labels: %v", l)
	}
}

func TestStatefulSetPorts(t *testing.T) {
	g := testGame()
	sts := DesiredStatefulSet(g, BuildParams{DefaultServerImage: "img"}, "hash123")
	if *sts.Spec.Replicas != 1 {
		t.Errorf("expected 1 replica, got %d", *sts.Spec.Replicas)
	}
	c := sts.Spec.Template.Spec.Containers[0]
	wantPorts := map[string]corev1.Protocol{
		"game":  corev1.ProtocolUDP,
		"query": corev1.ProtocolUDP,
		"rcon":  corev1.ProtocolTCP,
		"rest":  corev1.ProtocolTCP,
	}
	got := map[string]corev1.Protocol{}
	for _, p := range c.Ports {
		got[p.Name] = p.Protocol
	}
	for name, proto := range wantPorts {
		if got[name] != proto {
			t.Errorf("port %s: want %s, got %s", name, proto, got[name])
		}
	}
	// Settings hash must land on the pod template so changes roll the pod.
	if sts.Spec.Template.Annotations[AnnotationSettingsHash] != "hash123" {
		t.Errorf("settings hash annotation missing")
	}
	// Data volume claim template present and named "data".
	if len(sts.Spec.VolumeClaimTemplates) != 1 || sts.Spec.VolumeClaimTemplates[0].Name != "data" {
		t.Errorf("expected a single data volumeClaimTemplate")
	}
}

func TestAdminPasswordFromSecret(t *testing.T) {
	g := testGame()
	sts := DesiredStatefulSet(g, BuildParams{DefaultServerImage: "img"}, "h")
	c := sts.Spec.Template.Spec.Containers[0]
	var found bool
	for _, e := range c.Env {
		if e.Name == "ADMIN_PASSWORD" {
			if e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil {
				t.Errorf("ADMIN_PASSWORD must come from a secret ref, not a literal")
			}
			found = true
		}
		if e.Name == "ADMIN_PASSWORD" && e.Value != "" {
			t.Errorf("ADMIN_PASSWORD must not be a plaintext literal")
		}
	}
	if !found {
		t.Errorf("ADMIN_PASSWORD env not set")
	}
}

func TestSecurityContextOpenShiftVsVanilla(t *testing.T) {
	g := testGame()

	vanilla := DesiredStatefulSet(g, BuildParams{OpenShift: false, DefaultServerImage: "img"}, "h")
	sc := vanilla.Spec.Template.Spec.SecurityContext
	if sc.RunAsUser == nil || *sc.RunAsUser != 10000 {
		t.Errorf("vanilla k8s should pin runAsUser=10000")
	}
	if sc.FSGroup == nil {
		t.Errorf("vanilla k8s should set fsGroup so the group-root volume is writable")
	}

	ocp := DesiredStatefulSet(g, BuildParams{OpenShift: true, DefaultServerImage: "img"}, "h")
	osc := ocp.Spec.Template.Spec.SecurityContext
	if osc.RunAsUser != nil || osc.FSGroup != nil {
		t.Errorf("OpenShift should leave runAsUser/fsGroup for the SCC to inject")
	}
	if osc.RunAsNonRoot == nil || !*osc.RunAsNonRoot {
		t.Errorf("runAsNonRoot must be true in both environments")
	}
}

func TestConfigMapRendersSettings(t *testing.T) {
	g := testGame()
	g.Spec.ServerSettings.ServerName = "unit-test"
	cm, err := DesiredConfigMap(g)
	if err != nil {
		t.Fatalf("DesiredConfigMap error: %v", err)
	}
	ini := cm.Data["PalWorldSettings.ini"]
	if ini == "" {
		t.Fatalf("PalWorldSettings.ini not rendered")
	}
	if got := cm.Name; got != "srv-config" {
		t.Errorf("configmap name: want srv-config, got %s", got)
	}
}

func TestGameServiceType(t *testing.T) {
	g := testGame()
	g.Spec.Networking.ServiceType = palworldv1alpha1.ServiceTypeLoadBalancer
	svc := DesiredGameService(g)
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		t.Errorf("expected LoadBalancer service, got %s", svc.Spec.Type)
	}
	if svc.Spec.ExternalTrafficPolicy != corev1.ServiceExternalTrafficPolicyLocal {
		t.Errorf("LoadBalancer game service should use Local traffic policy for client IP preservation")
	}
	// Admin service must remain ClusterIP (never publicly exposed).
	admin := DesiredAdminService(g)
	if admin.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("admin service must be ClusterIP, got %s", admin.Spec.Type)
	}
}
