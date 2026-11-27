// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// WebRtcTransportRequestor's hold of early signals (#5045): a port of the
// groups "holdUnknownSignals" (:712-1038) and "signals invoked through the
// interaction server" (:1040-1446) of matter.js
// packages/node/test/behaviors/web-rtc-transport-requestor/
// WebRtcTransportRequestorServerTest.ts at db91886c, by meaning. The hold
// group's signals run through MatterInvoke under the context the bridge
// stamps for a CASE invoke, on a driven clock; the cases that need the
// session order run over the wire on fabrictest.
package camera_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/bridge/bridgetest"
	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/cluster/spec"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/im"
)

// holdFixture is the "holdUnknownSignals" fixture (:712-822): a requestor
// on a driven clock, its port the recorder.
type holdFixture struct {
	r   *camera.Requestor
	rec *recorder
	clk *clocktest.Fake
}

func newHoldFixture(t *testing.T) holdFixture {
	t.Helper()
	clk := clocktest.New(fabrictest.DefaultStart)
	rec := &recorder{}
	r, err := camera.NewRequestor(camera.RequestorConfig{Port: rec, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	return holdFixture{r: r, rec: rec, clk: clk}
}

// signal invokes c as fabricIndex's node peerNodeID on its own goroutine
// (signal, :767-781); the channel carries the outcome.
func (f holdFixture) signal(c commandCase, id uint16, fabricIndex uint8, peerNodeID uint64) <-chan error {
	out := make(chan error, 1)
	go func() { out <- c.invoke(peer(fabricIndex, peerNodeID), f.r, id) }()
	return out
}

func (f holdFixture) offer(id uint16) <-chan error {
	return f.signal(commandCases[0], id, fabric1, peerNode)
}

// held starts an offer for id and returns once it waits: its hold timer
// is armed.
func (f holdFixture) held(t *testing.T, id uint16) <-chan error {
	t.Helper()
	before := f.clk.Pending()
	out := f.offer(id)
	f.waitArmed(t, before+1)
	return out
}

// waitArmed waits until n hold timers are armed (holdTimers, :798-801).
func (f holdFixture) waitArmed(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for f.clk.Pending() != n {
		if time.Now().After(deadline) {
			t.Fatalf("%d hold timers armed, want %d", f.clk.Pending(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func (f holdFixture) register(t *testing.T, id uint16) {
	t.Helper()
	upsert(t, f.r, makeSession(withID(id), withPeer(peerNode), withFabric(fabric1)))
}

// pending reports that out has not settled within a short wall time
// (isSettled false).
func pending(t *testing.T, out <-chan error) {
	t.Helper()
	select {
	case err := <-out:
		t.Fatalf("the held signal settled: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
}

// settled waits for out (settlesPromptly true).
func settled(t *testing.T, out <-chan error) error {
	t.Helper()
	select {
	case err := <-out:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the held signal did not settle")
		return nil
	}
}

func (r *recorder) delivered(name string) []uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ids []uint16
	switch name {
	case "offer":
		for _, s := range r.offers {
			ids = append(ids, s.Id)
		}
	case "end":
		for _, s := range r.ends {
			ids = append(ids, s.Id)
		}
	}
	return ids
}

func (r *recorder) counts() (answers, ice int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.answers), r.ice
}

func (r *recorder) refused() []refusal {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.refusals)
}

// TestParityMatterJS_RequestorHoldUnknownSignals is "holdUnknownSignals"
// (:712-1038).
func TestParityMatterJS_RequestorHoldUnknownSignals(t *testing.T) { //nolint:gocognit,gocyclo,funlen,maintidx // one subtest per matter.js case
	for _, c := range commandCases { // :823-841
		t.Run("delivers "+c.name+" that arrives before the session is registered", func(t *testing.T) { // :824
			f := newHoldFixture(t)
			f.r.HoldUnknownSignals(fabric1, peerNode)
			out := f.signal(c, sessionID, fabric1, peerNode)
			f.waitArmed(t, 1)
			pending(t, out)

			f.register(t, sessionID)
			if err := settled(t, out); err != nil {
				t.Fatal(err)
			}
			answers, ice := f.rec.counts()
			switch c.name {
			case "answer":
				if answers != 1 {
					t.Fatalf("%d answers", answers)
				}
			case "iceCandidates":
				if ice != 1 {
					t.Fatalf("%d iceCandidates", ice)
				}
			default:
				if got := f.rec.delivered(c.name); !slices.Equal(got, []uint16{sessionID}) {
					t.Fatalf("delivered %v", got)
				}
			}
			if got := f.rec.refused(); len(got) != 0 {
				t.Fatalf("refusals %+v", got)
			}
		})
	}
	t.Run("releases a held signal once its session is registered, while the hold remains", func(t *testing.T) { // :843
		f := newHoldFixture(t)
		f.r.HoldUnknownSignals(fabric1, peerNode)
		out := f.held(t, sessionID)
		f.register(t, sessionID)
		if err := settled(t, out); err != nil {
			t.Fatal(err)
		}
		if n := f.clk.Pending(); n != 0 {
			t.Fatalf("%d hold timers left", n)
		}
	})
	// :858, and :873 in its Go form: the registration is stored (the
	// table is the one Sessions reads) before the held signal reaches the
	// port, so a signal is never delivered for a registration that did not
	// complete.
	t.Run("emits a held signal only after the session registration committed", func(t *testing.T) { // :858, :873
		clk := clocktest.New(fabrictest.DefaultStart)
		var r *camera.Requestor
		var trackedAtOffer []uint16
		port := &offerProbe{onOffer: func() {
			for _, s := range r.Sessions() {
				trackedAtOffer = append(trackedAtOffer, s.Id)
			}
		}}
		var err error
		r, err = camera.NewRequestor(camera.RequestorConfig{Port: port, Clock: clk})
		if err != nil {
			t.Fatal(err)
		}
		f := holdFixture{r: r, rec: &recorder{}, clk: clk}
		r.HoldUnknownSignals(fabric1, peerNode)
		out := f.held(t, sessionID)
		f.register(t, sessionID)
		if err := settled(t, out); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(trackedAtOffer, []uint16{sessionID}) {
			t.Fatalf("tracked at the offer %v", trackedAtOffer)
		}
	})
	t.Run("refuses a held signal once the hold is disposed and its session is still unknown", func(t *testing.T) { // :889
		f := newHoldFixture(t)
		release := f.r.HoldUnknownSignals(fabric1, peerNode)
		out := f.held(t, 99)
		f.register(t, sessionID)
		pending(t, out)

		release()
		wantStatus(t, settled(t, out), im.StatusNotFound)
		if want := []refusal{{camera.Signal("offer"), 99, fabric1, peerNode}}; !slices.Equal(f.rec.refused(), want) {
			t.Fatalf("refusals %+v, want %+v", f.rec.refused(), want)
		}
	})
	t.Run("refuses a held signal at once when the hold is disposed without a registration", func(t *testing.T) { // :905
		f := newHoldFixture(t)
		release := f.r.HoldUnknownSignals(fabric1, peerNode)
		out := f.held(t, sessionID)
		release()
		wantStatus(t, settled(t, out), im.StatusNotFound)
		if got := f.r.Sessions(); len(got) != 0 {
			t.Fatalf("sessions %+v", got)
		}
	})
	t.Run("keeps holding while another hold for the same peer remains, even if one is disposed twice", func(t *testing.T) { // :917
		f := newHoldFixture(t)
		first := f.r.HoldUnknownSignals(fabric1, peerNode)
		second := f.r.HoldUnknownSignals(fabric1, peerNode)
		out := f.held(t, sessionID)
		first()
		first()
		pending(t, out)

		second()
		wantStatus(t, settled(t, out), im.StatusNotFound)
	})
	t.Run("refuses a held signal after the hold time from its arrival", func(t *testing.T) { // :931
		f := newHoldFixture(t)
		f.r.HoldUnknownSignals(fabric1, peerNode)
		f.clk.Advance(4 * time.Second)
		out := f.held(t, sessionID)
		f.clk.Advance(4 * time.Second)
		pending(t, out)
		if got := f.rec.refused(); len(got) != 0 {
			t.Fatalf("refusals %+v", got)
		}

		f.clk.Advance(time.Second)
		wantStatus(t, settled(t, out), im.StatusNotFound)
		if want := []refusal{{camera.Signal("offer"), sessionID, fabric1, peerNode}}; !slices.Equal(f.rec.refused(), want) {
			t.Fatalf("refusals %+v, want %+v", f.rec.refused(), want)
		}
	})
	t.Run("counts the hold time from the signal's arrival, not restarted by a registration of another session", func(t *testing.T) { // :950
		f := newHoldFixture(t)
		f.r.HoldUnknownSignals(fabric1, peerNode)
		out := f.held(t, sessionID)
		f.clk.Advance(3 * time.Second)
		f.register(t, sessionID+1)
		pending(t, out)
		if got := f.clk.Armed(); !slices.Equal(got, []time.Duration{2 * time.Second}) {
			t.Fatalf("armed %v, want the arrival's timer with 2s left", got)
		}

		f.clk.Advance(2 * time.Second)
		wantStatus(t, settled(t, out), im.StatusNotFound)
	})
	t.Run("does not hold a signal from another peer node or fabric", func(t *testing.T) { // :965
		f := newHoldFixture(t)
		f.r.HoldUnknownSignals(fabric1, peerNode)
		for _, other := range []struct {
			fabric uint8
			node   uint64
		}{{fabric1, 99}, {2, peerNode}} {
			wantStatus(t, settled(t, f.signal(commandCases[0], sessionID, other.fabric, other.node)), im.StatusNotFound)
		}
		if n := f.clk.Pending(); n != 0 {
			t.Fatalf("%d hold timers armed", n)
		}
	})
	// The node going offline (:978), the requestor's endpoint closing
	// (:1013) and resetting (:1024) end the holds through one Go hook:
	// the bridge quiesces the server when it leaves the topology or stops.
	t.Run("refuses a held signal when the node goes offline, the endpoint closes or resets", func(t *testing.T) { // :978, :1013, :1024
		f := newHoldFixture(t)
		f.r.HoldUnknownSignals(fabric1, peerNode)
		out := f.held(t, sessionID)
		f.r.MatterQuiesce()
		wantStatus(t, settled(t, out), im.StatusNotFound)
		if want := []refusal{{camera.Signal("offer"), sessionID, fabric1, peerNode}}; !slices.Equal(f.rec.refused(), want) {
			t.Fatalf("refusals %+v, want %+v", f.rec.refused(), want)
		}
	})
	t.Run("does not hold signals with a hold taken before the node went offline", func(t *testing.T) { // :990
		f := newHoldFixture(t)
		release := f.r.HoldUnknownSignals(fabric1, peerNode)
		f.r.MatterQuiesce()
		wantStatus(t, settled(t, f.offer(sessionID)), im.StatusNotFound)
		release() // a hold the quiesce ended is released without effect
	})
	t.Run("holds signals again after the node comes back online", func(t *testing.T) { // :1000
		f := newHoldFixture(t)
		f.r.MatterQuiesce()
		f.r.HoldUnknownSignals(fabric1, peerNode)
		out := f.held(t, sessionID)
		f.register(t, sessionID)
		if err := settled(t, out); err != nil {
			t.Fatal(err)
		}
	})
}

// offerProbe is a RequestorPort that runs onOffer at the Offer.
type offerProbe struct{ onOffer func() }

func (p *offerProbe) Offer(context.Context, camera.WebRTCSession, wrdef.OfferRequest) { p.onOffer() }
func (*offerProbe) Answer(context.Context, camera.WebRTCSession, string)              {}
func (*offerProbe) IceCandidates(context.Context, camera.WebRTCSession, []camera.ICECandidate) {
}
func (*offerProbe) End(context.Context, camera.WebRTCSession, uint8) {}

// blockNextChange makes the requestor's next change notification wait
// until the returned release, so the change holds the requestor's table
// (its publication lock) meanwhile — the Go form of a transaction that
// holds matter.js's lock (holdRequestor :1184-1199, endHoldingTheLock
// :1301-1336). entered is closed when the notification waits.
func blockNextChange(r *camera.Requestor) (entered <-chan struct{}, release func()) {
	in, gate := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unsubscribe := r.OnAttributeChange(func(contract.Change) {
		first := false
		once.Do(func() { first = true })
		if first {
			close(in)
			<-gate
		}
	})
	var done sync.Once
	return in, func() {
		done.Do(func() { close(gate) })
		unsubscribe()
	}
}

func goEnd(r *camera.Requestor, id uint16) <-chan error {
	out := make(chan error, 1)
	go func() { out <- end(r, fabric1, peerNode, id) }()
	return out
}

func wait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func ids(sessions []camera.WebRTCSession) []uint16 {
	out := make([]uint16, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.Id)
	}
	return out
}

// TestParityMatterJS_RequestorSignalsAgainstChanges is the part of
// "signals invoked through the interaction server" (:1040-1446) that is
// about the requestor's lock: Go's is the publication lock a change of
// the table holds through its notification.
func TestParityMatterJS_RequestorSignalsAgainstChanges(t *testing.T) { //nolint:gocognit,funlen // one subtest per matter.js case
	session := func(id uint16) camera.WebRTCSession {
		return makeSession(withID(id), withPeer(peerNode), withFabric(fabric1))
	}
	t.Run("waits for another transaction that holds the requestor before an end removes the session", func(t *testing.T) { // :1211
		r := newRequestor(t, nil)
		upsert(t, r, session(7))
		entered, release := blockNextChange(r)
		go func() { _ = r.UpsertSession(session(8)) }()
		wait(t, entered)
		ended := goEnd(r, 7)
		pending(t, ended)

		release()
		if err := settled(t, ended); err != nil {
			t.Fatal(err)
		}
		if got := ids(r.Sessions()); !slices.Equal(got, []uint16{8}) {
			t.Fatalf("sessions %v", got)
		}
	})
	t.Run("ends a session once when the peer sends two ends for it at the same time", func(t *testing.T) { // :1227
		rec := &recorder{}
		r := newRequestor(t, rec)
		upsert(t, r, session(7))
		entered, release := blockNextChange(r)
		go func() { _ = r.UpsertSession(session(8)) }()
		wait(t, entered)
		first, second := goEnd(r, 7), goEnd(r, 7)
		release()

		var success, notFound int
		for _, err := range []error{settled(t, first), settled(t, second)} {
			if err == nil {
				success++
				continue
			}
			wantStatus(t, err, im.StatusNotFound)
			notFound++
		}
		if success != 1 || notFound != 1 {
			t.Fatalf("%d successes, %d NOT_FOUND", success, notFound)
		}
		if got := rec.delivered("end"); !slices.Equal(got, []uint16{7}) {
			t.Fatalf("ends %v", got)
		}
		if got := rec.refused(); len(got) != 1 || got[0].sessionID != 7 {
			t.Fatalf("refusals %+v", got)
		}
	})
	t.Run("refuses an end for a session the application removed while the end waited for the requestor", func(t *testing.T) { // :1253
		rec := &recorder{}
		r := newRequestor(t, rec)
		upsert(t, r, session(7))
		entered, release := blockNextChange(r)
		// One change registers 8 and removes 7, as the other transaction
		// of :1258-1260 does.
		go func() {
			_ = r.SetAttributes(map[uint32]any{wrdef.AttrCurrentSessions: spec.List[camera.WebRTCSession]{session(8)}})
		}()
		wait(t, entered)
		ended := goEnd(r, 7)
		release()

		wantStatus(t, settled(t, ended), im.StatusNotFound)
		if got := rec.delivered("end"); len(got) != 0 {
			t.Fatalf("ends %v", got)
		}
		if got := ids(r.Sessions()); !slices.Equal(got, []uint16{8}) {
			t.Fatalf("sessions %v", got)
		}
	})
	t.Run("registers a session once a signal handler that holds the requestor commits", func(t *testing.T) { // :1356
		r := newRequestor(t, nil)
		upsert(t, r, session(5))
		entered, release := blockNextChange(r)
		ended := goEnd(r, 5)
		wait(t, entered)
		registered := make(chan error, 1)
		go func() { registered <- r.UpsertSession(session(7)) }()
		pending(t, registered)

		release()
		if err := settled(t, ended); err != nil {
			t.Fatal(err)
		}
		if err := settled(t, registered); err != nil {
			t.Fatal(err)
		}
		if got := ids(r.Sessions()); !slices.Equal(got, []uint16{7}) {
			t.Fatalf("sessions %v", got)
		}
	})
	t.Run("removes a session once a signal handler that holds the requestor commits", func(t *testing.T) { // :1368
		r := newRequestor(t, nil)
		upsert(t, r, session(5), session(7))
		entered, release := blockNextChange(r)
		ended := goEnd(r, 5)
		wait(t, entered)
		removed := make(chan error, 1)
		go func() {
			r.RemoveSession(wrdef.KeyOf(session(7)))
			removed <- nil
		}()
		pending(t, removed)

		release()
		if err := settled(t, ended); err != nil {
			t.Fatal(err)
		}
		_ = settled(t, removed)
		if got := r.Sessions(); len(got) != 0 {
			t.Fatalf("sessions %+v", got)
		}
	})
	// Go's Offer looks its session up in the table the End's change has
	// already stored (the table is kept before the notification), so it
	// is refused at once instead of after the End's commit; the outcome is
	// matter.js's.
	t.Run("refuses an offer for a session whose end commits while the offer arrives", func(t *testing.T) { // :1416
		rec := &recorder{}
		r := newRequestor(t, rec)
		upsert(t, r, session(7))
		entered, release := blockNextChange(r)
		ended := goEnd(r, 7)
		wait(t, entered)
		_, err := r.MatterInvoke(peer(fabric1, peerNode), wrdef.CmdOffer, wrdef.OfferRequest{WebRtcSessionId: 7, Sdp: "offer"})
		wantStatus(t, err, im.StatusNotFound)
		release()

		if err := settled(t, ended); err != nil {
			t.Fatal(err)
		}
		if got := rec.delivered("offer"); len(got) != 0 {
			t.Fatalf("offers %v", got)
		}
	})
}

// TestParityMatterJS_RequestorHeldSignalsOverTheWire is the part of
// "signals invoked through the interaction server" (:1040-1446) that runs
// through the interaction server: the provider's signals reach the
// requestor over a CASE session while the host holds the provider's
// signals, on a site on the system clock (the requestor's hold timer is
// the system's too, and every hold here ends by a registration well
// inside it).
func TestParityMatterJS_RequestorHeldSignalsOverTheWire(t *testing.T) {
	rec := &recorder{}
	r := newRequestor(t, rec)
	_, pair, ep := cameraPairOn(t, r, clock.System{})
	ctx := wireCtx(t)
	fabs, err := pair.Device.Node().Store().ListFabrics(ctx)
	if err != nil || len(fabs) == 0 {
		t.Fatalf("fabrics %v, %v", fabs, err)
	}
	own := fabs[0].FabricIndex
	ctrl := bridgetest.DefaultControllerNodeID
	register := func(id uint16) {
		t.Helper()
		upsert(t, r, makeSession(withID(id), withPeer(ctrl), withFabric(own)))
	}
	path := func(cmd uint32) im.ConcreteCommandPath {
		return im.ConcreteCommandPath{Endpoint: ep, Cluster: wrdef.ClusterID, Command: cmd, HasEndpoint: true}
	}
	invoke := func(cmd uint32, fields spec.Encodable) <-chan error {
		out := make(chan error, 1)
		go func() {
			res, err := fabrictest.Invoke(ctx, pair, path(cmd), fields)
			if err == nil && res.Status != im.StatusSuccess {
				err = fmt.Errorf("status %s", res.Status)
			}
			out <- err
		}()
		return out
	}
	held := func(t *testing.T, out <-chan error) {
		t.Helper()
		select {
		case err := <-out:
			t.Fatalf("the held signal settled: %v", err)
		case <-time.After(300 * time.Millisecond):
		}
	}

	t.Run("delivers an offer the provider invokes before its response is processed", func(t *testing.T) { // :1174
		release := r.HoldUnknownSignals(own, ctrl)
		defer release()
		offered := invoke(wrdef.CmdOffer, wrdef.OfferRequest{WebRtcSessionId: 7, Sdp: "offer"})
		held(t, offered)
		register(7)
		if err := settled(t, offered); err != nil {
			t.Fatal(err)
		}
		if got := rec.delivered("offer"); !slices.Equal(got, []uint16{7}) {
			t.Fatalf("offers %v", got)
		}
	})
	t.Run("removes the session for an end the provider invokes before its response is processed", func(t *testing.T) { // :1276
		release := r.HoldUnknownSignals(own, ctrl)
		defer release()
		ended := invoke(wrdef.CmdEnd, wrdef.EndRequest{WebRtcSessionId: 9, Reason: userHangup})
		held(t, ended)
		register(9)
		if err := settled(t, ended); err != nil {
			t.Fatal(err)
		}
		if got := rec.delivered("end"); !slices.Equal(got, []uint16{9}) {
			t.Fatalf("ends %v", got)
		}
		if slices.Contains(ids(r.Sessions()), 9) {
			t.Fatalf("sessions %v", ids(r.Sessions()))
		}
	})
	// The two signals share the controller's one CASE session, whose
	// messages the device handles in arrival order: the held offer hands
	// the session's turn on, or the candidates would wait behind it until
	// its hold time ran out.
	t.Run("does not hold a signal for another session behind a held one", func(t *testing.T) { // :1401
		register(5)
		release := r.HoldUnknownSignals(own, ctrl)
		defer release()
		offered := invoke(wrdef.CmdOffer, wrdef.OfferRequest{WebRtcSessionId: 11, Sdp: "offer"})
		held(t, offered)
		_, iceBefore := rec.counts()

		candidates := invoke(wrdef.CmdIceCandidates, wrdef.IceCandidatesRequest{
			WebRtcSessionId: 5,
			IceCandidates:   []camera.ICECandidate{{Candidate: "c", SdpMid: spec.ValueOf("0"), SdpmLineIndex: spec.ValueOf[uint16](0)}},
		})
		select {
		case err := <-candidates:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(camera.EarlySignalHoldTime / 2):
			t.Fatal("the candidates waited behind the held offer")
		}
		if _, ice := rec.counts(); ice != iceBefore+1 {
			t.Fatalf("%d iceCandidates, want %d", ice, iceBefore+1)
		}
		held(t, offered)

		register(11)
		if err := settled(t, offered); err != nil {
			t.Fatal(err)
		}
	})
}
