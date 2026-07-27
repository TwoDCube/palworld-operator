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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
	"github.com/twodcube/palworld-operator/internal/palworld"
	"github.com/twodcube/palworld-operator/internal/resources"
)

// hasAPI reports whether the cluster serves the given GVK (used to gracefully
// degrade on clusters lacking OpenShift Routes or the Prometheus Operator).
func hasAPI(ctx context.Context, c client.Client, gvk schema.GroupVersionKind) bool {
	mapper := c.RESTMapper()
	_, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	return err == nil
}

// credentialsReader picks the reader to use for a game's credentials Secret.
//
// The manager's Secret cache is filtered to objects labelled
// app.kubernetes.io/managed-by=palworld-operator (see operatorCacheOptions in
// cmd/main.go). The operator-generated Secret carries that label and is served
// from cache; a user-supplied Secret (spec.credentials.secretName) does not, and
// reading it through the cached client returns NotFound even though it exists.
// Those reads go straight to the API server instead.
//
// uncached may be nil (unit tests, and any caller that has not wired an
// APIReader); the cached client is then used for both cases, which is correct
// whenever the cache is unfiltered.
func credentialsReader(cached client.Client, uncached client.Reader, g *palworldv1alpha1.PalworldGame) client.Reader {
	if g.Spec.Credentials.SecretName != "" && uncached != nil {
		return uncached
	}
	return cached
}

// adminPassword fetches the admin password from the credentials Secret backing
// the game. uncached is the manager's APIReader, used for user-supplied Secrets
// that the filtered cache cannot see; see credentialsReader.
func adminPassword(ctx context.Context, c client.Client, uncached client.Reader, g *palworldv1alpha1.PalworldGame) (string, error) {
	secretName := resources.CredentialsSecretName(g)
	var secret corev1.Secret
	reader := credentialsReader(c, uncached, g)
	if err := reader.Get(ctx, types.NamespacedName{Name: secretName, Namespace: g.Namespace}, &secret); err != nil {
		return "", err
	}
	key := resources.AdminPasswordKey(g)
	val, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("secret %s missing key %s", secretName, key)
	}
	return string(val), nil
}

// adminHost is the in-cluster DNS name of the admin (RCON/REST) service.
func adminHost(g *palworldv1alpha1.PalworldGame) string {
	return fmt.Sprintf("%s.%s.svc", resources.AdminServiceName(g), g.Namespace)
}

// restClientFor builds a REST client targeting the game's admin service.
// uncached is the manager's APIReader; see adminPassword.
func restClientFor(ctx context.Context, c client.Client, uncached client.Reader, g *palworldv1alpha1.PalworldGame) (*palworld.RESTClient, error) {
	pw, err := adminPassword(ctx, c, uncached, g)
	if err != nil {
		return nil, err
	}
	return palworld.NewRESTClient(adminHost(g), resources.RESTPort, pw), nil
}

// reconcileService creates or updates a Service, preserving server-assigned
// fields (ClusterIP and auto-allocated NodePorts) across updates.
func reconcileService(ctx context.Context, c client.Client, owner *palworldv1alpha1.PalworldGame, scheme *runtime.Scheme, desired *corev1.Service) error {
	existing := &corev1.Service{}
	err := c.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if apierrors.IsNotFound(err) {
		if err := controllerutil.SetControllerReference(owner, desired, scheme); err != nil {
			return err
		}
		return c.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// Preserve immutable / server-assigned fields.
	desired.Spec.ClusterIP = existing.Spec.ClusterIP
	desired.Spec.ClusterIPs = existing.Spec.ClusterIPs
	// Preserve auto-allocated NodePorts (match by port name) when not pinned.
	for i := range desired.Spec.Ports {
		if desired.Spec.Ports[i].NodePort != 0 {
			continue
		}
		for _, ep := range existing.Spec.Ports {
			if ep.Name == desired.Spec.Ports[i].Name {
				desired.Spec.Ports[i].NodePort = ep.NodePort
			}
		}
	}
	// Snapshot before mutating so an unchanged Service can skip the write.
	before := existing.DeepCopy()

	existing.Labels = desired.Labels
	existing.Annotations = desired.Annotations
	existing.Spec.Type = desired.Spec.Type
	existing.Spec.Selector = desired.Spec.Selector
	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.PublishNotReadyAddresses = desired.Spec.PublishNotReadyAddresses
	existing.Spec.ExternalTrafficPolicy = desired.Spec.ExternalTrafficPolicy
	existing.Spec.LoadBalancerIP = desired.Spec.LoadBalancerIP
	existing.Spec.LoadBalancerClass = desired.Spec.LoadBalancerClass
	if err := controllerutil.SetControllerReference(owner, existing, scheme); err != nil {
		return err
	}

	// Skip the write when nothing this operator manages actually changed.
	// Without this, every reconcile re-PUTs all 3-4 Services. Those writes are
	// usually no-ops server-side, but they are real API traffic and they make the
	// operator an active writer on a Service that another controller co-owns
	// (any LoadBalancer), which is exactly where write amplification bites.
	// With the guard the operator's managedFields entry on a steady-state Service
	// stops advancing at all.
	if equality.Semantic.DeepEqual(before, existing) {
		return nil
	}
	return c.Update(ctx, existing)
}

// reconcileUnstructured applies an unstructured object (used for ServiceMonitor)
// via a get-then-create/update, setting the owner reference.
func reconcileUnstructured(ctx context.Context, c client.Client, owner *palworldv1alpha1.PalworldGame, scheme *runtime.Scheme, desired *unstructured.Unstructured) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(desired.GroupVersionKind())
	err := c.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if apierrors.IsNotFound(err) {
		if err := controllerutil.SetControllerReference(owner, desired, scheme); err != nil {
			return err
		}
		return c.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	desired.SetResourceVersion(existing.GetResourceVersion())
	if err := controllerutil.SetControllerReference(owner, desired, scheme); err != nil {
		return err
	}
	// Same no-op guard as reconcileService: Routes and ServiceMonitors are also
	// written by other controllers, so an unconditional Update here would start
	// the same write/watch/reconcile loop. Only the fields this operator manages
	// are compared — the live object additionally carries controller-written
	// status and defaults that `desired` never sets.
	if unstructuredManagedEqual(existing, desired) {
		return nil
	}
	return c.Update(ctx, desired)
}

// unstructuredManagedEqual reports whether the fields this operator manages
// (labels, annotations, ownerReferences and spec) already match the live object.
// status and server-set metadata are ignored: they are owned by other actors and
// comparing them would make every reconcile look like a change.
func unstructuredManagedEqual(existing, desired *unstructured.Unstructured) bool {
	if !equality.Semantic.DeepEqual(existing.GetLabels(), desired.GetLabels()) ||
		!equality.Semantic.DeepEqual(existing.GetAnnotations(), desired.GetAnnotations()) ||
		!equality.Semantic.DeepEqual(existing.GetOwnerReferences(), desired.GetOwnerReferences()) {
		return false
	}
	return equality.Semantic.DeepEqual(existing.Object["spec"], desired.Object["spec"])
}

// containsString / removeString are small finalizer helpers.
func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func removeString(s []string, v string) []string {
	out := s[:0]
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
