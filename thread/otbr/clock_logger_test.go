// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/thread/otbr/otbrtest"
)

// Probe stamps ProbedAt on Config.Clock, and a BorderRouter logs a
// skipped diagnostics field on BorderRouterConfig.Logger (W4-ctlint-14):
// the clock and the logger are the option structs' fields, the zero
// values the system clock and slog.Default.
func TestProbeClockAndTranslateLogger(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	s := newStub().on("GET", "/node", 200, otbrtest.NodePascal).on("GET", "/api/diagnostics", 404, "").on("GET", "/diagnostics", 200, "[]")
	c, err := Probe(context.Background(), Config{Host: "br.example", Port: 8081, HTTPClient: &http.Client{Transport: s}, Clock: clocktest.New(at)})
	if err != nil || c == nil || !c.ProbedAt.Equal(at) {
		t.Fatalf("Probe = %+v, %v; want ProbedAt %v", c, err, at)
	}

	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if _, err := translateEntry(map[string]any{"mode": map[string]any{"RxOnWhenIdle": "not a number"}}, log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "otbr.diagnostics.field_skipped") {
		t.Errorf("the skipped field was not logged on the given logger: %q", logs.String())
	}
}
