// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/secure/channel"
)

// supersessionRegistry is a session lookup that knows the live sessions and
// their subjects, as the node's registry adapter does.
type supersessionRegistry struct {
	mu       sync.Mutex
	live     map[uint16]bool
	subjects map[uint16]uint64
	// fabrics overrides the fabric a live session resolves to (default 1).
	fabrics map[uint16]uint8
	// pase names the live sessions established over PASE.
	pase map[uint16]bool
}

func (r *supersessionRegistry) Lookup(id uint16) (*channel.Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.live[id] {
		return nil, false
	}
	s, err := channel.New(channel.Config{EncryptKey: make([]byte, 16), DecryptKey: make([]byte, 16)})
	return s, err == nil
}

func (r *supersessionRegistry) FabricFor(id uint16) (uint8, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if idx, ok := r.fabrics[id]; ok {
		return idx, r.live[id]
	}
	return 1, r.live[id]
}

func (r *supersessionRegistry) IsPASE(id uint16) (pase, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pase[id], r.live[id]
}

func (r *supersessionRegistry) SubjectFor(id uint16) (nodeID uint64, cats []uint32, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	nodeID, ok = r.subjects[id]
	return nodeID, nil, ok && r.live[id]
}

// TestSessionSupersededGuards: Bridge.SessionSuperseded, as a host without
// node wires it (decision 350), closes what it moved onto a newer session
// that closed before the move ran — its close hook may have run first —
// and moves nothing onto a session of another subject, which only a
// re-allocated id could name (Fable post-read P2b-B, F2 and F3).
func TestSessionSupersededGuards(t *testing.T) {
	t.Parallel()
	const former, newer uint16 = 50, 51
	const subject uint64 = 0xBBBB4444
	setup := func(t *testing.T, reg *supersessionRegistry) (*Bridge, *subscription.Subscription) {
		t.Helper()
		b := newStartedBridge(t)
		b.AttachSessionLookup(reg)
		m := subscription.NewManager(subscription.Config{}, nil, nil)
		b.AttachSubscriptionManager(m)
		sub, err := m.Subscribe(subscription.SubscribeArgs{
			FabricIndex: 1, PeerNodeID: subject, SessionID: former, MaxIntervalCeiling: 60,
			AttributePaths: []im.ConcreteAttributePath{{}},
		})
		if err != nil {
			t.Fatal(err)
		}
		b.routing.subTargets.Store(sub.ID, subTarget{
			src: &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, sessionID: former, fabricIndex: 1, subjectNodeID: subject,
		})
		return b, sub
	}
	t.Run("closes what moved onto a newer session already closed", func(t *testing.T) {
		t.Parallel()
		reg := &supersessionRegistry{live: map[uint16]bool{former: true}, subjects: map[uint16]uint64{former: subject, newer: subject}}
		b, sub := setup(t, reg)
		b.SessionSuperseded(former, newer)
		if !sub.IsClosed() {
			t.Fatalf("the subscription moved onto closed session %d lives on (on %d)", newer, sub.CurrentSessionID())
		}
	})
	t.Run("moves onto a live newer session", func(t *testing.T) {
		t.Parallel()
		reg := &supersessionRegistry{live: map[uint16]bool{former: true, newer: true}, subjects: map[uint16]uint64{former: subject, newer: subject}}
		b, sub := setup(t, reg)
		b.SessionSuperseded(former, newer)
		if sub.IsClosed() || sub.CurrentSessionID() != newer {
			t.Fatalf("closed %v, on %d; want open on %d", sub.IsClosed(), sub.CurrentSessionID(), newer)
		}
	})
	// F-b25ed960-42: a newer session the lookup still holds is judged, not
	// waved through. matter.js supersedes only with a CASE session of the
	// same peer (packages/protocol/src/session/SessionManager.ts:1242-1250
	// at db91886c: !newer.isPase && newer.peerIs(older.peerAddress)), so a
	// held newer session that resolves to no fabric, to node 0 or to PASE
	// takes nothing.
	for _, tc := range []struct {
		name string
		reg  func() *supersessionRegistry
	}{
		{"moves nothing onto a held session without a fabric", func() *supersessionRegistry {
			return &supersessionRegistry{
				live: map[uint16]bool{former: true, newer: true}, subjects: map[uint16]uint64{former: subject, newer: subject},
				fabrics: map[uint16]uint8{newer: 0},
			}
		}},
		{"moves nothing onto a held session of node 0", func() *supersessionRegistry {
			return &supersessionRegistry{live: map[uint16]bool{former: true, newer: true}, subjects: map[uint16]uint64{former: subject, newer: 0}}
		}},
		{"moves nothing onto a held PASE session", func() *supersessionRegistry {
			return &supersessionRegistry{
				live: map[uint16]bool{former: true, newer: true}, subjects: map[uint16]uint64{former: subject, newer: subject},
				pase: map[uint16]bool{newer: true},
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, sub := setup(t, tc.reg())
			b.SessionSuperseded(former, newer)
			if sub.IsClosed() || sub.CurrentSessionID() != former {
				t.Fatalf("closed %v, on %d; want left open on %d", sub.IsClosed(), sub.CurrentSessionID(), former)
			}
		})
	}
	t.Run("moves nothing onto a session of another subject", func(t *testing.T) {
		t.Parallel()
		reg := &supersessionRegistry{live: map[uint16]bool{former: true, newer: true}, subjects: map[uint16]uint64{former: subject, newer: 0xCCCC}}
		b, sub := setup(t, reg)
		b.SessionSuperseded(former, newer)
		if sub.IsClosed() || sub.CurrentSessionID() != former {
			t.Fatalf("closed %v, on %d; want left open on %d", sub.IsClosed(), sub.CurrentSessionID(), former)
		}
	})
}
