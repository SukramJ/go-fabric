// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// The proofs of ADR 0021, device to device as ADR 0020's are: the module's
// own OTA requestor downloads an image from the module's own provider (two
// node.Nodes commissioned into one fabric by the module's commissioner),
// and a DiagnosticLogs BDX transfer is received by a BDX receiver on the
// controller's exchange layer. The harness is the group-send one
// (groupsend_live_test.go): exchange manager, DNS-SD cache with each node's
// operational record injected, one fabric, commissioner.

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/bdx/otaimage"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	diaglogsdef "github.com/SukramJ/go-fabric/cluster/spec/diagnosticlogs"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/transport/udp"
)

const (
	otaLiveFabricID   uint64 = 0x0000_0000_0000_0A21
	otaLiveController uint64 = 112233
	otaLivePasscode   uint32 = 20202021
)

// liveFabric is the controller side of the proofs.
type liveFabric struct {
	mgr *exchange.Manager
	com *commissioner.Commissioner
	set *peer.Set
	fab *commissioner.Fabric
	dns *groupLiveMDNS
}

func newLiveFabric(t *testing.T) *liveFabric {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := exchange.NewManager(l, exchange.Config{Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, stopServe := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(serveCtx, mgr.HandleDatagram)
	}()
	conn := &groupLiveMDNS{out: make(chan mdns.QueryResponse, 64)}
	names := discovery.NewNames(conn, log)
	authority, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	fabric, err := commissioner.NewFabric(authority, commissioner.FabricConfig{FabricID: otaLiveFabricID, RootNodeID: otaLiveController, VendorID: 0xFFF1, Label: "ota"})
	if err != nil {
		t.Fatal(err)
	}
	resumption := &peer.MemoryResumptionStore{}
	set, err := peer.NewSet(peer.Context{Exchanges: mgr, Names: names, Fabrics: fabric.Lookup, Resumption: resumption, Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Logger: log})
	com, err := commissioner.New(commissioner.Context{
		Exchanges: mgr, Peers: set, Attestation: attestation.TestStore(), Logger: log,
		Subscriptions: reg, Fabrics: []*commissioner.Fabric{fabric}, Resumption: resumption,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reg.Close(context.Background())
		com.Close()
		set.Close(context.Background())
		mgr.Close()
		stopServe()
		<-served
		_ = names.Close()
	})
	return &liveFabric{mgr: mgr, com: com, set: set, fab: fabric, dns: conn}
}

// commission commissions n as nodeID and returns the controller's
// interaction with it.
func (f *liveFabric) commission(ctx context.Context, t *testing.T, n *node.Node, nodeID uint64, host string) *interaction.Interaction {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", n.Bridge().LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	op := discovery.OperationalQName(f.fab.Identity.CompressedFabricID, nodeID)
	hdr := func(name string, typ uint16) dns.RR_Header {
		return dns.RR_Header{Name: dns.Fqdn(name), Rrtype: typ, Class: dns.ClassINET | 0x8000, Ttl: 120}
	}
	msg := new(dns.Msg)
	msg.Response = true
	msg.Answer = []dns.RR{
		&dns.SRV{Hdr: hdr(op, dns.TypeSRV), Target: host + ".local.", Port: uint16(addr.Port)}, //nolint:gosec // a test port
		&dns.TXT{Hdr: hdr(op, dns.TypeTXT), Txt: []string{"SII=500", "SAI=300"}},
		&dns.A{Hdr: hdr(host+".local", dns.TypeA), A: net.ParseIP("127.0.0.1").To4()},
	}
	b, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	f.dns.out <- mdns.QueryResponse{Msg: b}
	res, err := f.com.Commission(ctx, commissioner.Options{
		Fabric: f.fab, Passcode: otaLivePasscode, NodeID: nodeID, Addresses: []*net.UDPAddr{addr}, OnAttestationFailure: attestation.Accept(),
	})
	if err != nil {
		t.Fatalf("Commission %s: %v", host, err)
	}
	return f.com.Interaction(f.set.Get(res.Address))
}

// liveNode is a node.Node on loopback; edit amends its configuration.
func liveNode(t *testing.T, name string, edit func(*node.Config)) *node.Node {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), name+".db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := node.Config{
		Network:            node.Network{Listen: "127.0.0.1:0", PreferIPv4: true},
		Commissioning:      node.Commissioning{Passcode: otaLivePasscode, Discriminator: 3840, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric test", ProductName: "ota live test", NodeLabel: name, SerialNumber: name,
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:      node.Store{DB: db},
		Advertiser: mdns.NewNoop(),
		Logger:     slog.New(slog.DiscardHandler),
	}
	if edit != nil {
		edit(&cfg)
	}
	n, err := node.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	return n
}

// staticOperational resolves every node to one address.
type staticOperational struct{ addr *net.UDPAddr }

func (s staticOperational) ResolveOperational(context.Context, [8]byte, uint64) ([]*net.UDPAddr, error) {
	return []*net.UDPAddr{s.addr}, nil
}

// statusLog records the provider's update states.
type statusLog struct {
	mu  sync.Mutex
	got []mattercore.OTAUpdateStatus
}

func (l *statusLog) add(_ mattercore.OTAPeer, s mattercore.OTAUpdateStatus, _ uint32) {
	l.mu.Lock()
	l.got = append(l.got, s)
	l.mu.Unlock()
}

func (l *statusLog) states() []mattercore.OTAUpdateStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.got)
}

// TestLiveOTAProvider: a requestor node told of the provider node by the
// controller (AnnounceOTAProvider, reason UpdateAvailable) reaches it over
// CASE as the initiator, QueryImage gets the image's BDX URI, the image
// comes down as a receiver-driven transfer from the provider's BDX
// responder, ApplyUpdateRequest answers Proceed and — the requestor in
// harness mode applies in place — NotifyUpdateApplied reaches the
// provider. The provider's log is the update's life (QueryImage →
// Querying, the transfer → Downloading, WaitForApply, ApplyUpdateRequest →
// Applying, NotifyUpdateApplied → Done); the requestor's StateTransition
// events walk Querying, Downloading, Applying and back to Idle, and it
// runs the new version.
func TestLiveOTAProvider(t *testing.T) {
	// Not parallel: the nodes and commissioner here starve the package's other live tests of their handshake budget.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	image, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 2, SoftwareVersionString: "2", Payload: bytes.Repeat([]byte{0x5A, 0xA5}, 2500),
	})
	if err != nil {
		t.Fatal(err)
	}
	statuses := &statusLog{}
	provider := liveNode(t, "provider", func(c *node.Config) {
		c.OTAProvider = &node.OTAProviderConfig{
			ImageSource: &oneImage{
				img:   mattercore.OTAImage{FileDesignator: bdx.FileDesignator("fff1-8001-v2.ota"), SoftwareVersion: 2, SoftwareVersionString: "2"},
				bytes: image,
			},
			OnStatusChange: statuses.add,
		}
	})
	requestor := liveNode(t, "requestor", func(c *node.Config) {
		// Harness mode applies in place and reports the update as after a
		// restart (node/ota.go otaApplier).
		c.OTA = &node.OtaConfig{Dir: filepath.Join(t.TempDir(), "ota"), Harness: true}
	})

	f := newLiveFabric(t)
	f.commission(ctx, t, provider, 0x4321, "provider")
	reqCtl := f.commission(ctx, t, requestor, 0x4322, "requestor")

	// The requestor finds the provider through its operational resolver;
	// the loopback test has no DNS-SD, so it is told the address.
	provAddr, err := net.ResolveUDPAddr("udp", provider.Bridge().LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	requestor.Bridge().AttachOperationalResolver(staticOperational{addr: provAddr})

	provEP := providerEndpoint(t, provider)
	if provEP == nil {
		t.Fatal("the provider node has no provider endpoint")
	}
	announce := otadef.AnnounceOtaProviderRequest{
		ProviderNodeId: 0x4321, VendorId: 0xFFF1, AnnouncementReason: otadef.AnnouncementReasonUpdateAvailable, Endpoint: provEP.ID,
	}
	if _, err := reqCtl.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: otadef.ClusterID, Command: otadef.CmdAnnounceOtaProvider}, announce, interaction.InvokeOptions{}); err != nil {
		t.Fatalf("AnnounceOTAProvider: %v", err)
	}

	want := []mattercore.OTAUpdateStatus{
		mattercore.OTAUpdateQuerying, mattercore.OTAUpdateDownloading, mattercore.OTAUpdateWaitForApply,
		mattercore.OTAUpdateApplying, mattercore.OTAUpdateDone,
	}
	deadline := time.Now().Add(90 * time.Second)
	for !slices.Equal(statuses.states(), want) {
		if time.Now().After(deadline) || len(statuses.states()) > len(want) {
			t.Fatalf("provider states %v, want %v", statuses.states(), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if v, _ := requestor.Root().BasicInformation.MatterRead(0x0009); v != uint32(2) {
		t.Errorf("requestor SoftwareVersion %v, want 2", v)
	}
	var transitions []otadef.UpdateStateEnum
	for _, rec := range requestor.Bridge().EventLog().Query(0, otadef.ClusterID, otadef.EventStateTransition, 0) {
		if ev, ok := rec.Payload.(otadef.StateTransitionEvent); ok {
			transitions = append(transitions, ev.NewState)
		}
	}
	for _, s := range []otadef.UpdateStateEnum{otadef.UpdateStateQuerying, otadef.UpdateStateDownloading, otadef.UpdateStateApplying, otadef.UpdateStateIdle} {
		i := slices.Index(transitions, s)
		if i < 0 {
			t.Fatalf("requestor transitions %v lack %d", transitions, s)
		}
		transitions = transitions[i+1:]
	}
}

// ctlBDX adapts the controller's exchange to bdx.Exchange: a test-only
// BDX receiver on controller/exchange, installed with
// Manager.SetProtocolHandler for the BDX protocol.
type ctlBDX struct{ x *exchange.Exchange }

func (c ctlBDX) Send(ctx context.Context, _ uint16, opcode uint8, payload []byte) error {
	// A StatusReport travels on the Secure Channel protocol by its opcode
	// (exchange.Exchange.Send); everything else on the exchange's, BDX.
	return c.x.Send(ctx, opcode, payload, exchange.SendOptions{})
}

func (c ctlBDX) Receive(ctx context.Context) (bdx.Message, error) {
	m, err := c.x.Next(ctx, exchange.ReceiveOptions{Timeout: 30 * time.Second})
	if err != nil {
		return bdx.Message{}, err
	}
	return bdx.Message{ProtocolID: m.ProtocolID, Opcode: m.Opcode, Payload: m.Payload}, nil
}

// MaxPayloadSize is matter.js's UDP maximum less the message overhead
// (MessageExchange.maxPayloadSize, 1232 - 54).
func (c ctlBDX) MaxPayloadSize() int { return 1178 }
func (c ctlBDX) Close() error        { c.x.Close(); return nil }

// memLogs is a LogProvider holding one log.
type memLogs []byte

func (m memLogs) Logs(context.Context, uint8) ([]byte, error) { return m, nil }

// TestLiveDiagnosticLogsBDX: RetrieveLogsRequest(BDX, "logs.txt") on a
// node holding 5 KiB of logs is answered Success once the controller's BDX
// receiver accepted the node's SendInit, which arrived on a new exchange
// of the same CASE session (connectedhomeip BDXDiagnosticLogsProvider.cpp
// :67-79, :167-174 at the harness pin); the receiver gets the whole log.
// A 100-byte log is answered Exhausted with the content in the response
// and no transfer (DiagnosticLogsCluster.cpp:137-140).
func TestLiveDiagnosticLogsBDX(t *testing.T) {
	// Not parallel: the nodes and commissioner here starve the package's other live tests of their handshake budget.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	n := liveNode(t, "logs", nil)
	logs := make([]byte, 5*1024)
	for i := range logs {
		logs[i] = "0123456789abcdef\n"[i%17]
	}
	n.Root().DiagnosticLogs.AttachProvider(memLogs(logs))

	f := newLiveFabric(t)
	type received struct {
		init bdx.Init
		data []byte
		err  error
	}
	got := make(chan received, 2)
	f.mgr.SetProtocolHandler(bdx.ProtocolID, func(x *exchange.Exchange, first *exchange.Message) {
		init, err := bdx.DecodeInit(first.Payload)
		if err != nil || bdx.MessageType(first.Opcode) != bdx.MessageSendInit {
			got <- received{err: fmt.Errorf("first message %d: %w", first.Opcode, err)}
			return
		}
		var buf bytes.Buffer
		_, err = bdx.Serve(context.Background(), bdx.NewMessenger(ctlBDX{x}, 30*time.Second), bdx.ServeOptions{
			InitType: bdx.MessageSendInit, Init: init, TransferFileDesignator: bdx.FileDesignator("logs.txt"),
			Sink: &bufSink{buf: &buf},
		})
		got <- received{init: init, data: buf.Bytes(), err: err}
	})
	ctl := f.commission(ctx, t, n, 0x4323, "logs")

	designator := "logs.txt"
	res, err := ctl.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: diaglogsdef.ClusterID, Command: diaglogsdef.CmdRetrieveLogsRequest},
		diaglogsdef.RetrieveLogsRequestRequest{Intent: diaglogsdef.IntentEndUserSupport, RequestedProtocol: diaglogsdef.TransferProtocolBdx, TransferFileDesignator: &designator},
		interaction.InvokeOptions{})
	if err != nil {
		t.Fatalf("RetrieveLogsRequest: %v", err)
	}
	resp, err := spec.DecodeStruct[diaglogsdef.RetrieveLogsResponse](res.Fields)
	if err != nil || resp.Status != diaglogsdef.StatusSuccess || len(resp.LogContent) != 0 || resp.UtcTimeStamp == nil {
		t.Fatalf("RetrieveLogsResponse %+v %v", resp, err)
	}
	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("receiver: %v", r.err)
		}
		if !r.init.TransferControl.SenderDrive || r.init.MaxBlockSize != 1024 || string(r.init.FileDesignator) != "logs.txt" {
			t.Fatalf("SendInit %+v", r.init)
		}
		if !bytes.Equal(r.data, logs) {
			t.Fatalf("received %d bytes, want %d", len(r.data), len(logs))
		}
	case <-ctx.Done():
		t.Fatal("no transfer")
	}

	small := bytes.Repeat([]byte("s"), 100)
	n.Root().DiagnosticLogs.AttachProvider(memLogs(small))
	res, err = ctl.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: diaglogsdef.ClusterID, Command: diaglogsdef.CmdRetrieveLogsRequest},
		diaglogsdef.RetrieveLogsRequestRequest{Intent: diaglogsdef.IntentEndUserSupport, RequestedProtocol: diaglogsdef.TransferProtocolBdx, TransferFileDesignator: &designator},
		interaction.InvokeOptions{})
	if err != nil {
		t.Fatalf("RetrieveLogsRequest (100 bytes): %v", err)
	}
	resp, err = spec.DecodeStruct[diaglogsdef.RetrieveLogsResponse](res.Fields)
	if err != nil || resp.Status != diaglogsdef.StatusExhausted || !bytes.Equal(resp.LogContent, small) {
		t.Fatalf("RetrieveLogsResponse (100 bytes) %+v %v", resp, err)
	}
	select {
	case r := <-got:
		t.Fatalf("a transfer for logs that fit the response: %+v", r)
	case <-time.After(200 * time.Millisecond):
	}
}

// bufSink is a bdx.Sink over a buffer.
type bufSink struct{ buf *bytes.Buffer }

func (s *bufSink) Write(p []byte) (int, error) { return s.buf.Write(p) }
func (s *bufSink) Close(error) error           { return nil }
