// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"bytes"
	"context"
	"io/fs"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/bdx"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
)

// anImage is an OTAImageSource offering one image to every requestor.
type anImage struct{ img mattercore.OTAImage }

func (s anImage) QueryImage(context.Context, mattercore.OTAPeer, provdef.QueryImageRequest) (mattercore.OTAImage, bool, error) {
	return s.img, true, nil
}

func (s anImage) Open(_ context.Context, file string) (bdx.Blob, error) {
	if file != string(s.img.FileDesignator) {
		return nil, fs.ErrNotExist
	}
	return bytes.NewReader(make([]byte, 64)), nil
}

// TestFabricRemovalReachesTheOTAProvider (review II, R2-P6-bound-1206,
// owner decision 167): the node's removal fan-out reaches the OTA
// provider's MatterDropFabric — the provider is among the fabric-scoped
// servers the node walks, and a fabric's consent it held is gone after
// the removal.
func TestFabricRemovalReachesTheOTAProvider(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg := mdnsTestConfig(t)
	cfg.Network.MDNS.Off = true
	cfg.DeviceTypeValidation = endpoint.DeviceTypeValidationOff
	cfg.OTAProvider = &OTAProviderConfig{ImageSource: anImage{img: mattercore.OTAImage{
		FileDesignator: bdx.FileDesignator("img.ota"), SoftwareVersion: 2, SoftwareVersionString: "2",
	}}}
	n := newMDNSTestNode(t, cfg)
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	p := n.OTAProvider()
	if p == nil {
		t.Fatal("no provider")
	}
	if !slices.ContainsFunc(n.fabricStateServers(nodeTopology{n}.topology(), n.br.Topology()), func(s contract.FabricScopedState) bool { return s == contract.FabricScopedState(p) }) {
		t.Fatal("the removal fan-out does not walk the OTA provider")
	}
	caseCtx := im.WithSubject(im.WithFabricFilter(ctx, false, 3), 0x99, nil)
	if _, err := p.MatterInvoke(caseCtx, provdef.CmdQueryImage, provdef.QueryImageRequest{
		VendorId: 0xFFF1, ProductId: 0x8001, SoftwareVersion: 1,
		ProtocolsSupported: []provdef.DownloadProtocolEnum{provdef.DownloadProtocolBdxSynchronous},
	}); err != nil {
		t.Fatalf("QueryImage: %v", err)
	}
	if got := p.MatterHeldFabrics(); !slices.Equal(got, []uint8{3}) {
		t.Fatalf("held before the removal %v, want [3]", got)
	}
	n.dropFabricState(ctx, 3)
	if got := p.MatterHeldFabrics(); len(got) != 0 {
		t.Errorf("held after the removal %v, want none", got)
	}
}
