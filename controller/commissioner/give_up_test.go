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
	"github.com/SukramJ/go-fabric/client/exchange"
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

// TestEstablishPASERefusedAfterPASEFreesThePASESession: an EstablishPASE
// whose ContinueAfterPASE refuses the session — another candidate having
// been selected — expires the fail-safe the device armed with it, so the
// device ends that session and the next PASE is accepted at once.
func TestEstablishPASERefusedAfterPASEFreesThePASESession(t *testing.T) {
	t.Parallel()
	n, addr := startNode(t, nil, "127.0.0.1:0")
	c := newController(t, discovery.NewNames(newFakeMDNS(), discard()), "127.0.0.1:0")
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	_, _, err := c.com.EstablishPASE(ctx, PASEOptions{
		Addresses: []*net.UDPAddr{addr}, Passcode: nodePasscode,
		ContinueAfterPASE: func() bool { return false },
	})
	if !errors.Is(err, ErrCommissioning) {
		t.Fatalf("EstablishPASE with ContinueAfterPASE refusing: %v", err)
	}
	if n.Root().GeneralCommissioning.FailSafeArmed() {
		t.Fatal("the fail-safe armed with the PASE session is still armed")
	}
	if _, _, err := c.com.EstablishPASE(ctx, PASEOptions{Addresses: []*net.UDPAddr{addr}, Passcode: nodePasscode}); err != nil {
		t.Fatalf("the next PASE: %v", err)
	}
}

// TestPASESessionThatLostTheRaceIsFreed: a PASE session established on one
// candidate after another candidate won the connection is given up with
// the fail-safe its device armed expired, so that device ends the session
// and accepts the next PASE at once. The first candidate's session is held
// back until the second has won.
func TestPASESessionThatLostTheRaceIsFreed(t *testing.T) {
	t.Parallel()
	loserNode, loserAddr := startNode(t, nil, "127.0.0.1:0")
	_, winnerAddr := startNode(t, nil, "127.0.0.1:0")
	c := newController(t, discovery.NewNames(newFakeMDNS(), discard()), "127.0.0.1:0")
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	loser := &candidate{identifier: "loser", addrs: []*net.UDPAddr{loserAddr}}
	winner := &candidate{identifier: "winner", addrs: []*net.UDPAddr{winnerAddr}}
	loserEstablished := make(chan struct{})
	var loserErr error
	var delay time.Duration
	res, err := connectPASE(ctx, connectionConfig{
		devices: []*candidate{loser, winner},
		timeout: 30 * time.Second,
		delay:   &delay,
		clock:   c.com.ctx.Clock,
		log:     discard(),
		giveUp:  c.com.giveUpPASE(ctx),
		establish: func(actx context.Context, addr *net.UDPAddr, cand *candidate, onPBKDF func()) (*exchange.Session, error) {
			if cand == winner {
				select {
				case <-loserEstablished:
				case <-actx.Done():
					return nil, context.Cause(actx)
				}
			}
			s, err := c.com.establishEphemeralNodeSession(actx, addr, nodePasscode, cand, true, onPBKDF)
			if cand == loser {
				loserErr = err
				close(loserEstablished)
				if err == nil {
					<-actx.Done()
				}
			}
			return s, err
		},
	})
	if err != nil {
		t.Fatalf("connectPASE: %v", err)
	}
	if res.device != winner {
		t.Fatalf("the connection was won by %q, want the second candidate", res.device.identifier)
	}
	if loserErr != nil {
		t.Fatalf("the first candidate's PASE: %v", loserErr)
	}
	eventually(t, "the losing device's fail-safe to be expired", func() bool {
		return !loserNode.Root().GeneralCommissioning.FailSafeArmed()
	})
	if _, _, err := c.com.EstablishPASE(ctx, PASEOptions{Addresses: []*net.UDPAddr{loserAddr}, Passcode: nodePasscode}); err != nil {
		t.Fatalf("the next PASE to the device whose session lost: %v", err)
	}
}
