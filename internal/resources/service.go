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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

func gamePorts(g *palworldv1alpha1.PalworldGame) []corev1.ServicePort {
	return []corev1.ServicePort{
		{Name: "game", Port: GamePort(g), TargetPort: intstr.FromString("game"), Protocol: corev1.ProtocolUDP},
		{Name: "query", Port: QueryPort(g), TargetPort: intstr.FromString("query"), Protocol: corev1.ProtocolUDP},
	}
}

// DesiredHeadlessService is the governing headless service for the StatefulSet.
// publishNotReadyAddresses lets the operator reach the pod (RCON/REST) while it
// is still starting up.
func DesiredHeadlessService(g *palworldv1alpha1.PalworldGame) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      HeadlessServiceName(g),
			Namespace: g.Namespace,
			Labels:    CommonLabels(g),
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			Selector:                 SelectorLabels(g),
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{
				{Name: "game", Port: GamePort(g), TargetPort: intstr.FromString("game"), Protocol: corev1.ProtocolUDP},
				{Name: "query", Port: QueryPort(g), TargetPort: intstr.FromString("query"), Protocol: corev1.ProtocolUDP},
				{Name: "rcon", Port: RCONPort, TargetPort: intstr.FromString("rcon"), Protocol: corev1.ProtocolTCP},
				{Name: "rest", Port: RESTPort, TargetPort: intstr.FromString("rest"), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

// DesiredGameService exposes the public game + query UDP ports. Because
// OpenShift Routes cannot carry UDP, this is a ClusterIP/NodePort/LoadBalancer
// service depending on the environment (LoadBalancer via MetalLB is the clean
// on-prem answer).
func DesiredGameService(g *palworldv1alpha1.PalworldGame) *corev1.Service {
	svcType := corev1.ServiceTypeClusterIP
	switch g.Spec.Networking.ServiceType {
	case palworldv1alpha1.ServiceTypeNodePort:
		svcType = corev1.ServiceTypeNodePort
	case palworldv1alpha1.ServiceTypeLoadBalancer:
		svcType = corev1.ServiceTypeLoadBalancer
	}

	ports := gamePorts(g)
	if svcType == corev1.ServiceTypeNodePort && g.Spec.Networking.NodePort > 0 {
		ports[0].NodePort = g.Spec.Networking.NodePort
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        GameServiceName(g),
			Namespace:   g.Namespace,
			Labels:      CommonLabels(g),
			Annotations: g.Spec.Networking.ServiceAnnotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     svcType,
			Selector: SelectorLabels(g),
			Ports:    ports,
			// Preserve client source IP where possible; game servers benefit
			// from correct client addressing.
			ExternalTrafficPolicy: externalTrafficPolicy(svcType),
			// Keep players reachable for the whole preStop shutdown countdown
			// (spec 07). An endpoint is marked ready=false the moment its pod
			// starts terminating, whatever the probes say, and with
			// ExternalTrafficPolicy: Local kube-proxy's healthCheckNodePort counts
			// only ready local endpoints -- so it returns 503 and the load
			// balancer withdraws the node. A game is a single-replica StatefulSet,
			// so there is no second endpoint to hold the health check up and the
			// VIP vanishes: players get the first "shutting down in 5 minutes"
			// broadcast and are disconnected moments later.
			PublishNotReadyAddresses: true,
		},
	}
	if svcType == corev1.ServiceTypeLoadBalancer {
		if g.Spec.Networking.LoadBalancerIP != "" {
			svc.Spec.LoadBalancerIP = g.Spec.Networking.LoadBalancerIP
		}
		svc.Spec.LoadBalancerClass = g.Spec.Networking.LoadBalancerClass
	}
	return svc
}

func externalTrafficPolicy(t corev1.ServiceType) corev1.ServiceExternalTrafficPolicy {
	if t == corev1.ServiceTypeLoadBalancer || t == corev1.ServiceTypeNodePort {
		return corev1.ServiceExternalTrafficPolicyLocal
	}
	return ""
}

// DesiredAdminService is an internal-only ClusterIP service exposing RCON and
// the REST API for the operator (never expose these to the internet).
func DesiredAdminService(g *palworldv1alpha1.PalworldGame) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AdminServiceName(g),
			Namespace: g.Namespace,
			Labels:    CommonLabels(g),
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP,
			Selector:                 SelectorLabels(g),
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{
				{Name: "rcon", Port: RCONPort, TargetPort: intstr.FromString("rcon"), Protocol: corev1.ProtocolTCP},
				{Name: "rest", Port: RESTPort, TargetPort: intstr.FromString("rest"), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

// DesiredMetricsService exposes the metrics-exporter sidecar for scraping. It
// carries a distinct component label so a ServiceMonitor can target only this
// service.
func DesiredMetricsService(g *palworldv1alpha1.PalworldGame) *corev1.Service {
	labels := CommonLabels(g)
	labels[labelComponent] = "metrics"
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MetricsServiceName(g),
			Namespace: g.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: SelectorLabels(g),
			Ports: []corev1.ServicePort{
				{Name: "metrics", Port: MetricsPort, TargetPort: intstr.FromString("metrics"), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}
