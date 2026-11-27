// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The order of a signal's delivery against its session's End, and a held
// signal whose session is registered and removed again before it looks
// (phase 9 P1-G repair, Fable findings F1 and F2).
package camera_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/camera"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/im"
)

// gatedPort is a RequestorPort whose Offer signals entered and waits for
// gate, then runs inOffer; it records the order of its calls.
type gatedPort struct {
	entered chan struct{}
	gate    chan struct{}
	inOffer func()

	mu  sync.Mutex
	seq []string
}

func newGatedPort() *gatedPort {
	return &gatedPort{entered: make(chan struct{}), gate: make(chan struct{})}
}

func (p *gatedPort) add(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq = append(p.seq, s)
}

func (p *gatedPort) order() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.seq)
}

func (p *gatedPort) Offer(context.Context, camera.WebRTCSession, wrdef.OfferRequest) {
	close(p.entered)
	<-p.gate
	if p.inOffer != nil {
		p.inOffer()
	}
	p.add("offer")
}
func (*gatedPort) Answer(context.Context, camera.WebRTCSession, string) {}
func (*gatedPort) IceCandidates(context.Context, camera.WebRTCSession, []camera.ICECandidate) {
}
func (p *gatedPort) End(context.Context, camera.WebRTCSession, uint8) { p.add("end") }

func TestRequestorEndWaitsForSignalAtPort(t *testing.T) {
	session := func(id uint16) camera.WebRTCSession {
		return makeSession(withID(id), withPeer(peerNode), withFabric(fabric1))
	}
	t.Run("an end leaves the table unchanged while an offer for its session is at the port", func(t *testing.T) {
		port := newGatedPort()
		r := newRequestor(t, port)
		upsert(t, r, session(7))
		offered := make(chan error, 1)
		go func() { offered <- commandCases[0].invoke(peer(fabric1, peerNode), r, 7) }()
		wait(t, port.entered)
		ended := goEnd(r, 7)
		pending(t, ended)
		if got := ids(r.Sessions()); !slices.Equal(got, []uint16{7}) {
			t.Fatalf("sessions %v while the offer is at the port", got)
		}

		close(port.gate)
		if err := settled(t, offered); err != nil {
			t.Fatal(err)
		}
		if err := settled(t, ended); err != nil {
			t.Fatal(err)
		}
		if got := port.order(); !slices.Equal(got, []string{"offer", "end"}) {
			t.Fatalf("port order %v", got)
		}
		if got := r.Sessions(); len(got) != 0 {
			t.Fatalf("sessions %+v", got)
		}
	})
	t.Run("a port changes the table from inside an offer while an end waits", func(t *testing.T) {
		port := newGatedPort()
		r := newRequestor(t, port)
		port.inOffer = func() {
			if err := r.UpsertSession(session(9)); err != nil {
				t.Error(err)
			}
			r.RemoveSession(wrdef.KeyOf(session(9)))
			if err := r.UpsertSession(session(8)); err != nil {
				t.Error(err)
			}
		}
		upsert(t, r, session(7))
		offered := make(chan error, 1)
		go func() { offered <- commandCases[0].invoke(peer(fabric1, peerNode), r, 7) }()
		wait(t, port.entered)
		ended := goEnd(r, 7)
		pending(t, ended)

		close(port.gate)
		if err := settled(t, offered); err != nil {
			t.Fatal(err)
		}
		if err := settled(t, ended); err != nil {
			t.Fatal(err)
		}
		if got := ids(r.Sessions()); !slices.Equal(got, []uint16{8}) {
			t.Fatalf("sessions %v", got)
		}
	})
}

// TestRequestorHeldSignalAfterRegisterAndRemove: a held signal whose
// session is registered and removed again before it looks leaves its
// wait — the registration committed — and is refused NOT_FOUND at once,
// without waiting for the release or the hold time (driven clock, never
// advanced).
func TestRequestorHeldSignalAfterRegisterAndRemove(t *testing.T) {
	f := newHoldFixture(t)
	release := f.r.HoldUnknownSignals(fabric1, peerNode)
	defer release()
	out := f.held(t, 7)

	// The removal happens while delivery is locked, as an End's does, so
	// the signal cannot be delivered between the registration and the
	// removal.
	unlock := camera.LockDeliver(f.r)
	f.register(t, 7)
	f.r.RemoveSession(wrdef.KeyOf(makeSession(withID(7), withPeer(peerNode), withFabric(fabric1))))
	unlock()

	wantStatus(t, settled(t, out), im.StatusNotFound)
	if got := f.rec.delivered("offer"); len(got) != 0 {
		t.Fatalf("offers %v", got)
	}
	if got := f.rec.refused(); len(got) != 1 || got[0].sessionID != 7 {
		t.Fatalf("refusals %+v", got)
	}
}
