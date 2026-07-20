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

package settings

import (
	"strings"
	"testing"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

func renderTest(t *testing.T, s *palworldv1alpha1.PalworldServerSettings) string {
	t.Helper()
	out, err := Render(s, InjectOptions{
		RCONEnabled:    true,
		RCONPort:       25575,
		RESTAPIEnabled: true,
		RESTAPIPort:    8212,
		PublicIP:       "",
		PublicPort:     8211,
	})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	return out
}

func TestRenderFormat(t *testing.T) {
	s := &palworldv1alpha1.PalworldServerSettings{
		ServerName:         `My "Great" Server`,
		Difficulty:         palworldv1alpha1.DifficultyNormal,
		ExpRate:            2.5,
		EnableInvaderEnemy: true,
		IsMultiplay:        false,
		CrossplayPlatforms: []palworldv1alpha1.CrossplayPlatform{
			palworldv1alpha1.CrossplayPlatformSteam,
			palworldv1alpha1.CrossplayPlatformPS5,
		},
	}
	out := renderTest(t, s)

	// Header line.
	if !strings.HasPrefix(out, "[/Script/Pal.PalGameWorldSettings]\n") {
		t.Errorf("missing header line, got:\n%s", out[:60])
	}
	// Single OptionSettings tuple.
	if !strings.Contains(out, "OptionSettings=(") || !strings.HasSuffix(strings.TrimSpace(out), ")") {
		t.Errorf("OptionSettings tuple malformed")
	}

	cases := map[string]string{
		"enum unquoted":         "Difficulty=Normal",
		"float 6dp":             "ExpRate=2.500000",
		"bool title-case true":  "bEnableInvaderEnemy=True",
		"bool title-case false": "bIsMultiplay=False",
		"quoted string escaped": `ServerName="My Great Server"`, // embedded quotes stripped
		"platforms tuple":       "CrossplayPlatforms=(Steam,PS5)",
		"admin placeholder":     `AdminPassword="__PALWORLD_ADMIN_PASSWORD__"`,
		"rcon injected":         "RCONEnabled=True",
		"rcon port injected":    "RCONPort=25575",
		"rest injected":         "RESTAPIEnabled=True",
		"rest port injected":    "RESTAPIPort=8212",
		"public port injected":  "PublicPort=8211",
	}
	for name, want := range cases {
		if !strings.Contains(out, want) {
			t.Errorf("%s: expected %q in output", name, want)
		}
	}

	// No literal double quote should survive inside ServerName's value.
	if strings.Contains(out, `My "Great"`) {
		t.Errorf("embedded quotes were not stripped: %s", out)
	}
	// Whole thing must be a single physical settings line (plus header + newline).
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("expected exactly 2 lines (header + settings), got %d", len(lines))
	}
}

func TestHashStability(t *testing.T) {
	s := &palworldv1alpha1.PalworldServerSettings{ServerName: "A"}
	a := renderTest(t, s)
	b := renderTest(t, s)
	if Hash(a) != Hash(b) {
		t.Errorf("hash is not stable for identical input")
	}
	s2 := &palworldv1alpha1.PalworldServerSettings{ServerName: "B"}
	if Hash(a) == Hash(renderTest(t, s2)) {
		t.Errorf("hash did not change for different input")
	}
}

func TestNoUnbalancedParens(t *testing.T) {
	s := &palworldv1alpha1.PalworldServerSettings{
		CrossplayPlatforms: []palworldv1alpha1.CrossplayPlatform{palworldv1alpha1.CrossplayPlatformSteam},
	}
	out := renderTest(t, s)
	open := strings.Count(out, "(")
	closeP := strings.Count(out, ")")
	if open != closeP {
		t.Errorf("unbalanced parentheses: %d open vs %d close", open, closeP)
	}
}
