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

// TestUncomparableRootServerIsServed (review II, R4-api-4753): a server
// the host mounts on the root through Config.RootServers whose dynamic
// type is uncomparable — with a writable nonvolatile attribute and a
// setter — is served and written without a panic; it is the host's, so
// nothing is stored for it.
func TestUncomparableRootServerIsServed(t *testing.T) {
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
	cfg.RootServers = []contract.ClusterServer{valueServer{Server: srv, tags: []string{"host"}}}
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
