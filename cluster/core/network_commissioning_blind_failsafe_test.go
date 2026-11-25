// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/im"
)

// blindPortLog is the message the network servers log once when the
// fabric-blind fail-safe port (deprecated) answers in place of FailSafe.
const blindPortLog = "netcomm.failsafe_blind_port"

func countBlindPortLogs(buf *bytes.Buffer) int {
	return strings.Count(buf.String(), blindPortLog)
}

// TestWiFiNetworkCommissioningBlindFailSafePortLogsOnce holds R2-C2-sec-1006
// (decision 515): a Wi-Fi server wired with only the deprecated
// IsFailSafeArmed port logs it once, however often the gate is consulted,
// and keeps answering as before; one wired with FailSafe never logs it.
func TestWiFiNetworkCommissioningBlindFailSafePortLogsOnce(t *testing.T) {
	t.Parallel()
	ctx := im.WithFabricFilter(context.Background(), false, 1)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{
		Driver: newCapableDriver("home"), IsFailSafeArmed: func() bool { return true }, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if !w.armedFor(ctx) {
			t.Fatal("the fabric-blind port, armed, no longer answers armed")
		}
	}
	if got := countBlindPortLogs(&buf); got != 1 {
		t.Errorf("blind port only: %d %q lines, want 1; log:\n%s", got, blindPortLog, buf.String())
	}

	var aware bytes.Buffer
	w2, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{
		Driver:          newCapableDriver("home"),
		FailSafe:        func() (uint8, bool) { return 1, true },
		IsFailSafeArmed: func() bool { return true },
		Logger:          slog.New(slog.NewTextHandler(&aware, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if !w2.armedFor(ctx) {
			t.Fatal("the fabric-aware port of the accessing fabric answers disarmed")
		}
	}
	if got := countBlindPortLogs(&aware); got != 0 {
		t.Errorf("fabric-aware port wired: %d %q lines, want 0; log:\n%s", got, blindPortLog, aware.String())
	}
}

// TestThreadNetworkCommissioningBlindFailSafePortLogsOnce is the Thread
// side of R2-C2-sec-1006 (decision 515), including the port AttachFailSafe
// wires after construction: once it is there, the blind port is no longer
// consulted and nothing more is logged.
func TestThreadNetworkCommissioningBlindFailSafePortLogsOnce(t *testing.T) {
	t.Parallel()
	ctx := im.WithFabricFilter(context.Background(), false, 1)

	// The logger comes through the config (review II, R3-sup-3002).
	var buf bytes.Buffer
	n, err := NewThreadNetworkCommissioning(context.Background(), ThreadNetworkCommissioningConfig{
		BorderRouter: &simBorderRouter{}, IsFailSafeArmed: func() bool { return true },
		Logger: slog.New(slog.NewTextHandler(&buf, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if !n.armedFor(ctx) {
			t.Fatal("the fabric-blind port, armed, no longer answers armed")
		}
	}
	if got := countBlindPortLogs(&buf); got != 1 {
		t.Errorf("blind port only: %d %q lines, want 1; log:\n%s", got, blindPortLog, buf.String())
	}

	armed2, crumbs2 := true, []uint64{}
	n2 := newThreadNC(t, &simBorderRouter{}, &armed2, &crumbs2)
	var aware bytes.Buffer
	n2.logger = slog.New(slog.NewTextHandler(&aware, nil))
	n2.AttachFailSafe(func() (uint8, bool) { return 1, true }, nil)
	for range 3 {
		if !n2.armedFor(ctx) {
			t.Fatal("the fabric-aware port of the accessing fabric answers disarmed")
		}
	}
	if got := countBlindPortLogs(&aware); got != 0 {
		t.Errorf("fabric-aware port attached: %d %q lines, want 0; log:\n%s", got, blindPortLog, aware.String())
	}
}
