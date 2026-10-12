// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node_test

// The host's observers (node.Config.Observers, issue #58), each proven to
// run after the facade's own handler for its event: what the facade's
// handler did is checked from inside the observer.

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

// commissionableRecord returns the discriminator and CM of the published
// commissionable record, ok false without one.
func commissionableRecord(adv *mdns.Noop) (discriminator, mode uint64, ok bool) {
	services := adv.Active()
	for i := range services {
		svc := &services[i]
		if svc.ServiceType != mdns.ServiceTypeCommissionable {
			continue
		}
		for _, txt := range svc.TXT {
			switch txt.Key {
			case "D":
				discriminator, _ = strconv.ParseUint(txt.Value, 10, 16)
			case "CM":
				mode, _ = strconv.ParseUint(txt.Value, 10, 8)
			}
		}
		return discriminator, mode, true
	}
	return 0, 0, false
}

// hasOperationalRecord reports whether an operational record is published.
func hasOperationalRecord(adv *mdns.Noop) bool {
	services := adv.Active()
	for i := range services {
		if services[i].ServiceType == mdns.ServiceTypeOperational {
			return true
		}
	}
	return false
}

// TestObserversOnCommissioning: a commissioning fires OnFabricAdded with
// the fabric AddNOC installed — after the facade announced its operational
// record — and then OnCommissioningComplete with the same fabric index.
func TestObserversOnCommissioning(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Advertiser = adv

	var (
		mu     sync.Mutex
		events []string
		added  node.FabricInfo
		recAt  bool
		doneAt uint8
	)
	cfg.Observers.OnFabricAdded = func(info node.FabricInfo) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, "added")
		added = info
		recAt = hasOperationalRecord(adv)
		return nil
	}
	cfg.Observers.OnCommissioningComplete = func(fabricIndex uint8) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, "complete")
		doneAt = fabricIndex
		// An observer's error is logged, not returned: the commissioning
		// still completes.
		return errors.New("observer failure")
	}
	n := newNode(t, cfg)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	opts := commissionerOptions()
	opts.Advertiser = adv
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), opts)
	if _, err := c.Commission(context.Background()); err != nil {
		t.Fatalf("Commission: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0] != "added" || events[1] != "complete" {
		t.Fatalf("observer events %v, want [added complete]", events)
	}
	if added.FabricIndex != c.FabricIndex() || added.FabricID != testFabricID || added.NodeID != testNodeID {
		t.Errorf("OnFabricAdded %+v, want index %d fabric 0x%X node 0x%X", added, c.FabricIndex(), testFabricID, testNodeID)
	}
	if added.CompressedFabricID != c.CompressedFabricID() {
		t.Errorf("OnFabricAdded compressed id %X, want %X", added.CompressedFabricID, c.CompressedFabricID())
	}
	if len(added.RootPublicKey) != 65 || added.RootPublicKey[0] != 0x04 {
		t.Errorf("OnFabricAdded root public key %X, want an uncompressed P-256 point", added.RootPublicKey)
	}
	if !recAt {
		t.Error("OnFabricAdded ran before the facade announced the operational record")
	}
	if doneAt != c.FabricIndex() {
		t.Errorf("OnCommissioningComplete fabric index %d, want %d", doneAt, c.FabricIndex())
	}
}

// TestObserverOnFabricRemoved: RemoveFabric fires OnFabricRemoved with the
// removed fabric's identity after the facade's cleanup — when the observer
// runs, the fabric and its ACL are gone from the store, the operational
// record is withdrawn and the node's own window is open again.
func TestObserverOnFabricRemoved(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Advertiser = adv

	var ref atomic.Pointer[node.Node]
	type seen struct {
		info       node.FabricInfo
		fabrics    int
		acl        int
		record     bool
		windowOpen bool
	}
	removed := make(chan seen, 1)
	cfg.Observers.OnFabricRemoved = func(info node.FabricInfo) error {
		n := ref.Load()
		ctx := context.Background()
		fabrics, _ := n.Fabrics(ctx)
		acl, _ := n.Store().ListACL(ctx, info.FabricIndex)
		removed <- seen{info: info, fabrics: len(fabrics), acl: len(acl), record: hasOperationalRecord(adv), windowOpen: n.CommissioningWindow().IsOpen()}
		return nil
	}
	n := newNode(t, cfg)
	ref.Store(n)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	opts := commissionerOptions()
	opts.Advertiser = adv
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), opts)
	s, err := c.Commission(context.Background())
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	if acl, err := n.Store().ListACL(context.Background(), c.FabricIndex()); err != nil || len(acl) == 0 {
		t.Fatalf("ACL before RemoveFabric = %+v, %v; want the commissioner's entry", acl, err)
	}
	if status, err := c.RemoveFabric(context.Background(), s); err != nil || status != 0 {
		t.Fatalf("RemoveFabric = %d, %v", status, err)
	}
	var got seen
	select {
	case got = <-removed:
	case <-time.After(5 * time.Second):
		t.Fatal("OnFabricRemoved did not run")
	}
	if got.info.FabricIndex != c.FabricIndex() || got.info.FabricID != testFabricID || got.info.NodeID != testNodeID || got.info.CompressedFabricID != c.CompressedFabricID() || len(got.info.RootPublicKey) != 65 {
		t.Errorf("OnFabricRemoved %+v, want the removed fabric's identity", got.info)
	}
	if got.fabrics != 0 || got.acl != 0 {
		t.Errorf("when OnFabricRemoved ran: %d fabrics, %d ACL entries; want the facade's cleanup done", got.fabrics, got.acl)
	}
	if got.record {
		t.Error("when OnFabricRemoved ran the operational record was still published")
	}
	if !got.windowOpen {
		t.Error("when OnFabricRemoved ran the node's own window was not reopened")
	}
}

// TestObserverOnWindowTransition: the node's own window opening at Start
// and its revocation each fire OnWindowTransition once, after the facade
// published (then withdrew) the commissionable record. A panicking
// observer does not stop the facade.
func TestObserverOnWindowTransition(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Advertiser = adv

	type seen struct {
		snap   node.WindowSnapshot
		record bool
	}
	var calls atomic.Int32
	transitions := make(chan seen, 4)
	cfg.Observers.OnWindowTransition = func(snap node.WindowSnapshot) error {
		_, _, record := commissionableRecord(adv)
		transitions <- seen{snap: snap, record: record}
		if calls.Add(1) == 1 {
			panic("observer panic")
		}
		return nil
	}
	n := newNode(t, cfg)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := n.CommissioningWindow().RevokeWindow(context.Background()); err != nil {
		t.Fatalf("RevokeWindow: %v", err)
	}

	next := func() seen {
		t.Helper()
		select {
		case s := <-transitions:
			return s
		case <-time.After(5 * time.Second):
			t.Fatal("OnWindowTransition did not run")
			return seen{}
		}
	}
	opened := next()
	if !opened.snap.Open || !opened.snap.Own || opened.snap.CommissioningMode != 1 || opened.snap.Discriminator != 3840 {
		t.Errorf("open transition %+v, want the own window, CM=1, discriminator 3840", opened.snap)
	}
	if opened.snap.Administrator.Status != wire.WindowStatusClosed {
		t.Errorf("open transition administrator status %d, want WindowNotOpen for the own window", opened.snap.Administrator.Status)
	}
	if !opened.record {
		t.Error("OnWindowTransition (open) ran before the facade published the commissionable record")
	}
	closed := next()
	if closed.snap.Open || closed.snap.CommissioningMode != 0 || closed.snap.Discriminator != 0 {
		t.Errorf("close transition %+v, want closed, no record", closed.snap)
	}
	if closed.record {
		t.Error("OnWindowTransition (close) ran before the facade withdrew the commissionable record")
	}
	select {
	case s := <-transitions:
		t.Errorf("a third transition %+v", s.snap)
	case <-time.After(100 * time.Millisecond):
	}
}
