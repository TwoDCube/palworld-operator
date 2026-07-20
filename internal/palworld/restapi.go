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

// Package palworld provides typed clients for the Palworld dedicated server's
// admin surfaces: the HTTP REST API and the Source RCON protocol. The operator
// uses these to observe live state (players, metrics), flush saves before
// backups, broadcast update warnings, and drive graceful shutdowns.
package palworld

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RESTClient talks to the Palworld dedicated server REST admin API.
type RESTClient struct {
	baseURL  string
	password string
	http     *http.Client
}

// NewRESTClient constructs a REST client for the server at host:port. The admin
// password is the server's AdminPassword; the API uses HTTP Basic auth with the
// fixed username "admin".
func NewRESTClient(host string, port int32, adminPassword string) *RESTClient {
	return &RESTClient{
		baseURL:  fmt.Sprintf("http://%s:%d", host, port),
		password: adminPassword,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// NewRESTClientURL constructs a REST client from a base URL (e.g.
// http://127.0.0.1:8212).
func NewRESTClientURL(baseURL, adminPassword string) *RESTClient {
	return &RESTClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		password: adminPassword,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

// WithTimeout returns a copy of the client using the given request timeout.
func (c *RESTClient) WithTimeout(d time.Duration) *RESTClient {
	cp := *c
	cp.http = &http.Client{Timeout: d}
	return &cp
}

// ServerInfo is the response from GET /v1/api/info.
type ServerInfo struct {
	Version     string `json:"version"`
	ServerName  string `json:"servername"`
	Description string `json:"description"`
	WorldGUID   string `json:"worldguid"`
}

// ServerMetrics is the response from GET /v1/api/metrics.
type ServerMetrics struct {
	ServerFPS        int32   `json:"serverfps"`
	CurrentPlayerNum int32   `json:"currentplayernum"`
	ServerFrameTime  float64 `json:"serverframetime"`
	MaxPlayerNum     int32   `json:"maxplayernum"`
	Uptime           int64   `json:"uptime"`
	Days             int32   `json:"days"`
}

// Player is a single connected player from GET /v1/api/players.
type Player struct {
	Name      string  `json:"name"`
	PlayerID  string  `json:"playerId"`
	UserID    string  `json:"userId"`
	IP        string  `json:"ip"`
	Ping      float64 `json:"ping"`
	Level     int32   `json:"level"`
	LocationX float64 `json:"location_x"`
	LocationY float64 `json:"location_y"`
}

type playersResponse struct {
	Players []Player `json:"players"`
}

func (c *RESTClient) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.SetBasicAuth("admin", c.password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("palworld REST %s %s: status %d: %s", method, path, resp.StatusCode, string(data))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// Info returns basic server information. A successful call is a good readiness
// signal.
func (c *RESTClient) Info(ctx context.Context) (*ServerInfo, error) {
	var info ServerInfo
	if err := c.do(ctx, http.MethodGet, "/v1/api/info", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// Metrics returns live server metrics (fps, players, uptime).
func (c *RESTClient) Metrics(ctx context.Context) (*ServerMetrics, error) {
	var m ServerMetrics
	if err := c.do(ctx, http.MethodGet, "/v1/api/metrics", nil, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Players returns the list of connected players.
func (c *RESTClient) Players(ctx context.Context) ([]Player, error) {
	var r playersResponse
	if err := c.do(ctx, http.MethodGet, "/v1/api/players", nil, &r); err != nil {
		return nil, err
	}
	return r.Players, nil
}

// Announce broadcasts a message to all players.
func (c *RESTClient) Announce(ctx context.Context, message string) error {
	return c.do(ctx, http.MethodPost, "/v1/api/announce", map[string]string{"message": message}, nil)
}

// Save flushes the world to disk. Call this before taking a backup snapshot to
// make it application-consistent.
func (c *RESTClient) Save(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/api/save", struct{}{}, nil)
}

// Shutdown asks the server to save and shut down after waitSeconds, optionally
// broadcasting a message first.
func (c *RESTClient) Shutdown(ctx context.Context, waitSeconds int32, message string) error {
	return c.do(ctx, http.MethodPost, "/v1/api/shutdown", map[string]any{
		"waittime": waitSeconds,
		"message":  message,
	}, nil)
}

// Stop forces an immediate shutdown without a countdown.
func (c *RESTClient) Stop(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/api/stop", struct{}{}, nil)
}

// Kick disconnects a player by userId.
func (c *RESTClient) Kick(ctx context.Context, userID, message string) error {
	return c.do(ctx, http.MethodPost, "/v1/api/kick", map[string]string{"userid": userID, "message": message}, nil)
}
