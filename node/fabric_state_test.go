// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
)

// scopedOnOff is an OnOff server that holds state for the fabrics in held
// ([contract.FabricScopedState]) and records every drop.
type scopedOnOff struct {
	*onoff.Server
	mu      sync.Mutex
	held    []uint8
	dropped []uint8
}

var _ contract.FabricScopedState = (*scopedOnOff)(nil)

func (s *scopedOnOff) MatterDropFabric(_ context.Context, fabricIndex uint8) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropped = append(s.dropped, fabricIndex)
	s.held = slices.DeleteFunc(s.held, func(f uint8) bool { return f == fabricIndex })
	return nil
}

func (s *scopedOnOff) MatterHeldFabrics() []uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.held)
}

func (s *scopedOnOff) drops() []uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.dropped)
}

// scopedPlug is a bridged OnOffPlugInUnit whose OnOff server is a
// scopedOnOff.
type scopedPlug struct {
	key string
	srv *scopedOnOff
}

func newScopedPlug(t *testing.T, key string, held ...uint8) *scopedPlug {
	t.Helper()
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT})
	if err != nil {
		t.Fatal(err)
	}
	return &scopedPlug{key: key, srv: &scopedOnOff{Server: srv, held: held}}
}

func (p *scopedPlug) Spec() endpoint.Spec {
	return endpoint.Spec{
		StableKey: endpoint.StringKey(p.key), DeviceAddress: p.key, ChannelAddress: p.key + ":0",
		DeviceType: onoff.DeviceTypeOnOffPlugInUnit, FriendlyName: p.key, Source: p,
	}
}

func (p *scopedPlug) MatterDeviceType() uint16 { return onoff.DeviceTypeOnOffPlugInUnit }

func (p *scopedPlug) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{p.srv}
}

// TestRemoveFabricDropsTheFabricStateOfEveryEndpoint: a fabric's removal
// reaches the removal contract of a bridged endpoint's server
// (decision 37: the node calls it for every server).
func TestRemoveFabricDropsTheFabricStateOfEveryEndpoint(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	p := newScopedPlug(t, "plug:scoped")
	if err := n.Aggregator().Add(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
	if _, err := c.Commission(ctx); err != nil {
		t.Fatalf("Commission: %v", err)
	}
	fi := c.FabricIndex()
	if err := n.RemoveFabric(ctx, fi); err != nil {
		t.Fatalf("RemoveFabric: %v", err)
	}
	if got := p.srv.drops(); !slices.Contains(got, fi) {
		t.Errorf("bridged server drops %v after RemoveFabric(%d), want %d among them", got, fi, fi)
	}
}

// TestRemoveFabricDropsTheExtensionEntry: a removed fabric's
// AccessControl Extension entry leaves memory and the store
// (review II, R2-P2-fabric-03): another fabric's unfiltered read lists
// none, and a restart on the same store restores none.
func TestRemoveFabricDropsTheExtensionEntry(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "node.db")
	n, c, _ := commissioned(t, minimalConfig(openDB(t, path)))
	fi := c.FabricIndex()
	ac := n.Root().AccessControl
	data := []byte{0x17, 0x18} // an anonymous list, then its end
	if err := ac.MatterWrite(im.WithFabricFilter(ctx, true, fi), 0x0001, []core.AccessControlExtensionEntry{{Data: data}}); err != nil {
		t.Fatalf("write Extension: %v", err)
	}
	if err := n.RemoveFabric(ctx, fi); err != nil {
		t.Fatalf("RemoveFabric: %v", err)
	}
	v, _ := ac.MatterReadFiltered(im.WithFabricFilter(ctx, false, fi+1), 0x0001)
	if got, _ := v.([]core.AccessControlExtensionEntry); len(got) != 0 {
		t.Errorf("unfiltered Extension after RemoveFabric(%d) = %+v, want none", fi, got)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	again := newNode(t, minimalConfig(openDB(t, path)))
	if held := again.Root().AccessControl.MatterHeldFabrics(); len(held) != 0 {
		t.Errorf("a restart restores the Extension of fabrics %v, want none", held)
	}
}

// TestStartReconcilesFabricStateAgainstTheTable: state a server holds for
// a fabric index the table does not hold is dropped before the topology
// is served, at start and when an endpoint joins a running node; state of
// a fabric the table holds stays (decision 37, R2-P2-fabric-06).
func TestStartReconcilesFabricStateAgainstTheTable(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "node.db")
	first, c, _ := commissioned(t, minimalConfig(openDB(t, path)))
	fi := c.FabricIndex()
	const stale = 200
	if fi == stale {
		t.Fatalf("the commissioned fabric has the stale index %d", stale)
	}
	if err := first.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	n := newNode(t, minimalConfig(openDB(t, path)))
	atStart := newScopedPlug(t, "plug:start", fi, stale)
	if err := n.Aggregator().Add(ctx, atStart); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := atStart.srv.drops(); !slices.Equal(got, []uint8{stale}) {
		t.Errorf("drops at start %v, want [%d]", got, stale)
	}
	if held := atStart.srv.MatterHeldFabrics(); !slices.Equal(held, []uint8{fi}) {
		t.Errorf("held after start %v, want [%d]", held, fi)
	}

	joining := newScopedPlug(t, "plug:joining", stale)
	if err := n.Aggregator().Add(ctx, joining); err != nil {
		t.Fatal(err)
	}
	if got := joining.srv.drops(); !slices.Equal(got, []uint8{stale}) {
		t.Errorf("drops when the endpoint joined %v, want [%d]", got, stale)
	}
}

// TestStartDropsAnExtensionWhoseFabricIsGone: an Extension entry stored
// for a fabric the table does not hold — a run that stopped between the
// store's removal and the fan-out — is dropped from memory and from its
// setting at start (R2-P2-fabric-06, findings-store design note 5).
func TestStartDropsAnExtensionWhoseFabricIsGone(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "node.db")
	const key = "access_control.extension.7"
	seed := newNode(t, minimalConfig(openDB(t, path)))
	if err := seed.Store().SetSetting(ctx, key, "1718"); err != nil {
		t.Fatal(err)
	}
	if err := seed.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	n := newNode(t, minimalConfig(openDB(t, path)))
	if held := n.Root().AccessControl.MatterHeldFabrics(); !slices.Equal(held, []uint8{7}) {
		t.Fatalf("restored Extension fabrics %v, want [7]", held)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if held := n.Root().AccessControl.MatterHeldFabrics(); len(held) != 0 {
		t.Errorf("Extension fabrics after start %v, want none", held)
	}
	if v, found, err := n.Store().GetSetting(ctx, key); err != nil || (found && v != "") {
		t.Errorf("setting %s after start = %q (found %v, err %v), want cleared", key, v, found, err)
	}
}
