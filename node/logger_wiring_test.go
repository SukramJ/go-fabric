// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// debugLog records the messages of every level.
type debugLog struct {
	mu   sync.Mutex
	msgs []string
}

func (h *debugLog) Enabled(context.Context, slog.Level) bool { return true }

func (h *debugLog) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.msgs = append(h.msgs, r.Message)
	h.mu.Unlock()
	return nil
}
func (h *debugLog) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *debugLog) WithGroup(string) slog.Handler      { return h }

func (h *debugLog) contains(sub string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

// TestNodeLoggerReachesTheRootClusters: node.Config.Logger is the logger of
// the root's BasicInformation (its construction-time validator warns about a
// config that would show an unknown vendor) and of GeneralDiagnostics (the
// BootReason line at Start) — both used to log through slog.Default().
func TestNodeLoggerReachesTheRootClusters(t *testing.T) {
	t.Parallel()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	log := &debugLog{}
	cfg.Logger = slog.New(log)
	// An empty VendorName is what BasicInformation's validator reports.
	cfg.BasicInformation.VendorName = ""
	n := newNode(t, cfg)
	if !log.contains("matter.basic_information.validate") {
		t.Error("BasicInformation's validator did not log through node.Config.Logger")
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !log.contains("matter.general_diagnostics.emit_bootreason") {
		t.Error("GeneralDiagnostics did not log through node.Config.Logger")
	}
}
