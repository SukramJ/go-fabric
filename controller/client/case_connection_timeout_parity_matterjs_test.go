// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/CaseConnectionTimeoutTest.ts
// at 862ad75f: the controller's budget for the operational CASE
// connection after AddNOC — commissioning fails when it expires and the
// peer is deleted, and a non-finite budget is refused.
//
// matter.js drops the device's Sigma1 at its router; the port drops it on
// fabrictest's network (an unsecured secure-channel message with opcode
// Sigma1 to the device). matter.js's PeerUnreachableError is
// *peer.UnreachableError, its ImplementationError the commissioner's
// error for a negative (Go's "forever") budget.
package client_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/transport/message"
)

// sigma1 is the secure channel's Sigma1 opcode.
const sigma1 = 0x30

// dropSigma1To drops every Sigma1 addressed to d — CaseConnectionTimeoutTest.ts:38-65.
func dropSigma1To(site *fabrictest.Site, d *fabrictest.Device) {
	dev := d.Addr().String()
	site.Net().Filter(func(dg fabrictest.Datagram) bool {
		if dg.To.String() != dev {
			return true
		}
		h, n, err := message.UnmarshalHeader(dg.Payload)
		if err != nil || h.SessionID != 0 {
			return true
		}
		ph, _, err := message.UnmarshalProtocolHeader(dg.Payload[n:])
		return err != nil || ph.ProtocolID != 0x0000 || ph.Opcode != sigma1
	})
}

func TestCaseConnectionTimeoutParity(t *testing.T) {
	t.Parallel()

	t.Run("fails commissioning when the budget expires and deletes the peer", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		p := site.UncommissionedPair(fabrictest.PairOptions{})
		dropSigma1To(site, p.Device)
		budget := 20 * time.Second
		o := client.CommissionOptions{}
		o.CASEConnectionTimeout = budget
		start := site.Clock().Now()
		_, err := p.Controller.CommissionNode(parityCtx(t), p.Device, o)
		if err == nil {
			t.Fatal("commissioning succeeded without CASE")
		}
		if elapsed := site.Clock().Now().Sub(start); elapsed >= 60*time.Second {
			t.Fatalf("commissioning failed after %v, want under 60 s", elapsed)
		}
		var ue *peer.UnreachableError
		if !errors.As(err, &ue) || !strings.Contains(ue.Error(), budget.String()) {
			t.Fatalf("commission: %v, want an UnreachableError naming %v", err, budget)
		}
		if n := p.Controller.Peers().Size(); n != 0 {
			t.Fatalf("peer set size %d, want 0", n)
		}
	})

	t.Run("refuses a non-finite budget", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		p := site.UncommissionedPair(fabrictest.PairOptions{})
		_, err := p.Controller.Commission(parityCtx(t), p.Device, fabrictest.CommissionOptions{Edit: func(o *commissioner.Options) {
			o.CASEConnectionTimeout = -1
		}})
		if err == nil || !strings.Contains(err.Error(), "CASEConnectionTimeout must be finite") {
			t.Fatalf("commission with a non-finite budget: %v", err)
		}
	})
}
