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

package v1alpha1

import (
	"context"
	"fmt"
	"strings"

	"github.com/robfig/cron/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var palworldgamelog = logf.Log.WithName("palworldgame-webhook")

// SetupWebhookWithManager registers the validating webhook for PalworldGame.
func (g *PalworldGame) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(g).
		WithValidator(&PalworldGameValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-palworld-twodcube-io-v1alpha1-palworldgame,mutating=false,failurePolicy=fail,sideEffects=None,groups=palworld.twodcube.io,resources=palworldgames,verbs=create;update,versions=v1alpha1,name=vpalworldgame.kb.io,admissionReviewVersions=v1

// PalworldGameValidator validates PalworldGame resources.
type PalworldGameValidator struct{}

var _ webhook.CustomValidator = &PalworldGameValidator{}

func (v *PalworldGameValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	game, ok := obj.(*PalworldGame)
	if !ok {
		return nil, fmt.Errorf("expected a PalworldGame, got %T", obj)
	}
	palworldgamelog.V(1).Info("validate create", "name", game.Name)
	return v.validate(game)
}

func (v *PalworldGameValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	game, ok := newObj.(*PalworldGame)
	if !ok {
		return nil, fmt.Errorf("expected a PalworldGame, got %T", newObj)
	}
	palworldgamelog.V(1).Info("validate update", "name", game.Name)
	return v.validate(game)
}

func (v *PalworldGameValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *PalworldGameValidator) validate(game *PalworldGame) (admission.Warnings, error) {
	var errs field.ErrorList
	var warnings admission.Warnings
	specPath := field.NewPath("spec")

	// String settings must not contain characters that break the single-line
	// PalWorldSettings.ini tuple (embedded quotes/newlines corrupt the whole
	// line, and the server then silently reverts to defaults).
	settingsPath := specPath.Child("serverSettings")
	stringFields := map[string]string{
		"serverName":        game.Spec.ServerSettings.ServerName,
		"serverDescription": game.Spec.ServerSettings.ServerDescription,
		"region":            game.Spec.ServerSettings.Region,
		"banListURL":        game.Spec.ServerSettings.BanListURL,
		"randomizerSeed":    game.Spec.ServerSettings.RandomizerSeed,
	}
	for name, val := range stringFields {
		if strings.ContainsAny(val, "\"\n\r") {
			errs = append(errs, field.Invalid(settingsPath.Child(name), val,
				"must not contain double quotes or newlines"))
		}
	}

	// Backup policy validation.
	if bp := game.Spec.Backup; bp != nil && bp.Enabled {
		bpPath := specPath.Child("backup")
		if bp.Schedule != "" {
			if _, err := cron.ParseStandard(bp.Schedule); err != nil {
				errs = append(errs, field.Invalid(bpPath.Child("schedule"), bp.Schedule, "invalid cron expression: "+err.Error()))
			}
		} else {
			errs = append(errs, field.Required(bpPath.Child("schedule"), "schedule is required when backups are enabled"))
		}
		errs = append(errs, validateDestination(bpPath.Child("destination"), bp.Destination)...)
	}

	// Update policy validation.
	if up := game.Spec.Update; up != nil {
		upPath := specPath.Child("update")
		if up.Strategy == UpdateScheduled {
			if up.Schedule == "" {
				errs = append(errs, field.Required(upPath.Child("schedule"), "schedule is required for the Scheduled update strategy"))
			} else if _, err := cron.ParseStandard(up.Schedule); err != nil {
				errs = append(errs, field.Invalid(upPath.Child("schedule"), up.Schedule, "invalid cron expression: "+err.Error()))
			}
		}
	}

	// The player countdown runs inside the preStop hook, and the kubelet's grace
	// clock covers preStop. An explicit grace period that cannot fit the countdown
	// is honoured (the operator does not silently rewrite it) but the countdown
	// gets clamped in the container, so say so rather than let players be cut off
	// mid-warning. Only warn: rejecting would break objects that already carry the
	// 120s value the CRD used to default.
	if grace := game.Spec.TerminationGracePeriodSeconds; grace != nil {
		warn := int64(game.ShutdownWarnSeconds())
		if minimum := warn + ShutdownReserveSeconds; warn > 0 && *grace < minimum {
			warnings = append(warnings, fmt.Sprintf(
				"spec.terminationGracePeriodSeconds=%d cannot fit the %ds shutdown warning: "+
					"players will only be warned for ~%ds. Set it to at least %d, or unset it to let the operator derive %d",
				*grace, warn, max(*grace-ShutdownReserveSeconds, 0), minimum, warn+ShutdownGraceHeadroomSeconds))
		}
	}

	// Networking sanity checks.
	if game.Spec.Networking.RESTAPI.Route {
		warnings = append(warnings, "spec.networking.restAPI.route only takes effect on clusters with the OpenShift Route API")
		warnings = append(warnings, "enabling the REST API Route publishes the password-protected admin API on the external router")
		if tls := game.Spec.Networking.RESTAPI.TLS; tls == "reencrypt" || tls == "passthrough" {
			errs = append(errs, field.Invalid(
				specPath.Child("networking", "restAPI", "tls"), tls,
				"the REST API is served as plain HTTP; only 'edge' termination is supported"))
		}
	}
	if game.Spec.Networking.ServiceType == ServiceTypeLoadBalancer {
		warnings = append(warnings, "spec.networking.serviceType=LoadBalancer requires a UDP-capable load balancer (e.g. MetalLB on-prem)")
	}

	if len(errs) == 0 {
		return warnings, nil
	}
	return warnings, apierrors.NewInvalid(
		schema.GroupKind{Group: GroupVersion.Group, Kind: "PalworldGame"},
		game.Name, errs)
}

func validateDestination(path *field.Path, dest BackupDestination) field.ErrorList {
	var errs field.ErrorList
	switch dest.Type {
	case BackupDestinationS3:
		if dest.S3 == nil {
			errs = append(errs, field.Required(path.Child("s3"), "s3 configuration is required for the S3 destination"))
			break
		}
		if dest.S3.Bucket == "" {
			errs = append(errs, field.Required(path.Child("s3", "bucket"), "bucket is required"))
		}
		if dest.S3.CredentialsSecret == "" {
			errs = append(errs, field.Required(path.Child("s3", "credentialsSecret"), "credentialsSecret is required"))
		}
	case BackupDestinationPVC:
		if dest.PVCName == "" {
			errs = append(errs, field.Required(path.Child("pvcName"), "pvcName is required for the PVC destination"))
		}
	}
	return errs
}
