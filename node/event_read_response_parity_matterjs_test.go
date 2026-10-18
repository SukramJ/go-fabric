// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/EventReadResponseTest.ts at
// 862ad75f: event reads against a server node — concrete events with and
// without payload, fabric-sensitive filtering, the event filter,
// non-existent paths, the wildcards, and a remote read past the last event.
//
// matter.js runs EventReadResponse on a MockServerNode as an administrator
// without fabric, or on fabric 5 (readEvRaw); the port reads over the wire
// from a commissioned fabrictest pair, whose fabric is the accessing one.
// The record a fabric-sensitive case owns is therefore the pair's fabric's,
// the record it must not see another fabric's (matter.js: 5 against
// NO_FABRIC, and 5 against 5). Events are emitted locally (Device.Act on
// the root's BasicInformation, a hand-written Messages server on a bridged
// endpoint — the module's generated server cannot serve Messages' fabric-
// scoped list). Event numbers are the Go node's own — its event log holds
// what booting and commissioning emitted — so the port asserts order and
// priority where matter.js asserts the mock's numbers 1 and 3, and the
// event filter is the StartUp number plus one where matter.js writes 2.
// matter.js's timestamp normalisation ("within 2 seconds") is a timestamp
// within 2 s of the site's clock.
//
// The Go root serves ReachableChanged (BasicInformation's Reachable is
// served), so the non-existent event is GeneralDiagnostics'
// HardwareFaultChange, absent on both roots.
package node_test

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	gendiag "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	"github.com/SukramJ/go-fabric/cluster/spec/messages"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

func TestEventReadResponseParity(t *testing.T) {
	t.Parallel()

	biEvent := func(ev uint32) im.ConcreteEventPath {
		return im.ConcreteEventPath{Cluster: basicinfo.ClusterID, Event: ev, HasCluster: true, HasEvent: true}
	}
	messageID := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00, 0xaa, 0xbb, 0xcc, 0xdd, 0xee}

	for _, fabricFiltered := range []bool{true, false} {
		name := "Read Event "
		if fabricFiltered {
			name += "as fabricScoped"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			t.Run("reads non-fabric-scoped concrete event with payload", func(t *testing.T) {
				t.Parallel()
				m := newEventPair(t)
				r := m.read(t, fabricFiltered, nil, biEvent(basicinfo.EventStartUp))
				ev := oneEvent(t, r, 0, basicinfo.ClusterID, basicinfo.EventStartUp, im.EventPriorityCritical)
				m.assertTimestamp(t, ev)
				// matter.js: { softwareVersion: 0 }, the mock's
				// SoftwareVersion; the Go node's is its configured one.
				sw, err := fabrictest.Read(parityCtx(t), m.pair, attrPath(0, basicinfo.ClusterID, basicinfo.AttrSoftwareVersion))
				if err != nil {
					t.Fatal(err)
				}
				var got basicinfo.StartUpEvent
				if err := got.DecodeTLV(ev.Value); err != nil || uint64(got.SoftwareVersion) != sw.Value.El.Uint {
					t.Fatalf("StartUp %+v, %v, want softwareVersion %d", got, err, sw.Value.El.Uint)
				}
			})

			// Leave has a FabricIndex field but is not fabric-sensitive.
			t.Run("reads non-fabric-scoped concrete event with a payload that has a fabricIndex field", func(t *testing.T) {
				t.Parallel()
				m := newEventPair(t)
				m.act(t, func(n *node.Node) { n.Root().BasicInformation.EmitLeave(4) })
				r := m.read(t, fabricFiltered, nil, biEvent(basicinfo.EventLeave))
				ev := oneEvent(t, r, 0, basicinfo.ClusterID, basicinfo.EventLeave, im.EventPriorityInfo)
				var got basicinfo.LeaveEvent
				if err := got.DecodeTLV(ev.Value); err != nil || got.FabricIndex != 4 {
					t.Fatalf("Leave %+v, %v, want fabricIndex 4", got, err)
				}
			})

			t.Run("reads non-fabric-scoped concrete event without payload", func(t *testing.T) {
				t.Parallel()
				m := newEventPair(t)
				m.act(t, func(n *node.Node) { n.Root().BasicInformation.EmitShutDown() })
				r := m.read(t, fabricFiltered, nil, biEvent(basicinfo.EventShutDown))
				ev := oneEvent(t, r, 0, basicinfo.ClusterID, basicinfo.EventShutDown, im.EventPriorityCritical)
				if len(ev.Value.Children) != 0 {
					t.Fatalf("ShutDown payload %+v, want none", ev.Value)
				}
			})

			// MessageComplete is fabric-sensitive ("S V"): a record is
			// reported to the fabric that owns it only, whatever the
			// FabricFiltered flag (core §8.4.3.2).
			t.Run("filters fabric-sensitive concrete event not owned by the accessing fabric", func(t *testing.T) {
				t.Parallel()
				m := newEventPair(t)
				m.msgs.emitComplete(m.msgsEP, messageID, m.fabricIndex(t)+4)
				r := m.read(t, fabricFiltered, nil, im.ConcreteEventPath{Cluster: messages.ClusterID, Event: messages.EventMessageComplete, HasCluster: true, HasEvent: true})
				if len(r.Events) != 0 || len(r.EventStatuses) != 0 {
					t.Fatalf("events %+v, statuses %+v, want none", r.Events, r.EventStatuses)
				}
			})

			t.Run("reads fabric-sensitive concrete event owned by the accessing fabric", func(t *testing.T) {
				t.Parallel()
				m := newEventPair(t)
				owner := m.fabricIndex(t)
				m.msgs.emitComplete(m.msgsEP, messageID, owner)
				r := m.read(t, fabricFiltered, nil, im.ConcreteEventPath{Cluster: messages.ClusterID, Event: messages.EventMessageComplete, HasCluster: true, HasEvent: true})
				ev := oneEvent(t, r, m.msgsEP, messages.ClusterID, messages.EventMessageComplete, im.EventPriorityInfo)
				var got messages.MessageCompleteEvent
				if err := got.DecodeTLV(ev.Value); err != nil || !bytes.Equal(got.MessageId, messageID) || got.FabricIndex != owner ||
					got.FutureMessagesPreference.Null || got.FutureMessagesPreference.Value != messages.FutureMessagePreferenceAllowed {
					t.Fatalf("MessageComplete %+v, %v", got, err)
				}
			})
		})
	}

	t.Run("reads concrete event as fabric scoped", func(t *testing.T) {
		t.Parallel()
		m := newEventPair(t)
		r := m.read(t, true, nil, biEvent(basicinfo.EventStartUp))
		m.assertTimestamp(t, oneEvent(t, r, 0, basicinfo.ClusterID, basicinfo.EventStartUp, im.EventPriorityCritical))
	})

	t.Run("reads concrete event with version filter", func(t *testing.T) {
		t.Parallel()
		m := newEventPair(t)
		startUp := oneEvent(t, m.read(t, false, nil, biEvent(basicinfo.EventStartUp)), 0, basicinfo.ClusterID, basicinfo.EventStartUp, im.EventPriorityCritical)
		r := m.read(t, false, []im.EventMinimumNumber{{EventMin: startUp.Number + 1}}, biEvent(basicinfo.EventStartUp))
		if len(r.Events) != 0 || len(r.EventStatuses) != 0 {
			t.Fatalf("events %+v, statuses %+v, want none", r.Events, r.EventStatuses)
		}
	})

	t.Run("reads non-existent concrete endpoint", func(t *testing.T) {
		t.Parallel()
		m := newEventPair(t)
		// Endpoint 3 is free: 1 is the Aggregator, 2 the Messages endpoint.
		p := biEvent(basicinfo.EventStartUp)
		p.Endpoint, p.HasEndpoint = 3, true
		oneEventStatus(t, m.read(t, false, nil, p), p, im.StatusUnsupportedEndpoint)
	})

	t.Run("reads non-existent concrete event", func(t *testing.T) {
		t.Parallel()
		m := newEventPair(t)
		p := im.ConcreteEventPath{Endpoint: 0, Cluster: gendiag.ClusterID, Event: gendiag.EventHardwareFaultChange, HasEndpoint: true, HasCluster: true, HasEvent: true}
		oneEventStatus(t, m.read(t, false, nil, p), p, im.StatusUnsupportedEvent)
	})

	rootBI := im.ConcreteEventPath{Endpoint: 0, Cluster: basicinfo.ClusterID, HasEndpoint: true, HasCluster: true}

	t.Run("reads wildcard endpoint & events with default events", func(t *testing.T) {
		t.Parallel()
		m := newEventPair(t)
		if got := countEvents(m.read(t, false, nil, rootBI)); len(got) != 1 || len(got[0]) != 1 || got[0][basicinfo.ClusterID] != 1 {
			t.Fatalf("counts %v, want {0: {40: 1}}", got)
		}
	})

	t.Run("reads wildcard endpoint & events with extra emitted events", func(t *testing.T) {
		t.Parallel()
		m := newEventPair(t)
		m.act(t, func(n *node.Node) { n.Root().BasicInformation.EmitStartUp() })
		if got := countEvents(m.read(t, false, nil, rootBI)); len(got) != 1 || len(got[0]) != 1 || got[0][basicinfo.ClusterID] != 2 {
			t.Fatalf("counts %v, want {0: {40: 2}}", got)
		}
	})

	t.Run("reads full wildcard", func(t *testing.T) {
		t.Parallel()
		m := newEventPair(t)
		m.act(t, func(n *node.Node) { n.Root().BasicInformation.EmitStartUp() })
		got := countEvents(m.read(t, false, nil, im.ConcreteEventPath{}))
		// matter.js: {0: {40: 2, 51: 1}} — two StartUps and BootReason.
		// The Go node's log holds what commissioning emitted too: the
		// AccessControlEntryChanged of the fabric's ACL entry.
		if got[0][basicinfo.ClusterID] != 2 || got[0][gendiag.ClusterID] != 1 {
			t.Fatalf("counts %v, want {0: {40: 2, 51: 1, …}}", got)
		}
		for ep, clusters := range got {
			for c := range clusters {
				if ep != 0 || (c != basicinfo.ClusterID && c != gendiag.ClusterID && c != 0x001F) {
					t.Fatalf("counts %v: events of %d/%d beyond the boot and commissioning ones", got, ep, c)
				}
			}
		}
	})

	t.Run("On commissioned node", func(t *testing.T) {
		t.Parallel()
		t.Run("Reads startup event via remote read", func(t *testing.T) {
			t.Parallel()
			m := newEventPair(t)
			p := biEvent(basicinfo.EventStartUp)
			p.Endpoint, p.HasEndpoint = 0, true
			r := m.read(t, true, []im.EventMinimumNumber{{EventMin: 12345}}, p)
			if !r.Empty() {
				t.Fatalf("report %+v, want empty", r)
			}
		})
	})
}

// eventPair is a commissioned pair whose device is MockServerNode's
// and serves a Messages cluster on a bridged endpoint.
type eventPair struct {
	*mockPair
	msgs   *messagesServer
	msgsEP uint16
}

func newEventPair(t *testing.T) *eventPair {
	t.Helper()
	site := quietSite(t)
	msgs := &messagesServer{fakeServer: fakeServer{cluster: messages.ClusterID, attrs: map[uint32]any{
		messages.AttrMessages: []any{}, messages.AttrActiveMessageIds: []any{},
	}}}
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{&customEndpoint{key: "messages:1", deviceType: 0x000A, servers: []contract.ClusterServer{msgs}}},
		Configure: func(cfg *node.Config) {
			cfg.BasicInformation.VendorName = mockVendorName
		},
	}})
	ep, ok := p.Device.EndpointOf("messages:1")
	if !ok {
		t.Fatal("no Messages endpoint")
	}
	return &eventPair{mockPair: &mockPair{site: site, pair: p}, msgs: msgs, msgsEP: ep}
}

func (m *eventPair) act(t *testing.T, fn func(n *node.Node)) {
	t.Helper()
	if err := m.pair.Device.Act(func(n *node.Node) error { fn(n); return nil }); err != nil {
		t.Fatal(err)
	}
}

// fabricIndex is the device's index of the pair's fabric.
func (m *eventPair) fabricIndex(t *testing.T) uint8 {
	t.Helper()
	fabrics, err := m.pair.Device.Node().Fabrics(parityCtx(t))
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("fabrics %+v, %v", fabrics, err)
	}
	return fabrics[0].FabricIndex
}

func (m *eventPair) read(t *testing.T, fabricFiltered bool, filters []im.EventMinimumNumber, paths ...im.ConcreteEventPath) *interaction.Report {
	t.Helper()
	r, err := fabrictest.ReadWith(parityCtx(t), m.pair, interaction.ReadRequest{Events: paths, EventFilters: filters, NoFabricFilter: !fabricFiltered})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (m *eventPair) assertTimestamp(t *testing.T, ev interaction.EventData) {
	t.Helper()
	now := m.site.Clock().Now()
	at := time.UnixMilli(int64(ev.Timestamp)) //nolint:gosec // a test clock's epoch milliseconds
	if d := now.Sub(at); d < 0 || d > 2*time.Second {
		t.Fatalf("timestamp %v, %v before the clock at %v", at, d, now)
	}
}

func oneEvent(t *testing.T, r *interaction.Report, ep uint16, cluster, event uint32, priority im.EventPriority) interaction.EventData {
	t.Helper()
	if len(r.Events) != 1 || len(r.EventStatuses) != 0 {
		t.Fatalf("events %+v, statuses %+v, want one event", r.Events, r.EventStatuses)
	}
	ev := r.Events[0]
	if ev.Path.Endpoint != ep || ev.Path.Cluster != cluster || ev.Path.Event != event || ev.Priority != priority || ev.Number == 0 {
		t.Fatalf("event %+v, want %d/%d/%d priority %d", ev, ep, cluster, event, priority)
	}
	return ev
}

func oneEventStatus(t *testing.T, r *interaction.Report, p im.ConcreteEventPath, want im.StatusCode) {
	t.Helper()
	if len(r.Events) != 0 || len(r.EventStatuses) != 1 {
		t.Fatalf("events %+v, statuses %+v, want one status", r.Events, r.EventStatuses)
	}
	s := r.EventStatuses[0]
	if s.Path.Endpoint != p.Endpoint || s.Path.Cluster != p.Cluster || s.Path.Event != p.Event || s.Status != want {
		t.Fatalf("status %+v, want 0x%02x at %d/%d/%d", s, uint8(want), p.Endpoint, p.Cluster, p.Event)
	}
}

// countEvents is matter.js countEvents (EventReadResponseTest.ts:481-496).
func countEvents(r *interaction.Report) map[uint16]map[uint32]int {
	out := map[uint16]map[uint32]int{}
	for i := range r.Events {
		e := &r.Events[i]
		if out[e.Path.Endpoint] == nil {
			out[e.Path.Endpoint] = map[uint32]int{}
		}
		out[e.Path.Endpoint][e.Path.Cluster]++
	}
	return out
}

// messagesServer is a Messages server that emits MessageComplete —
// matter.js's MessagesServer.with("ReceivedConfirmation").
type messagesServer struct {
	fakeServer
	mu      sync.Mutex
	emitter contract.EventEmitter
}

func (s *messagesServer) SetMatterEventEmitter(e contract.EventEmitter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitter = e
}

func (s *messagesServer) MatterEvents() []uint32 {
	return []uint32{messages.EventMessageQueued, messages.EventMessagePresented, messages.EventMessageComplete}
}

func (s *messagesServer) emitComplete(ep uint16, id []byte, fabricIndex uint8) {
	s.mu.Lock()
	e := s.emitter
	s.mu.Unlock()
	e.MatterEmitEvent(ep, messages.ClusterID, messages.EventMessageComplete, messages.MessageCompleteEvent{
		MessageId: id, FutureMessagesPreference: spec.Nullable[messages.FutureMessagePreferenceEnum]{Value: messages.FutureMessagePreferenceAllowed},
		FabricIndex: fabricIndex,
	}, contract.EventPriorityInfo)
}
