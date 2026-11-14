// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
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
