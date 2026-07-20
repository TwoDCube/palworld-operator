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

package resources

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
	"github.com/twodcube/palworld-operator/internal/settings"
)

// RenderSettings renders the PalWorldSettings.ini content (with secret
// placeholders) for a game, wiring in the operator-managed networking keys.
func RenderSettings(g *palworldv1alpha1.PalworldGame) (string, error) {
	s := g.Spec.ServerSettings
	opts := settings.InjectOptions{
		RCONEnabled:    true,
		RCONPort:       RCONPort,
		RESTAPIEnabled: true,
		RESTAPIPort:    RESTPort,
		PublicIP:       g.Spec.Networking.PublicIP,
		PublicPort:     g.Spec.Networking.PublicPort,
	}
	if opts.PublicPort == 0 {
		opts.PublicPort = GamePort(g)
	}
	return settings.Render(&s, opts)
}

// renderEngineINI turns the EngineSettings map ("Section/Key" -> value) into a
// deterministic Engine.ini document.
func renderEngineINI(engine map[string]string) string {
	if len(engine) == 0 {
		return ""
	}
	sections := map[string][]string{}
	for k, v := range engine {
		parts := strings.SplitN(k, "/", 2)
		section := "/Script/Engine.Engine"
		key := k
		if len(parts) == 2 {
			section = parts[0]
			key = parts[1]
		}
		sections[section] = append(sections[section], fmt.Sprintf("%s=%s", key, v))
	}
	secNames := make([]string, 0, len(sections))
	for s := range sections {
		secNames = append(secNames, s)
	}
	sort.Strings(secNames)
	var b strings.Builder
	for _, s := range secNames {
		b.WriteString("[" + s + "]\n")
		lines := sections[s]
		sort.Strings(lines)
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// DesiredConfigMap builds the ConfigMap holding the rendered server config.
func DesiredConfigMap(g *palworldv1alpha1.PalworldGame) (*corev1.ConfigMap, error) {
	ini, err := RenderSettings(g)
	if err != nil {
		return nil, err
	}
	data := map[string]string{
		"PalWorldSettings.ini": ini,
	}
	if engine := renderEngineINI(g.Spec.EngineSettings); engine != "" {
		data["Engine.ini"] = engine
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigMapName(g),
			Namespace: g.Namespace,
			Labels:    CommonLabels(g),
		},
		Data: data,
	}, nil
}

// SettingsHash returns a stable hash of the rendered config so the controller
// can trigger a rolling restart when settings change.
func SettingsHash(g *palworldv1alpha1.PalworldGame) (string, error) {
	ini, err := RenderSettings(g)
	if err != nil {
		return "", err
	}
	engine := renderEngineINI(g.Spec.EngineSettings)
	return settings.Hash(ini + "\x00" + engine), nil
}
