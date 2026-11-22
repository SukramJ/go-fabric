// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
)

// TestWithInvokeSessionLogsADroppedKey (review II, 5b-peer R4-1b): a key
// that is not a pointer stamps nothing, as before, and is logged. Not
// parallel: it swaps the default logger, and the package's parallel tests
// start only after the serial ones ended.
func TestWithInvokeSessionLogsADroppedKey(t *testing.T) { //nolint:paralleltest // swaps slog's default logger
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	ctx := core.WithInvokeSession(context.Background(), 42)
	if core.InvokeSessionFromContext(ctx) != nil {
		t.Error("a non-pointer key was stamped")
	}
	if !strings.Contains(buf.String(), "key_dropped") {
		t.Errorf("log %q, want the dropped key", buf.String())
	}
}
