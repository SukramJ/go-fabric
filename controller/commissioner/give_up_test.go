// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
)

// TestCommissionGivenUpBeforeTheFlowFreesThePASESession: a commissioning
// that gives up after PASE and before its flow runs — the node id claim
// refused, another candidate having won — expires the fail-safe the
// device armed with the PASE session, so the device ends that session
// and the next PASE is accepted at once; the device
// accepts no new PASE exchange while a PASE session exists that is not
// closing (matter.js packages/protocol/src/session/pase/PaseServer.ts:80-82
// at db021da0).
func TestCommissionGivenUpBeforeTheFlowFreesThePASESession(t *testing.T) {
	t.Parallel()
	n, addr := startNode(t, nil, "127.0.0.1:0")
	c := newController(t, discovery.NewNames(newFakeMDNS(), discard()), "127.0.0.1:0")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := c.com.Commission(ctx, Options{
		Fabric: c.fabric, Passcode: nodePasscode, Addresses: []*net.UDPAddr{addr},
		ClaimNodeIDAfterPASE: func() (uint64, bool) { return 0, false },
	})
	if !errors.Is(err, ErrCommissioning) {
		t.Fatalf("Commission with the claim refused: %v", err)
	}
	if n.Root().GeneralCommissioning.FailSafeArmed() {
		t.Fatal("the fail-safe armed with the PASE session is still armed")
	}
	if _, _, err := c.com.EstablishPASE(ctx, PASEOptions{Addresses: []*net.UDPAddr{addr}, Passcode: nodePasscode}); err != nil {
		t.Fatalf("the next PASE: %v", err)
	}
}
