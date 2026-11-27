// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/bridge/bridgetest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/mdns"
)

// TestRevokeClosesTheFacadePASESessionWithCloseSession: on the node facade,
// which wires no PaseSessionCloser of its own, a revoke over CASE closes an
// un-adopted PASE session through the bridge — by id, with a CloseSession
// the commissioner receives — not silently. matter.js's revoke closes the
// PASE session gracefully
// (packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:143-147
// at db91886c, `paseSession.initiateClose()`), and its gracefulClose is
// answered with a CloseSession
// (packages/protocol/src/protocol/ExchangeManager.ts:790-800 at db91886c).
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
	// closed, so a call on it fails with ErrSessionClosed — at once, or as
	// soon as the CloseSession arrives — and never answers after a silent
	// close, which leaves the call to its deadline.
	basic := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	rctx, rcancel := context.WithTimeout(ctx, 10*time.Second)
	defer rcancel()
	if _, err := s.Read(rctx, basic); !errors.Is(err, bridgetest.ErrSessionClosed) {
		t.Fatalf("Read on the commissioner's PASE session after the revoke: %v, want %v (closed by a CloseSession)", err, bridgetest.ErrSessionClosed)
	}
}
