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

// Package exporter implements a tiny Prometheus exporter that translates the
// Palworld REST metrics endpoint into Prometheus text format. It runs as a
// sidecar (`/manager exporter`) so no separate image is required.
package exporter

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/twodcube/palworld-operator/internal/palworld"
)

var log = ctrl.Log.WithName("exporter")

// Run starts the exporter HTTP server and blocks until the process is
// signalled. Configuration comes from the environment:
//
//	EXPORTER_ADDR   listen address (default ":9877")
//	REST_ENDPOINT   Palworld REST base URL (default "http://127.0.0.1:8212")
//	ADMIN_PASSWORD  Palworld admin password (REST basic-auth)
func Run() error {
	addr := envOr("EXPORTER_ADDR", ":9877")
	endpoint := envOr("REST_ENDPOINT", "http://127.0.0.1:8212")
	password := os.Getenv("ADMIN_PASSWORD")

	client := palworld.NewRESTClientURL(endpoint, password).WithTimeout(5 * time.Second)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		writeMetrics(r.Context(), w, client)
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Info("starting palworld metrics exporter", "addr", addr, "endpoint", endpoint)
	return server.ListenAndServe()
}

func writeMetrics(ctx context.Context, w http.ResponseWriter, client *palworld.RESTClient) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	metric := func(name, help, typ string, value float64, labels string) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
		if labels != "" {
			fmt.Fprintf(w, "%s{%s} %s\n", name, labels, strconv.FormatFloat(value, 'f', -1, 64))
		} else {
			fmt.Fprintf(w, "%s %s\n", name, strconv.FormatFloat(value, 'f', -1, 64))
		}
	}

	m, err := client.Metrics(ctx)
	if err != nil {
		metric("palworld_server_up", "1 if the Palworld REST API is reachable", "gauge", 0, "")
		return
	}
	metric("palworld_server_up", "1 if the Palworld REST API is reachable", "gauge", 1, "")
	metric("palworld_server_fps", "Current server frames per second", "gauge", float64(m.ServerFPS), "")
	metric("palworld_players_online", "Currently connected players", "gauge", float64(m.CurrentPlayerNum), "")
	metric("palworld_players_max", "Maximum player capacity", "gauge", float64(m.MaxPlayerNum), "")
	metric("palworld_server_frame_time_ms", "Server frame time in milliseconds", "gauge", m.ServerFrameTime, "")
	metric("palworld_uptime_seconds", "Server uptime in seconds", "counter", float64(m.Uptime), "")
	metric("palworld_world_days", "In-game day counter", "gauge", float64(m.Days), "")

	if info, err := client.Info(ctx); err == nil {
		metric("palworld_build_info", "Server build info", "gauge", 1,
			fmt.Sprintf("version=%q,servername=%q", info.Version, info.ServerName))
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
