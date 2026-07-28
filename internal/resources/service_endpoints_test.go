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

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

// Endpoint publication is what keeps players connected through the shutdown
// countdown. A pod is Terminating for the countdown's entire duration and is
// marked ready=false throughout, so a game Service that does not publish
// not-ready addresses loses its load-balancer VIP seconds after the first
// warning goes out (spec 08).

func gameServiceFor(t *testing.T, st palworldv1alpha1.ServiceType) *corev1.Service {
	t.Helper()
	g := testGame()
	g.Spec.Networking.ServiceType = st
	return DesiredGameService(g)
}

// The regression: with ExternalTrafficPolicy Local, kube-proxy's health check
// counts only ready endpoints, and a single-replica StatefulSet has none while
// terminating.
func TestGameServicePublishesNotReadyAddresses(t *testing.T) {
	for _, st := range []palworldv1alpha1.ServiceType{
		palworldv1alpha1.ServiceTypeLoadBalancer,
		palworldv1alpha1.ServiceTypeNodePort,
		palworldv1alpha1.ServiceTypeClusterIP,
	} {
		t.Run(string(st), func(t *testing.T) {
			svc := gameServiceFor(t, st)
			if !svc.Spec.PublishNotReadyAddresses {
				t.Errorf("game Service (%s) must publish not-ready addresses so the "+
					"shutdown countdown does not drop players", st)
			}
		})
	}
}

// Client-IP preservation must survive the fix: the two settings interact, and it
// is precisely ExternalTrafficPolicy Local that makes the flag necessary.
func TestGameServiceKeepsLocalTrafficPolicyForExternalTypes(t *testing.T) {
	for st, want := range map[palworldv1alpha1.ServiceType]corev1.ServiceExternalTrafficPolicy{
		palworldv1alpha1.ServiceTypeLoadBalancer: corev1.ServiceExternalTrafficPolicyLocal,
		palworldv1alpha1.ServiceTypeNodePort:     corev1.ServiceExternalTrafficPolicyLocal,
		palworldv1alpha1.ServiceTypeClusterIP:    "",
	} {
		t.Run(string(st), func(t *testing.T) {
			if got := gameServiceFor(t, st).Spec.ExternalTrafficPolicy; got != want {
				t.Errorf("externalTrafficPolicy = %q, want %q", got, want)
			}
		})
	}
}

// The headless and admin Services already relied on this; assert all three stay
// consistent so a future edit cannot silently regress one of them.
func TestAllPodBackedServicesPublishNotReadyAddresses(t *testing.T) {
	g := testGame()
	for name, svc := range map[string]*corev1.Service{
		"game":     DesiredGameService(g),
		"headless": DesiredHeadlessService(g),
		"admin":    DesiredAdminService(g),
	} {
		if !svc.Spec.PublishNotReadyAddresses {
			t.Errorf("%s Service does not publish not-ready addresses", name)
		}
	}
}
