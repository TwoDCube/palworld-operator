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

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
	"github.com/twodcube/palworld-operator/internal/resources"
)

// ensureStatefulSet creates or updates the server StatefulSet, respecting the
// immutability of the selector, service name, volume claim templates and pod
// management policy (only set on creation).
func (r *PalworldGameReconciler) ensureStatefulSet(ctx context.Context, game *palworldv1alpha1.PalworldGame) error {
	settingsHash, err := resources.SettingsHash(game)
	if err != nil {
		return err
	}
	desired := resources.DesiredStatefulSet(game, r.buildParams(), settingsHash)

	sts := &appsv1.StatefulSet{}
	sts.Name = desired.Name
	sts.Namespace = desired.Namespace
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, sts, func() error {
		sts.Labels = desired.Labels
		if sts.CreationTimestamp.IsZero() {
			// On creation set the whole spec (including immutable fields).
			sts.Spec = desired.Spec
		} else {
			// On update only mutate the fields the API server permits.
			sts.Spec.Replicas = desired.Spec.Replicas
			sts.Spec.Template = desired.Spec.Template
			sts.Spec.UpdateStrategy = desired.Spec.UpdateStrategy
			sts.Spec.MinReadySeconds = desired.Spec.MinReadySeconds
		}
		return controllerutil.SetControllerReference(game, sts, r.Scheme)
	})
	return err
}
