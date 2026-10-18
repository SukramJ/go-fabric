// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/PeerDescriptorTest.ts at
// 862ad75f, the cases on what the commissioning record stores of the peer's
// descriptor: matter.js sets the protocol peer's descriptor and reads the
// CommissioningClient state that follows it; the port applies the
// descriptor to a client.Record (applyPeer, the registry's follow of every
// descriptor save) and reads it back through its store encoding. The cases
// over a commissioned pair are in peer_descriptor_parity_matterjs_test.go.
package client

import (
	"testing"

	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// storedRecord is rec through its store encoding.
func storedRecord(t *testing.T, rec Record) Record {
	t.Helper()
	values, err := rec.encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := decodeRecord(values)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPeerDescriptorRecordParity(t *testing.T) {
	t.Parallel()

	t.Run("persists negotiated session intervals above the DNS-SD maximum", func(t *testing.T) {
		t.Parallel()
		var rec Record
		rec.applyPeer(peer.Descriptor{ReportedSessionParameters: &sessionparams.Parameters{
			SessionIdleInterval: 3_602_000, SessionActiveInterval: 3_603_000, MaxPathsPerInvoke: 1,
		}})
		got := storedRecord(t, rec).ReportedSessionParameters
		if got == nil || got.SessionIdleInterval != 3_602_000 || got.SessionActiveInterval != 3_603_000 {
			t.Fatalf("stored parameters %+v", got)
		}
	})

	t.Run("persists a vendor-specific advertised device type", func(t *testing.T) {
		t.Parallel()
		var rec Record
		dt := uint32(0xfff10001)
		rec.applyPeer(peer.Descriptor{DiscoveryData: &discovery.TXTData{DT: &dt}})
		if got := storedRecord(t, rec).DeviceType; got == nil || *got != dt {
			t.Fatalf("stored device type %v, want %#x", got, dt)
		}
	})

	t.Run("are stored as reported, without fallbacks", func(t *testing.T) {
		t.Parallel()
		var rec Record
		rec.applyPeer(peer.Descriptor{ReportedSessionParameters: &sessionparams.Parameters{SessionIdleInterval: 2000}})
		got := storedRecord(t, rec).ReportedSessionParameters
		if got == nil || got.SessionIdleInterval != 2000 || got.MaxPathsPerInvoke != 0 {
			t.Fatalf("stored parameters %+v, want the idle interval alone", got)
		}
	})
}
