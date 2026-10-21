// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// A port of matter.js packages/protocol/test/peer/PeerConnectionErrorHandlerTest.ts
// at 862ad75f: its "commissioning NoSharedTrustRoots handler" group, against
// the commissioner's CASE error handler (the other groups of the file are in
// client/peer). A handler's (delay, true, nil) replaces the peer's default
// delay — the full-flow half of two cases — as TestPeerConnectionErrorHandlerDelayContract
// holds in client/peer; the commissioner cannot be driven through that
// package's harness, so those two cases stay missing.

import (
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

func TestPeerConnectionErrorHandlerNoSharedTrustRoots(t *testing.T) {
	t.Parallel()
	c := &Commissioner{log: discard()}
	statusError := func(code uint16) error {
		return &exchange.StatusReportError{Report: exchange.StatusReport{
			GeneralCode: 1, ProtocolID: uint32(mrp.SecureChannelProtocolID), ProtocolCode: code,
		}}
	}

	t.Run("returns 15s for NoSharedTrustRoots", func(t *testing.T) {
		t.Parallel()
		d, handled, err := c.handleNoSharedTrustRoots(statusError(mrp.SCStatusProtocolNoSharedTrustRoots))
		if err != nil || !handled || d != 15*time.Second { // PeerConnectionErrorHandlerTest.ts:167-175
			t.Fatalf("got (%v, %v, %v), want (15s, true, nil)", d, handled, err)
		}
	})
	t.Run("returns undefined for other ChannelStatusResponseErrors", func(t *testing.T) {
		t.Parallel()
		if d, handled, err := c.handleNoSharedTrustRoots(statusError(mrp.SCStatusProtocolInvalidParameter)); handled || err != nil {
			t.Fatalf("got (%v, %v, %v), want unhandled", d, handled, err) // :177-185
		}
	})
	t.Run("returns undefined for non-ChannelStatusResponseError", func(t *testing.T) {
		t.Parallel()
		if d, handled, err := c.handleNoSharedTrustRoots(errors.New("generic error")); handled || err != nil {
			t.Fatalf("got (%v, %v, %v), want unhandled", d, handled, err) // :187-191
		}
	})
}
