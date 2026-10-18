// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// A port of matter.js packages/protocol/test/action/client/subscription/SustainedSubscriptionTest.ts
// at 862ad75f: the maxEventNumberChanged cases of its "event numbers" and
// "local processing failures" groups (SustainedSubscriptionTest.ts:1296-1477,
// :1613-1638), on the rig of sustained_parity_matterjs_test.go.

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
)

// ssChanges records the MaxEventNumberChanged calls.
type ssChanges struct {
	mu      sync.Mutex
	numbers []uint64
}

func (c *ssChanges) record(n uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.numbers = append(c.numbers, n)
	return nil
}

func (c *ssChanges) get() []uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.numbers)
}

// ssLevels is a slog handler counting the records of one message by level.
type ssLevels struct {
	mu     sync.Mutex
	msg    string
	levels []slog.Level
}

func (h *ssLevels) Enabled(context.Context, slog.Level) bool { return true }
func (h *ssLevels) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *ssLevels) WithGroup(string) slog.Handler            { return h }
func (h *ssLevels) Handle(_ context.Context, r slog.Record) error {
	if r.Message == h.msg {
		h.mu.Lock()
		h.levels = append(h.levels, r.Level)
		h.mu.Unlock()
	}
	return nil
}

func (h *ssLevels) warnings() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, l := range h.levels {
		if l == slog.LevelWarn {
			n++
		}
	}
	return n
}

// ssPushEvents pushes a single-chunk live report of the events numbered
// numbers on subscription id and answers our StatusResponse's ack.
func ssPushEvents(t *testing.T, r *ssRig, xid uint16, id uint32, numbers ...uint64) {
	t.Helper()
	r.s.push(t, xid, im.OpcodeReportData, ssEventReport(t, id, false, false, numbers...))
	r.s.ackPushed(t, expectStatus(t, r.s, im.StatusSuccess))
}

func TestSustainedSubscriptionMaxEventNumberChanged(t *testing.T) {
	t.Parallel()
	consume := func(*interaction.Report) {}

	t.Run("resubscribes for events above the highest one it delivered", func(t *testing.T) {
		t.Parallel()
		r := ssNewRig(t)
		states := newSSStates()
		changes := &ssChanges{}
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Events: ssAllEvents, EventFilters: []im.EventMinimumNumber{{EventMin: 3}},
			Sustain: true, ActiveChanged: states.record, Updated: consume, MaxEventNumberChanged: changes.record,
		})
		if err != nil {
			t.Fatal(err)
		}
		first := ssEstablish(t, r.s, 1, 60, 5, 9, 7) // SustainedSubscriptionTest.ts:1296
		states.wait(t, true)
		r.reg.CloseForPeer(sub.Peer())
		states.wait(t, false)
		second := ssEstablish(t, r.s, 2, 60)
		states.wait(t, true)
		if n, ok := sub.MaxEventNumber(); !ok || n != 9 {
			t.Fatalf("max event number %d (%v), want 9", n, ok)
		}
		if got := changes.get(); !slices.Equal(got, []uint64{9}) {
			t.Fatalf("changes %v, want [9]", got)
		}
		if !slices.Equal(first, []im.EventMinimumNumber{{EventMin: 3}}) || !slices.Equal(second, []im.EventMinimumNumber{{EventMin: 10}}) {
			t.Fatalf("event filters %+v, %+v; want eventMin 3, then 10", first, second)
		}
		sub.Close()
		await(t, sub.Done())
	})

	t.Run("does not lower its number when a later result carries lower event numbers", func(t *testing.T) {
		t.Parallel()
		r := ssNewRig(t)
		states := newSSStates()
		changes := &ssChanges{}
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Events: ssAllEvents, Sustain: true, ActiveChanged: states.record, Updated: consume, MaxEventNumberChanged: changes.record,
		})
		if err != nil {
			t.Fatal(err)
		}
		first := ssEstablish(t, r.s, 1, 60, 9) // SustainedSubscriptionTest.ts:1361
		states.wait(t, true)
		r.reg.CloseForPeer(sub.Peer())
		states.wait(t, false)
		second := ssEstablish(t, r.s, 2, 60, 5)
		states.wait(t, true)
		if n, ok := sub.MaxEventNumber(); !ok || n != 9 {
			t.Fatalf("max event number %d (%v), want 9", n, ok)
		}
		if got := changes.get(); !slices.Equal(got, []uint64{9}) {
			t.Fatalf("changes %v, want [9]", got)
		}
		if len(first) != 0 || !slices.Equal(second, []im.EventMinimumNumber{{EventMin: 10}}) {
			t.Fatalf("event filters %+v, %+v; want none, then eventMin 10", first, second)
		}
		sub.Close()
		await(t, sub.Done())
	})

	t.Run("keeps its number when a report without events follows", func(t *testing.T) {
		t.Parallel()
		r := ssNewRig(t)
		states := newSSStates()
		changes := &ssChanges{}
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Events: ssAllEvents, Sustain: true, ActiveChanged: states.record, Updated: consume, MaxEventNumberChanged: changes.record,
		})
		if err != nil {
			t.Fatal(err)
		}
		first := ssEstablish(t, r.s, 1, 60, 5) // SustainedSubscriptionTest.ts:1423
		states.wait(t, true)
		ssPushEvents(t, r, 0x7302, 1)
		r.reg.CloseForPeer(sub.Peer())
		states.wait(t, false)
		second := ssEstablish(t, r.s, 2, 60)
		states.wait(t, true)
		if n, ok := sub.MaxEventNumber(); !ok || n != 5 {
			t.Fatalf("max event number %d (%v), want 5", n, ok)
		}
		if got := changes.get(); !slices.Equal(got, []uint64{5}) {
			t.Fatalf("changes %v, want [5]", got)
		}
		if len(first) != 0 || !slices.Equal(second, []im.EventMinimumNumber{{EventMin: 6}}) {
			t.Fatalf("event filters %+v, %+v; want none, then eventMin 6", first, second)
		}
		sub.Close()
		await(t, sub.Done())
	})

	// matter.js holds the delivery's promise until the hook's settled; in
	// Go the report's StatusResponse, which follows the delivery, is not
	// sent while the hook runs.
	t.Run("settles the delivery of a report only after maxEventNumberChanged settled", func(t *testing.T) {
		t.Parallel()
		r := ssNewRig(t)
		states := newSSStates()
		entered := make(chan uint64, 1)
		release := make(chan struct{})
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Events: ssAllEvents, Sustain: true, ActiveChanged: states.record, Updated: consume,
			MaxEventNumberChanged: func(n uint64) error {
				entered <- n
				<-release
				return nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		ssEstablish(t, r.s, 1, 60)
		states.wait(t, true)
		r.s.push(t, 0x7303, im.OpcodeReportData, ssEventReport(t, 1, false, false, 4)) // SustainedSubscriptionTest.ts:1448-1470
		if n := await(t, entered); n != 4 {
			t.Fatalf("hook called with %d, want 4", n)
		}
		if n, ok := sub.MaxEventNumber(); !ok || n != 4 {
			t.Fatalf("max event number %d (%v), want 4", n, ok)
		}
		ssNoAnswerYet(t, r.s)
		close(release)
		r.s.ackPushed(t, expectStatus(t, r.s, im.StatusSuccess))
		sub.Close()
		await(t, sub.Done())
	})

	t.Run("logs it and delivers the report", func(t *testing.T) {
		t.Parallel()
		s := newScripted(t)
		reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
		reg.Register(s.mgr)
		t.Cleanup(func() { s.mgr.SetProtocolHandler(im.InteractionModelProtocolID, nil) })
		logs := &ssLevels{msg: "matter.controller.interaction.sustain_local_failure"}
		c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{
			Subscriptions: reg, Clock: s.clock, Rand: func() float64 { return 0 }, Logger: slog.New(logs),
		})
		r := &ssRig{s: s, reg: reg, c: c}
		states := newSSStates()
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Events: ssAllEvents, Sustain: true, ActiveChanged: states.record, Updated: consume,
			MaxEventNumberChanged: func(uint64) error { return errors.New("storing the number failed") },
		})
		if err != nil {
			t.Fatal(err)
		}
		ssEstablish(t, r.s, 1, 60)
		states.wait(t, true)
		ssPushEvents(t, r, 0x7304, 1, 4) // SustainedSubscriptionTest.ts:1625-1636
		if n, ok := sub.MaxEventNumber(); !ok || n != 4 {
			t.Fatalf("max event number %d (%v), want 4", n, ok)
		}
		if w := logs.warnings(); w != 1 {
			t.Fatalf("%d warnings, want 1", w)
		}
		ssPushEvents(t, r, 0x7305, 1, 5)
		if w := logs.warnings(); w != 1 {
			t.Fatalf("%d warnings after a second failure, want still 1 (the rest at debug)", w)
		}
		sub.Close()
		await(t, sub.Done())
	})
}

// ssNoAnswerYet fails when the manager has sent anything but a standalone
// acknowledgement so far, draining what it sent without waiting.
func ssNoAnswerYet(t *testing.T, s *scripted) {
	t.Helper()
	for {
		select {
		case b := <-s.conn.ch:
			hdr, hl, err := message.UnmarshalHeader(b)
			if err != nil {
				t.Fatal(err)
			}
			plain, _, err := s.peer.Decrypt(&hdr, hdr.NonceSecurityFlags(), b[hl:])
			if err != nil {
				t.Fatal(err)
			}
			proto, _, err := message.UnmarshalProtocolHeader(plain)
			if err != nil {
				t.Fatal(err)
			}
			if proto.ProtocolID != 0 || proto.Opcode != 0x10 {
				t.Fatalf("opcode 0x%02X sent while the hook runs, want the delivery unsettled", proto.Opcode)
			}
		default:
			return
		}
	}
}
