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
	"github.com/SukramJ/go-fabric/transport/tcp"
)

// otaProviderPeer is a fake OTA provider in its own UDP endpoint: the CASE
// responder of fakeController, answering QueryImage, ApplyUpdateRequest
// and NotifyUpdateApplied and serving the image over BDX as a
// receiver-driven sender.
type otaProviderPeer struct {
	*fakeController
	image []byte
	// nodeID is the provider's node id, which its ImageURI names.
	nodeID uint64

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
	p := &otaProviderPeer{fakeController: newFakeController(t, fab), image: image, nodeID: resumeCtrlNode, ackEOF: make(chan uint32, 1)}
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
		uri := fmt.Sprintf("bdx://%016X/fff1-8001-v2.ota", p.nodeID)
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

// peerListingRegistry adds the peer's session list to the fake registry,
// so ConnectOtaProvider finds the harness's session as the provider's.
type peerListingRegistry struct {
	*fakeSessionRegistry
	ids []uint16
}

func (r *peerListingRegistry) SessionIDsForPeer(uint8, uint64) []uint16 { return r.ids }

// TestOtaRequestor_EndToEnd_TCPBlockSize: over a session bound to a TCP
// connection, the requestor reuses that session for the provider (matter.js
// Peer.ts:418-438 newestSession of any channel) and asks for 8192-byte BDX
// blocks (OtaSoftwareUpdateRequestorServer.ts:54, :962-963); the provider's
// accept of that size carries the download in 8192-byte blocks, without
// MRP on the exchange the bridge opened.
func TestOtaRequestor_EndToEnd_TCPBlockSize(t *testing.T) {
	h := newTCPHarness(t)
	h.bridge.AttachSessionRegistry(&peerListingRegistry{fakeSessionRegistry: h.registry, ids: []uint16{harnessLocalSessionID}})
	h.bridge.mu.RLock()
	lookup, _ := h.bridge.sessions.(*harnessLookup)
	tl := h.bridge.tcpListener
	h.bridge.mu.RUnlock()
	if lookup == nil || tl == nil {
		t.Fatal("harness without lookup or TCP listener")
	}
	fabricIndex, _ := lookup.FabricFor(harnessLocalSessionID)

	image, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 2, SoftwareVersionString: "2.0", Payload: bytes.Repeat([]byte{0x5A}, 20000),
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := newOtaProviderPeer(t, newResumeFabric(t), image)
	provider.nodeID = harnessControllerNodeID
	// The bridge seals with the session's peer id (0x0777, the harness's),
	// the provider with the bridge's local id.
	provider.setSession(h.peer, 0x0777, harnessLocalSessionID)
	invokeReplies := make(chan []byte, 1)
	var d tcp.Dialer
	conn, err := d.Dial(context.Background(), tl.Addr().String(), func(_ *tcp.Conn, msg []byte, _ func()) {
		select {
		case invokeReplies <- msg: // the binding invoke's reply
			return
		default:
		}
		provider.handle(msg)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// Bind the session to the connection: its first message arrives on it.
	if err := conn.Send(h.sealedInvoke(0x310)); err != nil {
		t.Fatal(err)
	}
	waitReply(t, invokeReplies)
	invokeReplies <- nil // every later message goes to the provider
	provider.mu.Lock()
	provider.fakeController.mu.Lock()
	provider.secureOut = conn.Send
	provider.fakeController.mu.Unlock()
	provider.mu.Unlock()

	applier := &e2eApplier{applied: make(chan []byte, 1)}
	quick := 10 * time.Millisecond
	r, err := core.NewOTASoftwareUpdateRequestor(core.OTARequestorConfig{
		NodeInformation: func() core.OtaNodeInformation {
			return core.OtaNodeInformation{VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 1, HardwareVersion: 1, Location: "XX"}
		},
		Connector:                 h.bridge,
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
	ctx := im.WithFabricFilter(context.Background(), true, fabricIndex)
	if _, err := r.MatterInvoke(ctx, otadef.CmdAnnounceOtaProvider, otadef.AnnounceOtaProviderRequest{
		ProviderNodeId: harnessControllerNodeID, VendorId: 0xFFF1, AnnouncementReason: otadef.AnnouncementReasonUpdateAvailable, Endpoint: 0,
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
		if want := uint32((len(image) - 1) / 8192); n != want {
			t.Fatalf("BlockAckEof counter %d, want %d", n, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no BlockAckEof")
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.init == nil || provider.init.MaxBlockSize != 8192 {
		t.Fatalf("ReceiveInit %+v, want MaxBlockSize 8192", provider.init)
	}
	if n := h.ack.Pending(); n != 0 {
		t.Fatalf("%d acknowledgement obligations registered over TCP", n)
	}
}
