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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

// RouteGVK is the OpenShift Route GVK. Built unstructured so the operator does
// not depend on the openshift/api Go module (which tracks a much newer
// Kubernetes release and conflicts with controller-runtime's pinned version).
var RouteGVK = schema.GroupVersionKind{
	Group:   "route.openshift.io",
	Version: "v1",
	Kind:    "Route",
}

// DesiredRoute builds an OpenShift Route exposing the REST admin API. The REST
// API is HTTP, so unlike the UDP game port it can be fronted by the OpenShift
// Router. Only created when RESTAPI.Route is enabled and the cluster has the
// Route API.
func DesiredRoute(g *palworldv1alpha1.PalworldGame) *unstructured.Unstructured {
	// The REST API is served as plain HTTP inside the pod, so the only valid
	// Route termination is edge (router terminates TLS, cleartext to the pod).
	// reencrypt/passthrough would require the pod to serve TLS and are rejected
	// by the admission webhook; we hard-code edge here as a defense in depth.
	tls := map[string]any{
		"termination":                   "edge",
		"insecureEdgeTerminationPolicy": "Redirect",
	}

	labels := map[string]any{}
	for k, v := range CommonLabels(g) {
		labels[k] = v
	}

	spec := map[string]any{
		"to": map[string]any{
			"kind":   "Service",
			"name":   AdminServiceName(g),
			"weight": int64(100),
		},
		"port": map[string]any{
			"targetPort": "rest",
		},
		"tls": tls,
	}
	if g.Spec.Networking.RESTAPI.Host != "" {
		spec["host"] = g.Spec.Networking.RESTAPI.Host
	}

	route := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name":      RouteName(g),
				"namespace": g.Namespace,
				"labels":    labels,
			},
			"spec": spec,
		},
	}
	route.SetGroupVersionKind(RouteGVK)
	return route
}
