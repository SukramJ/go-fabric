// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	_ "expvar" // registers /debug/vars on http.DefaultServeMux
	"log/slog"
	"net"
	"net/http"
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

// serveMetrics serves expvar's /debug/vars on addr until ctx ends; a no-op
// for an empty addr. The listener is opened before it returns, so a bad
// address fails the start.
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
	mux.Handle("/debug/vars", http.DefaultServeMux)
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
