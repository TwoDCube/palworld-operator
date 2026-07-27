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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

// setStatusCondition is a generic condition setter for the Backup/Restore
// statuses, which carry a plain []metav1.Condition.
func setStatusCondition(conditions *[]metav1.Condition, condType string, status metav1.ConditionStatus, reason, message string, generation int64) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
}

func (r *PalworldGameReconciler) setCondition(game *palworldv1alpha1.PalworldGame, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&game.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: game.Generation,
	})
}

func (r *PalworldGameReconciler) setReady(game *palworldv1alpha1.PalworldGame, ready bool, reason, message string) {
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	r.setCondition(game, palworldv1alpha1.ConditionReady, status, reason, message)
}

func (r *PalworldGameReconciler) setProgressing(game *palworldv1alpha1.PalworldGame, reason, message string) {
	r.setCondition(game, palworldv1alpha1.ConditionProgressing, metav1.ConditionTrue, reason, message)
}

func (r *PalworldGameReconciler) clearProgressing(game *palworldv1alpha1.PalworldGame, reason, message string) {
	r.setCondition(game, palworldv1alpha1.ConditionProgressing, metav1.ConditionFalse, reason, message)
}

func (r *PalworldGameReconciler) setDegraded(game *palworldv1alpha1.PalworldGame, degraded bool, reason, message string) {
	status := metav1.ConditionFalse
	if degraded {
		status = metav1.ConditionTrue
	}
	r.setCondition(game, palworldv1alpha1.ConditionDegraded, status, reason, message)
}
