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

// Package settings renders a PalworldServerSettings API object into the
// single-line OptionSettings tuple that Palworld's PalWorldSettings.ini
// expects. It reflects over the `pal:"<IniKey>,<kind>,<quote>"` struct tags on
// v1alpha1.PalworldServerSettings so the key list, ordering, and quoting live
// in exactly one place (the API type).
package settings

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

const settingsHeader = "[/Script/Pal.PalGameWorldSettings]"

// Placeholder tokens replaced by the server entrypoint from Secret-backed env
// vars, so no plaintext secret ever lands in the rendered ConfigMap.
const (
	AdminPasswordPlaceholder  = "__PALWORLD_ADMIN_PASSWORD__"
	ServerPasswordPlaceholder = "__PALWORLD_SERVER_PASSWORD__"
)

// InjectOptions carries the operator-managed keys that are excluded from the
// user-facing settings struct (passwords, public address, RCON/REST).
type InjectOptions struct {
	RCONEnabled    bool
	RCONPort       int32
	RESTAPIEnabled bool
	RESTAPIPort    int32
	PublicIP       string
	PublicPort     int32
}

// Render produces the full PalWorldSettings.ini content (header + OptionSettings
// line) for the given settings and operator-injected options.
func Render(s *palworldv1alpha1.PalworldServerSettings, opts InjectOptions) (string, error) {
	pairs, err := renderStruct(s)
	if err != nil {
		return "", err
	}
	pairs = append(pairs, injectedPairs(opts)...)
	line := "OptionSettings=(" + strings.Join(pairs, ",") + ")"
	return settingsHeader + "\n" + line + "\n", nil
}

// Hash returns a short content hash used to detect configuration drift and
// trigger rolling restarts when settings change.
func Hash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:16]
}

func injectedPairs(opts InjectOptions) []string {
	boolStr := func(b bool) string {
		if b {
			return "True"
		}
		return "False"
	}
	pairs := []string{
		"AdminPassword=\"" + AdminPasswordPlaceholder + "\"",
		"ServerPassword=\"" + ServerPasswordPlaceholder + "\"",
		"PublicIP=\"" + escapeIniString(opts.PublicIP) + "\"",
		"RCONEnabled=" + boolStr(opts.RCONEnabled),
		"RCONPort=" + strconv.FormatInt(int64(opts.RCONPort), 10),
		"RESTAPIEnabled=" + boolStr(opts.RESTAPIEnabled),
		"RESTAPIPort=" + strconv.FormatInt(int64(opts.RESTAPIPort), 10),
	}
	if opts.PublicPort > 0 {
		pairs = append(pairs, "PublicPort="+strconv.FormatInt(int64(opts.PublicPort), 10))
	}
	return pairs
}

func renderStruct(s *palworldv1alpha1.PalworldServerSettings) ([]string, error) {
	v := reflect.ValueOf(s).Elem()
	t := v.Type()
	pairs := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("pal")
		if tag == "" {
			continue
		}
		parts := strings.Split(tag, ",")
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid pal tag %q on field %s", tag, field.Name)
		}
		key := parts[0]
		kind := parts[1]
		quote := "n"
		if len(parts) >= 3 {
			quote = parts[2]
		}
		val, err := formatValue(v.Field(i), kind, quote)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
		pairs = append(pairs, key+"="+val)
	}
	return pairs, nil
}

func formatValue(fv reflect.Value, kind, quote string) (string, error) {
	switch kind {
	case "bool":
		if fv.Bool() {
			return "True", nil
		}
		return "False", nil
	case "int":
		return strconv.FormatInt(fv.Int(), 10), nil
	case "float":
		return strconv.FormatFloat(fv.Float(), 'f', 6, 64), nil
	case "enum":
		return fv.String(), nil
	case "string":
		s := fv.String()
		if quote == "q" {
			return "\"" + escapeIniString(s) + "\"", nil
		}
		return s, nil
	case "platforms":
		items := make([]string, 0, fv.Len())
		for i := 0; i < fv.Len(); i++ {
			items = append(items, fv.Index(i).String())
		}
		return "(" + strings.Join(items, ",") + ")", nil
	default:
		return "", fmt.Errorf("unknown pal kind %q", kind)
	}
}

// escapeIniString removes characters that would break the single-line INI
// tuple. Palworld's parser does not support escaped quotes inside the tuple, so
// embedded double quotes are stripped; this is also validated by the admission
// webhook, which rejects them earlier with a clear message.
func escapeIniString(s string) string {
	s = strings.ReplaceAll(s, "\"", "")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
