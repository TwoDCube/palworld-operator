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
