// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	clustercamera "github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/im"
)

// Issue 464: the camera sends its Answer as soon as its ProvideOfferResponse
// is out, on the same CASE session, and the controller's served requestor
// answers NOT_FOUND to an Answer for a session it does not hold yet
// (cluster/camera Requestor.findSessionStrict, matter.js
// WebRtcTransportRequestorServer.ts:170-182 at db021da0). These cases run
// the real requestor behind the session layer: the Answer is invoked on it
// with the camera's CASE subject at the points of newSession the issue
// names.

// raceRequestor is the controller's requestor as the session layer sees it
// (client_io.go's requestor), with a seam after the requestor took the
// session: the end of newSession's establishing step.
type raceRequestor struct {
	requestor
	afterUpsert func(wrdef.WebRTCSessionStruct)
}

func (r raceRequestor) upsert(s wrdef.WebRTCSessionStruct) error {
	if err := r.requestor.upsert(s); err != nil {
		return err
	}
	if r.afterUpsert != nil {
		r.afterUpsert(s)
	}
	return nil
}

// racePort is the requestor's port as the controller's cameraPort wires
// it: every Answer goes to the service's signal routing; refusals are
// recorded.
type racePort struct {
	svc *Service

	mu      sync.Mutex
	refused []uint16
}

func (p *racePort) Offer(context.Context, clustercamera.WebRTCSession, wrdef.OfferRequest) {}

func (p *racePort) Answer(_ context.Context, s clustercamera.WebRTCSession, sdp string) {
	p.svc.onSignal(Signal{Kind: SignalAnswer, Session: s, SDP: sdp})
}

func (p *racePort) IceCandidates(context.Context, clustercamera.WebRTCSession, []clustercamera.ICECandidate) {
}

func (p *racePort) End(context.Context, clustercamera.WebRTCSession, uint8) {}

func (p *racePort) Refused(_ context.Context, _ clustercamera.Signal, id uint16, _ uint8, _ uint64) {
	p.mu.Lock()
	p.refused = append(p.refused, id)
	p.mu.Unlock()
}

func (p *racePort) refusals() []uint16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.refused)
}

// cameraAnswers invokes the camera's Answer on the requestor, as its CASE
// session to this controller does (psCamera on psFabric).
func cameraAnswers(r *clustercamera.Requestor, id uint16) error {
	ctx := im.WithSubject(im.WithFabricFilter(context.Background(), false, psFabric), psCamera, nil)
	_, err := r.MatterInvoke(ctx, wrdef.CmdAnswer, wrdef.AnswerRequest{WebRtcSessionId: id, Sdp: psAnswer})
	return err
}

// raceFixture is the provider-sessions fixture with the real requestor.
func raceFixture(t *testing.T) (*psFixture, *racePort, *clustercamera.Requestor, *raceRequestor) {
	t.Helper()
	f := newPSFixture()
	port := &racePort{svc: f.svc}
	r, err := clustercamera.NewRequestor(clustercamera.RequestorConfig{Port: port})
	if err != nil {
		t.Fatal(err)
	}
	rr := &raceRequestor{requestor: requestor{r: r}}
	f.svc.ps.requestor = rr
	return f, port, r, rr
}

// startEstablishing is Service.StartStream's establishing closure
// (session.go): the session is the service's from the id on.
func startEstablishing(f *psFixture, sess *Session) func(uint16) {
	return func(id uint16) {
		sess.ID = id
		f.svc.mu.Lock()
		f.svc.sessions[sessionKey{psCamera, psEndpoint, id}] = sess
		f.svc.mu.Unlock()
	}
}

// awaitSessionSignal is the session's next signal; its mailbox hands it
// over from a goroutine of its own.
func awaitSessionSignal(t *testing.T, sess *Session) Signal {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case sig := <-sess.Signals():
		return sig
	case <-timer.C:
		t.Fatal("the Answer did not reach the session's signals")
		return Signal{}
	}
}

func TestCameraAnswerAtEstablishing(t *testing.T) {
	t.Run("an Answer served at the end of the establishing step reaches the session's signals", func(t *testing.T) {
		f, port, r, rr := raceFixture(t)
		sess := &Session{box: newMailbox(t.Context())}
		var answerErr error
		answered := false
		rr.afterUpsert = func(s wrdef.WebRTCSessionStruct) {
			// newSession is held here: the requestor holds the session,
			// newSession has not returned.
			answered = true
			answerErr = cameraAnswers(r, s.Id)
		}
		if _, err := f.svc.ps.establish(context.Background(), target{psCamera, psEndpoint}, psOfferRequest(nil), startEstablishing(f, sess)); err != nil {
			t.Fatal(err)
		}
		if !answered || answerErr != nil {
			t.Fatalf("the Answer at the end of the establishing step: invoked %v, err %v", answered, answerErr)
		}
		if got := port.refusals(); len(got) != 0 {
			t.Fatalf("the requestor refused %v", got)
		}
		if sig := awaitSessionSignal(t, sess); sig.Kind != SignalAnswer || sig.Session.Id != 9 || sig.SDP != psNarrowed {
			t.Errorf("the session's signal %+v", sig)
		}
	})
	t.Run("the establishing step runs establishing, the offer and the requestor's upsert back to back", func(t *testing.T) {
		f, _, _, rr := raceFixture(t)
		var steps []string
		rr.afterUpsert = func(s wrdef.WebRTCSessionStruct) {
			if _, ok := f.svc.ps.offerOf(f.svc.ps.key(target{psCamera, psEndpoint}, s.Id)); ok {
				steps = append(steps, "offer")
			}
			steps = append(steps, "upsert")
		}
		establishing := func(id uint16) {
			if f.svc.ps.tracks(f.svc.ps.key(target{psCamera, psEndpoint}, id)) {
				steps = append(steps, "held-before-establishing")
			}
			steps = append(steps, "establishing")
		}
		if _, err := f.svc.ps.establish(context.Background(), target{psCamera, psEndpoint}, psOfferRequest(nil), establishing); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(steps, []string{"establishing", "offer", "upsert"}) {
			t.Errorf("steps %v", steps)
		}
	})
	t.Run("an Answer before the requestor holds the session is refused NOT_FOUND and observed", func(t *testing.T) {
		// The remaining window (the findings register's WebRTC
		// decode-window row): the requestor refuses, and the refusal is
		// what the controller's cameraPort reports.
		f, port, r, _ := raceFixture(t)
		var answerErr error
		establishing := func(id uint16) { answerErr = cameraAnswers(r, id) }
		if _, err := f.svc.ps.establish(context.Background(), target{psCamera, psEndpoint}, psOfferRequest(nil), establishing); err != nil {
			t.Fatal(err)
		}
		var status *spec.StatusError
		if !errors.As(answerErr, &status) || status.Status != im.StatusNotFound {
			t.Fatalf("the early Answer answered %v, want NOT_FOUND", answerErr)
		}
		if got := port.refusals(); !slices.Equal(got, []uint16{9}) {
			t.Errorf("refusals %v", got)
		}
	})
}

// TestCameraAnswerAtArrival: the session is registered when the response
// arrives, on the receive path, before newSession reads the invoke's
// result (issue 464, the queue notifier of client.InvokeOptions
// .ResponseQueued).
func TestCameraAnswerAtArrival(t *testing.T) {
	at := target{psCamera, psEndpoint}
	t.Run("an Answer served between the response's arrival and newSession's result reaches the session's signals", func(t *testing.T) {
		f, port, r, _ := raceFixture(t)
		sess := &Session{box: newMailbox(t.Context())}
		var answerErr error
		f.held = func(any) error {
			// newSession is held here: the response arrived, the invoke
			// has not returned it.
			answerErr = cameraAnswers(r, 9)
			return nil
		}
		if _, err := f.svc.ps.establish(context.Background(), at, psOfferRequest(nil), startEstablishing(f, sess)); err != nil {
			t.Fatal(err)
		}
		if answerErr != nil {
			t.Fatalf("the Answer after the arrival answered %v", answerErr)
		}
		if got := port.refusals(); len(got) != 0 {
			t.Fatalf("the requestor refused %v", got)
		}
		if sig := awaitSessionSignal(t, sess); sig.Kind != SignalAnswer || sig.Session.Id != 9 || sig.SDP != psNarrowed {
			t.Errorf("the session's signal %+v", sig)
		}
	})
	t.Run("the session is registered once, at its arrival", func(t *testing.T) {
		f, _, _, rr := raceFixture(t)
		var steps []string
		f.held = func(any) error { steps = append(steps, "result"); return nil }
		rr.afterUpsert = func(wrdef.WebRTCSessionStruct) { steps = append(steps, "upsert") }
		establishing := func(uint16) { steps = append(steps, "establishing") }
		if _, err := f.svc.ps.establish(context.Background(), at, psOfferRequest(nil), establishing); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(steps, []string{"establishing", "upsert", "result"}) {
			t.Errorf("steps %v", steps)
		}
	})
	t.Run("a result naming another session than its arrival is refused, and the arrival's session ended", func(t *testing.T) {
		f, _, _, _ := raceFixture(t)
		f.arrival = wpdef.ProvideOfferResponse{WebRtcSessionId: 7}
		var local *StackError
		if _, err := f.svc.ps.establish(context.Background(), at, psOfferRequest(nil), nil); !errors.As(err, &local) {
			t.Fatalf("err %v, want a stack error", err)
		}
		if f.svc.ps.tracks(f.svc.ps.key(at, 7)) || f.svc.ps.tracks(f.svc.ps.key(at, 9)) {
			t.Error("the requestor kept a session")
		}
		if _, ok := f.svc.ps.offerOf(f.svc.ps.key(at, 7)); ok {
			t.Error("the arrival's offer was not withdrawn")
		}
		if !raceEnded(f, 7) {
			t.Errorf("the arrival's session was not ended: %v", f.calls)
		}
	})
	t.Run("an invoke that fails after the arrival ends the session and drops it from the requestor", func(t *testing.T) {
		f, _, _, _ := raceFixture(t)
		cut := errors.New("the exchange closed")
		f.held = func(any) error { return cut }
		if _, err := f.svc.ps.establish(context.Background(), at, psOfferRequest(nil), nil); !errors.Is(err, cut) {
			t.Fatalf("err %v", err)
		}
		if f.svc.ps.tracks(f.svc.ps.key(at, 9)) {
			t.Error("the requestor kept the session")
		}
		if _, ok := f.svc.ps.offerOf(f.svc.ps.key(at, 9)); ok {
			t.Error("the offer was not withdrawn")
		}
		if !raceEnded(f, 9) {
			t.Errorf("the session was not ended: %v", f.calls)
		}
	})
	t.Run("an arrival heard after the invoke failed registers nothing", func(t *testing.T) {
		f, _, _, rr := raceFixture(t)
		cut := errors.New("the context ended")
		f.lateErr = cut
		established, upserted := false, false
		rr.afterUpsert = func(wrdef.WebRTCSessionStruct) { upserted = true }
		if _, err := f.svc.ps.establish(context.Background(), at, psOfferRequest(nil), func(uint16) { established = true }); !errors.Is(err, cut) {
			t.Fatalf("err %v", err)
		}
		f.late() // the receive path delivers the response now
		if established || upserted || f.svc.ps.tracks(f.svc.ps.key(at, 9)) {
			t.Errorf("the late arrival registered the session: establishing %v, upsert %v", established, upserted)
		}
		if _, ok := f.svc.ps.offerOf(f.svc.ps.key(at, 9)); ok {
			t.Error("the late arrival recorded the offer")
		}
	})
}

// raceEnded reports whether the fixture's camera was sent EndSession for
// id.
func raceEnded(f *psFixture, id uint16) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.ContainsFunc(f.calls, func(c string) bool {
		return strings.HasPrefix(c, callEndSession+":") && strings.Contains(c, fmt.Sprintf(`"webRtcSessionId":%d,`, id))
	})
}

// TestCameraRefusalReachesTheObserver: a command the requestor refused
// goes to Observer.Signal, never to a session, even one of that id.
func TestCameraRefusalReachesTheObserver(t *testing.T) {
	s := sigService()
	sess := &Session{box: newMailbox(t.Context())}
	s.sessions[sessionKey{psCamera, psEndpoint, 9}] = sess
	var observed []Signal
	s.cfg.Observer.Signal = func(sig Signal) { observed = append(observed, sig) }
	refused := Signal{Kind: SignalRefused, Refused: clustercamera.Signal("answer"), Session: wrdef.WebRTCSessionStruct{Id: 9, PeerNodeId: psCamera, FabricIndex: psFabric}}
	s.onSignal(refused)
	if len(observed) != 1 || observed[0].Kind != SignalRefused || observed[0].Refused != "answer" || observed[0].Session.Id != 9 {
		t.Fatalf("observed %+v", observed)
	}
	select {
	case sig := <-sess.Signals():
		t.Fatalf("the session got %+v", sig)
	default:
	}
}
