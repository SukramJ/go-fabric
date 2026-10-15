// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/bdx/otaimage"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

func testOtaImage(t *testing.T, vendor, product uint16, version uint32) []byte {
	t.Helper()
	img, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: vendor, ProductID: product, SoftwareVersion: version, SoftwareVersionString: fmt.Sprintf("%d.0", version),
		Payload: make([]byte, 4096),
	})
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// TestOtaApplier_Refusals: the image the daemon accepts is the version
// offered, intact (digest and size, OtaImageReader.extractPayload as the
// matter.js cert requestor runs it), for this vendor and product.
func TestOtaApplier_Refusals(t *testing.T) {
	t.Parallel()
	bi, err := mattercore.NewBasicInformation(mattercore.Config{VendorID: testVendorID, ProductID: testProductID, NodeLabel: "ota", SoftwareVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	a := &otaApplier{vendorID: testVendorID, productID: testProductID, basicInfo: bi, dir: dir, logger: slog.Default()}
	file := filepath.Join(dir, "ota-65521-32769")
	for name, img := range map[string][]byte{
		"other vendor":  testOtaImage(t, 0xFFF2, testProductID, 2),
		"other product": testOtaImage(t, testVendorID, 0x8002, 2),
		"other version": testOtaImage(t, testVendorID, testProductID, 3),
		"corrupt": func() []byte {
			b := testOtaImage(t, testVendorID, testProductID, 2)
			b[len(b)-1] ^= 0xFF
			return b
		}(),
	} {
		if err := os.WriteFile(file, img, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := a.ApplyUpdate(context.Background(), 2, file); err == nil {
			t.Fatalf("%s accepted", name)
		}
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatalf("%s: refused file left in the download location", name)
		}
	}
	// A zero vendor and product apply to every node (OtaImageHeader.ts:26-41).
	if err := os.WriteFile(file, testOtaImage(t, 0, 0, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.ApplyUpdate(context.Background(), 2, file); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "applied", "ota-fff1-8001-v2.ota")); err != nil {
		t.Fatal("applied image not kept")
	}
	if bi.OtaNodeInformation().SoftwareVersion != 1 {
		t.Fatal("outside harness mode the running version changed")
	}
	if ok, _ := a.RequestUserConsent(context.Background(), 2, "2.0"); ok {
		t.Fatal("consent outside harness mode")
	}
}

// harnessProvider is an in-memory OTA provider: QueryImage offers version
// 2, ApplyUpdateRequest proceeds, the image goes over BDX.
type harnessProvider struct {
	t        *testing.T
	image    []byte
	mu       sync.Mutex
	notified []provdef.NotifyUpdateAppliedRequest
}

func (p *harnessProvider) ConnectOtaProvider(context.Context, uint8, uint64) (mattercore.OtaProviderSession, error) {
	return p, nil
}

func (p *harnessProvider) node(v spec.Encodable) *spec.Node {
	enc := tlv.NewEncoder()
	v.EncodeTLV(enc, tlv.AnonymousTag())
	b, _ := enc.Bytes()
	dec := tlv.NewDecoder(b)
	el, _ := dec.Next()
	n, err := spec.ReadContainer(dec, el)
	if err != nil {
		p.t.Error(err)
	}
	return &n
}

func (p *harnessProvider) Invoke(_ context.Context, _ uint16, _, cmd uint32, fields spec.Encodable) (*spec.Node, error) {
	switch cmd {
	case provdef.CmdQueryImage:
		uri, v, s, token := "bdx://00000000000000AA/img.ota", uint32(2), "2.0", []byte("token-12345678")
		return p.node(provdef.QueryImageResponse{Status: provdef.StatusUpdateAvailable, ImageUri: &uri, SoftwareVersion: &v, SoftwareVersionString: &s, UpdateToken: &token}), nil
	case provdef.CmdApplyUpdateRequest:
		return p.node(provdef.ApplyUpdateResponse{Action: provdef.ApplyUpdateActionProceed}), nil
	case provdef.CmdNotifyUpdateApplied:
		p.mu.Lock()
		p.notified = append(p.notified, fields.(provdef.NotifyUpdateAppliedRequest)) //nolint:forcetypeassert // test
		p.mu.Unlock()
		return nil, nil
	}
	return nil, fmt.Errorf("command %d", cmd)
}

type memExchange struct{ in, out chan bdx.Message }

func (e *memExchange) Send(_ context.Context, proto uint16, op uint8, payload []byte) error {
	e.out <- bdx.Message{ProtocolID: proto, Opcode: op, Payload: slices.Clone(payload)}
	return nil
}

func (e *memExchange) Receive(ctx context.Context) (bdx.Message, error) {
	select {
	case m := <-e.in:
		return m, nil
	case <-ctx.Done():
		return bdx.Message{}, ctx.Err()
	}
}
func (e *memExchange) MaxPayloadSize() int { return 1178 }
func (e *memExchange) Close() error        { return nil }

func (p *harnessProvider) OpenExchange(context.Context) (bdx.Exchange, error) {
	a, b := make(chan bdx.Message, 64), make(chan bdx.Message, 64)
	peer := &memExchange{in: a, out: b}
	go func() {
		ctx := context.Background()
		m, _ := peer.Receive(ctx)
		init, _ := bdx.DecodeInit(m.Payload)
		l := uint64(len(p.image))
		acc, _ := bdx.ReceiveAccept{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: init.MaxBlockSize, Length: &l}.Encode()
		_ = peer.Send(ctx, bdx.ProtocolID, uint8(bdx.MessageReceiveAccept), acc)
		for off := 0; off < len(p.image); {
			q, _ := peer.Receive(ctx)
			c, _ := bdx.DecodeCounterOnly(q.Payload)
			end := min(off+int(init.MaxBlockSize), len(p.image))
			t := bdx.MessageBlock
			if end == len(p.image) {
				t = bdx.MessageBlockEOF
			}
			_ = peer.Send(ctx, bdx.ProtocolID, uint8(t), bdx.Block{BlockCounter: c.BlockCounter, Data: p.image[off:end]}.Encode())
			off = end
		}
	}()
	return &memExchange{in: b, out: a}, nil
}

type otaEvents struct {
	mu  sync.Mutex
	ids []uint32
}

func (e *otaEvents) MatterEmitEvent(_ uint16, cluster, event uint32, _ any, _ contract.EventPriority) {
	if cluster == otadef.ClusterID {
		e.mu.Lock()
		e.ids = append(e.ids, event)
		e.mu.Unlock()
	}
}

func (e *otaEvents) saw(id uint32) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Contains(e.ids, id)
}

// TestOtaHarness_UpdateReportedInPlace: in harness mode an announced update
// is downloaded, applied in place — SoftwareVersion reads the new version —
// and reported as after a restart: VersionApplied and NotifyUpdateApplied
// (#handlePreviousUpdateOnStart, OtaSoftwareUpdateRequestorServer.ts:256-314).
func TestOtaHarness_UpdateReportedInPlace(t *testing.T) {
	t.Parallel()
	bi, err := mattercore.NewBasicInformation(mattercore.Config{VendorID: testVendorID, ProductID: testProductID, NodeLabel: "ota", SoftwareVersion: 1, SoftwareVersionStr: "1"})
	if err != nil {
		t.Fatal(err)
	}
	provider := &harnessProvider{t: t, image: testOtaImage(t, testVendorID, testProductID, 2)}
	// The requestor as the node builds it around the daemon's applier in
	// harness mode (node/ota.go buildOtaRequestor), with the test's
	// provider as its connector.
	dir := t.TempDir()
	var r *mattercore.OTASoftwareUpdateRequestor
	applier := &otaApplier{
		vendorID: testVendorID, productID: testProductID, basicInfo: bi, dir: dir, harness: true, logger: slog.Default(),
		requestor: func() *mattercore.OTASoftwareUpdateRequestor { return r },
	}
	quick := 250 * time.Millisecond
	r, err = mattercore.NewOTASoftwareUpdateRequestor(mattercore.OTARequestorConfig{
		NodeInformation: bi.OtaNodeInformation, Connector: provider, Applier: applier,
		DownloadDir: dir, CanConsent: true, Logger: slog.Default(), AnnouncedUpdateQueryDelay: &quick,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Stop)
	events := &otaEvents{}
	r.SetMatterEventEmitter(events)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.MatterInvoke(im.WithFabricFilter(context.Background(), true, 1), otadef.CmdAnnounceOtaProvider, otadef.AnnounceOtaProviderRequest{
		ProviderNodeId: 0xAA, VendorId: testVendorID, AnnouncementReason: otadef.AnnouncementReasonUpdateAvailable,
	}); err != nil {
		t.Fatal(err)
	}
	// Applying stays readable for the hold, as on a device that flashes
	// and reboots; a subscriber would otherwise only see the Idle after it.
	deadline := time.Now().Add(5 * time.Second)
	var applyingSince time.Time
	for applyingSince.IsZero() {
		if v, _ := r.MatterRead(otadef.AttrUpdateState); v == uint8(otadef.UpdateStateApplying) {
			applyingSince = time.Now()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("never in Applying")
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(otaHarnessApplyHold / 2)
	if v, _ := r.MatterRead(otadef.AttrUpdateState); v != uint8(otadef.UpdateStateApplying) {
		t.Fatalf("state %v %v after entering Applying, want still Applying", v, time.Since(applyingSince))
	}
	deadline = time.Now().Add(5 * time.Second)
	for !events.saw(otadef.EventVersionApplied) {
		if time.Now().After(deadline) {
			t.Fatal("no VersionApplied")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if info := bi.OtaNodeInformation(); info.SoftwareVersion != 2 || info.SoftwareVersionString != "2.0" {
		t.Fatalf("running %+v", info)
	}
	for {
		provider.mu.Lock()
		n := len(provider.notified)
		provider.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no NotifyUpdateApplied")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if v, _ := r.MatterRead(otadef.AttrUpdateState); v != uint8(otadef.UpdateStateIdle) {
		t.Fatalf("state %v", v)
	}
}
