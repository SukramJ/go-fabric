// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"expvar"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/node"
	expvarmetrics "github.com/SukramJ/go-fabric/node/metrics/expvar"
)

// metricsPrefix is the expvar name prefix of the node's metrics:
// /debug/vars shows "fabric.sessions.open" and the rest of the set.
const metricsPrefix = "fabric."

// applyMetrics wires the node's metrics port to expvar when --metrics-expvar
// names an address (node.Config.Metrics); off without one.
func applyMetrics(cfg *node.Config, addr string) {
	if addr == "" {
		return
	}
	cfg.Metrics = expvarmetrics.New(metricsPrefix)
}

// serveMetrics serves the node's metrics as /debug/vars on addr until ctx
// ends ([metricsHandler]); a no-op for an empty addr. The listener is
// opened before it returns, so a bad address fails the start. The endpoint
// has no authentication: whoever reaches addr reads the counters.
func serveMetrics(ctx context.Context, addr string, logger *slog.Logger) error {
	if addr == "" {
		return nil
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/debug/vars", metricsHandler())
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Warn("metrics.serve", slog.Any("err", err))
		}
	}()
	logger.Info("metrics.expvar", slog.String("listen", ln.Addr().String()), slog.String("path", "/debug/vars"))
	return nil
}

// metricsHandler serves the expvar variables under [metricsPrefix] — the
// node's metrics — in the shape of expvar's own /debug/vars, one JSON
// object. It is not expvar.Handler: that one also publishes "cmdline",
// which is os.Args, and this daemon takes its setup passcode, the test
// enable key and the simulated Wi-Fi passphrases as flags; and it
// publishes whatever else a dependency registers. Here a variable is
// served because it is one of the node's, not because nothing excluded it.
func metricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		var b strings.Builder
		b.WriteString("{\n")
		first := true
		expvar.Do(func(kv expvar.KeyValue) {
			if !strings.HasPrefix(kv.Key, metricsPrefix) {
				return
			}
			if !first {
				b.WriteString(",\n")
			}
			first = false
			fmt.Fprintf(&b, "%q: %s", kv.Key, kv.Value)
		})
		b.WriteString("\n}\n")
		_, _ = w.Write([]byte(b.String()))
	})
}
