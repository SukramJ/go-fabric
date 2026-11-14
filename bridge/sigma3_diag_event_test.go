// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestSigma3RecordsACompletedHandshakeOnlyWhenOneCompleted: the diagnostic
// event "a controller completed a secure-session handshake" is recorded for
// a Sigma3 the handler answers with the success StatusReport, and not for
// one it refuses with a failure StatusReport — [CaseAdapter] hands a
// refusal back as a reply, not as an error.
func TestSigma3RecordsACompletedHandshakeOnlyWhenOneCompleted(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		handler CaseHandler
		want    int
	}{
		{"a Sigma3 that verifies", sigma2ThenSuccessHandler{}, 1},
		{"a Sigma3 the handler refuses", sigma2ThenRefusalHandler{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newUnsecuredRig(t)
			ring := diagevent.NewRing(16)
			r.b.AttachDiagnosticEvents(ring)
			r.b.AttachCaseHandler(tc.handler)
			hdr, proto := r.request(mrp.SCOpcodeSigma1, 0x5009)
			if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err != nil {
				t.Fatalf("Sigma1: %v", err)
			}
			hdr, proto = r.request(mrp.SCOpcodeSigma3, 0x5009)
			if err := r.b.dispatchSecureChannel(r.peer.LocalAddr(), hdr, proto, []byte{0x15, 0x18}); err != nil {
				t.Fatalf("Sigma3: %v", err)
			}
			got := 0
			for _, e := range ring.Snapshot() {
				if e.Kind == diagevent.KindSession && e.Message == "A controller completed a secure-session handshake with the bridge." {
					got++
				}
			}
			if got != tc.want {
				t.Errorf("completed-handshake events recorded: %d, want %d", got, tc.want)
			}
		})
	}
}
