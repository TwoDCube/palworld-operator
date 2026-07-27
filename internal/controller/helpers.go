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

// adminPassword fetches the admin password from the credentials Secret backing
// the game.
func adminPassword(ctx context.Context, c client.Client, g *palworldv1alpha1.PalworldGame) (string, error) {
	secretName := resources.CredentialsSecretName(g)
	var secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: g.Namespace}, &secret); err != nil {
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
func restClientFor(ctx context.Context, c client.Client, g *palworldv1alpha1.PalworldGame) (*palworld.RESTClient, error) {
	pw, err := adminPassword(ctx, c, g)
	if err != nil {
		return nil, err
	}
	return palworld.NewRESTClient(adminHost(g), resources.RESTPort, pw), nil
}

// rconClientFor builds an RCON client targeting the game's admin service.
func rconClientFor(ctx context.Context, c client.Client, g *palworldv1alpha1.PalworldGame) (*palworld.RCONClient, error) {
	pw, err := adminPassword(ctx, c, g)
	if err != nil {
		return nil, err
	}
	return palworld.NewRCONClient(adminHost(g), resources.RCONPort, pw), nil
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
	return c.Update(ctx, desired)
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
