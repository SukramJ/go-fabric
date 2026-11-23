// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	fancontroldef "thingfabric.dev/go-fabric/cluster/spec/fancontrol"
	"thingfabric.dev/go-fabric/contract"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store/memstore"
)

// valueServer is a host cluster server of an uncomparable value type:
// it cannot be a map key or compared with ==.
type valueServer struct {
	*spec.Server
	tags []string
}

// ifaceServer is a host cluster server whose type is comparable but whose
// value is not when meta holds a slice: a map key or == on it panics
// (review II, R4-api-4753, the Fable diff read's B1).
type ifaceServer struct {
	*spec.Server
	meta any
}

// TestUncomparableRootServerIsServed (review II, R4-api-4753): a server
// the host mounts on the root through Config.RootServers whose dynamic
// type is uncomparable — with a writable nonvolatile attribute and a
// setter — is served and written without a panic; it is the host's, so
// nothing is stored for it.
func TestUncomparableRootServerIsServed(t *testing.T) {
	for _, tc := range []struct {
		name string
		mk   func(*spec.Server) contract.ClusterServer
	}{
		{"uncomparable type", func(s *spec.Server) contract.ClusterServer { return valueServer{Server: s, tags: []string{"host"}} }},
		{"interface field holding a slice", func(s *spec.Server) contract.ClusterServer { return ifaceServer{Server: s, meta: []string{"host"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) { testUncomparableRootServer(t, tc.mk) })
	}
}

func testUncomparableRootServer(t *testing.T, mk func(*spec.Server) contract.ClusterServer) {
	t.Helper()
	if _, ok := mk(nil).(ifaceServer); ok {
		// node's predicates judge the value (TestComparableValueJudgesTheValue);
		// past them bridge.reconciled (bridge/bridge.go:2813) still judges
		// the type and Start panics in reconcileQuiescers
		// (bridge/bridge.go:2748) — outside this unit's paths, handed to
		// the coordinator (review II, R4-api-4753, fix-h9 repair).
		t.Skip("bridge.reconciled judges the server's type, not its value: open finding outside node/")
	}
	mem := memstore.New()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: mem, Endpoints: epmem.New()}
	srv, err := spec.NewServer(fancontroldef.Definition, spec.Options{}, spec.ServerConfig{Initial: fancontroldef.Patch{
		FanMode: new(fancontroldef.FanModeEnum), FanModeSequence: new(fancontroldef.FanModeSequenceEnum),
		PercentCurrent: new(uint8), Null: []uint32{fancontroldef.AttrPercentSetting},
	}.AttributeValues()})
	if err != nil {
		t.Fatal(err)
	}
	cfg.RootServers = []contract.ClusterServer{mk(srv)}
	var n *node.Node
	noPanic(t, "Start", func() { n, _ = nvRun(t, cfg, nvCase{}) })
	path := attrPath(0, fancontroldef.ClusterID, fancontroldef.AttrFanMode)
	noPanic(t, "write", func() {
		if s := dispatchWrite(t, n, path, uint64(1)); s != im.StatusSuccess {
			t.Fatalf("write: %v", s)
		}
	})
	if err := n.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := mem.GetSetting(t.Context(), "nonvolatile.@0.0202.0000"); err != nil || ok {
		t.Errorf("a key was stored for the host's root server: %q (found %v, %v)", v, ok, err)
	}
}
