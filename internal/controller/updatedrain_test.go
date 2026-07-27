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
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

// drainReconciler builds a reconciler over a fake client. No credentials Secret
// exists, so restClientFor fails and the best-effort announce is skipped -- the
// drain's control flow is what these tests exercise, and it must not depend on a
// reachable game server.
func drainReconciler(t *testing.T) *PalworldGameReconciler {
	t.Helper()
	s := drainScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).
		WithStatusSubresource(&palworldv1alpha1.PalworldGame{}).Build()
	return &PalworldGameReconciler{
		Client:    c,
		APIReader: c,
		Scheme:    s,
		Recorder:  record.NewFakeRecorder(64),
	}
}

// drainGame returns a game mid-update with the given policy and player count.
func drainGame(timeout int32, players int32) *palworldv1alpha1.PalworldGame {
	g := &palworldv1alpha1.PalworldGame{
		ObjectMeta: metav1.ObjectMeta{Name: "srv", Namespace: "games"},
	}
	g.Spec.Update = &palworldv1alpha1.UpdatePolicy{
		DrainTimeoutSeconds: timeout,
		WarnMessage:         "restart in %d seconds",
		WarnIntervalSeconds: 60,
	}
	g.Status.PlayersOnline = players
	return g
}

// drainTimeoutSeconds: 0 opts out of the wait entirely.
func TestDrainDisabledProceedsImmediately(t *testing.T) {
	r := drainReconciler(t)
	g := drainGame(0, 3)

	done, _ := r.drainUpdate(context.Background(), g)
	if !done {
		t.Error("expected the update to proceed immediately with drainTimeoutSeconds=0")
	}
	if g.Status.UpdateDrainStartTime != nil {
		t.Error("no drain should have been started")
	}
}

// An empty server must not stall the rollout for the full timeout.
func TestDrainSkippedWhenNoPlayersOnline(t *testing.T) {
	r := drainReconciler(t)
	g := drainGame(300, 0)

	done, _ := r.drainUpdate(context.Background(), g)
	if !done {
		t.Error("expected the update to proceed with no players online")
	}
	if g.Status.UpdateDrainStartTime != nil {
		t.Error("no drain should have been started for an empty server")
	}
}

// With players connected the first pass must start the clock and requeue, not
// restart -- this is the wait that gives players a chance to log off.
func TestDrainStartsAndWaitsWhilePlayersConnected(t *testing.T) {
	r := drainReconciler(t)
	g := drainGame(300, 2)

	done, res := r.drainUpdate(context.Background(), g)
	if done {
		t.Fatal("expected the drain to hold the update back while players are connected")
	}
	if g.Status.UpdateDrainStartTime == nil {
		t.Error("expected updateDrainStartTime to be recorded so the deadline survives reconciles")
	}
	if g.Status.UpdateDrainLastWarnTime == nil {
		t.Error("expected updateDrainLastWarnTime to be recorded")
	}
	if res.RequeueAfter != drainPollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, drainPollInterval)
	}
}

// The drain ends as soon as the last player leaves, rather than waiting out the
// remaining timeout.
func TestDrainCompletesEarlyWhenPlayersLeave(t *testing.T) {
	r := drainReconciler(t)
	g := drainGame(300, 0)
	started := metav1.NewTime(time.Now().Add(-30 * time.Second))
	g.Status.UpdateDrainStartTime = &started

	done, _ := r.drainUpdate(context.Background(), g)
	if !done {
		t.Error("expected the drain to finish once all players disconnected")
	}
}

// Players who ignore the warning must not block the update forever.
func TestDrainCompletesOnTimeout(t *testing.T) {
	r := drainReconciler(t)
	g := drainGame(300, 2)
	started := metav1.NewTime(time.Now().Add(-301 * time.Second))
	g.Status.UpdateDrainStartTime = &started

	done, _ := r.drainUpdate(context.Background(), g)
	if !done {
		t.Error("expected the drain to give up once the timeout elapsed")
	}
}

// The controller is reconciled far more often than drainPollInterval, so a
// re-broadcast must be gated on elapsed time, not on reconcile entry.
func TestDrainRebroadcastIsRateLimited(t *testing.T) {
	r := drainReconciler(t)
	g := drainGame(300, 2)
	started := metav1.NewTime(time.Now().Add(-100 * time.Second))
	g.Status.UpdateDrainStartTime = &started
	recent := metav1.NewTime(time.Now().Add(-5 * time.Second)) // well inside the 60s interval
	g.Status.UpdateDrainLastWarnTime = &recent

	if done, _ := r.drainUpdate(context.Background(), g); done {
		t.Fatal("drain should still be running")
	}
	if !g.Status.UpdateDrainLastWarnTime.Time.Equal(recent.Time) {
		t.Error("expected no re-broadcast within warnIntervalSeconds")
	}
}

func TestDrainRebroadcastsAfterInterval(t *testing.T) {
	r := drainReconciler(t)
	g := drainGame(300, 2)
	started := metav1.NewTime(time.Now().Add(-100 * time.Second))
	g.Status.UpdateDrainStartTime = &started
	stale := metav1.NewTime(time.Now().Add(-90 * time.Second)) // past the 60s interval
	g.Status.UpdateDrainLastWarnTime = &stale

	if done, _ := r.drainUpdate(context.Background(), g); done {
		t.Fatal("drain should still be running")
	}
	if g.Status.UpdateDrainLastWarnTime.Time.Equal(stale.Time) {
		t.Error("expected a re-broadcast once warnIntervalSeconds elapsed")
	}
}

// Progressing must report the drain so an operator watching the resource can see
// why the update has not restarted yet.
func TestDrainReportsProgress(t *testing.T) {
	r := drainReconciler(t)
	g := drainGame(300, 2)

	r.drainUpdate(context.Background(), g)

	var found bool
	for _, c := range g.Status.Conditions {
		if c.Type == palworldv1alpha1.ConditionProgressing && c.Reason == "Draining" {
			found = true
			if !strings.Contains(c.Message, "2 player") {
				t.Errorf("Progressing message %q should name the player count", c.Message)
			}
		}
	}
	if !found {
		t.Error("expected a Progressing=Draining condition during the drain")
	}
}

// A pending update that goes away mid-drain must not leave a stale deadline that
// would make the next update think a drain was already under way.
func TestClearDrainStateResetsBookkeeping(t *testing.T) {
	r := drainReconciler(t)
	g := drainGame(300, 2)
	now := metav1.Now()
	g.Status.UpdateDrainStartTime = &now
	g.Status.UpdateDrainLastWarnTime = &now

	r.clearDrainState(g)

	if g.Status.UpdateDrainStartTime != nil || g.Status.UpdateDrainLastWarnTime != nil {
		t.Error("expected drain bookkeeping to be cleared")
	}
}

func TestRenderUpdateWarning(t *testing.T) {
	cases := []struct {
		name      string
		template  string
		remaining int
		want      string
	}{
		{"substitutes remaining seconds", "restart in %d seconds", 120, "restart in 120 seconds"},
		{"empty falls back to a default", "", 60, "Server will restart for updates shortly"},
		{"message without a placeholder is unchanged", "maintenance now", 60, "maintenance now"},
		{"repeated placeholders all substituted", "%d... %d!", 5, "5... 5!"},
		// fmt.Sprintf would render this as "%!s(MISSING)" in players' chat.
		{"stray verb is left alone", "restart in %d s (%s)", 30, "restart in 30 s (%s)"},
		{"bare percent is left alone", "50%% off, restart in %d", 10, "50%% off, restart in 10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderUpdateWarning(tc.template, tc.remaining); got != tc.want {
				t.Errorf("renderUpdateWarning(%q, %d) = %q, want %q", tc.template, tc.remaining, got, tc.want)
			}
		})
	}
}

// Objects persisted before warnIntervalSeconds existed carry 0 and must fall back
// to the default rather than re-broadcasting on every poll.
func TestUpdateWarnIntervalSecondsDefaults(t *testing.T) {
	if got := updateWarnIntervalSeconds(&palworldv1alpha1.UpdatePolicy{}); got != palworldv1alpha1.DefaultUpdateWarnIntervalSeconds {
		t.Errorf("updateWarnIntervalSeconds(zero) = %d, want %d", got, palworldv1alpha1.DefaultUpdateWarnIntervalSeconds)
	}
	if got := updateWarnIntervalSeconds(&palworldv1alpha1.UpdatePolicy{WarnIntervalSeconds: 15}); got != 15 {
		t.Errorf("updateWarnIntervalSeconds(15) = %d, want 15", got)
	}
}
