// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

type fakeAuxACL struct {
	entries []store.ACLEntry
	err     error
	asked   []uint8
}

func (f *fakeAuxACL) AuxiliaryACL(_ context.Context, fabric uint8) ([]store.ACLEntry, error) {
	f.asked = append(f.asked, fabric)
	return f.entries, f.err
}

// TestCheckACLWithTheAuxiliaryFeature pins matter.js AccessControlServer
// #applyFabricAcl (the effective ACL is the stored entries plus the
// auxiliary ones) and FabricAccessControl's Auxiliary rule (a Group entry
// without targets does not reach endpoint 0): an auxiliary Groupcast grant
// lets a group Operate its listener endpoints and nothing beyond them, never
// a unicast subject, and an unreadable auxiliary source denies.
func TestCheckACLWithTheAuxiliaryFeature(t *testing.T) {
	t.Parallel()
	ep3, ep4 := uint16(3), uint16(4)
	aux := []store.ACLEntry{{
		FabricIndex: 1, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeGroup,
		Subjects: []uint64{0x0101}, Targets: []store.ACLTarget{{Endpoint: &ep3}},
	}}
	group := func(id uint16) context.Context {
		return im.WithGroupSubject(context.Background(), im.GroupSubject{GroupID: id, HasValidMapping: true, Endpoints: []uint16{0, 3, 4}})
	}
	check := func(stored []store.ACLEntry, src AuxiliaryACLLister, ctx context.Context, endpoint uint16, priv uint8) im.StatusCode {
		d := &TopologyDispatcher{topology: &Topology{}}
		d.SetACLLister(fakeACLLister{entries: stored})
		d.SetAuxiliaryACL(src)
		return d.CheckACL(ctx, 1, 0x1B669, nil, endpoint, 0x0006, priv)
	}

	src := &fakeAuxACL{entries: aux}
	if got := check(nil, src, group(0x0101), 3, privOperate); got != im.StatusSuccess {
		t.Errorf("auxiliary grant on its listener endpoint: %v", got)
	}
	if len(src.asked) != 1 || src.asked[0] != 1 {
		t.Errorf("auxiliary source asked for fabrics %v, want [1]", src.asked)
	}
	for name, tc := range map[string]struct {
		ctx      context.Context
		endpoint uint16
		priv     uint8
	}{
		"another endpoint":       {group(0x0101), ep4, privOperate},
		"another group":          {group(0x0202), ep3, privOperate},
		"beyond Operate":         {group(0x0101), ep3, privManage},
		"a unicast CASE subject": {context.Background(), ep3, privOperate},
	} {
		if got := check(nil, &fakeAuxACL{entries: aux}, tc.ctx, tc.endpoint, tc.priv); got != im.StatusUnsupportedAccess {
			t.Errorf("%s: %v, want a denial", name, got)
		}
	}
	if got := check(nil, &fakeAuxACL{entries: aux, err: errors.New("down")}, group(0x0101), 3, privOperate); got != im.StatusUnsupportedAccess {
		t.Errorf("unreadable auxiliary source: %v, want a denial", got)
	}

	wildcard := []store.ACLEntry{{FabricIndex: 1, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeGroup}}
	if got := check(wildcard, nil, group(0x0101), 0, privOperate); got != im.StatusSuccess {
		t.Errorf("without the Auxiliary feature a wildcard Group entry reaches endpoint 0: got %v", got)
	}
	if got := check(wildcard, &fakeAuxACL{}, group(0x0101), 0, privOperate); got != im.StatusUnsupportedAccess {
		t.Errorf("with the Auxiliary feature a wildcard Group entry reached endpoint 0: %v", got)
	}
	if got := check(wildcard, &fakeAuxACL{}, group(0x0101), 4, privOperate); got != im.StatusSuccess {
		t.Errorf("with the Auxiliary feature a wildcard Group entry still reaches a bridged endpoint: got %v", got)
	}
	ep0 := uint16(0)
	explicit := []store.ACLEntry{{FabricIndex: 1, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeGroup, Targets: []store.ACLTarget{{Endpoint: &ep0}}}}
	if got := check(explicit, &fakeAuxACL{}, group(0x0101), 0, privOperate); got != im.StatusSuccess {
		t.Errorf("an explicit endpoint-0 target is not the wildcard the rule removes: %v", got)
	}
	(*TopologyDispatcher)(nil).SetAuxiliaryACL(nil) // nil-safe
}

// TestCheckACLAuxiliaryFollowsTheAdvertisedFeature: the endpoint 0 rule of
// the Auxiliary feature follows the root AccessControl's FeatureMap, as
// matter.js AccessControlServer.#applyFabricAcl derives
// auxiliaryFeatureEnabled from the cluster's own feature. A host that
// advertises AUX (mounting Groupcast turns it on) but never attaches the
// auxiliary lister used to let a target-less Group entry reach endpoint 0;
// it now fails closed.
func TestCheckACLAuxiliaryFollowsTheAdvertisedFeature(t *testing.T) {
	t.Parallel()
	wildcard := []store.ACLEntry{{FabricIndex: 1, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeGroup}}
	ctx := im.WithGroupSubject(context.Background(), im.GroupSubject{GroupID: 0x0101, HasValidMapping: true, Endpoints: []uint16{0}})
	for _, tc := range []struct {
		featureMap uint32
		want       im.StatusCode
	}{
		{0x1, im.StatusSuccess},                 // EXTS only: no Auxiliary rule
		{0x1 | 0x4, im.StatusUnsupportedAccess}, // AUX advertised, no lister attached
	} {
		root := rootEndpointWith(&fakeServerFull{id: 0x001F, readVal: tc.featureMap, readOK: true})
		d := &TopologyDispatcher{topology: &Topology{Endpoints: []*Endpoint{root}}}
		d.SetACLLister(fakeACLLister{entries: wildcard})
		if got := d.CheckACL(ctx, 1, 0x1B669, nil, 0, 0x0006, privOperate); got != tc.want {
			t.Errorf("FeatureMap 0x%X: a wildcard Group entry at endpoint 0 got %v, want %v", tc.featureMap, got, tc.want)
		}
	}
}
