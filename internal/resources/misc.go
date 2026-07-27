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

// DesiredNetworkPolicy restricts ingress to the server pod: the public game and
// query UDP ports are open to all, while RCON and the REST API are only
// reachable from pods in the same namespace and from the operator's namespace.
// The metrics port is reachable from the operator namespace for scraping.
func DesiredNetworkPolicy(g *palworldv1alpha1.PalworldGame, operatorNamespace string) *networkingv1.NetworkPolicy {
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP
	gamePort := intstr.FromInt32(GamePort(g))
	queryPort := intstr.FromInt32(QueryPort(g))
	rconPort := intstr.FromInt32(RCONPort)
	restPort := intstr.FromInt32(RESTPort)
	metricsPort := intstr.FromInt32(MetricsPort)

	// Admins/operator reachable ports are restricted to same-namespace and
	// operator-namespace sources.
	adminFrom := []networkingv1.NetworkPolicyPeer{
		{PodSelector: &metav1.LabelSelector{}},
		{NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"kubernetes.io/metadata.name": operatorNamespace},
		}},
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
					// RCON and REST are admin surfaces: same-namespace + operator
					// namespace only.
					From: adminFrom,
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &rconPort},
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
