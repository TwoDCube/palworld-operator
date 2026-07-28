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

package scripts

import (
	"testing"
	"time"
)

// The countdown schedule tightens as the deadline approaches: the coarse
// interval, plus a fixed 30s warning, plus one warning per second over the final
// 10s. A minute-granularity countdown leaves a player with no signal at all
// through the last 59 seconds, which is exactly when they need to reach a safe
// spot and log out.
//
// These run in real time against the real script, like the rest of this package,
// so they are deliberately sized to the smallest window that still exercises the
// behaviour.

func assertAnnounces(t *testing.T, got, want []string, out string) {
	t.Helper()
	// The stop sequence adds one final announce after the countdown.
	if len(got) != len(want)+1 {
		t.Fatalf("expected %d countdown warnings + 1 final announce, got %d: %v\n%s",
			len(want), len(got), got, out)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("warning %d: want %q, got %q", i, w, got[i])
		}
	}
}

// The headline case: a 31s countdown must hit the 30s mark and then every second
// from 10 down, rather than jumping from 31 straight to the end.
func TestShutdownAnnouncesThirtySecondsThenEverySecondOverFinalTen(t *testing.T) {
	stub := newRESTStub(t, 1)
	start := time.Now()
	out := stub.run(t, map[string]string{
		"SHUTDOWN_WARN_SECONDS": "31",
		// Coarse interval far larger than the window: every announce below must
		// come from the fixed 30s point and the final-ten tail.
		"SHUTDOWN_WARN_INTERVAL_SECONDS": "60",
		"SHUTDOWN_WARN_MESSAGE":          "down in %d",
	})
	elapsed := time.Since(start)

	_, announced := stub.snapshot()
	assertAnnounces(t, announced, []string{
		"down in 31", "down in 30",
		"down in 10", "down in 9", "down in 8", "down in 7", "down in 6",
		"down in 5", "down in 4", "down in 3", "down in 2", "down in 1",
	}, out)

	// Sleeping the gap between points (not a fixed step) must keep the total
	// honest: the countdown still spans exactly warnSeconds.
	if elapsed < 31*time.Second {
		t.Errorf("countdown took %v, expected to span at least its 31s window", elapsed)
	}
	if elapsed > 50*time.Second {
		t.Errorf("countdown took %v, far longer than its 31s window", elapsed)
	}
}

// Points at or below zero are dropped, so a short warnSeconds simply starts
// further down the same schedule instead of misbehaving.
func TestShutdownShortWarnSecondsStartsPartWayDownTheSchedule(t *testing.T) {
	stub := newRESTStub(t, 1)
	out := stub.run(t, map[string]string{
		"SHUTDOWN_WARN_SECONDS":          "8",
		"SHUTDOWN_WARN_INTERVAL_SECONDS": "60",
		"SHUTDOWN_WARN_MESSAGE":          "down in %d",
	})
	_, announced := stub.snapshot()
	assertAnnounces(t, announced, []string{
		"down in 8", "down in 7", "down in 6", "down in 5",
		"down in 4", "down in 3", "down in 2", "down in 1",
	}, out)
}

// The humanised form must stay grammatical at the new one-second granularity.
func TestShutdownHumanisesFinalSecondsSingularly(t *testing.T) {
	stub := newRESTStub(t, 1)
	out := stub.run(t, map[string]string{
		"SHUTDOWN_WARN_SECONDS":          "2",
		"SHUTDOWN_WARN_INTERVAL_SECONDS": "60",
		"SHUTDOWN_WARN_MESSAGE":          "down in %s",
	})
	_, announced := stub.snapshot()
	assertAnnounces(t, announced, []string{"down in 2 seconds", "down in 1 second"}, out)
}
