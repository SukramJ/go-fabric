// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/bdx/otaimage"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// otaProviderPeer is a fake OTA provider in its own UDP endpoint: the CASE
// responder of fakeController, answering QueryImage, ApplyUpdateRequest
// and NotifyUpdateApplied and serving the image over BDX as a
// receiver-driven sender.
type otaProviderPeer struct {
	*fakeController
	image []byte

	mu       sync.Mutex
	queries  []provdef.QueryImageRequest
	applies  []provdef.ApplyUpdateRequestRequest
	notifies []provdef.NotifyUpdateAppliedRequest
	init     *bdx.Init
	ackEOF   chan uint32
	offset   int
	block    int
}

func newOtaProviderPeer(t *testing.T, fab resumeFabric, image []byte) *otaProviderPeer {
	t.Helper()
	p := &otaProviderPeer{fakeController: newFakeController(t, fab), image: image, ackEOF: make(chan uint32, 1)}
	p.mu.Lock()
	p.fakeController.mu.Lock()
	p.secureHook = p.handleSecure
	p.fakeController.mu.Unlock()
	p.mu.Unlock()
	return p
}

func (p *otaProviderPeer) handleSecure(hdr *message.Header, proto message.ProtocolHeader, payload []byte) bool {
	switch {
	case proto.ProtocolID == im.InteractionModelProtocolID && proto.Opcode == im.OpcodeInvokeRequest:
		p.answerInvoke(hdr, proto, payload)
		return true
	case proto.ProtocolID == bdx.ProtocolID:
		p.answerBDX(hdr, proto, payload)
		return true
	}
	return false
}

func (p *otaProviderPeer) answerInvoke(hdr *message.Header, proto message.ProtocolHeader, payload []byte) {
	req, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(payload), func(_ im.ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
		return spec.ReadContainer(dec, el)
	})
	if err != nil || len(req.Invokes) != 1 {
		p.t.Errorf("provider: InvokeRequest %v", err)
		return
	}
	inv := req.Invokes[0]
	fields := inv.Fields.(spec.Node) //nolint:forcetypeassert // the reader above returns a Node
	path := im.ConcreteCommandPath{Endpoint: 0, Cluster: provdef.ClusterID, HasEndpoint: true, HasCluster: true, HasCommand: true}
	var resp im.InvokeResponseEntry
	switch inv.Path.Command {
	case provdef.CmdQueryImage:
		q, _ := spec.DecodeStruct[provdef.QueryImageRequest](fields)
		p.mu.Lock()
		p.queries = append(p.queries, q)
		p.mu.Unlock()
		uri := fmt.Sprintf("bdx://%016X/fff1-8001-v2.ota", resumeCtrlNode)
		v, s, token := uint32(2), "2.0", []byte("token-12345678")
		path.Command = provdef.CmdQueryImageResponse
		resp = im.InvokeResponseEntry{Path: path, HasResponse: true, Response: provdef.QueryImageResponse{
			Status: provdef.StatusUpdateAvailable, ImageUri: &uri, SoftwareVersion: &v, SoftwareVersionString: &s, UpdateToken: &token,
		}}
	case provdef.CmdApplyUpdateRequest:
		a, _ := spec.DecodeStruct[provdef.ApplyUpdateRequestRequest](fields)
		p.mu.Lock()
		p.applies = append(p.applies, a)
		p.mu.Unlock()
		path.Command = provdef.CmdApplyUpdateResponse
		resp = im.InvokeResponseEntry{Path: path, HasResponse: true, Response: provdef.ApplyUpdateResponse{Action: provdef.ApplyUpdateActionProceed}}
	case provdef.CmdNotifyUpdateApplied:
		n, _ := spec.DecodeStruct[provdef.NotifyUpdateAppliedRequest](fields)
		p.mu.Lock()
		p.notifies = append(p.notifies, n)
		p.mu.Unlock()
		path.Command = provdef.CmdNotifyUpdateApplied
		resp = im.InvokeResponseEntry{Path: path, IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess}}
	default:
		p.t.Errorf("provider: command %d", inv.Path.Command)
		return
	}
	enc := tlv.NewEncoder()
	im.InvokeResponse{Responses: []im.InvokeResponseEntry{resp}}.MarshalTLV(enc, func(enc *tlv.Encoder, tag tlv.Tag, v any) {
		v.(spec.Encodable).EncodeTLV(enc, tag) //nolint:forcetypeassert // test responses are generated structs
	})
	b, err := enc.Bytes()
	if err != nil {
		p.t.Error(err)
		return
	}
	p.sendSecureAck(proto, hdr.MessageCounter, im.OpcodeInvokeResponse, b)
}

func (p *otaProviderPeer) answerBDX(hdr *message.Header, proto message.ProtocolHeader, payload []byte) {
	switch bdx.MessageType(proto.Opcode) {
	case bdx.MessageReceiveInit:
		init, err := bdx.DecodeInit(payload)
		if err != nil {
			p.t.Errorf("provider: ReceiveInit %v", err)
			return
		}
		p.mu.Lock()
		p.init = &init
		p.block = int(init.MaxBlockSize)
		p.mu.Unlock()
		l := uint64(len(p.image))
		acc, _ := bdx.ReceiveAccept{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: init.MaxBlockSize, Length: &l}.Encode()
		p.sendSecureAck(proto, hdr.MessageCounter, uint8(bdx.MessageReceiveAccept), acc)
	case bdx.MessageBlockQuery:
		q, _ := bdx.DecodeCounterOnly(payload)
		p.mu.Lock()
		off := p.offset
		end := min(off+p.block, len(p.image))
		p.offset = end
		p.mu.Unlock()
		t := bdx.MessageBlock
		if end == len(p.image) {
			t = bdx.MessageBlockEOF
		}
		p.sendSecureAck(proto, hdr.MessageCounter, uint8(t), bdx.Block{BlockCounter: q.BlockCounter, Data: p.image[off:end]}.Encode())
	case bdx.MessageBlockAckEOF:
		c, _ := bdx.DecodeCounterOnly(payload)
		select {
		case p.ackEOF <- c.BlockCounter:
		default:
		}
	default:
		p.t.Errorf("provider: BDX message %s", bdx.MessageType(proto.Opcode))
	}
}

type e2eApplier struct {
	applied chan []byte
}

func (a *e2eApplier) RequestUserConsent(context.Context, uint32, string) (bool, error) {
	return true, nil
}

func (a *e2eApplier) ApplyUpdate(_ context.Context, version uint32, file string) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if version != 2 {
		return fmt.Errorf("version %d", version)
	}
	a.applied <- b
	return nil
}

// TestOtaRequestor_EndToEnd_QueryBDXApply runs the requestor against a
// provider in its own UDP endpoint over the bridge: AnnounceOTAProvider →
// CASE to the provider as initiator (ADR 0008's initiator, generalised) →
// QueryImage → the BDX download on a second exchange of the same session →
// ApplyUpdateRequest → the host's ApplyUpdate with the bytes the provider
// served. Mirrors the flow of matter.js OtaSoftwareUpdateRequestorServer.ts
// #queryForUpdate (:584-642) through #applyPreparedUpdate (:1262-1285).
func TestOtaRequestor_EndToEnd_QueryBDXApply(t *testing.T) {
	t.Parallel()
	fab := newResumeFabric(t)
	image, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 2, SoftwareVersionString: "2.0", Payload: bytes.Repeat([]byte{0x5A}, 5000),
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := newOtaProviderPeer(t, fab, image)
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, sessions *operational.Manager) {
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: provider.addr})
	})
	provider.setBridge(dev.addr(t))

	applier := &e2eApplier{applied: make(chan []byte, 1)}
	quick := 10 * time.Millisecond
	r, err := core.NewOTASoftwareUpdateRequestor(core.OTARequestorConfig{
		NodeInformation: func() core.OtaNodeInformation {
			return core.OtaNodeInformation{VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 1, HardwareVersion: 1, Location: "XX"}
		},
		Connector:                 dev.br,
		Applier:                   applier,
		DownloadDir:               t.TempDir(),
		AnnouncedUpdateQueryDelay: &quick,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Stop)
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := im.WithFabricFilter(context.Background(), true, resumeFabricIndex)
	if _, err := r.MatterInvoke(ctx, otadef.CmdAnnounceOtaProvider, otadef.AnnounceOtaProviderRequest{
		ProviderNodeId: resumeCtrlNode, VendorId: 0xFFF1, AnnouncementReason: otadef.AnnouncementReasonUpdateAvailable, Endpoint: 0,
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-applier.applied:
		if !bytes.Equal(got, image) {
			t.Fatalf("applied %d bytes, the provider served %d", len(got), len(image))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the update was never applied")
	}
	select {
	case n := <-provider.ackEOF:
		if want := uint32((len(image) - 1) / 1024); n != want {
			t.Fatalf("BlockAckEof counter %d, want %d", n, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no BlockAckEof")
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.queries) != 1 || provider.queries[0].VendorId != 0xFFF1 || provider.queries[0].SoftwareVersion != 1 {
		t.Fatalf("QueryImage %+v", provider.queries)
	}
	if provider.init == nil || provider.init.MaxBlockSize != 1024 || !provider.init.TransferControl.ReceiverDrive || provider.init.TransferControl.SenderDrive ||
		string(provider.init.FileDesignator) != "fff1-8001-v2.ota" {
		t.Fatalf("ReceiveInit %+v", provider.init)
	}
	if len(provider.applies) != 1 || provider.applies[0].NewVersion != 2 {
		t.Fatalf("ApplyUpdateRequest %+v", provider.applies)
	}
}
