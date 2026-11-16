// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tlv

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestLoggerFollowsSlogSetDefault holds R2-P6-api-0224: a host that calls
// slog.SetDefault after the package initialised (the usual order) gets the
// codec's diagnostics through its new default handler, at Warn — and a
// Logger the host set explicitly still wins. Not parallel: it swaps the
// process-wide slog default.
func TestLoggerFollowsSlogSetDefault(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	var host bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&host, &slog.HandlerOptions{Level: slog.LevelWarn})))

	enc := NewEncoder()
	enc.PutUTF8WithMax(AnonymousTag(), "abcdef", 3)
	if out := host.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "tlv.utf8_trimmed") {
		t.Fatalf("host default handler got %q, want a WARN tlv.utf8_trimmed record", out)
	}

	var own bytes.Buffer
	saved := Logger
	t.Cleanup(func() { Logger = saved })
	Logger = slog.New(slog.NewTextHandler(&own, nil))
	host.Reset()
	NewEncoder().PutUTF8WithMax(AnonymousTag(), "abcdef", 3)
	if !strings.Contains(own.String(), "tlv.utf8_trimmed") || host.Len() != 0 {
		t.Fatalf("explicit Logger: own %q, default %q; want the record on the explicit Logger only", own.String(), host.String())
	}

	Logger = nil
	host.Reset()
	NewEncoder().PutUTF8WithMax(AnonymousTag(), "abcdef", 3)
	if !strings.Contains(host.String(), "tlv.utf8_trimmed") {
		t.Fatalf("nil Logger: default handler got %q, want the record", host.String())
	}
}
