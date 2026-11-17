// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
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

// TestFirstCloseBoundedByContext (R2-P5-apinode-19, N-life-5): the first
// Close waits for a raw provider command in flight only until its ctx
// ends, returns ctx's error and leaves the service closing; a later Close
// waits for the teardown and returns nil once the command returned.
// Before, the first Close waited on the command with no bound.
func TestFirstCloseBoundedByContext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		f := newPSFixture()
		later := make(chan error, 1)
		err := sendDuringClose(t, f, func() {
			gone, cancel := context.WithCancel(context.Background())
			cancel()
			if err := f.svc.Close(gone); !errors.Is(err, context.Canceled) {
				t.Errorf("the first Close with an ended ctx = %v, want context.Canceled", err)
			}
			go func() { later <- f.svc.Close(context.Background()) }()
			synctest.Wait()
			select {
			case err := <-later:
				t.Errorf("a later Close returned (%v) while the command was in flight", err)
			default:
			}
		})
		if !errors.Is(err, ErrClosed) {
			t.Errorf("Send answered during Close = %v, want ErrClosed", err)
		}
		if err := <-later; err != nil {
			t.Fatalf("the later Close: %v", err)
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

// hookRequestor is the fixture's requestor that runs a hook, once armed,
// at the next call of the method it is armed for.
type hookRequestor struct {
	f                       *psFixture
	mu                      sync.Mutex
	onSessions, beforeWrite func()
}

func (h *hookRequestor) take(hook *func()) func() {
	h.mu.Lock()
	defer h.mu.Unlock()
	fn := *hook
	*hook = nil
	return fn
}

func (h *hookRequestor) endpointID() uint16 { return h.f.endpointID() }

func (h *hookRequestor) sessions() []wrdef.WebRTCSessionStruct {
	if fn := h.take(&h.onSessions); fn != nil {
		fn()
	}
	return h.f.sessions()
}

func (h *hookRequestor) upsert(s wrdef.WebRTCSessionStruct) error {
	if fn := h.take(&h.beforeWrite); fn != nil {
		fn()
	}
	return h.f.upsert(s)
}

func (h *hookRequestor) remove(k wrdef.SessionKey) { h.f.remove(k) }

// closeBegins starts the service's Close and returns once it has begun;
// the returned channel carries its result.
func closeBegins(f *psFixture) <-chan error {
	closed := make(chan error, 1)
	go func() { closed <- f.svc.Close(context.Background()) }()
	for !f.svc.isClosed() {
		runtime.Gosched()
	}
	return closed
}

// TestReOfferCloseWindowLeavesNoEntry (review II, R2-P6-conc-2201, carried
// N-rw1-1): a re-offer whose closed check passed and whose Close begins
// before its requestor upsert leaves no entry once Close returned — the
// check and the upsert are one step against Close, and an entry that made
// it in before Close is ended by Close. Before, the re-offer added the
// entry after Close's sweep and nothing removed it.
func TestReOfferCloseWindowLeavesNoEntry(t *testing.T) {
	t.Parallel()
	f := newPSFixture()
	h := &hookRequestor{f: f}
	f.svc.ps.requestor = h
	var closed <-chan error
	f.respond = func(command string) (any, error) {
		if command == callProvideOffer {
			h.mu.Lock()
			// The re-offer's first requestor read follows its closed check.
			h.onSessions = func() { closed = closeBegins(f) }
			h.mu.Unlock()
		}
		return wpdef.ProvideOfferResponse{WebRtcSessionId: 9}, nil
	}
	p := &Provider{s: f.svc, t: target{psCamera, psEndpoint}}
	payload, _ := json.Marshal(map[string]any{"webRtcSessionId": 9, "sdp": psOffer, "streamUsage": 3, "videoStreams": []int{1}})
	_, _, _ = p.Send(t.Context(), "ProvideOffer", payload)
	if closed == nil {
		t.Fatal("the re-offer read no requestor entry: the hook never ran")
	}
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := f.sessions(); len(got) != 0 {
		t.Errorf("requestor after Close holds %+v, want nothing", got)
	}
}

// TestNewRawSessionCloseWindowLeavesNoEntry (review II, R2-P6-fsm-2202,
// R2-P6-conc-2201): a raw new session whose arrival passed the closed
// check and whose Close begins before the requestor upsert is ended by
// Close, and the requestor keeps nothing. Before, the entry stayed.
func TestNewRawSessionCloseWindowLeavesNoEntry(t *testing.T) {
	t.Parallel()
	f := newPSFixture()
	h := &hookRequestor{f: f}
	f.svc.ps.requestor = h
	var closed <-chan error
	h.beforeWrite = func() { closed = closeBegins(f) }
	p := &Provider{s: f.svc, t: target{psCamera, psEndpoint}}
	payload, _ := json.Marshal(map[string]any{"webRtcSessionId": nil, "sdp": psOffer, "streamUsage": 3, "videoStreams": []int{1}})
	_, _, _ = p.Send(t.Context(), "ProvideOffer", payload)
	if closed == nil {
		t.Fatal("the offer wrote no requestor entry: the hook never ran")
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
}

// TestCloseEndsRawSessions (review II, R2-P6-fsm-2202): Close ends every
// requestor entry of its fabric the registry does not track — a session a
// host opened through the raw route — with an EndSession (UserHangup)
// within the cleanup budget, drops the entry and announces the end; a
// session the registry tracks is ended by the registry's own stop, not a
// second time, and another fabric's entry is left alone. Before, Close
// sent nothing for the raw session, the requestor kept it, and nothing in
// the module could end it afterwards.
func TestCloseEndsRawSessions(t *testing.T) {
	t.Parallel()
	f := newPSFixture()
	var ends []SessionEnd
	f.svc.cfg.Observer.SessionEnded = func(e SessionEnd) { ends = append(ends, e) }
	f.svc.wire()
	p := &Provider{s: f.svc, t: target{psCamera, psEndpoint}}
	payload, _ := json.Marshal(map[string]any{"webRtcSessionId": nil, "sdp": psOffer, "streamUsage": 3, "videoStreams": []int{1}})
	if _, _, err := p.Send(t.Context(), "ProvideOffer", payload); err != nil {
		t.Fatalf("raw offer: %v", err)
	}
	// Session 7 is the registry's; another fabric's 4 is not this
	// service's.
	reg := f.svc.m.sessions
	pending := reg.begin(psCamera, psEndpoint, "conn-1", "k")
	reg.establishing(pending, 7)
	if refusal := reg.track(pending, managedSession{webRTCSessionID: 7, nodeID: psCamera, endpointID: psEndpoint, connectionID: "conn-1", clientSessionKey: "k"}); refusal != trackAccepted {
		t.Fatalf("track: %v", refusal)
	}
	reg.finish(pending)
	_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id = 7 }))
	_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id, s.FabricIndex = 4, psOtherFabric }))
	before := len(f.callsNow())
	if err := f.svc.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	calls := f.callsNow()[before:]
	want := []string{callEndSession + `:5/1:{"webRtcSessionId":9,"reason":2}`}
	if !slices.Equal(calls, want) {
		t.Errorf("provider calls during Close = %q, want %q (7 is the registry's stop, 4 another fabric's)", calls, want)
	}
	if got := f.sessions(); len(got) != 2 || slices.ContainsFunc(got, func(s wrdef.WebRTCSessionStruct) bool { return s.Id == 9 }) {
		t.Errorf("requestor after Close = %+v, want 7 (the fixture's registry stop bypasses it) and 4", got)
	}
	if !slices.ContainsFunc(ends, func(e SessionEnd) bool { return e.SessionID == 9 && e.Cause == EndReleased }) {
		t.Errorf("ends announced = %+v, want session 9 released", ends)
	}
}
