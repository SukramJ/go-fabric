// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// handoff is what the initiating commissioner's FinalizeCommissioning
// hands over.
type handoff struct {
	addr   handshake.PeerAddress
	dd     *discovery.Device
	params *sessionparams.Parameters
}

// initiate runs the commissioner up to its operational hand-off — PASE,
// attestation, the NOC — and returns what FinalizeCommissioning was
// handed; neither CASE nor CommissioningComplete follows.
func initiate(t *testing.T, site *fabrictest.Site, u *fabrictest.Pair, nodeID uint64) handoff {
	t.Helper()
	var h handoff
	err := site.Run(parityCtx(t), func(ctx context.Context) error {
		_, err := u.Controller.Commissioner().Commission(ctx, commissioner.Options{
			Fabric: u.Controller.Fabric(), Passcode: u.Device.Passcode(), NodeID: nodeID, Addresses: []*net.UDPAddr{u.Device.Addr()},
			FinalizeCommissioning: func(_ context.Context, addr handshake.PeerAddress, dd *discovery.Device, params *sessionparams.Parameters) error {
				h = handoff{addr, dd, params}
				return nil
			},
		})
		return err
	})
	if err != nil {
		t.Fatalf("commissioning up to the hand-off: %v", err)
	}
	return h
}

// TestSplitCommissioningParity ports matter.js's SplitCommissioningTest
// (packages/node/test/node/SplitCommissioningTest.ts at 862ad75f) onto
// Registry.CompleteCommissioning (Peers.ts:236-359). matter.js runs the
// two halves on two controllers that share a fabric; fabrictest gives each
// controller its own CA and fabric, so the port runs both halves on one
// controller — the commissioner's FinalizeCommissioning hands off, the
// registry completes — which exercises the same completion.
func TestSplitCommissioningParity(t *testing.T) {
	t.Parallel()

	t.Run("completes a PASE-only commissioning another commissioner started", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		u := site.UncommissionedPair(fabrictest.PairOptions{})
		h := initiate(t, site, u, 0x4242)
		reg := u.Controller.Registry()
		var n *client.Node
		err := site.Run(parityCtx(t), func(ctx context.Context) (err error) {
			n, err = reg.CompleteCommissioning(ctx, h.addr.NodeID, client.CompleteOptions{DiscoveryData: h.dd, SessionParameters: h.params})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if !n.IsCommissioned() || n.Record().CommissionedAt.IsZero() {
			t.Fatal("the completed node is not commissioned")
		}
		if err := site.Eventually(parityCtx(t), "the default subscription", n.SubscriptionActive); err != nil {
			t.Fatal(err)
		}
		if u.Device.Node().Root().GeneralCommissioning.FailSafeArmed() {
			t.Fatal("the device's fail-safe is still armed")
		}
		// A second completion for the committed peer is refused and leaves it.
		err = site.Run(parityCtx(t), func(ctx context.Context) error {
			_, err := reg.CompleteCommissioning(ctx, h.addr.NodeID, client.CompleteOptions{})
			return err
		})
		if !errors.Is(err, client.ErrAlreadyCommissioned) || reg.Get(n.ID()) != n {
			t.Fatalf("second completion: %v", err)
		}
	})

	t.Run("leaves no phantom peer when the node cannot be reached", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		u := site.UncommissionedPair(fabrictest.PairOptions{})
		reg := u.Controller.Registry()
		before := reg.Size()
		ctx, cancel := context.WithTimeout(parityCtx(t), 0)
		defer cancel()
		_, err := reg.CompleteCommissioning(ctx, 0xDEAD, client.CompleteOptions{})
		if err == nil {
			t.Fatal("an unreachable node completed")
		}
		if reg.Size() != before {
			t.Fatalf("%d nodes after the failure, want %d", reg.Size(), before)
		}
	})

	t.Run("rejects a fabric the registry does not hold", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		u := site.UncommissionedPair(fabrictest.PairOptions{})
		other := site.UncommissionedPair(fabrictest.PairOptions{})
		_, err := u.Controller.Registry().CompleteCommissioning(parityCtx(t), 0xDEAD, client.CompleteOptions{Fabric: other.Controller.Fabric()})
		if !errors.Is(err, client.ErrForeignFabric) {
			t.Fatalf("a foreign fabric: %v", err)
		}
	})
}
