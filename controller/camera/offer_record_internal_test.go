// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"errors"
	"testing"

	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
)

// TestRefusedReOfferRestoresTheOffer (review II, R2-P6-wire-2204): a
// re-offer the camera refuses restores the session's earlier recorded
// offer, so a later Answer is still narrowed to it; a withdraw whose offer
// was replaced since changes nothing, and one with no earlier offer drops
// the key. Before, the refusal deleted the key and later Answers passed
// un-narrowed.
func TestRefusedReOfferRestoresTheOffer(t *testing.T) {
	t.Parallel()
	f := newPSFixture()
	if _, err := f.establish(t, psEndpoint, psOfferRequest(nil)); err != nil {
		t.Fatalf("establish: %v", err)
	}
	k := f.svc.ps.key(target{psCamera, psEndpoint}, 9)
	f.respond = func(string) (any, error) { return nil, errors.New("refused") }
	re := psOfferRequest(func(r *wpdef.ProvideOfferRequest) {
		r.WebRtcSessionId.Null, r.WebRtcSessionId.Value = false, 9
		r.Sdp = psAnswer
	})
	if _, err := f.establish(t, psEndpoint, re); err == nil {
		t.Fatal("the refused re-offer succeeded")
	}
	if sdp, ok := f.svc.ps.offerOf(k); !ok || sdp != psOffer {
		t.Errorf("recorded offer after the refused re-offer = %q, %v; want the session's offer", sdp, ok)
	}
	if got := f.answerSDP(trackedSession(nil)); got != psNarrowed {
		t.Errorf("Answer after the refused re-offer = %q, want it narrowed to the session's offer", got)
	}

	// The same SDP sent twice: each withdraw acts on its own send.
	ps := f.svc.ps
	other := ps.key(target{psCamera, psEndpoint}, 10)
	first := ps.offerSent(other, psOffer)
	second := ps.offerSent(other, psOffer)
	first()
	if sdp, ok := ps.offerOf(other); !ok || sdp != psOffer {
		t.Errorf("an older send's withdraw changed the newer record: %q, %v", sdp, ok)
	}
	second()
	if sdp, ok := ps.offerOf(other); !ok || sdp != psOffer {
		t.Errorf("the newer send's withdraw = %q, %v; want the older send's record back", sdp, ok)
	}
	none := ps.key(target{psCamera, psEndpoint}, 11)
	ps.offerSent(none, psOffer)()
	if _, ok := ps.offerOf(none); ok {
		t.Error("a withdraw with no earlier offer kept the key")
	}
}
