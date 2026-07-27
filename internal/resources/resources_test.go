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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
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

func TestReservedLabelsWinOverPodLabels(t *testing.T) {
	g := testGame()
	g.Spec.PodLabels = map[string]string{
		labelInstance: "attacker", // must not override the identity label
		"team":        "red",      // custom labels are preserved
	}
	l := CommonLabels(g)
	if l[labelInstance] != g.Name {
		t.Errorf("reserved instance label was overridden by PodLabels: %q", l[labelInstance])
	}
	if l["team"] != "red" {
		t.Errorf("custom PodLabel was dropped")
	}
}

func TestRenderEngineININestedSection(t *testing.T) {
	out := renderEngineINI(map[string]string{
		"/Script/OnlineSubsystemUtils.IpNetDriver/NetServerMaxTickRate": "120",
	})
	if !strings.Contains(out, "[/Script/OnlineSubsystemUtils.IpNetDriver]") {
		t.Errorf("section name with slashes was mangled:\n%s", out)
	}
	if !strings.Contains(out, "NetServerMaxTickRate=120") {
		t.Errorf("key/value missing:\n%s", out)
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

// findRuleByPort returns the single ingress rule exposing the given TCP port.
// It fails the test if the port appears in zero or several rules, which is
// itself the property the NetworkPolicy tests below rely on: each admin port
// gets exactly one rule, so a peer added for one port cannot leak onto another.
func findRuleByPort(t *testing.T, np *networkingv1.NetworkPolicy, port int32) networkingv1.NetworkPolicyIngressRule {
	t.Helper()
	var found []networkingv1.NetworkPolicyIngressRule
	for _, rule := range np.Spec.Ingress {
		for _, p := range rule.Ports {
			if p.Port != nil && p.Port.IntVal == port {
				found = append(found, rule)
				break
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly 1 ingress rule for port %d, got %d", port, len(found))
	}
	return found[0]
}

// hasRouterPeer reports whether any peer selects the OpenShift ingress router
// namespace.
func hasRouterPeer(rule networkingv1.NetworkPolicyIngressRule) bool {
	for _, peer := range rule.From {
		if peer.NamespaceSelector == nil {
			continue
		}
		if _, ok := peer.NamespaceSelector.MatchLabels[IngressRouterNamespaceLabel]; ok {
			return true
		}
	}
	return false
}

func TestNetworkPolicyGamePortsOpenToAll(t *testing.T) {
	np := DesiredNetworkPolicy(testGame(), "operator-ns")
	rule := findRuleByPort(t, np, DefaultGamePort)
	if len(rule.From) != 0 {
		t.Errorf("game UDP port must be open to all sources, got %d peers", len(rule.From))
	}
}

// Without a Route the REST port must keep the original narrow peer list; adding
// the router unconditionally would expose the admin API on every cluster.
func TestNetworkPolicyNoRouterPeerWhenRouteDisabled(t *testing.T) {
	g := testGame()
	g.Spec.Networking.RESTAPI.Route = false
	np := DesiredNetworkPolicy(g, "operator-ns")

	if hasRouterPeer(findRuleByPort(t, np, RESTPort)) {
		t.Errorf("REST rule must not admit the ingress router when restAPI.route is false")
	}
	if hasRouterPeer(findRuleByPort(t, np, RCONPort)) {
		t.Errorf("RCON rule must never admit the ingress router")
	}
}

// The regression this fixes: with restAPI.route enabled the router could not
// reach 8212 and the Route answered 503.
func TestNetworkPolicyRouterPeerWhenRouteEnabled(t *testing.T) {
	g := testGame()
	g.Spec.Networking.RESTAPI.Route = true
	np := DesiredNetworkPolicy(g, "operator-ns")

	if !hasRouterPeer(findRuleByPort(t, np, RESTPort)) {
		t.Errorf("REST rule must admit the ingress router when restAPI.route is true")
	}
}

// RCON is a raw admin channel: enabling the REST Route must not widen it. This
// also guards the slice-aliasing hazard — restFrom is built by copy, so
// appending the router peer cannot write into the array backing adminFrom.
func TestNetworkPolicyRCONNeverAdmitsRouter(t *testing.T) {
	g := testGame()
	g.Spec.Networking.RESTAPI.Route = true
	np := DesiredNetworkPolicy(g, "operator-ns")

	rcon := findRuleByPort(t, np, RCONPort)
	if hasRouterPeer(rcon) {
		t.Fatalf("RCON rule admits the ingress router: %+v", rcon.From)
	}
	if len(rcon.From) != 2 {
		t.Errorf("RCON rule should keep exactly its 2 admin peers, got %d", len(rcon.From))
	}
}

// Enabling the Route must add the router peer, not replace the existing ones —
// the operator itself reaches REST from its own namespace.
func TestNetworkPolicyRestKeepsAdminPeersWithRoute(t *testing.T) {
	g := testGame()
	g.Spec.Networking.RESTAPI.Route = true
	np := DesiredNetworkPolicy(g, "operator-ns")

	rule := findRuleByPort(t, np, RESTPort)
	if len(rule.From) != 3 {
		t.Fatalf("expected 3 REST peers (same-ns, operator-ns, router), got %d", len(rule.From))
	}
	var sameNS, operatorNS bool
	for _, peer := range rule.From {
		if peer.PodSelector != nil && len(peer.PodSelector.MatchLabels) == 0 {
			sameNS = true
		}
		if peer.NamespaceSelector != nil &&
			peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == "operator-ns" {
			operatorNS = true
		}
	}
	if !sameNS {
		t.Errorf("REST rule lost the same-namespace peer")
	}
	if !operatorNS {
		t.Errorf("REST rule lost the operator-namespace peer")
	}
}

// The manager scopes its cache to this label (spec 10); if CommonLabels ever
// stopped emitting it the cache would silently go empty.
func TestCommonLabelsAlwaysCarriesManagedBy(t *testing.T) {
	g := testGame()
	// A user trying to override the reserved label must not win.
	g.Spec.PodLabels = map[string]string{LabelManagedBy: "someone-else"}
	if got := CommonLabels(g)[LabelManagedBy]; got != ManagedByValue {
		t.Errorf("managed-by label = %q, want %q", got, ManagedByValue)
	}
}

// envOf returns the server container's env as a map for assertions.
func envOf(t *testing.T, g *palworldv1alpha1.PalworldGame) map[string]string {
	t.Helper()
	sts := DesiredStatefulSet(g, BuildParams{DefaultServerImage: "img"}, "h")
	got := map[string]string{}
	for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
		got[e.Name] = e.Value
	}
	return got
}

// The countdown runs inside preStop and the kubelet's grace clock covers preStop,
// so an unset grace period must be derived from the warning -- never a fixed
// number that could be shorter than the countdown.
func TestTerminationGracePeriodDerivedFromShutdownWarning(t *testing.T) {
	g := testGame()
	sts := DesiredStatefulSet(g, BuildParams{DefaultServerImage: "img"}, "h")
	got := *sts.Spec.Template.Spec.TerminationGracePeriodSeconds
	want := int64(palworldv1alpha1.DefaultShutdownWarnSeconds) + palworldv1alpha1.ShutdownGraceHeadroomSeconds
	if got != want {
		t.Errorf("derived terminationGracePeriodSeconds = %d, want %d", got, want)
	}
	if got <= int64(palworldv1alpha1.DefaultShutdownWarnSeconds) {
		t.Errorf("grace period %d does not outlast the %ds countdown", got, palworldv1alpha1.DefaultShutdownWarnSeconds)
	}
}

func TestTerminationGracePeriodTracksCustomWarning(t *testing.T) {
	g := testGame()
	g.Spec.Shutdown = &palworldv1alpha1.ShutdownPolicy{WarnSeconds: 900}
	sts := DesiredStatefulSet(g, BuildParams{DefaultServerImage: "img"}, "h")
	if got, want := *sts.Spec.Template.Spec.TerminationGracePeriodSeconds,
		int64(900)+palworldv1alpha1.ShutdownGraceHeadroomSeconds; got != want {
		t.Errorf("terminationGracePeriodSeconds = %d, want %d", got, want)
	}
}

func TestTerminationGracePeriodExplicitWins(t *testing.T) {
	g := testGame()
	explicit := int64(45)
	g.Spec.TerminationGracePeriodSeconds = &explicit
	sts := DesiredStatefulSet(g, BuildParams{DefaultServerImage: "img"}, "h")
	if got := *sts.Spec.Template.Spec.TerminationGracePeriodSeconds; got != explicit {
		t.Errorf("explicit terminationGracePeriodSeconds = %d, want %d", got, explicit)
	}
}

// graceful-shutdown.sh reads the countdown entirely from env, so a missing or
// wrong variable silently disables the player warning.
func TestShutdownEnvDefaults(t *testing.T) {
	env := envOf(t, testGame())
	want := map[string]string{
		"SHUTDOWN_WARN_SECONDS":          "300",
		"SHUTDOWN_WARN_INTERVAL_SECONDS": "60",
		"SHUTDOWN_WARN_MESSAGE":          palworldv1alpha1.DefaultShutdownWarnMessage,
		"SHUTDOWN_GRACE_SECONDS":         "600",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("env %s = %q, want %q", k, env[k], v)
		}
	}
}

func TestShutdownEnvFromPolicy(t *testing.T) {
	g := testGame()
	g.Spec.Shutdown = &palworldv1alpha1.ShutdownPolicy{
		WarnSeconds:         120,
		WarnIntervalSeconds: 30,
		WarnMessage:         "back in %s",
	}
	env := envOf(t, g)
	want := map[string]string{
		"SHUTDOWN_WARN_SECONDS":          "120",
		"SHUTDOWN_WARN_INTERVAL_SECONDS": "30",
		"SHUTDOWN_WARN_MESSAGE":          "back in %s",
		"SHUTDOWN_GRACE_SECONDS":         "420",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("env %s = %q, want %q", k, env[k], v)
		}
	}
}

// SHUTDOWN_GRACE_SECONDS is what lets the container clamp its countdown to the
// pod's real budget, so it must match the pod spec exactly.
func TestShutdownGraceEnvMatchesPodSpec(t *testing.T) {
	g := testGame()
	explicit := int64(90)
	g.Spec.TerminationGracePeriodSeconds = &explicit
	sts := DesiredStatefulSet(g, BuildParams{DefaultServerImage: "img"}, "h")
	var graceEnv string
	for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "SHUTDOWN_GRACE_SECONDS" {
			graceEnv = e.Value
		}
	}
	if graceEnv != "90" {
		t.Errorf("SHUTDOWN_GRACE_SECONDS = %q, want %q", graceEnv, "90")
	}
}

// extraEnv is appended last so an operator can still override the countdown.
func TestExtraEnvCanOverrideShutdownWarning(t *testing.T) {
	g := testGame()
	g.Spec.ExtraEnv = []corev1.EnvVar{{Name: "SHUTDOWN_WARN_SECONDS", Value: "10"}}
	sts := DesiredStatefulSet(g, BuildParams{DefaultServerImage: "img"}, "h")
	env := sts.Spec.Template.Spec.Containers[0].Env
	var last string
	for _, e := range env {
		if e.Name == "SHUTDOWN_WARN_SECONDS" {
			last = e.Value
		}
	}
	if last != "10" {
		t.Errorf("last SHUTDOWN_WARN_SECONDS = %q, want %q (extraEnv must win)", last, "10")
	}
}

// The countdown only runs if preStop actually invokes the script.
func TestPreStopRunsGracefulShutdown(t *testing.T) {
	sts := DesiredStatefulSet(testGame(), BuildParams{DefaultServerImage: "img"}, "h")
	lc := sts.Spec.Template.Spec.Containers[0].Lifecycle
	if lc == nil || lc.PreStop == nil || lc.PreStop.Exec == nil {
		t.Fatal("server container has no preStop exec hook")
	}
	if got := lc.PreStop.Exec.Command; len(got) != 1 || !strings.HasSuffix(got[0], "graceful-shutdown.sh") {
		t.Errorf("preStop command = %v, want graceful-shutdown.sh", got)
	}
}
