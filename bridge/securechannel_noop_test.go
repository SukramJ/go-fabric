// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for noopPaseHandler, noopCaseHandler, and
// resolveCaseHandler. Lives in package bridge to access unexported types.

import (
	"errors"
	"testing"
)

// ─── noopPaseHandler ──────────────────────────────────────────────────────────

func TestNoopPaseHandler_ProcessPBKDFParamRequest(t *testing.T) {
	t.Parallel()
	h := noopPaseHandler{}
	_, _, err := h.ProcessPBKDFParamRequest(nil)
	if !errors.Is(err, ErrPaseHandlerMissing) {
		t.Errorf("expected ErrPaseHandlerMissing, got %v", err)
	}
}

func TestNoopPaseHandler_ProcessPake1(t *testing.T) {
	t.Parallel()
	h := noopPaseHandler{}
	_, _, err := h.ProcessPake1(nil)
	if !errors.Is(err, ErrPaseHandlerMissing) {
		t.Errorf("expected ErrPaseHandlerMissing, got %v", err)
	}
}

func TestNoopPaseHandler_ProcessPake3(t *testing.T) {
	t.Parallel()
	h := noopPaseHandler{}
	_, _, err := h.ProcessPake3(nil)
	if !errors.Is(err, ErrPaseHandlerMissing) {
		t.Errorf("expected ErrPaseHandlerMissing, got %v", err)
	}
}

// ─── noopCaseHandler ──────────────────────────────────────────────────────────

func TestNoopCaseHandler_ProcessSigma1(t *testing.T) {
	t.Parallel()
	h := noopCaseHandler{}
	_, _, err := h.ProcessSigma1(nil)
	if !errors.Is(err, ErrCaseHandlerMissing) {
		t.Errorf("expected ErrCaseHandlerMissing, got %v", err)
	}
}

func TestNoopCaseHandler_ProcessSigma3(t *testing.T) {
	t.Parallel()
	h := noopCaseHandler{}
	_, _, err := h.ProcessSigma3(nil)
	if !errors.Is(err, ErrCaseHandlerMissing) {
		t.Errorf("expected ErrCaseHandlerMissing, got %v", err)
	}
}

func TestNoopCaseHandler_ProcessSigma2Resume(t *testing.T) {
	t.Parallel()
	h := noopCaseHandler{}
	_, _, err := h.ProcessSigma2Resume(nil)
	if !errors.Is(err, ErrCaseHandlerMissing) {
		t.Errorf("expected ErrCaseHandlerMissing, got %v", err)
	}
}

// ─── resolveCaseHandler ───────────────────────────────────────────────────────

func TestResolveCaseHandler_NoProvider_ReturnsSingleton(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	singleton := &recordingCaseHandler{}
	b.AttachCaseHandler(singleton)
	h := b.resolveCaseHandler(1)
	if h != singleton {
		t.Errorf("expected singleton, got %T", h)
	}
}

func TestResolveCaseHandler_ProviderOverridesSingleton(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	singleton := &recordingCaseHandler{}
	b.AttachCaseHandler(singleton)
	fromProvider := &recordingCaseHandler{}
	b.AttachCaseHandlerProvider(func(_ uint16) CaseHandler { return fromProvider })
	h := b.resolveCaseHandler(1)
	if h != fromProvider {
		t.Errorf("expected provider result, got %T", h)
	}
}

func TestResolveCaseHandler_ProviderReturningNilFallsBack(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	singleton := &recordingCaseHandler{}
	b.AttachCaseHandler(singleton)
	b.AttachCaseHandlerProvider(func(_ uint16) CaseHandler { return nil })
	h := b.resolveCaseHandler(1)
	if h != singleton {
		t.Errorf("nil provider result: expected singleton fallback, got %T", h)
	}
}
