// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for EncodeStatusResponse and debugReplyError.
// Lives in package bridge to access the functions directly.

import (
	"bytes"
	"errors"
	"log/slog"
	"net"
	"testing"

	"thingfabric.dev/go-fabric/im"
)

// ─── debugReplyError ─────────────────────────────────────────────────────────

func TestDebugReplyError_NilError_IsNoop(t *testing.T) {
	t.Parallel()
	// nil error → must not log, must not panic (review II, R2-P5-tassert-05).
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	src := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
	debugReplyError(logger, "test-stage", src, nil)
	if buf.Len() != 0 {
		t.Fatalf("a nil error logged %q", buf.String())
	}
	// The same logger records a real error, so the empty buffer above is
	// the nil branch, not a silent handler.
	debugReplyError(logger, "test-stage", src, errors.New("boom"))
	if buf.Len() == 0 {
		t.Fatal("a non-nil error logged nothing at debug level")
	}
}

func TestDebugReplyError_NonNilError_DoesNotPanic(t *testing.T) {
	t.Parallel()
	logger := slog.Default()
	src := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540}
	debugReplyError(logger, "encrypt", src, errors.New("test error"))
}

// ─── EncodeStatusResponse ─────────────────────────────────────────────────────

func TestEncodeStatusResponse_Success(t *testing.T) {
	t.Parallel()
	b, err := EncodeStatusResponse(im.StatusResponse{Status: im.StatusSuccess})
	if err != nil {
		t.Fatalf("EncodeStatusResponse Success: %v", err)
	}
	if len(b) == 0 {
		t.Error("EncodeStatusResponse: returned empty bytes for Success")
	}
}

func TestEncodeStatusResponse_Failure(t *testing.T) {
	t.Parallel()
	b, err := EncodeStatusResponse(im.StatusResponse{Status: im.StatusConstraintError})
	if err != nil {
		t.Fatalf("EncodeStatusResponse ConstraintError: %v", err)
	}
	if len(b) == 0 {
		t.Error("EncodeStatusResponse: returned empty bytes for ConstraintError")
	}
	// Success and ConstraintError must produce different encodings.
	bSuccess, _ := EncodeStatusResponse(im.StatusResponse{Status: im.StatusSuccess})
	if len(bSuccess) == len(b) {
		same := true
		for i := range b {
			if b[i] != bSuccess[i] {
				same = false
				break
			}
		}
		if same {
			t.Error("EncodeStatusResponse: Success and ConstraintError produced identical bytes")
		}
	}
}
