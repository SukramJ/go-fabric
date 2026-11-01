// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TestProviderPortStatusError (#356): a ProviderPort's contract.StatusError
// — the facade's way to carry a status, contract/status.go — is the
// command's answer, by value or by pointer, as an im.StatusCodeError is;
// any other error answers FAILURE. A relay that cannot produce an offer of
// its own refuses SolicitOffer with INVALID_IN_STATE.
func TestProviderPortStatusError(t *testing.T) {
	ctx := peer(1, 0x55)
	for _, tc := range []struct {
		name   string
		err    error
		status im.StatusCode
	}{
		{"host status by value", contract.StatusError{Status: uint8(im.StatusInvalidInState), Message: "the relay takes the client's offer only"}, im.StatusInvalidInState},
		{"host status by pointer", &contract.StatusError{Status: uint8(im.StatusBusy)}, im.StatusBusy},
		{"IM status", spec.Errorf(im.StatusResourceExhausted, "no slot"), im.StatusResourceExhausted},
		{"plain error", errors.New("relay down"), im.StatusFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, m := newProvider(t, cameraStreams())
			m.err = tc.err
			_, err := p.MatterInvoke(ctx, wpdef.CmdSolicitOffer, solicit(wpdef.StreamUsageLiveView))
			if got := spec.StatusOf(err); got != tc.status {
				t.Fatalf("SolicitOffer: %v, status %s, want %s", err, got, tc.status)
			}
			u, ep := wpdef.StreamUsageLiveView, uint16(1)
			offer := wpdef.ProvideOfferRequest{WebRtcSessionId: spec.NullOf[uint16](), Sdp: "v=0", StreamUsage: &u, OriginatingEndpointId: &ep, VideoStreamId: nnull()}
			_, err = p.MatterInvoke(ctx, wpdef.CmdProvideOffer, offer)
			if got := spec.StatusOf(err); got != tc.status {
				t.Fatalf("ProvideOffer: %v, status %s, want %s", err, got, tc.status)
			}
			if n := len(p.Sessions()); n != 0 {
				t.Fatalf("%d sessions stored after the port refused", n)
			}
		})
	}
	// camera.ErrPayloadTooLarge is one value with bridge's: the node's
	// PeerSession raises it, a camera host recognises it here.
	if camera.ErrPayloadTooLarge == nil || !errors.Is(camera.ErrPayloadTooLarge, camera.ErrPayloadTooLarge) {
		t.Fatal("camera.ErrPayloadTooLarge is not a sentinel")
	}
}
