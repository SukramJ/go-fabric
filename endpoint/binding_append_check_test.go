// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// TestBindingEntryAppendedOverTheWireIsApplied: a Binding entry a
// controller appends to the list (a chunked list write: REPLACE with the
// empty list, then one ADD per entry) reaches the dispatcher in the Binding
// server's own entry type, in which a zero Node, Group, Endpoint or Cluster
// stands for a field the entry lacks. The check of the written value
// against the definition (owner decision 64) reads the entry in its
// definition form ([mattercore.TargetStruct.DefinitionForm]): a node entry
// is not refused for a Group "0", a group entry not for an Endpoint it does
// not carry. TC-BIND-2.1 steps 5 and 9 and TC-BIND-2.3 step 10 of the Go
// certification cases write exactly these.
func TestBindingEntryAppendedOverTheWireIsApplied(t *testing.T) {
	t.Parallel()
	srv := sourcedBinding()
	eps := []*Endpoint{{ID: 0}, {ID: 1, Aggregator: true, DeviceType: 0x000E}, {ID: 2, Source: gateSource{srvs: []contract.ClusterServer{srv}}}}
	d := NewTopologyDispatcher(&Topology{Endpoints: eps, VendorID: 0xFFF1, ProductID: 0x8000})
	d.SetACLLister(fakeACLLister{entries: []store.ACLEntry{caseEntry(1, store.PrivilegeAdminister, nil)}})
	list := concreteAttrPath(2, bindingdef.ClusterID, bindingdef.AttrBinding)
	item := list
	item.ListAppend = true

	// One interaction, as a controller chunks a list write: the REPLACE
	// with the empty list, then the ADDs.
	write := func(what string, entries ...mattercore.TargetStruct) []im.StatusCode {
		t.Helper()
		writes := make([]im.AttributeWrite, 0, 1+len(entries))
		writes = append(writes, writeOf(list, []mattercore.TargetStruct{}))
		for _, e := range entries {
			writes = append(writes, writeOf(item, []mattercore.TargetStruct{e}))
		}
		resp := im.HandleWriteRequest(caseCtx(false), d, im.WriteRequest{Writes: writes})
		if len(resp.Responses) != len(writes) {
			t.Fatalf("%s: %d responses to %d writes", what, len(resp.Responses), len(writes))
		}
		out := make([]im.StatusCode, 0, len(writes))
		for _, r := range resp.Responses {
			out = append(out, r.Status.Status)
		}
		return out
	}
	// What the definition refuses is still refused: a group entry that
	// names an endpoint (Group's conformance is "!Endpoint").
	if st := write("a group with an endpoint", mattercore.TargetStruct{Group: 7, Endpoint: 2}); st[0] != im.StatusSuccess || st[1] == im.StatusSuccess {
		t.Errorf("a group entry that names an endpoint: statuses %v, want the REPLACE applied and the ADD refused", st)
	}
	if n := len(srv.Entries()); n != 0 {
		t.Errorf("the refused entry is in the list: %d entries", n)
	}
	appended := []mattercore.TargetStruct{
		{Node: 12498508630697887223, Endpoint: 2, Cluster: 6}, // a node, an endpoint and a cluster
		{Node: 0x1122334455667788, EndpointPresent: true},     // a node and endpoint 0
		{Group: 7}, // a group
	}
	for i, st := range write("three entries", appended...) {
		if st != im.StatusSuccess {
			t.Errorf("write %d of the chunked list (0 is the REPLACE): status %v, want Success", i, st)
		}
	}
	if got := srv.Entries(); len(got) != len(appended) {
		t.Fatalf("the server holds %d entries after %d appends: %+v", len(got), len(appended), got)
	}
}
