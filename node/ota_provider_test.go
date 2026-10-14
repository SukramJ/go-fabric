// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"context"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/bdx"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/endpoint"
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
