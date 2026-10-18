// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/PeerDescriptorTest.ts at
// 862ad75f, the cases over a commissioned pair: the session parameters the
// controller holds of its peer, kept in the commissioning record and
// restored into the protocol peer after a controller restart. matter.js
// assigns the protocol peer's descriptor fields; the Go descriptor changes
// only through sessions, DNS-SD and AddKnownPeer, so where a case sets a
// value the port sets the record the descriptor follows into
// (client.UpdateRecord) — the record is what the restart restores from.
package client_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

func TestPeerDescriptorParity(t *testing.T) {
	t.Parallel()

	t.Run("stores the intervals of a later session and restores them after a restart", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		addr, _ := cp.node.PeerAddress()
		sii, sai := 500*time.Millisecond, 300*time.Millisecond
		client.UpdateRecord(cp.node, func(r *client.Record) {
			p := *r.ReportedSessionParameters
			p.SessionIdleInterval, p.SessionActiveInterval, p.SessionActiveThreshold = 2000, 400, 5000
			r.ReportedSessionParameters = &p
			r.AdvertisedIntervals = &client.AdvertisedIntervals{IdleInterval: &sii, ActiveInterval: &sai}
		})
		expect := func(what string, p sessionparams.Parameters) {
			t.Helper()
			if p.SessionIdleInterval != 2000 || p.SessionActiveInterval != 400 || p.SessionActiveThreshold != 5000 {
				t.Fatalf("%s: %+v, want idle 2000, active 400, threshold 5000", what, p)
			}
		}
		expect("the record", *cp.node.Record().ReportedSessionParameters)
		id := cp.node.ID()
		if err := cp.pair.Controller.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		restored := cp.pair.Controller.Registry().Get(id)
		if restored == nil || restored.Record().ReportedSessionParameters == nil {
			t.Fatal("no restored record")
		}
		expect("the restored record", *restored.Record().ReportedSessionParameters)
		// The reported parameters win over the advertised 500/300.
		expect("the restored peer", cp.pair.Controller.Peers().Get(addr).SessionParameters())
	})

	t.Run("restores the stored session parameters into the peer before its first session after a restart", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		addr, _ := cp.node.PeerAddress()
		stored := cp.node.Record().ReportedSessionParameters
		if stored == nil || stored.InteractionModelRevision == exchange.PeerParameters(nil).InteractionModelRevision {
			t.Fatalf("stored parameters %+v carry no interaction model revision of the device", stored)
		}
		if err := cp.pair.Controller.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		p := cp.pair.Controller.Peers().Get(addr)
		if p == nil || p.HasSession() {
			t.Fatal("the restored peer has a session")
		}
		got := p.SessionParameters()
		if got.InteractionModelRevision != stored.InteractionModelRevision || got.SpecificationVersion != stored.SpecificationVersion ||
			got.MaxPathsPerInvoke != stored.MaxPathsPerInvoke || got.SupportedTransports != stored.EffectiveTransports() {
			t.Fatalf("restored parameters %+v, want those stored %+v", got, *stored)
		}
	})

	t.Run("hold only what the device sent in its session", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		addr, _ := cp.node.PeerAddress()
		d := cp.pair.Controller.Peers().Get(addr).Descriptor()
		if d.ReportedSessionParameters == nil || d.ReportedSessionParameters.MaxTCPMessageSize != 0 {
			t.Fatalf("reported parameters %+v", d.ReportedSessionParameters)
		}
	})
}
