// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
)

// TestRevokeClosesTheFacadePASESessionWithCloseSession: on the node facade,
// which wires no PaseSessionCloser of its own, a revoke over CASE closes an
// un-adopted PASE session through the bridge — by id, with a CloseSession
// the commissioner receives — not silently. matter.js's revoke closes the
// PASE session gracefully
// (packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:143-147
// at b25ed960, `paseSession.initiateClose()`), and its gracefulClose is
// answered with a CloseSession
// (packages/protocol/src/protocol/ExchangeManager.ts:790-800 at b25ed960).
// The deferral while an exchange is in flight is held at the bridge
// (bridge:TestRevokeClosesThePASESessionWhenItIsIdle).
func TestRevokeClosesTheFacadePASESessionWithCloseSession(t *testing.T) {
	t.Parallel()
	n := startedNode(t, mdns.NewNoop())
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := c.PASE(ctx)
	if err != nil {
		t.Fatalf("PASE: %v", err)
	}
	if got := paseSessions(n); got != 1 {
		t.Fatalf("PASE sessions after the handshake = %d, want 1", got)
	}
	// A revoke over CASE (no interaction on the PASE session), the PASE
	// session idle: the close runs at once.
	if err := n.Bridge().CommissioningWindow().RevokeWindow(context.Background()); err != nil {
		t.Fatalf("RevokeWindow: %v", err)
	}
	if got := paseSessions(n); got != 0 {
		t.Fatalf("PASE sessions after the revoke = %d, want 0", got)
	}
	// The commissioner learns it from the CloseSession: its session is
	// closed, and a call on it fails with ErrSessionClosed instead of
	// running into its timeout as after a silent close.
	basic := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rctx, rcancel := context.WithTimeout(ctx, 300*time.Millisecond)
		_, err := s.Read(rctx, basic)
		rcancel()
		if errors.Is(err, bridgetest.ErrSessionClosed) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the commissioner's PASE session is not closed by a CloseSession: last Read error %v", err)
		}
	}
}
