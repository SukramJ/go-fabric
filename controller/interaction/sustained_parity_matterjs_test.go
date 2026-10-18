// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// A port of matter.js packages/protocol/test/action/client/subscription/SustainedSubscriptionTest.ts
// at 862ad75f: closing a sustained subscription, its active state, its
// maximum interval and keepalives, and the event number it resubscribes
// above. matter.js drives SustainedSubscription with injected subscribe and
// probe functions; the Go port drives the same loop through
// Interaction.Subscribe with Sustain against the scripted peer, a lost
// subscription being the registry's CloseForPeer and the peer subscription's
// close its leaving the registry.

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// ssRig is a scripted peer with the registry registered for pushed
// reports and an interaction on the fake clock with a zero jitter draw
// (SustainedSubscriptionTest.ts:66-69).
type ssRig struct {
	s   *scripted
	reg *interaction.Subscriptions
	c   *interaction.Interaction
}

func ssNewRig(t *testing.T) *ssRig {
	t.Helper()
	s := newScripted(t)
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
	reg.Register(s.mgr)
	t.Cleanup(func() { s.mgr.SetProtocolHandler(im.InteractionModelProtocolID, nil) })
	c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg, Clock: s.clock, Rand: func() float64 { return 0 }})
	return &ssRig{s: s, reg: reg, c: c}
}

// ssStates records the ActiveChanged emissions.
type ssStates struct {
	mu     sync.Mutex
	values []bool
	ch     chan bool
}

func newSSStates() *ssStates { return &ssStates{ch: make(chan bool, 16)} }

func (a *ssStates) record(v bool) {
	a.mu.Lock()
	a.values = append(a.values, v)
	a.mu.Unlock()
	a.ch <- v
}

func (a *ssStates) wait(t *testing.T, want bool) {
	t.Helper()
	if got := await(t, a.ch); got != want {
		t.Fatalf("active %v, want %v", got, want)
	}
}

func (a *ssStates) all() []bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.values)
}

// ssEventReport encodes a report of the events numbered numbers, each the
// Info event 1 of cluster 0x3b on endpoint 1 without data stamped with its
// number (SustainedSubscriptionTest.ts:1197-1207).
func ssEventReport(t *testing.T, id uint32, more, suppress bool, numbers ...uint64) []byte {
	t.Helper()
	rd := im.ReportData{SubscriptionID: id, HasSubscription: id != 0, MoreChunkedMessages: more, SuppressResponse: suppress}
	for _, n := range numbers {
		rd.EventReports = append(rd.EventReports, im.EventReport{
			Path:      im.ConcreteEventPath{Endpoint: 1, HasEndpoint: true, Cluster: 0x3b, HasCluster: true, Event: 1, HasEvent: true},
			Number:    n,
			Priority:  im.EventPriorityInfo,
			Timestamp: n,
		})
	}
	return encode(t, func(e *tlv.Encoder) { rd.MarshalTLV(e, func(*tlv.Encoder, tlv.Tag, im.AttributeValue) {}) })
}

// ssEstablish answers the next SubscribeRequest with a priming report of
// the events numbered numbers and subscription id, granting maxInterval;
// it returns the request's event filters.
func ssEstablish(t *testing.T, s *scripted, id uint32, maxInterval uint16, numbers ...uint64) []im.EventMinimumNumber {
	t.Helper()
	r := s.next(t)
	if r.proto.Opcode != im.OpcodeSubscribeRequest {
		t.Fatalf("opcode 0x%02X, want a SubscribeRequest", r.proto.Opcode)
	}
	req, err := im.UnmarshalSubscribeRequestTLV(tlv.NewDecoder(r.payload))
	if err != nil {
		t.Fatal(err)
	}
	s.reply(t, r, im.InteractionModelProtocolID, im.OpcodeReportData, ssEventReport(t, 0, false, true, numbers...))
	s.reply(t, r, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, id, maxInterval))
	return req.EventFilters
}

var ssAllEvents = []im.ConcreteEventPath{{}}

// TestSustainedSubscriptionClose: SustainedSubscriptionTest.ts:115-131 —
// Close ends the loop although the peer never reports a closure, the
// subscription not active and its peer subscription closed.
func TestSustainedSubscriptionClose(t *testing.T) {
	t.Parallel()
	t.Run("close() resolves done() even when peer never fires request.closed", func(t *testing.T) {
		t.Parallel()
		r := ssNewRig(t)
		states := newSSStates()
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}, Sustain: true, ActiveChanged: states.record})
		if err != nil {
			t.Fatal(err)
		}
		ssEstablish(t, r.s, 11, 60)
		states.wait(t, true)
		if !sub.Active() {
			t.Fatal("not active once established")
		}
		sub.Close()
		await(t, sub.Done())
		if sub.Active() {
			t.Fatal("active after Close")
		}
		if r.reg.GetPeer(sub.Peer(), 11) != nil || len(r.reg.Active()) != 0 {
			t.Fatal("the peer subscription was not closed")
		}
	})
}

// TestSustainedSubscriptionNonICD: the non-ICD describe group
// (SustainedSubscriptionTest.ts:133-454).
func TestSustainedSubscriptionNonICD(t *testing.T) {
	t.Parallel()
	t.Run("never reports active when closed while the subscribe is in flight and then succeeds", func(t *testing.T) {
		t.Parallel()
		r := ssNewRig(t)
		states := newSSStates()
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}, Sustain: true, ActiveChanged: states.record})
		if err != nil {
			t.Fatal(err)
		}
		m := r.s.next(t)
		if m.proto.Opcode != im.OpcodeSubscribeRequest {
			t.Fatalf("opcode 0x%02X", m.proto.Opcode)
		}
		sub.Close()
		r.s.reply(t, m, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, false, true))
		r.s.reply(t, m, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, 11, 60))
		await(t, sub.Done())
		if got := states.all(); len(got) != 0 || sub.Active() {
			t.Fatalf("active emissions %v, active %v", got, sub.Active())
		}
		if len(r.reg.Active()) != 0 || r.reg.GetPeer(sub.Peer(), 11) != nil {
			t.Fatal("a peer subscription stayed open")
		}
	})

	t.Run("reports not live when closed while the active observers run", func(t *testing.T) {
		t.Parallel()
		r := ssNewRig(t)
		states := newSSStates()
		var self atomic.Pointer[interaction.Subscription]
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{onOff}, Sustain: true,
			ActiveChanged: func(a bool) {
				states.record(a)
				if a {
					self.Load().Close()
				}
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		self.Store(sub)
		ssEstablish(t, r.s, 11, 60)
		await(t, sub.Done())
		if got := states.all(); !slices.Equal(got, []bool{true, false}) || sub.Active() {
			t.Fatalf("active emissions %v, active %v", got, sub.Active())
		}
	})

	t.Run("reports maxInterval in seconds, using a seconds fallback before a subscription is established", func(t *testing.T) {
		t.Parallel()
		r := ssNewRig(t)
		states := newSSStates()
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}, Sustain: true, ActiveChanged: states.record})
		if err != nil {
			t.Fatal(err)
		}
		// MaxInterval is a time.Duration in Go: the fallback is one hour.
		if got := sub.MaxInterval(); got != time.Hour {
			t.Fatalf("max interval %v before a subscription, want 1h", got)
		}
		ssEstablish(t, r.s, 11, 60)
		states.wait(t, true)
		if got := sub.MaxInterval(); got != 60*time.Second {
			t.Fatalf("max interval %v, want 60s", got)
		}
		sub.Close()
		await(t, sub.Done())
	})

	t.Run("forwards a caller-provided keepaliveReceived", func(t *testing.T) {
		t.Parallel()
		r := ssNewRig(t)
		states := newSSStates()
		keepalives := make(chan struct{}, 4)
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{onOff}, Sustain: true, ActiveChanged: states.record,
			Updated:           func(*interaction.Report) {},
			KeepaliveReceived: func() { keepalives <- struct{}{} },
		})
		if err != nil {
			t.Fatal(err)
		}
		ssEstablish(t, r.s, 5, 60)
		states.wait(t, true)
		// The peer's empty report is the keepalive the subscription forwards.
		r.s.push(t, 0x7201, im.OpcodeReportData, pushedReport(t, 5, false, false, true, false))
		await(t, keepalives)
		if len(keepalives) != 0 {
			t.Fatal("the keepalive was forwarded more than once")
		}
		sub.Close()
		await(t, sub.Done())
	})
}

// TestSustainedSubscriptionEventNumbers: the event numbers describe group
// (SustainedSubscriptionTest.ts:1196-1520).
func TestSustainedSubscriptionEventNumbers(t *testing.T) {
	t.Parallel()
	consume := func(*interaction.Report) {}

	t.Run("keeps the caller's event filters until it delivered an event", func(t *testing.T) {
		t.Parallel()
		r := ssNewRig(t)
		states := newSSStates()
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Events: ssAllEvents, EventFilters: []im.EventMinimumNumber{{EventMin: 7}},
			Sustain: true, ActiveChanged: states.record, Updated: consume,
		})
		if err != nil {
			t.Fatal(err)
		}
		first := ssEstablish(t, r.s, 1, 60)
		states.wait(t, true)
		r.reg.CloseForPeer(sub.Peer())
		states.wait(t, false)
		second := ssEstablish(t, r.s, 2, 60)
		states.wait(t, true)
		want := []im.EventMinimumNumber{{EventMin: 7}}
		if !slices.Equal(first, want) || !slices.Equal(second, want) {
			t.Fatalf("event filters %+v, %+v; want %+v twice", first, second, want)
		}
		if n, ok := sub.MaxEventNumber(); ok {
			t.Fatalf("max event number %d, want none", n)
		}
		sub.Close()
		await(t, sub.Done())
	})

	t.Run("counts a result only once its consumer resolved", func(t *testing.T) {
		t.Parallel()
		r := ssNewRig(t)
		states := newSSStates()
		var self atomic.Pointer[interaction.Subscription]
		var seenOK atomic.Bool
		seen := make(chan struct{}, 1)
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Events: ssAllEvents, Sustain: true, ActiveChanged: states.record,
			Updated: func(*interaction.Report) {
				_, ok := self.Load().MaxEventNumber()
				seenOK.Store(ok)
				seen <- struct{}{}
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		self.Store(sub)
		ssEstablish(t, r.s, 1, 60, 4)
		states.wait(t, true)
		await(t, seen)
		if seenOK.Load() {
			t.Fatal("the result counted while its consumer ran")
		}
		if n, ok := sub.MaxEventNumber(); !ok || n != 4 {
			t.Fatalf("max event number %d (%v), want 4", n, ok)
		}
		sub.Close()
		await(t, sub.Done())
	})

	// matter.js yields the events of one report from an asynchronous
	// chunk; the Go counterpart of a report delivered piecewise is one in
	// two chunks, merged before Updated.
	t.Run("counts events of reports with asynchronous chunks", func(t *testing.T) {
		t.Parallel()
		r := ssNewRig(t)
		states := newSSStates()
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Events: ssAllEvents, Sustain: true, ActiveChanged: states.record, Updated: consume,
		})
		if err != nil {
			t.Fatal(err)
		}
		ssEstablish(t, r.s, 5, 60)
		states.wait(t, true)
		r.s.push(t, 0x7301, im.OpcodeReportData, ssEventReport(t, 5, true, false, 3))
		first := expectStatus(t, r.s, im.StatusSuccess)
		r.s.continueOn(t, first, im.OpcodeReportData, ssEventReport(t, 5, false, false, 6))
		// The final status follows the delivery (readDataReports).
		r.s.ackPushed(t, expectStatus(t, r.s, im.StatusSuccess))
		if n, ok := sub.MaxEventNumber(); !ok || n != 6 {
			t.Fatalf("max event number %d (%v), want 6", n, ok)
		}
		sub.Close()
		await(t, sub.Done())
	})

	t.Run("replaces the event filters a refreshRequest returns with its own once it delivered an event", func(t *testing.T) {
		t.Parallel()
		r := ssNewRig(t)
		states := newSSStates()
		sub, err := r.c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Events: ssAllEvents, Sustain: true, ActiveChanged: states.record, Updated: consume,
			RefreshRequest: func(req interaction.SubscribeRequest) interaction.SubscribeRequest {
				req.EventFilters = []im.EventMinimumNumber{{EventMin: 1}}
				return req
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		first := ssEstablish(t, r.s, 1, 60, 5)
		states.wait(t, true)
		r.reg.CloseForPeer(sub.Peer())
		states.wait(t, false)
		second := ssEstablish(t, r.s, 2, 60)
		states.wait(t, true)
		if len(first) != 0 || !slices.Equal(second, []im.EventMinimumNumber{{EventMin: 6}}) {
			t.Fatalf("event filters %+v, %+v; want none, then eventMin 6", first, second)
		}
		sub.Close()
		await(t, sub.Done())
	})
}
