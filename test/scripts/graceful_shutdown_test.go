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

// Package scripts tests the shell scripts shipped in the game-server image
// (build/palworld-server/). They are part of the product's behaviour -- the
// shutdown countdown that decides whether players are warned before a restart
// lives there, not in Go -- so they get the same test bar as the operator code.
//
// The scripts talk to the Palworld REST API over 127.0.0.1, so each test points
// REST_PORT at an httptest stub and asserts on the recorded call sequence.
// Countdowns run with seconds-scale values to keep the suite fast.
package scripts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const shutdownScript = "../../build/palworld-server/graceful-shutdown.sh"

// restStub records the calls graceful-shutdown.sh makes and answers them like the
// Palworld REST API does.
type restStub struct {
	server *httptest.Server

	mu         sync.Mutex
	calls      []string // paths, in order
	announced  []string // announce message bodies, in order
	playerJSON string   // body for GET /v1/api/players
	playersErr bool     // when true, the player query fails
}

func newRESTStub(t *testing.T, players int) *restStub {
	t.Helper()
	s := &restStub{playerJSON: playersBody(players)}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/api/players", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls = append(s.calls, "players")
		if s.playersErr {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, s.playerJSON)
	})
	mux.HandleFunc("/v1/api/announce", func(w http.ResponseWriter, r *http.Request) {
		// Decode strictly: a message that produced invalid JSON would mean players
		// never saw the warning at all.
		var body struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("announce body is not valid JSON: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls = append(s.calls, "announce")
		s.announced = append(s.announced, body.Message)
	})
	for _, path := range []string{"/v1/api/save", "/v1/api/shutdown"} {
		name := strings.TrimPrefix(path, "/v1/api/")
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.calls = append(s.calls, name)
		})
	}
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func playersBody(n int) string {
	entries := make([]string, 0, n)
	for i := range n {
		entries = append(entries, fmt.Sprintf(`{"name":"p%d"}`, i))
	}
	return `{"players":[` + strings.Join(entries, ",") + `]}`
}

func (s *restStub) port(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(s.server.URL)
	if err != nil {
		t.Fatalf("parsing stub URL: %v", err)
	}
	return u.Port()
}

func (s *restStub) snapshot() (calls, announced []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...), append([]string(nil), s.announced...)
}

// run executes graceful-shutdown.sh against the stub with env overrides applied
// on top of a fast, self-contained baseline.
func (s *restStub) run(t *testing.T, env map[string]string) string {
	t.Helper()
	base := map[string]string{
		"REST_ENABLED":                   "true",
		"REST_PORT":                      s.port(t),
		"ADMIN_PASSWORD":                 "secret",
		"SHUTDOWN_WAIT_SECONDS":          "0",
		"SHUTDOWN_WARN_SECONDS":          "2",
		"SHUTDOWN_WARN_INTERVAL_SECONDS": "1",
		"SHUTDOWN_GRACE_SECONDS":         "0",
		"SHUTDOWN_MARKER":                filepath.Join(t.TempDir(), "marker"),
	}
	for k, v := range env {
		base[k] = v
	}

	cmd := exec.Command("bash", shutdownScript)
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH"))
	for k, v := range base {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("graceful-shutdown.sh failed: %v\n%s", err, out)
	}
	return string(out)
}

// The countdown is the whole point of the script: players must get repeated
// warnings before the world is flushed and the server stops.
func TestShutdownWarnsPlayersOnEveryIntervalThenSavesAndStops(t *testing.T) {
	stub := newRESTStub(t, 1)
	start := time.Now()
	out := stub.run(t, map[string]string{
		"SHUTDOWN_WARN_SECONDS":          "3",
		"SHUTDOWN_WARN_INTERVAL_SECONDS": "1",
		"SHUTDOWN_WARN_MESSAGE":          "down in %s",
	})
	elapsed := time.Since(start)

	calls, announced := stub.snapshot()

	// One warning per interval tick: T-3s, T-2s, T-1s.
	wantWarnings := []string{"down in 3 seconds", "down in 2 seconds", "down in 1 second"}
	if len(announced) != len(wantWarnings)+1 {
		t.Fatalf("expected %d countdown warnings + 1 final announce, got %v\n%s",
			len(wantWarnings), announced, out)
	}
	for i, want := range wantWarnings {
		if announced[i] != want {
			t.Errorf("warning %d: want %q, got %q", i, want, announced[i])
		}
	}

	// Save must precede shutdown, or the world is lost.
	saveAt, shutdownAt := indexOf(calls, "save"), indexOf(calls, "shutdown")
	if saveAt < 0 || shutdownAt < 0 {
		t.Fatalf("expected save and shutdown calls, got %v", calls)
	}
	if saveAt > shutdownAt {
		t.Errorf("save must come before shutdown, got %v", calls)
	}
	// Every countdown announce must land before the save.
	if lastAnnounce := lastIndexBefore(calls, "announce", saveAt); lastAnnounce < 0 {
		t.Errorf("expected announces before the save, got %v", calls)
	}

	// The countdown must actually consume wall-clock time; an instant "countdown"
	// is the bug this feature exists to fix.
	if elapsed < 3*time.Second {
		t.Errorf("countdown finished in %v, expected to span at least 3s", elapsed)
	}
}

// An idle server must not stall every rolling restart for the full warning.
func TestShutdownSkipsCountdownWhenNoPlayersOnline(t *testing.T) {
	stub := newRESTStub(t, 0)
	start := time.Now()
	out := stub.run(t, map[string]string{
		"SHUTDOWN_WARN_SECONDS":          "30",
		"SHUTDOWN_WARN_INTERVAL_SECONDS": "1",
	})
	elapsed := time.Since(start)

	calls, announced := stub.snapshot()
	for _, msg := range announced {
		if strings.Contains(msg, "in ") {
			t.Errorf("unexpected countdown warning on an empty server: %q", msg)
		}
	}
	if indexOf(calls, "save") < 0 || indexOf(calls, "shutdown") < 0 {
		t.Errorf("expected save and shutdown even with no players, got %v", calls)
	}
	if elapsed > 25*time.Second {
		t.Errorf("empty server waited %v; expected the countdown to be skipped\n%s", elapsed, out)
	}
	if !strings.Contains(out, "no players connected") {
		t.Errorf("expected a log line about skipping the warning, got:\n%s", out)
	}
}

// If the player list cannot be read we must assume someone is on and warn, rather
// than silently dropping a live session.
func TestShutdownWarnsWhenPlayerCountUnknown(t *testing.T) {
	stub := newRESTStub(t, 1)
	stub.playersErr = true

	stub.run(t, map[string]string{
		"SHUTDOWN_WARN_SECONDS":          "1",
		"SHUTDOWN_WARN_INTERVAL_SECONDS": "1",
		"SHUTDOWN_WARN_MESSAGE":          "down in %s",
	})

	_, announced := stub.snapshot()
	if len(announced) == 0 || announced[0] != "down in 1 second" {
		t.Errorf("expected a countdown warning when the player count is unknown, got %v", announced)
	}
}

// The script runs twice per termination (preStop, then the entrypoint's SIGTERM
// trap). A second countdown would overrun terminationGracePeriodSeconds and get
// the pod SIGKILLed mid-save, so the second invocation must be a no-op.
func TestShutdownSecondInvocationIsNoOp(t *testing.T) {
	stub := newRESTStub(t, 1)
	marker := filepath.Join(t.TempDir(), "marker")

	env := map[string]string{
		"SHUTDOWN_MARKER":       marker,
		"SHUTDOWN_WARN_SECONDS": "1",
	}
	stub.run(t, env)
	firstCalls, _ := stub.snapshot()
	if len(firstCalls) == 0 {
		t.Fatal("expected the first invocation to talk to the REST API")
	}

	out := stub.run(t, env) // same marker: simulates the SIGTERM trap
	secondCalls, _ := stub.snapshot()

	if len(secondCalls) != len(firstCalls) {
		t.Errorf("second invocation issued %d extra REST calls; expected none\n%s",
			len(secondCalls)-len(firstCalls), out)
	}
	if !strings.Contains(out, "already in progress") {
		t.Errorf("expected the guard to log that a shutdown is in progress, got:\n%s", out)
	}
}

// An explicit terminationGracePeriodSeconds too small for the configured warning
// must shorten the countdown rather than let the kubelet SIGKILL a save in flight.
func TestShutdownClampsCountdownToGraceBudget(t *testing.T) {
	stub := newRESTStub(t, 1)
	start := time.Now()
	out := stub.run(t, map[string]string{
		"SHUTDOWN_WARN_SECONDS":          "600",
		"SHUTDOWN_WARN_INTERVAL_SECONDS": "1",
		"SHUTDOWN_GRACE_SECONDS":         "32", // 32 - 30 reserved = 2s of warning
	})
	elapsed := time.Since(start)

	if elapsed > 30*time.Second {
		t.Fatalf("countdown was not clamped: took %v\n%s", elapsed, out)
	}
	if !strings.Contains(out, "clamping warning") {
		t.Errorf("expected a clamp log line, got:\n%s", out)
	}
	_, announced := stub.snapshot()
	// 2s of warning at a 1s interval: two ticks, plus the final announce.
	if len(announced) != 3 {
		t.Errorf("expected 2 clamped warnings + 1 final announce, got %v", announced)
	}
}

// warnSeconds: 0 must stop promptly, for operators who want the old behaviour.
func TestShutdownZeroWarnSecondsStopsImmediately(t *testing.T) {
	stub := newRESTStub(t, 1)
	stub.run(t, map[string]string{"SHUTDOWN_WARN_SECONDS": "0"})

	calls, announced := stub.snapshot()
	for _, msg := range announced {
		if strings.Contains(msg, "in ") {
			t.Errorf("unexpected countdown warning with warnSeconds=0: %q", msg)
		}
	}
	if indexOf(calls, "shutdown") < 0 {
		t.Errorf("expected a shutdown call, got %v", calls)
	}
}

// A quote in an operator's message must not corrupt the announce body -- the stub
// decodes strictly, so invalid JSON fails the test.
func TestShutdownEscapesQuotesInWarnMessage(t *testing.T) {
	stub := newRESTStub(t, 1)
	stub.run(t, map[string]string{
		"SHUTDOWN_WARN_SECONDS":          "1",
		"SHUTDOWN_WARN_INTERVAL_SECONDS": "1",
		"SHUTDOWN_WARN_MESSAGE":          `He said "maintenance" -- back in %s`,
	})

	_, announced := stub.snapshot()
	if len(announced) == 0 {
		t.Fatal("expected an announce call")
	}
	if want := `He said "maintenance" -- back in 1 second`; announced[0] != want {
		t.Errorf("want %q, got %q", want, announced[0])
	}
}

// Without REST there is no way to broadcast, so the script must not burn the
// grace period on a countdown nobody can see.
func TestShutdownSkipsCountdownWithoutREST(t *testing.T) {
	stub := newRESTStub(t, 1)
	start := time.Now()
	out := stub.run(t, map[string]string{
		"REST_ENABLED":          "false",
		"SHUTDOWN_WARN_SECONDS": "30",
	})
	elapsed := time.Since(start)

	if elapsed > 25*time.Second {
		t.Errorf("expected no countdown without REST, took %v", elapsed)
	}
	if calls, _ := stub.snapshot(); len(calls) != 0 {
		t.Errorf("expected no REST calls when REST is disabled, got %v", calls)
	}
	if !strings.Contains(out, "REST unavailable") {
		t.Errorf("expected a log line about REST being unavailable, got:\n%s", out)
	}
}

// A non-numeric override must fall back to the default instead of breaking
// arithmetic part-way through a termination.
func TestShutdownIgnoresNonNumericWarnSeconds(t *testing.T) {
	stub := newRESTStub(t, 0) // no players, so the default 300s is not actually waited out
	out := stub.run(t, map[string]string{"SHUTDOWN_WARN_SECONDS": "abc"})

	if !strings.Contains(out, "ignoring non-numeric value") {
		t.Errorf("expected a log line about the bad value, got:\n%s", out)
	}
	if calls, _ := stub.snapshot(); indexOf(calls, "shutdown") < 0 {
		t.Errorf("expected the shutdown to proceed, got %v", calls)
	}
}

func indexOf(haystack []string, needle string) int {
	for i, v := range haystack {
		if v == needle {
			return i
		}
	}
	return -1
}

func lastIndexBefore(haystack []string, needle string, before int) int {
	found := -1
	for i, v := range haystack {
		if i >= before {
			break
		}
		if v == needle {
			found = i
		}
	}
	return found
}
