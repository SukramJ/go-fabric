// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"context"
	"io/fs"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/store"

	"github.com/SukramJ/go-fabric/bdx"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// oneImage is an OTAImageSource with one image for every query.
type oneImage struct {
	img   mattercore.OTAImage
	bytes []byte
}

func (s *oneImage) QueryImage(context.Context, mattercore.OTAPeer, provdef.QueryImageRequest) (mattercore.OTAImage, bool, error) {
	return s.img, true, nil
}

func (s *oneImage) Open(_ context.Context, file string) (bdx.Blob, error) {
	if file != string(s.img.FileDesignator) {
		return nil, fs.ErrNotExist
	}
	return bytes.NewReader(s.bytes), nil
}

// providerEndpoint returns the endpoint serving the OTA provider cluster.
func providerEndpoint(t *testing.T, n *node.Node) *endpoint.Endpoint {
	t.Helper()
	for _, ep := range n.Bridge().Topology().Endpoints {
		for _, srv := range endpoint.ClusterServers(ep) {
			if srv.MatterClusterID() == provdef.ClusterID {
				return ep
			}
		}
	}
	return nil
}

func descriptorOf(t *testing.T, ep *endpoint.Endpoint) *mattercore.Descriptor {
	t.Helper()
	for _, srv := range endpoint.ClusterServers(ep) {
		if d, ok := srv.(*mattercore.Descriptor); ok {
			return d
		}
	}
	t.Fatalf("endpoint %d has no Descriptor", ep.ID)
	return nil
}

// TestOTAProviderEndpoint: Config.OTAProvider adds an endpoint of device
// type OtaProvider (0x0014, revision 1) with the OTA Software Update
// Provider (0x0029) — matter.js's OtaProviderEndpoint
// (packages/node/src/endpoints/ota-provider.ts:57-64) — as a child of the
// root beside the Aggregator: in the root's PartsList, not in the
// Aggregator's, no BridgedDeviceBasicInformation. Its number is kept
// across a restart.
func TestOTAProviderEndpoint(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "node.db")
	cfg := minimalConfig(openDB(t, path))
	cfg.OTAProvider = &node.OTAProviderConfig{ImageSource: &oneImage{img: mattercore.OTAImage{FileDesignator: bdx.FileDesignator("img.ota"), SoftwareVersion: 2, SoftwareVersionString: "2"}}}
	n := newNode(t, cfg)
	if err := n.Aggregator().Add(context.Background(), newPlug(t, "plug-1")); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n.OTAProvider() == nil {
		t.Fatal("no provider")
	}
	ep := providerEndpoint(t, n)
	if ep == nil {
		t.Fatal("no endpoint serves the OTA provider")
	}
	if ep.ID < 2 || ep.DeviceType != 0x0014 || !ep.HasParentEndpointID || ep.ParentEndpointID != 0 {
		t.Fatalf("endpoint %d device type 0x%04X parent %d", ep.ID, ep.DeviceType, ep.ParentEndpointID)
	}
	d := descriptorOf(t, ep)
	dts, _ := d.MatterRead(0x0000)
	if got := dts.([]mattercore.DeviceTypeStruct); len(got) != 1 || got[0].DeviceType != 0x0014 || got[0].Revision != 1 {
		t.Fatalf("DeviceTypeList %+v", got)
	}
	servers, _ := d.MatterRead(0x0001)
	if got := servers.([]uint32); !slices.Contains(got, 0x0029) || !slices.Contains(got, 0x001D) || slices.Contains(got, 0x0039) {
		t.Fatalf("ServerList %v", got)
	}
	topo := n.Bridge().Topology()
	rootParts := readAttr(t, topo, 0, 0x001D, 0x0003).([]uint16)
	aggParts := readAttr(t, topo, 1, 0x001D, 0x0003).([]uint16)
	plugEP, _ := endpointOf(t, n, "plug-1")
	if !slices.Contains(rootParts, ep.ID) || !slices.Contains(rootParts, plugEP) {
		t.Fatalf("root PartsList %v lacks %d or %d", rootParts, ep.ID, plugEP)
	}
	if slices.Contains(aggParts, ep.ID) || !slices.Contains(aggParts, plugEP) {
		t.Fatalf("Aggregator PartsList %v", aggParts)
	}
	for _, v := range n.Bridge().DeviceTypeViolations() {
		if v.Endpoint == ep.ID {
			t.Errorf("provider endpoint violates %+v", v)
		}
	}
	id := ep.ID
	if err := n.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	m := newNode(t, func() node.Config {
		c := minimalConfig(openDB(t, path))
		c.OTAProvider = cfg.OTAProvider
		return c
	}())
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ep := providerEndpoint(t, m); ep == nil || ep.ID != id {
		t.Fatalf("after the restart the provider is on %v, was %d", ep, id)
	}
}

// TestOTAProviderNotConfigured: without Config.OTAProvider — or without its
// image source — the node has no provider and no extra endpoint.
func TestOTAProviderNotConfigured(t *testing.T) {
	t.Parallel()
	for _, p := range []*node.OTAProviderConfig{nil, {}} {
		cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
		cfg.OTAProvider = p
		n := newNode(t, cfg)
		if err := n.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		if n.OTAProvider() != nil || providerEndpoint(t, n) != nil || len(n.Bridge().Topology().Endpoints) != 2 {
			t.Fatalf("config %+v: provider %v, %d endpoints", p, n.OTAProvider(), len(n.Bridge().Topology().Endpoints))
		}
	}
}

// otaProviderACLFabrics starts a node with an OTA provider on a store of
// two fabrics, cfg adjusted by adjust, and returns the indexes of the
// fabrics whose ACL holds the provider's Operate/CASE entry.
func otaProviderACLFabrics(t *testing.T, adjust func(*node.OTAProviderConfig)) (all, granted []uint8) {
	t.Helper()
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	for i := range 2 {
		idx, err := st.AddFabric(ctx, store.FabricRecord{FabricID: uint64(0x0A00 + i), NodeID: uint64(0x0B00 + i), VendorID: 0xFFF1, RootPublicKey: make([]byte, 65), CompressedID: [8]byte{byte(i + 1)}})
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, idx)
	}
	cfg := minimalConfig(db)
	cfg.OTAProvider = &node.OTAProviderConfig{ImageSource: &oneImage{img: mattercore.OTAImage{FileDesignator: bdx.FileDesignator("img.ota"), SoftwareVersion: 2, SoftwareVersionString: "2"}}}
	if adjust != nil {
		adjust(cfg.OTAProvider)
	}
	n := newNode(t, cfg)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for _, idx := range all {
		acl, err := st.ListACL(ctx, idx)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range acl {
			if e.Privilege == store.PrivilegeOperate && e.AuthMode == store.AuthModeCASE && len(e.Subjects) == 0 &&
				len(e.Targets) == 1 && e.Targets[0].Cluster != nil && *e.Targets[0].Cluster == provdef.ClusterID {
				granted = append(granted, idx)
			}
		}
	}
	return all, granted
}

// TestOTAProviderACLNotWrittenUnasked: without
// OTAProviderConfig.GrantFabric no fabric's ACL gets the provider's entry.
// matter.js writes it only into the first fabric its own controller
// administers (OtaSoftwareUpdateProviderServer.ts:116-158 at db021da0,
// FabricAuthority.ts:112-114, :126-128), and a node has no controller of
// its own.
func TestOTAProviderACLNotWrittenUnasked(t *testing.T) {
	if _, granted := otaProviderACLFabrics(t, nil); len(granted) != 0 {
		t.Errorf("fabrics %v hold the OTA provider ACL entry without a grant, want none", granted)
	}
}

// TestOTAProviderACLOnGrantedFabrics: the provider's entry goes into the
// ACL of each fabric OTAProviderConfig.GrantFabric grants, and of no other.
func TestOTAProviderACLOnGrantedFabrics(t *testing.T) {
	var asked []uint8
	all, granted := otaProviderACLFabrics(t, func(c *node.OTAProviderConfig) {
		c.GrantFabric = func(f node.FabricInfo) bool {
			asked = append(asked, f.FabricIndex)
			return f.FabricID == 0x0A01
		}
	})
	if !slices.Equal(asked, all) {
		t.Errorf("GrantFabric asked for fabrics %v, want %v", asked, all)
	}
	if want := all[1:]; !slices.Equal(granted, want) {
		t.Errorf("fabrics %v hold the OTA provider ACL entry, want %v (the granted one)", granted, want)
	}
}

// TestOTAProviderACLWithdrawnGrantKeepsTheEntry: a grant withdrawn between
// two starts — GrantFabric that granted every fabric, as the node granted
// every fabric before OTAProviderConfig.GrantFabric, then none — removes no
// ACL entry. The node never deletes an entry from a fabric's ACL: one it
// wrote cannot be told from one the fabric's administrator wrote, and
// removing it would again write into that ACL unasked. The administrator
// removes it.
func TestOTAProviderACLWithdrawnGrantKeepsTheEntry(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	db := openDB(t, path)
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	all := make([]uint8, 0, 2)
	for i := range 2 {
		idx, err := st.AddFabric(ctx, store.FabricRecord{FabricID: uint64(0x0A00 + i), NodeID: uint64(0x0B00 + i), VendorID: 0xFFF1, RootPublicKey: make([]byte, 65), CompressedID: [8]byte{byte(i + 1)}})
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, idx)
	}
	start := func(grant func(node.FabricInfo) bool) *node.Node {
		t.Helper()
		cfg := minimalConfig(db)
		cfg.OTAProvider = &node.OTAProviderConfig{
			ImageSource: &oneImage{img: mattercore.OTAImage{FileDesignator: bdx.FileDesignator("img.ota"), SoftwareVersion: 2, SoftwareVersionString: "2"}},
			GrantFabric: grant,
		}
		n := newNode(t, cfg)
		if err := n.Start(ctx); err != nil {
			t.Fatal(err)
		}
		return n
	}
	holding := func() (out []uint8) {
		t.Helper()
		for _, idx := range all {
			acl, err := st.ListACL(ctx, idx)
			if err != nil {
				t.Fatal(err)
			}
			if slices.ContainsFunc(acl, func(e store.ACLEntry) bool {
				return e.Privilege == store.PrivilegeOperate && e.AuthMode == store.AuthModeCASE && len(e.Subjects) == 0 &&
					len(e.Targets) == 1 && e.Targets[0].Cluster != nil && *e.Targets[0].Cluster == provdef.ClusterID
			}) {
				out = append(out, idx)
			}
		}
		return out
	}
	first := start(func(node.FabricInfo) bool { return true })
	if got := holding(); !slices.Equal(got, all) {
		t.Fatalf("after the granting start fabrics %v hold the entry, want %v", got, all)
	}
	if err := first.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	for _, grant := range []func(node.FabricInfo) bool{nil, func(node.FabricInfo) bool { return false }} {
		n := start(grant)
		if got := holding(); !slices.Equal(got, all) {
			t.Errorf("after a start without the grant fabrics %v hold the entry, want %v (the entries stay)", got, all)
		}
		if err := n.Stop(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// TestOTAProviderStatusWithAHostManager (C-28): the node's status
// listeners learn the statuses of a provider whose update attempts a
// host's OTAUpdateManager owns — the provider observes what it reports
// (mattercore.OTAProviderConfig.OnAttemptStatus), whichever manager runs.
func TestOTAProviderStatusWithAHostManager(t *testing.T) {
	t.Parallel()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.OTAProvider = &node.OTAProviderConfig{
		ImageSource: &oneImage{img: mattercore.OTAImage{FileDesignator: bdx.FileDesignator("img.ota"), SoftwareVersion: 2, SoftwareVersionString: "2"}},
		Manager:     mattercore.NewOTAStandaloneManager(mattercore.OTAProviderConfig{}),
	}
	n := newNode(t, cfg)
	var mu sync.Mutex
	var got []mattercore.OTAUpdateStatus
	n.OnOTAProviderStatus(func(_ node.OTAPeer, s node.OTAUpdateStatus, _ uint32) {
		mu.Lock()
		got = append(got, s)
		mu.Unlock()
	})
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := im.WithSubject(im.WithFabricFilter(context.Background(), false, 1), 0x55, nil)
	ctx = mattercore.WithInvokeSessionID(ctx, 7)
	resp, err := n.OTAProvider().QueryImage(ctx, provdef.QueryImageRequest{
		VendorId: 0xFFF1, ProductId: 0x8001, SoftwareVersion: 1,
		ProtocolsSupported: []provdef.DownloadProtocolEnum{provdef.DownloadProtocolBdxSynchronous},
	})
	if err != nil || resp.UpdateToken == nil {
		t.Fatalf("QueryImage %+v %v", resp, err)
	}
	if err := n.OTAProvider().NotifyUpdateApplied(ctx, provdef.NotifyUpdateAppliedRequest{UpdateToken: *resp.UpdateToken, SoftwareVersion: 2}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(got, []mattercore.OTAUpdateStatus{mattercore.OTAUpdateQuerying, mattercore.OTAUpdateDone}) {
		t.Errorf("statuses %v, want [Querying Done]", got)
	}
}
