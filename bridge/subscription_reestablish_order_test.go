// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/transport/message"
)

// savedOrderStore is a [SubscriptionStore] that returns its rows least
// recently saved first, the order store.ServerSubscriptions documents
// for LoadServerSubscriptions and the SQL store implements (ORDER BY
// updated_at).
type savedOrderStore struct {
	mu    sync.Mutex
	order []uint32
	rows  map[uint32][]byte
}

func (s *savedOrderStore) SaveServerSubscription(_ context.Context, id uint32, _ uint8, _ uint64, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows == nil {
		s.rows = make(map[uint32][]byte)
	}
	if i := slices.Index(s.order, id); i >= 0 {
		s.order = slices.Delete(s.order, i, i+1)
	}
	s.order = append(s.order, id)
	s.rows[id] = append([]byte(nil), payload...)
	return nil
}

func (s *savedOrderStore) DeleteServerSubscription(_ context.Context, id uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := slices.Index(s.order, id); i >= 0 {
		s.order = slices.Delete(s.order, i, i+1)
	}
	delete(s.rows, id)
	return nil
}

func (s *savedOrderStore) DeleteServerSubscriptionsByFabric(context.Context, uint8) error {
	return nil
}

func (s *savedOrderStore) LoadServerSubscriptions(context.Context) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.rows[id])
	}
	return out, nil
}

func (s *savedOrderStore) ClearServerSubscriptions(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = nil
	clear(s.rows)
	return nil
}

func (s *savedOrderStore) saved() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.order)
}

// subscribeAgainOverCASE subscribes once more on the session
// subscribeOverCASE opened and returns the new subscription's id.
func subscribeAgainOverCASE(t *testing.T, ctrl *fakeController) uint32 {
	t.Helper()
	ctrl.subscribe(im.SubscribeRequest{
		KeepSubscriptions:  true,
		MaxIntervalCeiling: 120,
		FabricFiltered:     true,
		AttributeRequests:  []im.ConcreteAttributePath{accessControlACLPath()},
	})
	priming := ctrl.nextReport(t, 3*time.Second)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	select {
	case id := <-ctrl.subResp:
		if id == 0 || id != priming.subID {
			t.Fatalf("SubscribeResponse id %#x, priming id %#x", id, priming.subID)
		}
		return id
	case <-ctx.Done():
		t.Fatal("no SubscribeResponse")
		return 0
	}
}

// TestReestablishTriesTheNewestFirst: a peer's former subscriptions are
// re-established newest first, so an older row the peer no longer knows —
// left by a session that closed while the node ran — cannot end the
// peer's loop before its live subscription was tried. Mirrors matter.js
// SubscriptionsServer.ts:323-325 at db021da0 (`peerSubscriptions.reverse()`)
// and :356-360 (InvalidSubscription ends the peer's loop). Review II,
// R2-P2-sess-01.
func TestReestablishTriesTheNewestFirst(t *testing.T) {
	fab := newResumeFabric(t)
	st := &savedOrderStore{}
	ctrl := newFakeController(t, fab)
	run1 := startResumeDevice(t, st, nil)
	ctrl.setBridge(run1.addr(t))

	subA := subscribeOverCASE(t, run1, ctrl)
	waitFor(t, "A persisted", func() bool { return slices.Equal(st.saved(), []uint32{subA}) })
	// The session closes while the node runs; A's row stays.
	if err := run1.sessions.Close(resumeDeviceSIDRun); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "A closed", func() bool { _, err := run1.subs.Get(subA); return err != nil })
	subB := subscribeOverCASE(t, run1, ctrl)
	waitFor(t, "B persisted", func() bool { return slices.Equal(st.saved(), []uint32{subA, subB}) })
	run1.stop()

	// The controller knows B only: A's priming report is answered
	// InvalidSubscription.
	ctrl.mu.Lock()
	ctrl.secureHook = func(hdr *message.Header, proto message.ProtocolHeader, payload []byte) bool {
		if proto.ProtocolID != im.InteractionModelProtocolID || proto.Opcode != im.OpcodeReportData {
			return false
		}
		if id, _ := decodeReportSummary(payload); id != subA {
			return false
		}
		sr, _ := EncodeStatusResponse(im.StatusResponse{Status: im.StatusInvalidSubscription})
		ctrl.sendSecureAck(proto, hdr.MessageCounter, im.OpcodeStatusResponse, sr)
		return true
	}
	ctrl.mu.Unlock()

	run2 := startResumeDevice(t, st, func(br *Bridge, sessions *operational.Manager) {
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: ctrl.addr})
	})
	ctrl.setBridge(run2.addr(t))
	res := run2.br.ReestablishFormerSubscriptions(context.Background())
	if !slices.Equal(res.Reestablished, []uint32{subB}) {
		t.Fatalf("re-established %v, want the live subscription B=%#x only (A=%#x is stale)", res.Reestablished, subB, subA)
	}
	if got := st.saved(); !slices.Equal(got, []uint32{subB}) {
		t.Fatalf("store holds %v after the re-establishment, want [B=%#x]", got, subB)
	}
}

// TestReestablishKeepsTheCreationOrder: re-establishment runs newest
// first, so it re-saves a peer's subscriptions in reverse; the store is
// put back into creation order afterwards, which the next restart's
// newest-first walk depends on. Mirrors matter.js
// SubscriptionsServer.#restoreCreationOrder (SubscriptionsServer.ts:193-204,
// called at :371, at db021da0). Review II, R2-P2-sess-01.
func TestReestablishKeepsTheCreationOrder(t *testing.T) {
	fab := newResumeFabric(t)
	st := &savedOrderStore{}
	ctrl := newFakeController(t, fab)
	run1 := startResumeDevice(t, st, nil)
	ctrl.setBridge(run1.addr(t))

	subA := subscribeOverCASE(t, run1, ctrl)
	// The subject is the order after re-establishment: A is recorded
	// before B subscribes, as in TestReestablishTriesTheNewestFirst (the
	// order of two activations is TestPersistedOrderIsTheActivationOrder's).
	waitFor(t, "A persisted", func() bool { return slices.Equal(st.saved(), []uint32{subA}) })
	subB := subscribeAgainOverCASE(t, ctrl)
	waitFor(t, "A and B persisted", func() bool { return slices.Equal(st.saved(), []uint32{subA, subB}) })
	run1.stop()

	run2 := startResumeDevice(t, st, func(br *Bridge, sessions *operational.Manager) {
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: ctrl.addr})
	})
	ctrl.setBridge(run2.addr(t))
	res := run2.br.ReestablishFormerSubscriptions(context.Background())
	if !slices.Equal(res.Reestablished, []uint32{subB, subA}) {
		t.Fatalf("re-established %v, want newest first [B=%#x A=%#x]", res.Reestablished, subB, subA)
	}
	if got := st.saved(); !slices.Equal(got, []uint32{subA, subB}) {
		t.Fatalf("store holds %v after the re-establishment, want the creation order [A=%#x B=%#x]", got, subA, subB)
	}
}

// parkFirstSubscribe is a slog handler that holds the first
// "matter.rx.im.subscribe" record — logged by a Subscribe handler after
// it activated its subscription and before it records it
// (sendSubscribeResponse, then persistSubscription) — until release is
// closed; parked is closed once it holds it. Every other record is
// dropped.
type parkFirstSubscribe struct {
	first   atomic.Bool
	parked  chan struct{}
	release chan struct{}
}

func (h *parkFirstSubscribe) Enabled(context.Context, slog.Level) bool { return true }

func (h *parkFirstSubscribe) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "matter.rx.im.subscribe" {
		return nil
	}
	// Not a sync.Once: a later record's Do would wait for the held one.
	if h.first.CompareAndSwap(false, true) {
		close(h.parked)
		<-h.release
	}
	return nil
}

func (h *parkFirstSubscribe) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *parkFirstSubscribe) WithGroup(string) slog.Handler      { return h }

// TestPersistedOrderIsTheActivationOrder (review II P3b-R, candidate 3):
// a peer's subscriptions are recorded in the order they were activated,
// whichever handler reaches the store first. A's handler is held between
// its activation and its record (on its debug log line, which sits
// there); B subscribes meanwhile on the same session — its handler runs
// on a goroutine of its own, A's having left the session's order — and
// its record writes A first. Before, B's handler recorded B alone and A
// followed once released: [B, A], the reverse of the creation order the
// newest-first re-establishment rests on. Mirrors matter.js, which
// appends on ServerSubscription.activate in emission order
// (ServerSubscription.ts:425, SubscriptionsServer.ts:55, :134-166,
// Reactors.ts:173-175, :276-289, :359-371 at b25ed960).
func TestPersistedOrderIsTheActivationOrder(t *testing.T) {
	fab := newResumeFabric(t)
	st := &savedOrderStore{}
	ctrl := newFakeController(t, fab)
	park := &parkFirstSubscribe{parked: make(chan struct{}), release: make(chan struct{})}
	released := false
	release := func() {
		if !released {
			released = true
			close(park.release)
		}
	}
	t.Cleanup(release)
	run1 := startResumeDevice(t, st, func(br *Bridge, _ *operational.Manager) {
		br.logger = slog.New(park)
	})
	ctrl.setBridge(run1.addr(t))

	subA := subscribeOverCASE(t, run1, ctrl)
	waitFor(t, "A's handler held on its debug record", func() bool {
		select {
		case <-park.parked:
			return true
		default:
			return false
		}
	})
	subB := subscribeAgainOverCASE(t, ctrl)
	waitFor(t, "B persisted", func() bool { return slices.Contains(st.saved(), subB) })
	if got := st.saved(); !slices.Equal(got, []uint32{subA, subB}) {
		t.Fatalf("store holds %#x while A's handler is held, want the activation order [A=%#x B=%#x]", got, subA, subB)
	}
	release()
	run1.stop()
	if got := st.saved(); !slices.Equal(got, []uint32{subA, subB}) {
		t.Fatalf("store holds %#x after A's handler went on, want [A=%#x B=%#x]", got, subA, subB)
	}
}

// TestReestablishStopsWithTheBridge (review II, Fable read 5b-write F4): a
// bridge stopped while a former subscription's priming report waits for
// its first chunk's answer sends no chunk of the peer's remaining
// records — the wait ends with the bridge's handler context and the
// peer's loop ends with it.
func TestReestablishStopsWithTheBridge(t *testing.T) {
	fab := newResumeFabric(t)
	st := &savedOrderStore{}
	ctrl := newFakeController(t, fab)
	run1 := startResumeDevice(t, st, nil)
	ctrl.setBridge(run1.addr(t))
	subA := subscribeOverCASE(t, run1, ctrl)
	subB := subscribeAgainOverCASE(t, ctrl)
	waitFor(t, "A and B persisted", func() bool { return slices.Equal(st.saved(), []uint32{subA, subB}) })
	run1.stop()

	run2 := startResumeDevice(t, st, func(br *Bridge, sessions *operational.Manager) {
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: ctrl.addr})
	})
	var mu sync.Mutex
	primed := map[uint32]int{}
	seen := make(chan uint32, 8)
	ctrl.mu.Lock()
	ctrl.secureHook = func(_ *message.Header, proto message.ProtocolHeader, payload []byte) bool {
		if proto.ProtocolID != im.InteractionModelProtocolID || proto.Opcode != im.OpcodeReportData {
			return false
		}
		id, _ := decodeReportSummary(payload)
		mu.Lock()
		primed[id]++
		mu.Unlock()
		seen <- id
		// The newest record's first chunk is left unanswered and the
		// bridge stops meanwhile: what Stop cancels first.
		run2.br.mu.RLock()
		cancel := run2.br.serveCancel
		run2.br.mu.RUnlock()
		if cancel != nil {
			cancel()
		}
		return true
	}
	ctrl.mu.Unlock()
	ctrl.setBridge(run2.addr(t))
	sentBefore := run2.br.load.imSent.Load()
	done := make(chan ReestablishResult, 1)
	go func() { done <- run2.br.ReestablishFormerSubscriptions(context.Background()) }()
	wait, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	select {
	case res := <-done:
		if len(res.Reestablished) != 0 {
			t.Errorf("re-established %v after the bridge stopped, want none", res.Reestablished)
		}
	case <-wait.Done():
		t.Fatal("the re-establishment still runs 10 s after the bridge stopped")
	}
	// A chunk the bridge sent before it returned reaches the controller
	// within this window.
	late, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	for drained := false; !drained; {
		select {
		case <-seen:
		case <-late.Done():
			drained = true
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if primed[subA] != 0 || primed[subB] != 1 {
		t.Fatalf("priming chunks sent per subscription %v, want B=%#x's first alone, none of A=%#x", primed, subB, subA)
	}
	// Not even attempted: one IM message went out, B's first chunk.
	if n := run2.br.load.imSent.Load() - sentBefore; n != 1 {
		t.Fatalf("%d IM messages sent during the re-establishment, want B's first chunk alone", n)
	}
}
