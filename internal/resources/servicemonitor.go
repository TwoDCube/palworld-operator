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

// ServiceMonitorGVK is the Prometheus Operator ServiceMonitor GVK. We build it
// unstructured so the operator does not depend on the prometheus-operator Go
// module and degrades gracefully on clusters without the monitoring API.
var ServiceMonitorGVK = schema.GroupVersionKind{
	Group:   "monitoring.coreos.com",
	Version: "v1",
	Kind:    "ServiceMonitor",
}

// DesiredServiceMonitor builds a ServiceMonitor scraping the metrics service.
func DesiredServiceMonitor(g *palworldv1alpha1.PalworldGame) *unstructured.Unstructured {
	labels := map[string]any{}
	for k, v := range CommonLabels(g) {
		labels[k] = v
	}
	// Target only the metrics service (component=metrics), not the game/admin
	// services which share the common labels.
	selector := map[string]any{
		labelInstance:  g.Name,
		labelComponent: "metrics",
	}

	sm := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name":      g.Name,
				"namespace": g.Namespace,
				"labels":    labels,
			},
			"spec": map[string]any{
				"selector": map[string]any{
					"matchLabels": selector,
				},
				"endpoints": []any{
					map[string]any{
						"port":     "metrics",
						"path":     "/metrics",
						"interval": "30s",
					},
				},
			},
		},
	}
	sm.SetGroupVersionKind(ServiceMonitorGVK)
	return sm
}
