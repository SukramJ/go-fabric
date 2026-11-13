// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
)

// goneEvents is an event source whose log no longer holds what was fired.
type goneEvents struct{}

func (goneEvents) Query(uint16, uint32, uint32, uint64) []im.EventRecord { return nil }

// unselectedEvents is an event source that answers with an event of
// another path only.
type unselectedEvents struct{}

func (unselectedEvents) Query(uint16, uint32, uint32, uint64) []im.EventRecord {
	return []im.EventRecord{{Number: 9, Priority: im.EventPriorityInfo, Endpoint: 7, Cluster: 0x0099, EventID: 0x02}}
}

// TestEventDrainThatSendsNothingIsNoReport: a pass an urgent event asked
// for that yields no event — no event source, the event gone from the
// log, none of the subscription's paths — is not a report: it does not
// move the time of the last report. Before, it did, so a dirty attribute
// waited another whole MinIntervalFloor and the keep-alive another whole
// send interval after a report that was never sent.
func TestEventDrainThatSendsNothingIsNoReport(t *testing.T) {
	t.Parallel()
	eventPath := im.ConcreteEventPath{HasEndpoint: true, HasCluster: true, HasEvent: true, Endpoint: 1, Cluster: 0x003B, Event: 0x01}
	urgentPath := eventPath
	urgentPath.IsUrgent = true
	attrPath := im.ConcreteAttributePath{HasEndpoint: true, HasCluster: true, HasAttribute: true, Endpoint: 1, Cluster: 0x0006, Attribute: 0}
	sources := map[string]EventSource{
		"no event source":                nil,
		"the event is gone from the log": goneEvents{},
		"no selected event":              unselectedEvents{},
	}
	for name, src := range sources {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := NewManager(Config{}, nil, nil)
			sub, err := m.Subscribe(SubscribeArgs{
				FabricIndex: 1, PeerNodeID: 1, SessionID: 1,
				MinIntervalFloor: 10, MaxIntervalCeiling: 60,
				AttributePaths: []im.ConcreteAttributePath{attrPath},
				EventPaths:     []im.ConcreteEventPath{eventPath},
			})
			if err != nil {
				t.Fatal(err)
			}
			t0 := time.Unix(1_800_000_000, 0)
			sub.TouchLastReport(t0)
			sub.queueEvent(5, true)

			now := t0.Add(11 * time.Second) // past the floor
			if work, due := sub.drainDue(now, src, false); due {
				t.Fatalf("a pass with nothing to send is due: %+v", work)
			}
			sub.mu.Lock()
			last := sub.lastReport
			sub.mu.Unlock()
			if !last.Equal(t0) {
				t.Fatalf("the last report moved to %v after a drain that sent nothing, want %v", last, t0)
			}
			sub.markDirty(attrPath)
			if work, due := sub.drainDue(now, src, false); !due || len(work.paths) != 1 || len(work.events) != 0 {
				t.Fatalf("the dirty attribute past its floor is held back: due=%v, work %+v, want its 1 path", due, work)
			}
		})
	}

	t.Run("a drain that sends is a report", func(t *testing.T) {
		t.Parallel()
		m := NewManager(Config{}, nil, nil)
		log := im.NewEventLog()
		m.SetEventSource(log)
		sub, err := m.Subscribe(SubscribeArgs{
			FabricIndex: 1, PeerNodeID: 1, SessionID: 1,
			MinIntervalFloor: 10, MaxIntervalCeiling: 60,
			EventPaths: []im.ConcreteEventPath{urgentPath},
		})
		if err != nil {
			t.Fatal(err)
		}
		t0 := time.Unix(1_800_000_000, 0)
		sub.TouchLastReport(t0)
		n := log.Append(im.EventRecord{Priority: im.EventPriorityInfo, Endpoint: 1, Cluster: 0x003B, EventID: 0x01})
		m.OnEventFired(EventFiring{Path: eventPath, Number: n, Priority: im.EventPriorityInfo})
		now := t0.Add(11 * time.Second)
		if work, due := sub.drainDue(now, log, false); !due || len(work.events) != 1 {
			t.Fatalf("due=%v with %d events, want 1 event", due, len(work.events))
		}
		sub.mu.Lock()
		last := sub.lastReport
		sub.mu.Unlock()
		if !last.Equal(now) {
			t.Fatalf("the last report = %v after a drain that sends, want %v", last, now)
		}
	})
}
