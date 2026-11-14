// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
)

// sendDuringClose runs a raw ProvideOffer whose response arrives while
// Close runs: the camera's answer is held until during, called with the
// service, has returned. It returns Send's error.
func sendDuringClose(t *testing.T, f *psFixture, during func()) error {
	t.Helper()
	f.respond = func(command string) (any, error) {
		if command == callProvideOffer {
			during()
		}
		return wpdef.ProvideOfferResponse{WebRtcSessionId: 9}, nil
	}
	p := &Provider{s: f.svc, t: target{psCamera, psEndpoint}}
	payload, _ := json.Marshal(map[string]any{"webRtcSessionId": nil, "sdp": psOffer, "streamUsage": 3, "videoStreams": []int{1}})
	_, _, err := p.Send(t.Context(), "ProvideOffer", payload)
	return err
}

// TestProviderOfferAnsweredDuringCloseIsEnded (review II, R2-P2-stop-03):
// a raw ProvideOffer that passed Send's closed check before Close began,
// and whose response arrives after, registers no session with the
// requestor: it ends the camera's session (EndSession) and returns
// ErrClosed, and Close waits for it. Before, the session was registered
// after Close's sweep and nothing ended it.
func TestProviderOfferAnsweredDuringCloseIsEnded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		f := newPSFixture()
		closed := make(chan error, 1)
		err := sendDuringClose(t, f, func() {
			go func() { closed <- f.svc.Close(context.Background()) }()
			synctest.Wait()
		})
		if !errors.Is(err, ErrClosed) {
			t.Errorf("Send answered during Close = %v, want ErrClosed", err)
		}
		if err := <-closed; err != nil {
			t.Fatalf("Close: %v", err)
		}
		if got := f.sessions(); len(got) != 0 {
			t.Errorf("requestor after Close holds %+v, want nothing", got)
		}
		end := callEndSession + `:5/1:{"webRtcSessionId":9,`
		if !slices.ContainsFunc(f.callsNow(), func(c string) bool { return strings.HasPrefix(c, end) }) {
			t.Errorf("calls %q: the camera's session 9 was not ended", f.callsNow())
		}
	})
}

// TestSecondCloseWaitsForTheFirst (review II, R2-P2-stop-07): a second
// Close returns only once the first has ended its sessions — here, once
// the raw offer in flight has ended its own — and a second Close whose ctx
// ends first returns ctx's error. Before, the second Close returned nil at
// once.
func TestSecondCloseWaitsForTheFirst(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		f := newPSFixture()
		first, second := make(chan error, 1), make(chan error, 1)
		_ = sendDuringClose(t, f, func() {
			go func() { first <- f.svc.Close(context.Background()) }()
			synctest.Wait()
			go func() { second <- f.svc.Close(context.Background()) }()
			synctest.Wait()
			select {
			case err := <-second:
				t.Errorf("the second Close returned (%v) while the first was ending a session", err)
			default:
			}
			gone, cancel := context.WithCancel(context.Background())
			cancel()
			if err := f.svc.Close(gone); !errors.Is(err, context.Canceled) {
				t.Errorf("a second Close with an ended ctx = %v, want context.Canceled", err)
			}
		})
		if err := <-first; err != nil {
			t.Fatalf("first Close: %v", err)
		}
		if err := <-second; err != nil {
			t.Fatalf("second Close: %v", err)
		}
	})
}

// TestProviderReOfferAnsweredDuringCloseAddsNothing (review II, rework of
// R2-P2-stop-03): a raw ProvideOffer naming a session the camera holds —
// one the requestor does not track — whose response arrives once Close
// began adds no requestor entry and returns ErrClosed. A re-offer never
// ends a session, so none is ended. Before, the entry was added after
// Close's sweep and nothing removed it.
func TestProviderReOfferAnsweredDuringCloseAddsNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		f := newPSFixture()
		closed := make(chan error, 1)
		f.respond = func(command string) (any, error) {
			if command == callProvideOffer {
				go func() { closed <- f.svc.Close(context.Background()) }()
				synctest.Wait()
			}
			return wpdef.ProvideOfferResponse{WebRtcSessionId: 9}, nil
		}
		p := &Provider{s: f.svc, t: target{psCamera, psEndpoint}}
		payload, _ := json.Marshal(map[string]any{"webRtcSessionId": 9, "sdp": psOffer, "streamUsage": 3, "videoStreams": []int{1}})
		if _, _, err := p.Send(t.Context(), "ProvideOffer", payload); !errors.Is(err, ErrClosed) {
			t.Errorf("re-offer answered during Close = %v, want ErrClosed", err)
		}
		if err := <-closed; err != nil {
			t.Fatalf("Close: %v", err)
		}
		if got := f.sessions(); len(got) != 0 {
			t.Errorf("requestor after Close holds %+v, want nothing", got)
		}
	})
}
