// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	fancontroldef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	"github.com/SukramJ/go-fabric/contract"
	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestNonVolatileHostRootServerIsTheHosts: a server the host mounts on the
// root (Config.RootServers) keeps its own state: a controller's write of
// its writable nonvolatile attribute is not stored, and the restart serves
// the host's Initial (R2-P2-dr-node-05, owner decision 38).
func TestNonVolatileHostRootServerIsTheHosts(t *testing.T) {
	t.Parallel()
	mem := memstore.New()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: mem, Endpoints: epmem.New()}
	hostFan := func(t *testing.T) contract.ClusterServer {
		t.Helper()
		srv, err := spec.NewServer(fancontroldef.Definition, spec.Options{}, spec.ServerConfig{Initial: fancontroldef.Patch{
			FanMode: new(fancontroldef.FanModeEnum), FanModeSequence: new(fancontroldef.FanModeSequenceEnum),
			PercentCurrent: new(uint8), Null: []uint32{fancontroldef.AttrPercentSetting},
		}.AttributeValues()})
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}
	path := attrPath(0, fancontroldef.ClusterID, fancontroldef.AttrFanMode)
	c := cfg
	c.RootServers = []contract.ClusterServer{hostFan(t)}
	n, _ := nvRun(t, c, nvCase{})
	before := dispatchRead(t, n, path)
	if s := dispatchWrite(t, n, path, uint64(1)); s != im.StatusSuccess {
		t.Fatalf("write: %v", s)
	}
	if err := n.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := mem.GetSetting(t.Context(), "nonvolatile.@0.0202.0000"); err != nil || ok {
		t.Errorf("a key was stored for the host's root server: %q (found %v, %v)", v, ok, err)
	}
	c.RootServers = []contract.ClusterServer{hostFan(t)}
	again, _ := nvRun(t, c, nvCase{})
	if got := dispatchRead(t, again, path); got != before {
		t.Errorf("the host's FanMode after the restart %s, its Initial %s", got, before)
	}
}
