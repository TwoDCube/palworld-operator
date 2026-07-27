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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
	"github.com/twodcube/palworld-operator/internal/resources"
)

// credsGame builds a game whose credentials Secret is either operator-generated
// (secretName empty) or user-supplied.
func credsGame(secretName string) *palworldv1alpha1.PalworldGame {
	g := &palworldv1alpha1.PalworldGame{
		ObjectMeta: metav1.ObjectMeta{Name: "srv", Namespace: "games"},
	}
	g.Spec.Credentials.SecretName = secretName
	return g
}

func credsSecret(name, ns, password string, labelled bool) *corev1.Secret {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Data:       map[string][]byte{"adminPassword": []byte(password)},
	}
	if labelled {
		s.Labels = map[string]string{resources.LabelManagedBy: resources.ManagedByValue}
	}
	return s
}

func TestCredentialsReaderSelectsUncachedForUserSecret(t *testing.T) {
	s := drainScheme(t)
	cached := fake.NewClientBuilder().WithScheme(s).Build()
	uncached := fake.NewClientBuilder().WithScheme(s).Build()

	if got := credentialsReader(cached, uncached, credsGame("my-creds")); got != client.Reader(uncached) {
		t.Errorf("user-supplied secret must be read through the uncached APIReader")
	}
	if got := credentialsReader(cached, uncached, credsGame("")); got != client.Reader(cached) {
		t.Errorf("operator-generated secret is labelled and must be served from cache")
	}
}

// Callers that never wired an APIReader (unit tests, vanilla unfiltered caches)
// must keep working rather than nil-panic.
func TestCredentialsReaderFallsBackWhenNoAPIReader(t *testing.T) {
	s := drainScheme(t)
	cached := fake.NewClientBuilder().WithScheme(s).Build()

	if got := credentialsReader(cached, nil, credsGame("my-creds")); got != client.Reader(cached) {
		t.Errorf("a nil APIReader must fall back to the cached client")
	}
}

// The regression this guards: with the Secret cache filtered to the operator's
// managed-by label, a user-supplied Secret is absent from the cache. Reading it
// through the cached client returns NotFound even though the Secret exists.
func TestAdminPasswordReadsUserSecretMissingFromFilteredCache(t *testing.T) {
	s := drainScheme(t)
	g := credsGame("my-creds")

	// The user Secret carries no operator label, so a label-filtered cache does
	// not hold it — modelled here by an empty cached client.
	cached := fake.NewClientBuilder().WithScheme(s).Build()
	uncached := fake.NewClientBuilder().WithScheme(s).
		WithObjects(credsSecret("my-creds", "games", "s3cr3t", false)).Build()

	pw, err := adminPassword(context.Background(), cached, uncached, g)
	if err != nil {
		t.Fatalf("adminPassword through the APIReader failed: %v", err)
	}
	if pw != "s3cr3t" {
		t.Errorf("password = %q, want %q", pw, "s3cr3t")
	}

	// Without the APIReader this is exactly the failure the fix addresses.
	if _, err := adminPassword(context.Background(), cached, nil, g); err == nil {
		t.Errorf("expected NotFound when a user Secret is read through the filtered cache")
	}
}

// The operator-generated Secret is labelled, so it stays on the cached path and
// must not require an API round-trip.
func TestAdminPasswordReadsGeneratedSecretFromCache(t *testing.T) {
	s := drainScheme(t)
	g := credsGame("")
	name := resources.GeneratedSecretName(g)

	cached := fake.NewClientBuilder().WithScheme(s).
		WithObjects(credsSecret(name, "games", "generated-pw", true)).Build()
	// Deliberately empty: a cache hit must not fall through to the API server.
	uncached := fake.NewClientBuilder().WithScheme(s).Build()

	pw, err := adminPassword(context.Background(), cached, uncached, g)
	if err != nil {
		t.Fatalf("adminPassword from cache failed: %v", err)
	}
	if pw != "generated-pw" {
		t.Errorf("password = %q, want %q", pw, "generated-pw")
	}
}

func TestAdminPasswordMissingKeyIsReported(t *testing.T) {
	s := drainScheme(t)
	g := credsGame("")
	name := resources.GeneratedSecretName(g)

	secret := credsSecret(name, "games", "", true)
	secret.Data = map[string][]byte{"wrongKey": []byte("x")}
	cached := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()

	_, err := adminPassword(context.Background(), cached, nil, g)
	if err == nil {
		t.Fatalf("expected an error when the admin password key is absent")
	}
	// The operator surfaces which secret and key so the failure is actionable.
	if got := err.Error(); got != "secret srv-credentials missing key adminPassword" {
		t.Errorf("unhelpful error message: %q", got)
	}
}

// svcScheme builds a scheme with corev1 for the Service reconcile tests.
func svcTestGame() *palworldv1alpha1.PalworldGame {
	return &palworldv1alpha1.PalworldGame{
		ObjectMeta: metav1.ObjectMeta{Name: "srv", Namespace: "games", UID: "uid-1"},
	}
}

func lbService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "srv-game",
			Namespace: "games",
			Labels:    map[string]string{resources.LabelManagedBy: resources.ManagedByValue},
		},
		Spec: corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeLoadBalancer,
			Selector:              map[string]string{"app": "srv"},
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			Ports: []corev1.ServicePort{
				{Name: "game", Port: 8211, Protocol: corev1.ProtocolUDP},
			},
		},
	}
}

// countingClient records how many Updates reach the API server.
type countingClient struct {
	client.Client
	updates int
}

func (c *countingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.updates++
	return c.Client.Update(ctx, obj, opts...)
}

// Without the guard, every reconcile re-PUTs all 3-4 Services. That is real API
// traffic and it makes the operator an active writer on Services co-owned by
// another controller (any LoadBalancer), where write amplification bites.
func TestReconcileServiceSkipsNoOpUpdate(t *testing.T) {
	s := drainScheme(t)
	g := svcTestGame()

	live := lbService()
	// Server-assigned fields a LoadBalancer picks up in the cluster.
	live.Spec.ClusterIP = "172.30.1.1"
	live.Spec.ClusterIPs = []string{"172.30.1.1"}
	live.Spec.Ports[0].NodePort = 31990
	live.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "10.2.100.4"}}

	base := fake.NewClientBuilder().WithScheme(s).WithObjects(live).Build()
	c := &countingClient{Client: base}

	// Reconciling the same desired state repeatedly must converge to zero writes.
	for i := 0; i < 3; i++ {
		if err := reconcileService(context.Background(), c, g, s, lbService()); err != nil {
			t.Fatalf("reconcileService: %v", err)
		}
	}
	// The first pass sets the controller reference (a real change); after that
	// nothing may be written.
	if c.updates > 1 {
		t.Errorf("reconcileService issued %d updates for an unchanged Service; "+
			"repeated no-op writes cause a reconcile loop on co-owned Services", c.updates)
	}
}

// The guard must not suppress genuine changes.
func TestReconcileServiceStillAppliesRealChanges(t *testing.T) {
	s := drainScheme(t)
	g := svcTestGame()

	live := lbService()
	live.Spec.ClusterIP = "172.30.1.1"
	base := fake.NewClientBuilder().WithScheme(s).WithObjects(live).Build()
	c := &countingClient{Client: base}

	changed := lbService()
	changed.Spec.Ports = append(changed.Spec.Ports,
		corev1.ServicePort{Name: "query", Port: 27015, Protocol: corev1.ProtocolUDP})

	if err := reconcileService(context.Background(), c, g, s, changed); err != nil {
		t.Fatalf("reconcileService: %v", err)
	}
	if c.updates == 0 {
		t.Fatalf("a changed port list must be written")
	}

	var got corev1.Service
	if err := base.Get(context.Background(),
		client.ObjectKey{Name: "srv-game", Namespace: "games"}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Spec.Ports) != 2 {
		t.Errorf("expected the new port to be persisted, got %d ports", len(got.Spec.Ports))
	}
}

// Server-assigned fields must survive, otherwise every reconcile would look like
// a change (and would strip the allocated NodePort / ClusterIP).
func TestReconcileServicePreservesServerAssignedFields(t *testing.T) {
	s := drainScheme(t)
	g := svcTestGame()

	live := lbService()
	live.Spec.ClusterIP = "172.30.1.1"
	live.Spec.Ports[0].NodePort = 31990
	base := fake.NewClientBuilder().WithScheme(s).WithObjects(live).Build()

	if err := reconcileService(context.Background(), base, g, s, lbService()); err != nil {
		t.Fatalf("reconcileService: %v", err)
	}

	var got corev1.Service
	if err := base.Get(context.Background(),
		client.ObjectKey{Name: "srv-game", Namespace: "games"}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Spec.ClusterIP != "172.30.1.1" {
		t.Errorf("clusterIP not preserved: %q", got.Spec.ClusterIP)
	}
	if got.Spec.Ports[0].NodePort != 31990 {
		t.Errorf("auto-allocated nodePort not preserved: %d", got.Spec.Ports[0].NodePort)
	}
}
