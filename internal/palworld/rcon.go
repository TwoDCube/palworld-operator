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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gorcon/rcon"
)

// RCONClient wraps a Source RCON connection to a Palworld server.
//
// It is the fallback control channel for a server with the REST API disabled,
// and for commands only exposed over RCON. No controller currently dials it --
// the operator drives every live interaction over REST (spec 08) -- so this is a
// standalone client, not part of any reconcile path.
type RCONClient struct {
	address  string
	password string
	timeout  time.Duration
}

// NewRCONClient builds an RCON client for host:port using the RCON password
// (which the operator sets equal to the admin password).
func NewRCONClient(host string, port int32, password string) *RCONClient {
	return &RCONClient{
		address:  fmt.Sprintf("%s:%d", host, port),
		password: password,
		timeout:  10 * time.Second,
	}
}

func (c *RCONClient) exec(command string) (string, error) {
	conn, err := rcon.Dial(c.address, c.password,
		rcon.SetDialTimeout(c.timeout),
		rcon.SetDeadline(c.timeout),
	)
	if err != nil {
		return "", fmt.Errorf("rcon dial %s: %w", c.address, err)
	}
	defer func() { _ = conn.Close() }()
	resp, err := conn.Execute(command)
	if err != nil {
		return "", fmt.Errorf("rcon exec %q: %w", command, err)
	}
	return resp, nil
}

// Info returns the server info string.
func (c *RCONClient) Info() (string, error) {
	return c.exec("Info")
}

// Broadcast sends a message to all players. Palworld's Broadcast command
// replaces spaces oddly on some builds; callers should keep messages simple.
func (c *RCONClient) Broadcast(message string) error {
	_, err := c.exec("Broadcast " + strings.ReplaceAll(message, " ", "_"))
	return err
}

// Save flushes the world to disk.
func (c *RCONClient) Save() error {
	_, err := c.exec("Save")
	return err
}

// Shutdown schedules a shutdown after the given seconds with a broadcast
// message.
func (c *RCONClient) Shutdown(seconds int32, message string) error {
	_, err := c.exec("Shutdown " + strconv.Itoa(int(seconds)) + " " + strings.ReplaceAll(message, " ", "_"))
	return err
}

// DoExit forces an immediate exit.
func (c *RCONClient) DoExit() error {
	_, err := c.exec("DoExit")
	return err
}

// ShowPlayers returns the raw CSV player list (name,playeruid,steamid).
func (c *RCONClient) ShowPlayers() (string, error) {
	return c.exec("ShowPlayers")
}

// CountPlayers parses ShowPlayers output into a connected-player count.
func (c *RCONClient) CountPlayers() (int, error) {
	out, err := c.ShowPlayers()
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	count := 0
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// First line is the CSV header: name,playeruid,steamid
		if i == 0 && strings.HasPrefix(strings.ToLower(line), "name") {
			continue
		}
		count++
	}
	return count, nil
}
