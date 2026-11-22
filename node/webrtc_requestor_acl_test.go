// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
)

// requestorACLSite is a node store of two fabrics and a start of a node
// serving a camera controller's WebRtcTransportRequestor on it.
type requestorACLSite struct {
	t   *testing.T
	st  *store.Store
	all []uint8
	cfg func() node.Config
}

// newRequestorACLSite opens a store with two fabrics.
func newRequestorACLSite(t *testing.T) *requestorACLSite {
	t.Helper()
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := &requestorACLSite{t: t, st: store.New(db), cfg: func() node.Config { return minimalConfig(db) }}
	for i := range 2 {
		idx, err := s.st.AddFabric(ctx, store.FabricRecord{FabricID: uint64(0x0A00 + i), NodeID: uint64(0x0B00 + i), VendorID: 0xFFF1, RootPublicKey: make([]byte, 65), CompressedID: [8]byte{byte(i + 1)}})
		if err != nil {
			t.Fatal(err)
		}
		s.all = append(s.all, idx)
	}
	return s
}

// start starts a node with a camera controller on the site's store, its
// WebRTCRequestor configuration rc, and returns it.
func (s *requestorACLSite) start(rc *node.WebRTCRequestorConfig) *node.Node {
	s.t.Helper()
	ctx := context.Background()
	cfg := s.cfg()
	cfg.WebRTCRequestor = rc
	n := newNode(s.t, cfg)
	ctl, err := device.NewCameraController(device.Options{StableKey: endpoint.StringKey("ctl:1"), Name: "ctl"})
	if err != nil {
		s.t.Fatal(err)
	}
	if err := n.Aggregator().Add(ctx, ctl); err != nil {
		s.t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		s.t.Fatal(err)
	}
	return n
}

// holding returns the indexes of the fabrics whose ACL holds the
// requestor's Operate/CASE entry.
func (s *requestorACLSite) holding() (out []uint8) {
	s.t.Helper()
	for _, idx := range s.all {
		acl, err := s.st.ListACL(context.Background(), idx)
		if err != nil {
			s.t.Fatal(err)
		}
		if slices.ContainsFunc(acl, func(e store.ACLEntry) bool {
			return e.Privilege == store.PrivilegeOperate && e.AuthMode == store.AuthModeCASE && len(e.Subjects) == 0 &&
				len(e.Targets) == 1 && e.Targets[0].Cluster != nil && *e.Targets[0].Cluster == wrdef.ClusterID
		}) {
			out = append(out, idx)
		}
	}
	return out
}

// TestWebRTCRequestorACLNotWrittenUnasked: without
// WebRTCRequestorConfig.GrantFabric no fabric's ACL gets the requestor's
// entry. matter.js writes it only into the first fabric its own controller
// administers (WebRtcTransportRequestorServer.ts:56-94 at db021da0,
// fabricAuthority.fabrics[0] at :58; FabricAuthority.ts:112-114,
// :126-128), and a node has no controller of its own.
func TestWebRTCRequestorACLNotWrittenUnasked(t *testing.T) {
	for _, rc := range []*node.WebRTCRequestorConfig{nil, {}} {
		s := newRequestorACLSite(t)
		s.start(rc)
		if got := s.holding(); len(got) != 0 {
			t.Errorf("config %+v: fabrics %v hold the requestor's ACL entry without a grant, want none", rc, got)
		}
	}
}

// TestWebRTCRequestorACLOnGrantedFabrics: the requestor's entry goes into
// the ACL of each fabric WebRTCRequestorConfig.GrantFabric grants, and of
// no other — the second fabric too, where matter.js's own fabric is the
// first its controller administers.
func TestWebRTCRequestorACLOnGrantedFabrics(t *testing.T) {
	s := newRequestorACLSite(t)
	var asked []uint8
	s.start(&node.WebRTCRequestorConfig{GrantFabric: func(f node.FabricInfo) bool {
		asked = append(asked, f.FabricIndex)
		return f.FabricID == 0x0A01
	}})
	if !slices.Equal(asked, s.all) {
		t.Errorf("GrantFabric asked for fabrics %v, want %v", asked, s.all)
	}
	if got, want := s.holding(), s.all[1:]; !slices.Equal(got, want) {
		t.Errorf("fabrics %v hold the requestor's ACL entry, want %v (the granted one)", got, want)
	}
}

// TestWebRTCRequestorACLWithdrawnGrantKeepsTheEntry: a grant withdrawn
// between two starts — GrantFabric that granted every fabric, then none —
// removes no ACL entry, as the entry a version before GrantFabric wrote
// into the first fabric stays. The node never deletes an entry from a
// fabric's ACL: one it wrote cannot be told from one the fabric's
// administrator wrote, and removing it would again write into that ACL
// unasked. The administrator removes it.
func TestWebRTCRequestorACLWithdrawnGrantKeepsTheEntry(t *testing.T) {
	ctx := context.Background()
	s := newRequestorACLSite(t)
	first := s.start(&node.WebRTCRequestorConfig{GrantFabric: func(node.FabricInfo) bool { return true }})
	if got := s.holding(); !slices.Equal(got, s.all) {
		t.Fatalf("after the granting start fabrics %v hold the entry, want %v", got, s.all)
	}
	if err := first.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	for _, rc := range []*node.WebRTCRequestorConfig{nil, {}, {GrantFabric: func(node.FabricInfo) bool { return false }}} {
		n := s.start(rc)
		if got := s.holding(); !slices.Equal(got, s.all) {
			t.Errorf("after a start without the grant (%+v) fabrics %v hold the entry, want %v (the entries stay)", rc, got, s.all)
		}
		if err := n.Stop(ctx); err != nil {
			t.Fatal(err)
		}
	}
}
