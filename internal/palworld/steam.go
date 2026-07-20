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

package palworld

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PalworldSteamAppID is the Steam application id for the Palworld dedicated
// server.
const PalworldSteamAppID = "2394010"

// DefaultSteamInfoEndpoint is the public steamcmd.net info API used to look up
// the latest published build id for a branch without needing SteamCMD.
const DefaultSteamInfoEndpoint = "https://api.steamcmd.net/v1/info"

// SteamPoller resolves the latest published build id for a Steam app + branch.
type SteamPoller struct {
	endpoint string
	http     *http.Client
}

// NewSteamPoller returns a poller using the given info endpoint (empty selects
// the default steamcmd.net API).
func NewSteamPoller(endpoint string) *SteamPoller {
	if endpoint == "" {
		endpoint = DefaultSteamInfoEndpoint
	}
	return &SteamPoller{
		endpoint: endpoint,
		http:     &http.Client{Timeout: 20 * time.Second},
	}
}

// steamCmdInfoResponse models the subset of the steamcmd.net response we need.
type steamCmdInfoResponse struct {
	Status string `json:"status"`
	Data   map[string]struct {
		Depots struct {
			Branches map[string]struct {
				BuildID     string `json:"buildid"`
				TimeUpdated string `json:"timeupdated"`
			} `json:"branches"`
		} `json:"depots"`
	} `json:"data"`
}

// LatestBuildID returns the build id currently published on the given branch
// (default "public") for the Palworld dedicated server app.
func (p *SteamPoller) LatestBuildID(ctx context.Context, branch string) (string, error) {
	if branch == "" {
		branch = "public"
	}
	url := fmt.Sprintf("%s/%s", p.endpoint, PalworldSteamAppID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("steam info request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("steam info: status %d", resp.StatusCode)
	}
	var parsed steamCmdInfoResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode steam info: %w", err)
	}
	app, ok := parsed.Data[PalworldSteamAppID]
	if !ok {
		return "", fmt.Errorf("steam info: app %s not present in response", PalworldSteamAppID)
	}
	br, ok := app.Depots.Branches[branch]
	if !ok {
		return "", fmt.Errorf("steam info: branch %q not found", branch)
	}
	if br.BuildID == "" {
		return "", fmt.Errorf("steam info: empty build id for branch %q", branch)
	}
	return br.BuildID, nil
}
