// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
)

func samplePeerSubscription() PeerSubscription {
	return PeerSubscription{
		SubscriptionID: 0xA1B2C3D4,
		FabricIndex:    2,
		PeerNodeID:     0x1122334455667788,
		AttributeRequests: []im.ConcreteAttributePath{
			{HasEndpoint: true, Endpoint: 3, HasCluster: true, Cluster: 0x6, HasAttribute: true, Attribute: 0},
			{}, // full wildcard
			{HasNode: true, Node: 9, HasCluster: true, Cluster: 0x1D, HasListIndex: true, ListIndex: 4},
		},
		EventRequests: []im.ConcreteEventPath{
			{HasEndpoint: true, Endpoint: 0, HasCluster: true, Cluster: 0x28, HasEvent: true, Event: 1, IsUrgent: true},
			{},
		},
		IsFabricFiltered:   true,
		MinIntervalFloor:   2,
		MaxIntervalCeiling: 600,
		MaxInterval:        600,
		SendInterval:       300 * time.Second,
	}
}

func TestPeerSubscription_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	in := samplePeerSubscription()
	b, err := MarshalPeerSubscription(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := UnmarshalPeerSubscription(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip lost data:\n in  %+v\n out %+v", in, out)
	}
}

func TestUnmarshalPeerSubscription_RejectsUnknownVersionAndBadIntervals(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{
		"version":  `{"v":99,"subscriptionId":1}`,
		"negative": `{"v":1,"subscriptionId":1,"minIntervalFloor":-1}`,
		"overflow": `{"v":1,"subscriptionId":1,"maxInterval":99999999999}`,
		"garbage":  `not json`,
	} {
		if _, err := UnmarshalPeerSubscription([]byte(raw)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// TestRestore_KeepsIDAndNegotiatedIntervals: a restored subscription runs
// under its old id with the stored max and send intervals, not freshly
// negotiated ones (matter.js useAsMaxInterval / useAsSendInterval).
func TestRestore_KeepsIDAndNegotiatedIntervals(t *testing.T) {
	t.Parallel()
	m := NewManager(Config{MaxIntervalCeilingSeconds: 60}, nil, nil)
	p := samplePeerSubscription()
	p.SendInterval = 123 * time.Second
	sub, err := m.Restore(p, 0x4242)
	if err != nil {
		t.Fatal(err)
	}
	if sub.ID != p.SubscriptionID || sub.SessionID != 0x4242 {
		t.Fatalf("restored id/session = %#x/%#x", sub.ID, sub.SessionID)
	}
	if sub.MaxIntervalCeiling != 600 {
		t.Fatalf("max interval re-negotiated to %d, want the stored 600 (no clamp on restore)", sub.MaxIntervalCeiling)
	}
	if got := sub.SendInterval(); got != 123*time.Second {
		t.Fatalf("send interval = %s, want the stored 123s", got)
	}
	if got, err := m.Get(p.SubscriptionID); err != nil || got != sub {
		t.Fatal("restored subscription not addressable by its old id")
	}
	if back := sub.PeerSubscription(true); back.SendInterval != 123*time.Second || back.SubscriptionID != p.SubscriptionID {
		t.Fatalf("re-captured record = %+v", back)
	}
	// The allocator must never hand out the restored id again.
	for range 100 {
		fresh, err := m.Subscribe(SubscribeArgs{FabricIndex: 3, MinIntervalFloor: 1, MaxIntervalCeiling: 10, AttributePaths: []im.ConcreteAttributePath{{}}, KeepSubscriptions: true})
		if err != nil {
			t.Fatal(err)
		}
		if fresh.ID == p.SubscriptionID {
			t.Fatal("allocator reused a restored id")
		}
		_ = m.Release(fresh.ID)
	}
}

func TestRestore_Refusals(t *testing.T) {
	t.Parallel()
	m := NewManager(Config{MaxSubscriptionsPerFabric: 1}, nil, nil)
	p := samplePeerSubscription()
	if _, err := m.Restore(p, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore(p, 2); !errors.Is(err, ErrIDInUse) {
		t.Fatalf("duplicate id: err = %v, want ErrIDInUse", err)
	}
	q := p
	q.SubscriptionID++
	if _, err := m.Restore(q, 2); !errors.Is(err, ErrFabricQuotaExceeded) {
		t.Fatalf("quota: err = %v, want ErrFabricQuotaExceeded", err)
	}
	z := p
	z.SubscriptionID = 0
	if _, err := m.Restore(z, 2); err == nil {
		t.Fatal("zero id must be refused")
	}
	inv := p
	inv.SubscriptionID = 77
	inv.FabricIndex = 9
	inv.MinIntervalFloor, inv.MaxInterval = 10, 5
	if _, err := m.Restore(inv, 2); !errors.Is(err, ErrCadenceOutOfRange) {
		t.Fatalf("inverted cadence: err = %v", err)
	}
}

// terminationRecorder counts the terminated hook per subscription id.
type terminationRecorder struct {
	mu  sync.Mutex
	ids []uint32
}

func (r *terminationRecorder) record(id uint32) {
	r.mu.Lock()
	r.ids = append(r.ids, id)
	r.mu.Unlock()
}

func (r *terminationRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ids)
}

// TestTerminatedHook_SeparatesTerminationFromClose pins which close paths
// end a subscription for good (matter.js `isTerminated`) and which only
// close it with its session.
func TestTerminatedHook_SeparatesTerminationFromClose(t *testing.T) {
	t.Parallel()
	ep := uint16(7)
	cases := []struct {
		name       string
		close      func(m *Manager, sub *Subscription)
		terminated bool
	}{
		{"Close", func(m *Manager, s *Subscription) { _ = m.Close(s.ID) }, true},
		{"Release", func(m *Manager, s *Subscription) { _ = m.Release(s.ID) }, false},
		{"ClosePeer", func(m *Manager, s *Subscription) { m.ClosePeer(s.FabricIndex, s.PeerNodeID) }, true},
		{"CloseEndpoint", func(m *Manager, _ *Subscription) { m.CloseEndpoint(ep) }, true},
		{"CloseFabric", func(m *Manager, s *Subscription) { m.CloseFabric(s.FabricIndex) }, true},
		{"CloseSession", func(m *Manager, s *Subscription) { m.CloseSession(s.SessionID) }, false},
		{"CloseFabricExcept", func(m *Manager, s *Subscription) { m.CloseFabricExcept(s.FabricIndex, 0) }, false},
		{"ReplaceSessionDuplicate", func(m *Manager, s *Subscription) {
			if _, err := m.Subscribe(SubscribeArgs{
				FabricIndex: s.FabricIndex, PeerNodeID: s.PeerNodeID, SessionID: s.SessionID,
				MinIntervalFloor: 1, MaxIntervalCeiling: 10,
				AttributePaths: []im.ConcreteAttributePath{{}}, ReplaceSessionDuplicate: true,
			}); err != nil {
				t.Error(err)
			}
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := NewManager(Config{}, nil, nil)
			var rec terminationRecorder
			closed := 0
			m.SetOnSubscriptionTerminated(rec.record)
			m.SetOnSubscriptionClosed(func(uint32) { closed++ })
			sub, err := m.Subscribe(SubscribeArgs{
				FabricIndex: 1, PeerNodeID: 0xC0, SessionID: 5, MinIntervalFloor: 1, MaxIntervalCeiling: 10,
				AttributePaths: []im.ConcreteAttributePath{{HasEndpoint: true, Endpoint: ep}},
			})
			if err != nil {
				t.Fatal(err)
			}
			tc.close(m, sub)
			if closed == 0 {
				t.Fatal("the close hook must fire on every close path")
			}
			want := 0
			if tc.terminated {
				want = 1
			}
			if got := rec.count(); got != want {
				t.Fatalf("terminated hook fired %d times, want %d", got, want)
			}
		})
	}
}
