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

package main

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/twodcube/palworld-operator/internal/resources"
)

// wantSelector is the label selector every filtered type must carry. It is
// asserted as a string so a change to either the label key or the value in
// internal/resources is caught here rather than at runtime, where the symptom
// would be a silently empty cache.
const wantSelector = "app.kubernetes.io/managed-by=palworld-operator"

func TestOperatorCacheSelectorMatchesCommonLabels(t *testing.T) {
	// Guard the constants the selector is built from against drift.
	if resources.LabelManagedBy+"="+resources.ManagedByValue != wantSelector {
		t.Fatalf("managed-by constants changed: %s=%s, want %s",
			resources.LabelManagedBy, resources.ManagedByValue, wantSelector)
	}
}

// filteredTypes indexes cache.Options.ByObject by concrete Go type.
//
// ByObject is keyed by client.Object interface values, so a freshly constructed
// &corev1.Secret{} is a *different* map key than the one the options were built
// with — direct lookups always miss. controller-runtime resolves the keys via
// the scheme rather than by pointer identity, so the map is correct at runtime;
// the test just has to index it the same way.
func filteredTypes(opts cache.Options) map[reflect.Type]cache.ByObject {
	byType := make(map[reflect.Type]cache.ByObject, len(opts.ByObject))
	for obj, byObj := range opts.ByObject {
		byType[reflect.TypeOf(obj)] = byObj
	}
	return byType
}

// Every type the operator only ever reads its own objects back from must be
// label-filtered; an unfiltered Secret/ConfigMap cache is what OOMKilled the
// manager on a real cluster.
func TestOperatorCacheFiltersOwnedTypes(t *testing.T) {
	byType := filteredTypes(operatorCacheOptions())

	wantFiltered := []client.Object{
		&corev1.Secret{},
		&corev1.ConfigMap{},
		&corev1.Service{},
		&corev1.ServiceAccount{},
		&corev1.PersistentVolumeClaim{},
		&corev1.Pod{},
		&appsv1.StatefulSet{},
		&batchv1.Job{},
		&policyv1.PodDisruptionBudget{},
		&networkingv1.NetworkPolicy{},
	}

	for _, obj := range wantFiltered {
		byObj, ok := byType[reflect.TypeOf(obj)]
		if !ok {
			t.Errorf("%T is not label-filtered in the cache", obj)
			continue
		}
		if byObj.Label == nil {
			t.Errorf("%T has a cache entry but no label selector", obj)
			continue
		}
		if got := byObj.Label.String(); got != wantSelector {
			t.Errorf("%T selector = %q, want %q", obj, got, wantSelector)
		}
	}

	if len(byType) != len(wantFiltered) {
		t.Errorf("cache filters %d types, expected exactly %d — a new entry needs a "+
			"deliberate decision about whether the operator labels that type",
			len(byType), len(wantFiltered))
	}
}

// Node must stay unfiltered: drain detection looks up arbitrary nodes by name
// and nodes carry no operator label, so filtering them would make every lookup
// return NotFound and silently disable drain handling.
func TestOperatorCacheDoesNotFilterNodes(t *testing.T) {
	byType := filteredTypes(operatorCacheOptions())
	if _, filtered := byType[reflect.TypeOf(&corev1.Node{})]; filtered {
		t.Errorf("Node must not be label-filtered; drain detection reads unlabelled nodes")
	}
}

// Regression guard. Stripping managedFields from the cache is unsafe here
// because reconcileService and reconcileUnstructured read an object from the
// cached client, mutate it, and Update it back: the write would carry empty
// managedFields and force the API server to recompute field ownership on a write
// the operator did not intend to make.
func TestOperatorCacheDoesNotStripManagedFields(t *testing.T) {
	opts := operatorCacheOptions()
	if opts.DefaultTransform != nil {
		t.Errorf("DefaultTransform must stay nil: the controllers read-modify-write " +
			"cached objects, so a stripped copy would be written back")
	}
	for obj, byObj := range opts.ByObject {
		if byObj.Transform != nil {
			t.Errorf("%T has a per-object Transform; same write-loop hazard applies", obj)
		}
	}
}
