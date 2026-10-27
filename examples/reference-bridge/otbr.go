// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	tbrmdef "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement"
	tnddef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/cluster/threadbr"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/thread/otbr"
)

// --otbr http://[host]:8081 (ADR 0027 decision 6, off by default): one
// ThreadBorderRouter endpoint over an existing OpenThread Border Router's
// REST API — ThreadBorderRouterManagement with PanChange and
// ThreadNetworkDiagnostics with the MLE and MAC counts, both on the
// border router thread/otbr reaches. The daemon joins no Thread mesh.
// Without the flag nothing moves, so the default composition and the
// chip-tool guard runs are unchanged.

// otbrThreadVersion is the Thread version reported when the border
// router's own diagnostics record carries none: 5, Thread 1.4, the
// version the device type asks a border router to be built on (matter.js
// packages/node/src/devices/thread-border-router.ts:55-56 at 862ad75f).
const otbrThreadVersion = 5

// otbrRefreshInterval is how often the daemon reads the border router
// again, so a change made outside Matter reaches a subscriber.
const otbrRefreshInterval = 30 * time.Second

// demoBorderRouter is the --otbr endpoint.
type demoBorderRouter struct {
	device *device.ThreadBorderRouter
	logger *slog.Logger
}

// parseOTBRURL is the flag's border router: an http URL, the port 8081
// when it names none.
func parseOTBRURL(raw string) (host string, port int, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0, fmt.Errorf("--otbr: %w", err)
	}
	if u.Scheme != "http" || u.Hostname() == "" {
		return "", 0, errors.New("--otbr: want http://host[:port]")
	}
	port = otbr.DefaultPort
	if p := u.Port(); p != "" {
		if port, err = strconv.Atoi(p); err != nil {
			return "", 0, fmt.Errorf("--otbr: port %q: %w", p, err)
		}
	}
	return u.Hostname(), port, nil
}

// newDemoBorderRouter builds the ThreadBorderRouter endpoint on the
// border router at raw; the border router must answer (its agent id and
// name are read at construction).
func newDemoBorderRouter(raw string, logger *slog.Logger) (*demoBorderRouter, error) {
	host, port, err := parseOTBRURL(raw)
	if err != nil {
		return nil, err
	}
	c, err := otbr.NewClient(otbr.Config{Host: host, Port: port})
	if err != nil {
		return nil, err
	}
	br, err := otbr.NewBorderRouter(c, otbr.BorderRouterConfig{Name: "go-fabric reference OTBR", ThreadVersion: otbrThreadVersion})
	if err != nil {
		return nil, err
	}
	d, err := device.NewThreadBorderRouter(device.Options{StableKey: device.StringKey("otbr"), Name: "Thread Border Router"},
		tbrmdef.Declare{BorderRouter: br, Features: tbrmdef.FeaturePanChange},
		tnddef.Declare{Features: threadbr.DiagnosticsFeatures})
	if err != nil {
		return nil, fmt.Errorf("--otbr: %w", err)
	}
	return &demoBorderRouter{device: d, logger: logger}, nil
}

// refresh reads the border router again.
func (b *demoBorderRouter) refresh(ctx context.Context) {
	type refresher interface{ Refresh(context.Context) error }
	for _, srv := range []any{b.device.ThreadBorderRouterManagement().Underlying(), b.device.ThreadNetworkDiagnostics().Underlying()} {
		r, ok := srv.(refresher)
		if !ok {
			continue
		}
		if err := r.Refresh(ctx); err != nil {
			b.logger.Warn("otbr.refresh", slog.Any("err", err))
		}
	}
}

// watch refreshes every otbrRefreshInterval until ctx ends.
func (b *demoBorderRouter) watch(ctx context.Context) {
	tick := time.NewTicker(otbrRefreshInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			b.refresh(ctx)
		}
	}
}
