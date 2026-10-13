// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package handshake_test

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
	"github.com/SukramJ/go-fabric/transport/udp"
)

const testPasscode uint32 = 20202021

// startNode starts an uncommissioned node on loopback, as the node tests
// do (node/node_test.go:30-65), and returns it with its UDP address.
func startNode(t *testing.T) (*node.Node, *net.UDPAddr) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "node.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	n, err := node.New(node.Config{
		Network:       node.Network{Listen: "127.0.0.1:0", PreferIPv4: true},
		Commissioning: node.Commissioning{Passcode: testPasscode, Discriminator: 3840, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: node.ProductDescription{
			VendorID: 0xFFF1, ProductID: 0x8001,
		},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric test", ProductName: "handshake test", NodeLabel: "handshake node", SerialNumber: "HS-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:  node.Store{DB: db},
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	addr, err := net.ResolveUDPAddr("udp", n.Bridge().LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	return n, addr
}

// recorder is the manager's socket: a udp.Listener whose sends are kept
// and optionally observed.
type recorder struct {
	l      *udp.Listener
	mu     sync.Mutex
	sent   [][]byte
	onSend func(datagram []byte)
}

func (r *recorder) Send(dst *net.UDPAddr, payload []byte) error {
	r.mu.Lock()
	r.sent = append(r.sent, append([]byte(nil), payload...))
	hook := r.onSend
	r.mu.Unlock()
	if hook != nil {
		hook(payload)
	}
	return r.l.Send(dst, payload)
}

func (r *recorder) setHook(f func([]byte)) {
	r.mu.Lock()
	r.onSend = f
	r.mu.Unlock()
}

// unsecuredStatusReports returns the Secure Channel StatusReports the
// manager sent on the unsecured session, a retransmission (the same
// message counter) once.
func (r *recorder) unsecuredStatusReports() []exchange.StatusReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []exchange.StatusReport
	seen := map[uint32]bool{}
	for _, d := range r.sent {
		opcode, payload, ok := unsecuredMessage(d)
		if !ok || opcode != mrp.SCOpcodeStatusReport {
			continue
		}
		hdr, _, _ := message.UnmarshalHeader(d)
		if seen[hdr.MessageCounter] {
			continue
		}
		seen[hdr.MessageCounter] = true
		if sr, ok := exchange.DecodeStatusReport(payload); ok {
			out = append(out, sr)
		}
	}
	return out
}

// opcodesSent lists the opcodes of the unsecured messages sent.
func (r *recorder) opcodesSent() []uint8 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []uint8
	for _, d := range r.sent {
		if opcode, _, ok := unsecuredMessage(d); ok {
			out = append(out, opcode)
		}
	}
	return out
}

// unsecuredMessage splits an unsecured datagram into opcode and payload.
func unsecuredMessage(d []byte) (opcode uint8, payload []byte, ok bool) {
	hdr, hl, err := message.UnmarshalHeader(d)
	if err != nil || hdr.SessionID != 0 {
		return 0, nil, false
	}
	proto, pl, err := message.UnmarshalProtocolHeader(d[hl:])
	if err != nil {
		return 0, nil, false
	}
	return proto.Opcode, d[hl+pl:], true
}

// newManager opens a controller socket on loopback and its manager.
func newManager(t *testing.T) (*exchange.Manager, *recorder) {
	t.Helper()
	l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{l: l}
	m, err := exchange.NewManager(rec, exchange.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = l.Serve(ctx, m.HandleDatagram)
	}()
	t.Cleanup(func() {
		m.Close()
		cancel()
		<-done
	})
	return m, rec
}

// readNodeLabel reads BasicInformation.NodeLabel over s: one IM exchange
// that proves the session's keys in both directions.
func readNodeLabel(t *testing.T, m *exchange.Manager, s *exchange.Session) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	x, err := m.Initiate(s, im.InteractionModelProtocolID, exchange.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	enc := tlv.NewEncoder()
	im.ReadRequest{AttributeRequests: []im.ConcreteAttributePath{{
		Endpoint: 0, Cluster: 0x0028, Attribute: 0x0005, HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}}, FabricFiltered: true}.MarshalTLV(enc)
	req, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Send(ctx, im.OpcodeReadRequest, req, exchange.SendOptions{}); err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}
	msg, err := x.Next(ctx, exchange.ReceiveOptions{})
	if err != nil {
		t.Fatalf("ReportData: %v", err)
	}
	if msg.Opcode != im.OpcodeReportData || !bytes.Contains(msg.Payload, []byte("handshake node")) {
		t.Fatalf("reply opcode 0x%02X payload %x, want a ReportData with the NodeLabel", msg.Opcode, msg.Payload)
	}
}

// invalidParams counts the StatusReports (Failure, InvalidParam) sent.
func invalidParams(reports []exchange.StatusReport) int {
	n := 0
	for _, r := range reports {
		if r.GeneralCode == mrp.SCStatusGeneralFailure && r.ProtocolCode == mrp.SCStatusProtocolInvalidParameter {
			n++
		}
	}
	return n
}
