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
	"strings"
	"testing"
)

func game() *PalworldGame {
	return &PalworldGame{}
}

func validate(t *testing.T, g *PalworldGame) error {
	t.Helper()
	v := &PalworldGameValidator{}
	_, err := v.ValidateCreate(context.Background(), g)
	return err
}

func TestValidateValid(t *testing.T) {
	g := game()
	g.Spec.ServerSettings.ServerName = "A friendly server"
	if err := validate(t, g); err != nil {
		t.Errorf("expected valid game, got error: %v", err)
	}
}

func TestValidateQuoteInName(t *testing.T) {
	g := game()
	g.Spec.ServerSettings.ServerName = `bad"name`
	if err := validate(t, g); err == nil {
		t.Errorf("expected error for quote in serverName")
	}
}

func TestValidateBackupRequiresSchedule(t *testing.T) {
	g := game()
	g.Spec.Backup = &BackupPolicy{Enabled: true}
	if err := validate(t, g); err == nil {
		t.Errorf("expected error for enabled backup without schedule")
	}
}

func TestValidateBackupBadCron(t *testing.T) {
	g := game()
	g.Spec.Backup = &BackupPolicy{Enabled: true, Schedule: "not a cron"}
	if err := validate(t, g); err == nil {
		t.Errorf("expected error for invalid cron schedule")
	}
}

func TestValidateS3RequiresConfig(t *testing.T) {
	g := game()
	g.Spec.Backup = &BackupPolicy{
		Enabled:     true,
		Schedule:    "0 * * * *",
		Destination: BackupDestination{Type: BackupDestinationS3},
	}
	if err := validate(t, g); err == nil {
		t.Errorf("expected error for S3 destination without s3 config")
	}
}

func TestValidateS3Valid(t *testing.T) {
	g := game()
	g.Spec.Backup = &BackupPolicy{
		Enabled:  true,
		Schedule: "0 * * * *",
		Destination: BackupDestination{
			Type: BackupDestinationS3,
			S3: &S3Destination{
				Bucket:            "b",
				CredentialsSecret: "s",
			},
		},
	}
	if err := validate(t, g); err != nil {
		t.Errorf("expected valid S3 backup, got: %v", err)
	}
}

func TestValidateScheduledUpdateRequiresSchedule(t *testing.T) {
	g := game()
	g.Spec.Update = &UpdatePolicy{Strategy: UpdateScheduled}
	if err := validate(t, g); err == nil {
		t.Errorf("expected error for Scheduled update without schedule")
	}
}

func TestValidateRouteReencryptRejected(t *testing.T) {
	g := game()
	g.Spec.Networking.RESTAPI.Route = true
	g.Spec.Networking.RESTAPI.TLS = "reencrypt"
	if err := validate(t, g); err == nil {
		t.Errorf("expected error: reencrypt is invalid for the HTTP REST backend")
	}
}

func TestValidateRouteEdgeAllowed(t *testing.T) {
	g := game()
	g.Spec.Networking.RESTAPI.Route = true
	g.Spec.Networking.RESTAPI.TLS = "edge"
	if err := validate(t, g); err != nil {
		t.Errorf("edge termination should be valid, got: %v", err)
	}
}

func TestValidateLoadBalancerWarns(t *testing.T) {
	g := game()
	g.Spec.Networking.ServiceType = ServiceTypeLoadBalancer
	v := &PalworldGameValidator{}
	warnings, err := v.ValidateCreate(context.Background(), g)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Errorf("expected a warning for LoadBalancer service type")
	}
}

// validateWarnings returns the admission warnings for a game.
func validateWarnings(t *testing.T, g *PalworldGame) []string {
	t.Helper()
	v := &PalworldGameValidator{}
	w, err := v.ValidateCreate(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	return w
}

func hasWarningContaining(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

// An explicit grace period shorter than the countdown silently truncates the
// player warning, which is exactly the failure this policy exists to prevent.
func TestValidateShortGracePeriodWarns(t *testing.T) {
	g := game()
	short := int64(120) // the value the CRD used to default to
	g.Spec.TerminationGracePeriodSeconds = &short

	warnings := validateWarnings(t, g)
	if !hasWarningContaining(warnings, "cannot fit the 300s shutdown warning") {
		t.Errorf("expected a warning about the too-short grace period, got %v", warnings)
	}
	// The warning must tell the operator what to set instead.
	if !hasWarningContaining(warnings, "330") {
		t.Errorf("expected the warning to name the minimum (330), got %v", warnings)
	}
}

// A grace period with room for the countdown must not nag.
func TestValidateSufficientGracePeriodDoesNotWarn(t *testing.T) {
	g := game()
	ample := int64(600)
	g.Spec.TerminationGracePeriodSeconds = &ample
	if w := validateWarnings(t, g); hasWarningContaining(w, "shutdown warning") {
		t.Errorf("unexpected shutdown warning for a 600s grace period: %v", w)
	}
}

// The common case: unset means derived, so there is nothing to warn about.
func TestValidateUnsetGracePeriodDoesNotWarn(t *testing.T) {
	if w := validateWarnings(t, game()); hasWarningContaining(w, "shutdown warning") {
		t.Errorf("unexpected shutdown warning when the grace period is unset: %v", w)
	}
}

// warnSeconds: 0 opts out of the countdown, so any grace period is fine.
func TestValidateZeroWarnSecondsDoesNotWarn(t *testing.T) {
	g := game()
	short := int64(30)
	g.Spec.TerminationGracePeriodSeconds = &short
	g.Spec.Shutdown = &ShutdownPolicy{WarnSeconds: 0}
	if w := validateWarnings(t, g); hasWarningContaining(w, "shutdown warning") {
		t.Errorf("unexpected shutdown warning with warnSeconds=0: %v", w)
	}
}

func TestShutdownAccessorDefaults(t *testing.T) {
	g := game() // spec.shutdown omitted, as on objects created before the field existed
	if got := g.ShutdownWarnSeconds(); got != DefaultShutdownWarnSeconds {
		t.Errorf("ShutdownWarnSeconds() = %d, want %d", got, DefaultShutdownWarnSeconds)
	}
	if got := g.ShutdownWarnIntervalSeconds(); got != DefaultShutdownWarnIntervalSeconds {
		t.Errorf("ShutdownWarnIntervalSeconds() = %d, want %d", got, DefaultShutdownWarnIntervalSeconds)
	}
	if got := g.ShutdownWarnMessage(); got != DefaultShutdownWarnMessage {
		t.Errorf("ShutdownWarnMessage() = %q, want %q", got, DefaultShutdownWarnMessage)
	}
	if got, want := g.EffectiveTerminationGracePeriodSeconds(),
		int64(DefaultShutdownWarnSeconds)+ShutdownGraceHeadroomSeconds; got != want {
		t.Errorf("EffectiveTerminationGracePeriodSeconds() = %d, want %d", got, want)
	}
}

// A configured 0 must be honoured (stop immediately), not treated as "unset".
func TestShutdownAccessorHonoursExplicitZeroWarn(t *testing.T) {
	g := game()
	g.Spec.Shutdown = &ShutdownPolicy{WarnSeconds: 0}
	if got := g.ShutdownWarnSeconds(); got != 0 {
		t.Errorf("ShutdownWarnSeconds() = %d, want 0", got)
	}
	if got, want := g.EffectiveTerminationGracePeriodSeconds(), ShutdownGraceHeadroomSeconds; got != want {
		t.Errorf("EffectiveTerminationGracePeriodSeconds() = %d, want %d", got, want)
	}
}

// A 0 interval would spin the announce loop without advancing the countdown.
func TestShutdownAccessorRejectsZeroInterval(t *testing.T) {
	g := game()
	g.Spec.Shutdown = &ShutdownPolicy{WarnIntervalSeconds: 0}
	if got := g.ShutdownWarnIntervalSeconds(); got != DefaultShutdownWarnIntervalSeconds {
		t.Errorf("ShutdownWarnIntervalSeconds() = %d, want %d", got, DefaultShutdownWarnIntervalSeconds)
	}
}
