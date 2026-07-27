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

package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

func drainScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := palworldv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add corev1: %v", err)
	}
	return s
}

func gameOnNode(name, ns, node string) *palworldv1alpha1.PalworldGame {
	g := &palworldv1alpha1.PalworldGame{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
	}
	g.Status.CurrentNode = node
	return g
}

func TestGamesOnNodeCordonedMatches(t *testing.T) {
	s := drainScheme(t)
	g1 := gameOnNode("a", "ns1", "node-1")
	g2 := gameOnNode("b", "ns2", "node-2")
	c := fake.NewClientBuilder().WithScheme(s).
		WithStatusSubresource(&palworldv1alpha1.PalworldGame{}).
		WithObjects(g1, g2).Build()
	r := &PalworldGameReconciler{Client: c, Scheme: s}

	cordoned := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}
	cordoned.Spec.Unschedulable = true

	reqs := r.gamesOnNode(context.Background(), cordoned)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request for the game on node-1, got %d", len(reqs))
	}
	if reqs[0].Name != "a" || reqs[0].Namespace != "ns1" {
		t.Errorf("unexpected request: %v", reqs[0])
	}
}

func TestGamesOnNodeSchedulableIgnored(t *testing.T) {
	s := drainScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).
		WithStatusSubresource(&palworldv1alpha1.PalworldGame{}).
		WithObjects(gameOnNode("a", "ns1", "node-1")).Build()
	r := &PalworldGameReconciler{Client: c, Scheme: s}

	// A schedulable node must not enqueue anything.
	healthy := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}
	if reqs := r.gamesOnNode(context.Background(), healthy); len(reqs) != 0 {
		t.Errorf("schedulable node should enqueue nothing, got %d", len(reqs))
	}
}

func TestDrainPolicyHelpers(t *testing.T) {
	// Nil policy → enabled, default grace.
	g := &palworldv1alpha1.PalworldGame{}
	if nodeDrainDisabled(g) {
		t.Errorf("nil NodeDrain should be enabled")
	}
	if drainGrace(g) != defaultDrainGrace {
		t.Errorf("nil NodeDrain should use default grace %s, got %s", defaultDrainGrace, drainGrace(g))
	}

	// Explicit disable.
	g.Spec.NodeDrain = &palworldv1alpha1.NodeDrainPolicy{Disabled: true, GracePeriodSeconds: 10}
	if !nodeDrainDisabled(g) {
		t.Errorf("explicit Disabled should disable")
	}
	if drainGrace(g) != 10*time.Second {
		t.Errorf("expected 10s grace, got %s", drainGrace(g))
	}
}
