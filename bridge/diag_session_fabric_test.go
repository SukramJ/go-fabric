// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"thingfabric.dev/go-fabric/diagevent"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// establishedSuccessHandler answers Sigma3 with success and reports the
// session it established, as [CaseAdapter] does.
type establishedSuccessHandler struct {
	sigma2ThenSuccessHandler
	sid uint16
}

func (h establishedSuccessHandler) establishedSession() (uint16, bool) { return h.sid, true }

// sessionEvents are the KindSession events of ring.
func sessionEvents(ring *diagevent.Ring) []diagevent.Event {
	var out []diagevent.Event
	for _, e := range ring.Snapshot() {
		if e.Kind == diagevent.KindSession {
			out = append(out, e)
		}
	}
	return out
}

// TestSessionEventsNameTheirFabric: every session event of the diagnostic
// ring — a completed CASE handshake, a peer's CloseSession, a TCP
// connection's drop — carries the session's fabric in the detail
// "fabric_index", so the DiagnosticLogs a controller retrieves can serve
// its own fabric's peers and leave out the others' (review II,
// R2-C2-sec-2451, owner decision 524). A session whose fabric the bridge
// cannot name carries none.
func TestSessionEventsNameTheirFabric(t *testing.T) {
	t.Parallel()
	const sid, fabric = 0x4242, 3
	table := sessionTable{sid: {fabric, 0x1234}}
	check := func(t *testing.T, ring *diagevent.Ring, want string) {
		t.Helper()
		evs := sessionEvents(ring)
		if len(evs) != 1 {
			t.Fatalf("%d session events, want 1: %+v", len(evs), evs)
		}
		if got := evs[0].Detail["fabric_index"]; got != want {
			t.Errorf("%q: fabric_index %q, want %q", evs[0].Message, got, want)
		}
	}
	for _, known := range []bool{true, false} {
		want, suffix := "3", "/fabric known"
		if !known {
			want, suffix = "", "/fabric unknown"
		}
		t.Run("Sigma3"+suffix, func(t *testing.T) {
			t.Parallel()
			r := newUnsecuredRig(t)
			ring := diagevent.NewRing(16)
			r.b.AttachDiagnosticEvents(ring)
			if known {
				r.b.mu.Lock()
				r.b.sessions = table
				r.b.mu.Unlock()
			}
			r.b.AttachCaseHandler(establishedSuccessHandler{sid: sid})
			hdr, proto := r.request(mrp.SCOpcodeSigma1, 0x5009)
			if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err != nil {
				t.Fatalf("Sigma1: %v", err)
			}
			hdr, proto = r.request(mrp.SCOpcodeSigma3, 0x5009)
			if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err != nil {
				t.Fatalf("Sigma3: %v", err)
			}
			check(t, ring, want)
		})
		t.Run("CloseSession"+suffix, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			ring := diagevent.NewRing(16)
			b.AttachDiagnosticEvents(ring)
			b.AttachSessionRegistry(&fakeSessionRegistry{})
			if known {
				b.mu.Lock()
				b.sessions = table
				b.mu.Unlock()
			}
			hdr := scHdr()
			hdr.SessionID = sid
			body := mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), scStatusProtocolCloseSession, nil)
			if err := b.dispatchSecureChannel(loopbackSrc(), hdr, scProto(mrp.SCOpcodeStatusReport, 3, false, 0), body); err != nil {
				t.Fatalf("CloseSession: %v", err)
			}
			check(t, ring, want)
		})
		t.Run("TCP drop"+suffix, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			ring := diagevent.NewRing(16)
			b.AttachDiagnosticEvents(ring)
			b.AttachSessionRegistry(&fakeSessionRegistry{})
			if known {
				b.mu.Lock()
				b.sessions = table
				b.mu.Unlock()
			}
			b.evictSession(sid, &connRoute{})
			check(t, ring, want)
		})
	}
}
