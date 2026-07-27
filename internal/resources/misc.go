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
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

// DesiredServiceAccount is the ServiceAccount the server pod runs as.
func DesiredServiceAccount(g *palworldv1alpha1.PalworldGame) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceAccountName(g),
			Namespace: g.Namespace,
			Labels:    CommonLabels(g),
		},
	}
}

// DesiredPodDisruptionBudget protects the single server pod from voluntary
// disruptions (node drains). MinAvailable defaults to 1 so the platform will
// not evict the only game pod without the operator's involvement.
func DesiredPodDisruptionBudget(g *palworldv1alpha1.PalworldGame) *policyv1.PodDisruptionBudget {
	minAvailable := intstr.FromInt32(1)
	if g.Spec.PodDisruptionBudget != nil && g.Spec.PodDisruptionBudget.MinAvailable != nil {
		minAvailable = intstr.FromInt32(*g.Spec.PodDisruptionBudget.MinAvailable)
	}
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PDBName(g),
			Namespace: g.Namespace,
			Labels:    CommonLabels(g),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector:     &metav1.LabelSelector{MatchLabels: SelectorLabels(g)},
		},
	}
}

// IngressRouterNamespaceLabel is the label OpenShift maintains on the
// namespace(s) hosting the ingress router. Selecting on it is the supported way
// to admit router traffic without hard-coding the `openshift-ingress` namespace
// name, which is configurable.
const IngressRouterNamespaceLabel = "policy-group.network.openshift.io/ingress"

// DesiredNetworkPolicy restricts ingress to the server pod: the public game and
// query UDP ports are open to all, while RCON and the REST API are only
// reachable from pods in the same namespace and from the operator's namespace.
// The metrics port is left open for Prometheus.
//
// When the REST API is fronted by an OpenShift Route
// (spec.networking.restAPI.route), the ingress router is additionally admitted —
// but only to the REST port. The router runs in its own namespace and matches
// neither of the other peers, so without this the Route answers HTTP 503.
// RCON keeps the narrower peer list in a rule of its own: it is a raw admin
// channel that must never be reachable from the router.
func DesiredNetworkPolicy(g *palworldv1alpha1.PalworldGame, operatorNamespace string) *networkingv1.NetworkPolicy {
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP
	gamePort := intstr.FromInt32(GamePort(g))
	queryPort := intstr.FromInt32(QueryPort(g))
	rconPort := intstr.FromInt32(RCONPort)
	restPort := intstr.FromInt32(RESTPort)
	metricsPort := intstr.FromInt32(MetricsPort)

	// Admin-reachable ports are restricted to same-namespace and
	// operator-namespace sources.
	adminFrom := []networkingv1.NetworkPolicyPeer{
		{PodSelector: &metav1.LabelSelector{}},
		{NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"kubernetes.io/metadata.name": operatorNamespace},
		}},
	}

	// The REST rule starts from the same peers and gains the router only when a
	// Route was actually requested. Copied rather than aliased so appending here
	// cannot mutate the backing array shared with the RCON rule.
	restFrom := make([]networkingv1.NetworkPolicyPeer, len(adminFrom), len(adminFrom)+1)
	copy(restFrom, adminFrom)
	if g.Spec.Networking.RESTAPI.Route {
		restFrom = append(restFrom, networkingv1.NetworkPolicyPeer{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{IngressRouterNamespaceLabel: ""},
			},
		})
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      NetworkPolicyName(g),
			Namespace: g.Namespace,
			Labels:    CommonLabels(g),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: SelectorLabels(g)},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					// Public game + query UDP: open to everyone.
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &udp, Port: &gamePort},
						{Protocol: &udp, Port: &queryPort},
					},
				},
				{
					// RCON: same-namespace + operator namespace only, never the
					// ingress router.
					From: adminFrom,
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &rconPort},
					},
				},
				{
					// REST: the same peers, plus the ingress router when a Route
					// fronts this port.
					From: restFrom,
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &restPort},
					},
				},
				{
					// Metrics carry only non-sensitive operational data (player
					// count, fps) and must be reachable by Prometheus, which
					// typically lives in a separate namespace whose identity we
					// cannot know here, so the port is left open. Restrict it
					// further with your own NetworkPolicy if required.
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &metricsPort},
					},
				},
			},
		},
	}
}
