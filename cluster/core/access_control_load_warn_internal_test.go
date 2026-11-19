// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// failingSettings is an ACL store whose every settings read fails.
type failingSettings struct {
	ACLStoreFacade
	fail bool
}

func (s *failingSettings) GetSetting(context.Context, string) (value string, ok bool, err error) {
	if s.fail {
		return "", false, errors.New("busy")
	}
	return "", false, nil
}

func (*failingSettings) SetSetting(context.Context, string, string) error { return nil }

// countingHandler counts the records of one message.
type countingHandler struct {
	mu    sync.Mutex
	msg   string
	count int
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *countingHandler) Handle(_ context.Context, r slog.Record) error {
	if strings.Contains(r.Message, h.msg) {
		h.mu.Lock()
		h.count++
		h.mu.Unlock()
	}
	return nil
}
func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }

// A settings store that fails at construction logs one warning, not one per
// probed fabric index; every fabric keeps its own load-error record (review
// II, R2-P7-acl-05).
func TestExtensionLoadFailureWarnsOnce(t *testing.T) {
	t.Parallel()
	st := &failingSettings{}
	a, err := NewAccessControl(st)
	if err != nil {
		t.Fatal(err)
	}
	h := &countingHandler{msg: "matter.access_control.extension_load_failed"}
	a.SetLogger(slog.New(h))
	st.fail = true
	a.loadExtensions(context.Background())
	if h.count != 1 {
		t.Errorf("%d warnings for a failing settings store, want 1", h.count)
	}
	a.mu.Lock()
	n := len(a.extLoadErr)
	a.mu.Unlock()
	if n != 254 {
		t.Errorf("%d load-error records, want 254", n)
	}
}
