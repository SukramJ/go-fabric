// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	userlabeldef "thingfabric.dev/go-fabric/cluster/spec/userlabel"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store/memstore"
)

// TestBridgedUserLabelHostChangeOwedUntilTheStop (review II,
// R2-P6-store-2416): a host's change of a bridged UserLabel list that the
// store refuses is owed — written again when the node stops — as every
// other nonvolatile change is, so the list served is the one a restart
// restores.
func TestBridgedUserLabelHostChangeOwedUntilTheStop(t *testing.T) {
	t.Parallel()
	st := &refusingKeys{Port: memstore.New(), keys: []string{"bridged_user_label."}}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	tc := nvCase{key: "light", build: func(t *testing.T, key string) node.Endpoint {
		t.Helper()
		d, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey(key), Name: key}, userlabeldef.Declare{})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}}
	n, ep := nvRun(t, cfg, tc)
	var setter spec.AttributeSetter
	for _, srv := range endpoint.ClusterServers(n.Bridge().Topology().FindByID(ep)) {
		if srv.MatterClusterID() == userlabeldef.ClusterID {
			setter, _ = srv.(spec.AttributeSetter)
		}
	}
	if setter == nil {
		t.Fatal("no UserLabel server")
	}
	st.refuse.Store(true)
	if err := setter.SetAttributes(map[uint32]any{userlabeldef.AttrLabelList: spec.List[userlabeldef.LabelStruct]{{Label: "room", Value: "hall"}}}); err != nil {
		t.Fatal(err)
	}
	served := dispatchRead(t, n, attrPath(ep, userlabeldef.ClusterID, userlabeldef.AttrLabelList))
	st.refuse.Store(false)
	if err := n.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	again, ep := nvRun(t, cfg, tc)
	if got := dispatchRead(t, again, attrPath(ep, userlabeldef.ClusterID, userlabeldef.AttrLabelList)); got != served {
		t.Errorf("LabelList after the restart %s, served %s before it", got, served)
	}
}
