// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/NodeLifecycleSeededTest.ts at
// 862ad75f: when the client node considers its structure seeded, and the
// warning for a node of its root only (Peers.ts #evaluateSeeded); at
// b25ed960 the placement cases of a7ac64f19 and 097e5a8da — an
// aggregator's devices reported before the root's PartsList, and describe
// "ClientNode with a root that reports no device type". matter.js
// applies Descriptor reports to an unread peer through
// clientStructureOf(peer).mutate; the port applies them to a node through
// Node.apply. matter.js installs BasicInformation on the unread peer by
// hand; the port reports its cluster with the root's Descriptor. The
// commissioned-pair cases are in node_lifecycle_seeded_parity_matterjs_test.go.
//
// Where matter.js also reads an unreported mandatory list as empty
// (deviceTypeList, partsList), the Go image holds no value; the port
// asserts the seeding and the warning only.
package client

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
)

const (
	dtOTAProvider = 0x0014
	rootOnlyMsg   = "matter.controller.client.root_only"
)

// descriptorReports is NodeLifecycleSeededTest.ts:29-85 descriptorReports:
// endpoint ep's Descriptor — DeviceTypeList dts (nil: not reported),
// PartsList parts (nil: not reported); the root's ServerList names
// BasicInformation unless !withBasicInformation, and then the report
// carries its cluster.
func descriptorReports(ep uint16, dts []uint64, parts []uint16, withBasicInformation bool) *report {
	b := &report{}
	servers := []uint64{uint64(descriptor.ClusterID)}
	if ep == 0 && withBasicInformation {
		servers = append(servers, clBasicInfo)
	}
	values := map[uint32]spec.Node{
		descriptor.AttrServerList: listNode(servers...),
		descriptor.AttrClientList: listNode(),
	}
	if dts != nil {
		values[descriptor.AttrDeviceTypeList] = deviceTypes(dts...)
	}
	if parts != nil {
		values[descriptor.AttrPartsList] = listNode(ids16(parts...)...)
	}
	b.cluster(ep, descriptor.ClusterID, 0, values)
	if ep == 0 && withBasicInformation {
		b.cluster(0, clBasicInfo, 0, map[uint32]spec.Node{0: uintNode(17)})
	}
	return b
}

// seededNode is a node with a Seeded counter and its logs.
type seededNode struct {
	n    *Node
	logs *logRecorder
	mu   sync.Mutex
	hits int
}

func newSeededNode(t *testing.T, st Store) *seededNode {
	t.Helper()
	s := &seededNode{logs: &logRecorder{}}
	n, err := New(Config{ID: "n", Store: st, Interaction: interaction.Bind(nil, interaction.Config{}), Hooks: Hooks{Seeded: func() {
		s.mu.Lock()
		s.hits++
		s.mu.Unlock()
	}}, Logger: slog.New(s.logs)})
	if err != nil {
		t.Fatal(err)
	}
	s.n = n
	return s
}

func (s *seededNode) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

// warnings is the number of root-only warnings logged.
func (s *seededNode) warnings() int {
	s.logs.mu.Lock()
	defer s.logs.mu.Unlock()
	n := 0
	for _, m := range s.logs.msgs {
		if m == rootOnlyMsg {
			n++
		}
	}
	return n
}

func (s *seededNode) apply(b *report) { s.n.apply(wildcard, &b.r, true) }

// logged is the number of records logged as msg.
func (s *seededNode) logged(msg string) int {
	s.logs.mu.Lock()
	defer s.logs.mu.Unlock()
	n := 0
	for _, m := range s.logs.msgs {
		if m == msg {
			n++
		}
	}
	return n
}

// ownerIs fails unless endpoint ep is installed below owner.
func ownerIs(t *testing.T, n *Node, ep, owner uint16) {
	t.Helper()
	e := n.Endpoint(ep)
	if e == nil || e.Owner() == nil || e.Owner().Number() != owner {
		t.Fatalf("endpoint %d: not below endpoint %d (%s)", ep, owner, tree(n))
	}
}

func TestNodeLifecycleSeededParity(t *testing.T) {
	t.Parallel()

	t.Run("does not seed before the structure is read", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		b := &report{}
		b.cluster(0, clBasicInfo, 0, map[uint32]spec.Node{0: uintNode(17)})
		b.r.Attributes = append(b.r.Attributes, descriptorReports(1, []uint64{dtOnOffLight}, []uint16{}, false).r.Attributes...)
		s.apply(b)
		if s.n.Seeded() || s.count() != 0 {
			t.Fatal("seeded without the root's Descriptor")
		}
	})

	t.Run("seeds a root-only node once commissioning has read it and logs the warning once", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(descriptorReports(0, []uint64{dtRootNode}, []uint16{}, true))
		if len(s.n.Endpoints()) != 1 || !s.n.Seeded() || s.warnings() != 1 {
			t.Fatalf("endpoints %d, seeded %v, warnings %d", len(s.n.Endpoints()), s.n.Seeded(), s.warnings())
		}
		ctx := context.Background()
		if err := s.n.Disable(ctx); err != nil {
			t.Fatal(err)
		}
		if err := s.n.Enable(ctx); err != nil {
			t.Fatal(err)
		}
		s.apply(descriptorReports(0, []uint64{dtRootNode}, []uint16{}, true))
		if !s.n.Seeded() || s.warnings() != 1 {
			t.Fatalf("after a re-read: seeded %v, warnings %d, want one", s.n.Seeded(), s.warnings())
		}
	})

	t.Run("seeds a root-only node whose PartsList arrives in a later read than BasicInformation", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		b := &report{}
		b.cluster(0, clBasicInfo, 0, map[uint32]spec.Node{0: uintNode(17)})
		s.apply(b)
		s.apply(descriptorReports(0, []uint64{dtRootNode}, []uint16{}, true))
		if !s.n.Seeded() || s.count() != 1 || s.warnings() != 1 {
			t.Fatalf("seeded %v ×%d, warnings %d", s.n.Seeded(), s.count(), s.warnings())
		}
	})

	t.Run("seeds a root-only node with a node-scoped device type without the warning", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(descriptorReports(0, []uint64{dtRootNode, dtOTAProvider}, []uint16{}, true))
		if !s.n.Seeded() || s.warnings() != 0 {
			t.Fatalf("seeded %v, warnings %d", s.n.Seeded(), s.warnings())
		}
	})

	t.Run("does not seed while the root PartsList names endpoints that are not installed yet", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		var mu sync.Mutex
		var order []string
		s.n.cfg.Hooks.ClusterInstalled = func(ep *Endpoint, c *Cluster) {
			mu.Lock()
			order = append(order, "installed")
			mu.Unlock()
		}
		s.n.cfg.Hooks.EndpointInstalled = func(ep *Endpoint) {
			mu.Lock()
			order = append(order, "endpoint")
			mu.Unlock()
		}
		seeded := s.n.cfg.Hooks.Seeded
		s.n.cfg.Hooks.Seeded = func() {
			mu.Lock()
			order = append(order, "seeded")
			mu.Unlock()
			seeded()
		}
		s.apply(descriptorReports(0, []uint64{dtRootNode}, []uint16{1}, true))
		if len(s.n.Endpoints()) != 1 || s.n.Seeded() || s.count() != 0 {
			t.Fatalf("after the root: endpoints %d, seeded %v", len(s.n.Endpoints()), s.n.Seeded())
		}
		mu.Lock()
		order = nil
		mu.Unlock()
		s.apply(descriptorReports(1, []uint64{dtOnOffLight}, []uint16{}, false))
		if s.n.Endpoint(1) == nil || !s.n.Seeded() || s.count() != 1 {
			t.Fatalf("after endpoint 1: seeded %v ×%d", s.n.Seeded(), s.count())
		}
		mu.Lock()
		defer mu.Unlock()
		if len(order) == 0 || order[len(order)-1] != "seeded" || order[0] == "seeded" {
			t.Fatalf("hooks %v: the endpoint's announcement must precede Seeded", order)
		}
	})

	t.Run("seeds once the root PartsList stops naming an endpoint that never arrived", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(descriptorReports(0, []uint64{dtRootNode}, []uint16{1}, true))
		if s.n.Seeded() {
			t.Fatal("seeded while endpoint 1 is missing")
		}
		s.apply(descriptorReports(0, []uint64{dtRootNode}, []uint16{}, true))
		if !s.n.Seeded() || s.count() != 1 || s.warnings() != 1 {
			t.Fatalf("seeded %v ×%d, warnings %d", s.n.Seeded(), s.count(), s.warnings())
		}
	})

	t.Run("does not seed a node without BasicInformation", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(descriptorReports(0, []uint64{dtRootNode}, []uint16{}, false))
		if s.n.Seeded() || s.count() != 0 {
			t.Fatal("seeded without BasicInformation")
		}
	})

	t.Run("warns about a root-only node with an empty DeviceTypeList", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(descriptorReports(0, []uint64{}, []uint16{}, true))
		if !s.n.Seeded() || s.warnings() != 1 {
			t.Fatalf("seeded %v, warnings %d", s.n.Seeded(), s.warnings())
		}
	})

	t.Run("does not warn about a root-only node that did not report its DeviceTypeList", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(descriptorReports(0, nil, []uint16{}, true))
		if !s.n.Seeded() || s.warnings() != 0 {
			t.Fatalf("seeded %v, warnings %d", s.n.Seeded(), s.warnings())
		}
	})

	t.Run("does not seed on a root Descriptor that did not report its PartsList", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(descriptorReports(0, []uint64{dtRootNode}, nil, true))
		if s.n.Seeded() {
			t.Fatal("seeded without a PartsList")
		}
		s.apply(descriptorReports(0, []uint64{dtRootNode}, []uint16{}, true))
		if !s.n.Seeded() || s.count() != 1 {
			t.Fatalf("seeded %v ×%d", s.n.Seeded(), s.count())
		}
	})

	t.Run("seeds a root-only peer restored from storage while it is offline", func(t *testing.T) {
		t.Parallel()
		st := &MemoryStore{}
		s := newSeededNode(t, st)
		s.apply(descriptorReports(0, []uint64{dtRootNode, dtOTAProvider}, []uint16{}, true))
		if !s.n.Seeded() {
			t.Fatal("not seeded")
		}
		restored := newSeededNode(t, st)
		if len(restored.n.Endpoints()) != 1 || !restored.n.Seeded() {
			t.Fatalf("restored: endpoints %d, seeded %v", len(restored.n.Endpoints()), restored.n.Seeded())
		}
	})

	t.Run("does not wait for a PartsList entry that is no endpoint number", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(descriptorReports(0, []uint64{dtRootNode}, []uint16{0xFFFF}, true))
		if !s.n.Seeded() || s.warnings() != 1 {
			t.Fatalf("seeded %v, warnings %d", s.n.Seeded(), s.warnings())
		}
	})
}

// rootWithoutDeviceTypeMsg is ClientStructure's warning for a root whose
// reported DeviceTypeList names no device type.
const rootWithoutDeviceTypeMsg = "matter.controller.client.root_without_device_type"

// TestNodeLifecycleSeededRootWithoutDeviceTypeParity is describe("ClientNode
// with a root that reports no device type") (NodeLifecycleSeededTest.ts
// :456-610 at b25ed960, a7ac64f19 and 097e5a8da).
func TestNodeLifecycleSeededRootWithoutDeviceTypeParity(t *testing.T) {
	t.Parallel()
	both := func(rs ...*report) *report {
		out := &report{}
		for _, r := range rs {
			out.r.Attributes = append(out.r.Attributes, r.r.Attributes...)
		}
		return out
	}

	t.Run("installs and seeds the endpoints the root names and warns once", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(both(descriptorReports(0, []uint64{}, []uint16{1}, true), descriptorReports(1, []uint64{dtOnOffLight}, []uint16{}, false)))
		if got := tree(s.n); got != "0:0x16:- 1:0x100:0" || !s.n.Seeded() || s.count() != 1 || s.logged(rootWithoutDeviceTypeMsg) != 1 {
			t.Fatalf("tree %s, seeded %v ×%d, warnings %d", got, s.n.Seeded(), s.count(), s.logged(rootWithoutDeviceTypeMsg))
		}
		s.apply(descriptorReports(0, []uint64{}, []uint16{1}, true))
		if n := s.logged(rootWithoutDeviceTypeMsg); n != 1 {
			t.Fatalf("warnings %d after a second read, want one", n)
		}
	})

	t.Run("places a nested part below its closest claimant", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(both(
			descriptorReports(0, []uint64{}, []uint16{1, 2}, true),
			descriptorReports(1, []uint64{dtAggregator}, []uint16{2}, false),
			descriptorReports(2, []uint64{dtOnOffLight}, []uint16{}, false)))
		if !s.n.Seeded() {
			t.Fatal("not seeded")
		}
		ownerIs(t, s.n, 1, 0)
		ownerIs(t, s.n, 2, 1)
	})

	t.Run("places parts below an aggregator when the root reports only an application device type", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(both(
			descriptorReports(0, []uint64{dtOnOffLight}, []uint16{1, 2}, true),
			descriptorReports(1, []uint64{dtAggregator}, []uint16{2}, false),
			descriptorReports(2, []uint64{dtOnOffLight}, []uint16{}, false)))
		ownerIs(t, s.n, 1, 0)
		ownerIs(t, s.n, 2, 1)
	})

	t.Run("places the devices an aggregator names when the root also omits one of them", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		for range 2 {
			s.apply(both(
				descriptorReports(0, []uint64{}, []uint16{1, 2}, true),
				descriptorReports(1, []uint64{dtAggregator}, []uint16{2, 3}, false),
				descriptorReports(2, []uint64{dtOnOffLight}, []uint16{}, false),
				descriptorReports(3, []uint64{dtOnOffLight}, []uint16{}, false)))
		}
		if !s.n.Seeded() {
			t.Fatal("not seeded")
		}
		ownerIs(t, s.n, 1, 0)
		ownerIs(t, s.n, 2, 1)
		ownerIs(t, s.n, 3, 1)
		if a, b := s.logged(rootWithoutDeviceTypeMsg), s.logged(incompleteRootMsg); a != 1 || b != 1 {
			t.Fatalf("root without device type warned %d, incomplete root %d, want one each", a, b)
		}
	})

	t.Run("restores the endpoints from storage and warns again after a restart", func(t *testing.T) {
		t.Parallel()
		st := &MemoryStore{}
		s := newSeededNode(t, st)
		s.apply(both(descriptorReports(0, []uint64{}, []uint16{1}, true), descriptorReports(1, []uint64{dtOnOffLight}, []uint16{}, false)))
		restored := newSeededNode(t, st)
		if got := tree(restored.n); got != "0:0x16:- 1:0x100:0" || !restored.n.Seeded() {
			t.Fatalf("restored tree %s, seeded %v", got, restored.n.Seeded())
		}
		// One warning per start: matter.js counts two across both.
		if a, b := s.logged(rootWithoutDeviceTypeMsg), restored.logged(rootWithoutDeviceTypeMsg); a != 1 || b != 1 {
			t.Fatalf("warnings %d before and %d after the restart, want one each", a, b)
		}
	})

	t.Run("does not warn about a non-root endpoint that reports no device type", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(both(descriptorReports(0, []uint64{dtRootNode}, []uint16{1}, true), descriptorReports(1, []uint64{}, []uint16{}, false)))
		ownerIs(t, s.n, 1, 0)
		if n := s.logged(rootWithoutDeviceTypeMsg); n != 0 {
			t.Fatalf("warnings %d", n)
		}
	})

	t.Run("does not warn about a root that has not reported its DeviceTypeList", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(descriptorReports(0, nil, []uint16{}, true))
		s.apply(descriptorReports(0, nil, []uint16{}, true))
		if n := s.logged(rootWithoutDeviceTypeMsg); n != 0 {
			t.Fatalf("warnings %d", n)
		}
	})

	t.Run("does not warn about a root that reports Root Node", func(t *testing.T) {
		t.Parallel()
		s := newSeededNode(t, &MemoryStore{})
		s.apply(both(descriptorReports(0, []uint64{dtRootNode}, []uint16{1}, true), descriptorReports(1, []uint64{dtOnOffLight}, []uint16{}, false)))
		if !s.n.Seeded() || s.logged(rootWithoutDeviceTypeMsg) != 0 {
			t.Fatalf("seeded %v, warnings %d", s.n.Seeded(), s.logged(rootWithoutDeviceTypeMsg))
		}
	})
}
