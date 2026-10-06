// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// A CASE-equivalent harness for over-the-wire tests: a bridge whose root
// carries real cluster servers backed by a real SQLite store, one installed
// fabric, and a secure session that resolves to that fabric's controller.
// Requests are sealed by the controller half of the session, pushed through
// [Bridge.dispatch] — the production receive pipeline — and the reply is
// read off a real UDP socket and opened by the controller half again. No
// cluster method is called directly: everything a test observes crossed the
// wire codec in both directions.

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// harnessControllerNodeID is the operational node id of the controller the
// harness session authenticates.
const harnessControllerNodeID uint64 = 0x0000_0000_0001_B669

// harnessLocalSessionID is the bridge-local id of the harness session.
const harnessLocalSessionID uint16 = 0x0042

// openHarnessStore opens a file-backed SQLite store with the module schema
// applied and foreign keys enforced on every pooled connection, so the
// cascades the production store relies on run here too.
func openHarnessStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "matter.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open store db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Apply(context.Background(), db); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return store.New(db)
}

// addHarnessFabric installs one fabric row (plus its IPK key set 0, as
// AddNOC does) and returns its index.
func addHarnessFabric(t *testing.T, st *store.Store, fabricID uint64, compressed [8]byte) uint8 {
	t.Helper()
	ctx := context.Background()
	root := make([]byte, 65)
	root[0] = 0x04
	for i := 1; i < len(root); i++ {
		root[i] = byte(i) ^ byte(fabricID)
	}
	idx, err := st.AddFabric(ctx, store.FabricRecord{
		FabricID: fabricID, NodeID: 0x0000_0000_0000_0B0B, RootPublicKey: root,
		VendorID: 0xFFF1, CompressedID: compressed,
	})
	if err != nil {
		t.Fatalf("AddFabric: %v", err)
	}
	if err := st.UpsertGroupKeySet(ctx, store.GroupKeySet{
		FabricIndex: idx, GroupKeySetID: 0, EpochKey0: make([]byte, 16),
	}); err != nil {
		t.Fatalf("install IPK key set: %v", err)
	}
	return idx
}

// harnessLookup is a CASE session lookup: one secure session bound to one
// fabric and one controller subject.
type harnessLookup struct {
	mu      sync.Mutex
	session *channel.Session
	fabric  uint8
	subject uint64
	// pase makes the session a PASE session (see [secureHarness.asPASE]).
	pase bool
}

func (h *harnessLookup) Lookup(id uint16) (*channel.Session, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if id != harnessLocalSessionID || h.session == nil {
		return nil, false
	}
	return h.session, true
}

func (h *harnessLookup) FabricFor(id uint16) (uint8, bool) {
	if id != harnessLocalSessionID {
		return 0, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fabric, true
}

func (h *harnessLookup) SubjectFor(id uint16) (nodeID uint64, cats []uint32, ok bool) {
	if id != harnessLocalSessionID {
		return 0, nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.subject, nil, true
}

func (h *harnessLookup) IsPASE(id uint16) (pase, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pase, id == harnessLocalSessionID
}

// secureHarness is a started bridge plus the controller half of a CASE
// session into it.
type secureHarness struct {
	t      *testing.T
	bridge *Bridge
	store  *store.Store
	fabric uint8
	peer   *channel.Session
	conn   *net.UDPConn
	lookup *harnessLookup

	mu         sync.Mutex
	exchangeID uint16
}

// harnessFabricID / harnessCompressedID identify the harness fabric.
const harnessFabricID uint64 = 0x0000_0000_0000_0FAB

var harnessCompressedID = [8]byte{0x87, 0xE1, 0xB0, 0x04, 0xE2, 0x35, 0xA1, 0x30}

// newSecureHarness starts a bridge with root clusters built by roots (given
// the store and the installed fabric index), wires the store as the ACL
// lister and installs the CASE session.
func newSecureHarness(t *testing.T, snap Snapshotter, roots func(st *store.Store, fabric uint8) []contract.ClusterServer) *secureHarness {
	t.Helper()
	return newSecureHarnessWith(t, func(st *store.Store, fabric uint8) (Snapshotter, []contract.ClusterServer) {
		var servers []contract.ClusterServer
		if roots != nil {
			servers = roots(st, fabric)
		}
		return snap, servers
	})
}

// newSecureHarnessWith is newSecureHarness for a setup that needs the
// store before it can build its topology (a group state, say).
func newSecureHarnessWith(t *testing.T, setup func(st *store.Store, fabric uint8) (Snapshotter, []contract.ClusterServer)) *secureHarness {
	t.Helper()
	st := openHarnessStore(t)
	fabric := addHarnessFabric(t, st, harnessFabricID, harnessCompressedID)

	snap, roots := setup(st, fabric)
	if snap == nil {
		snap = wbEmptySnapshotter
	}
	b := newStartedBridgeWithSnapshotter(t, snap)
	b.AttachACLLister(st)
	if roots != nil {
		b.AttachRootClusters(roots)
	}
	if err := b.Reassemble(context.Background()); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}

	encKey := make([]byte, 16) // bridge → controller
	decKey := make([]byte, 16) // controller → bridge
	for i := range encKey {
		encKey[i] = byte(0x40 + i)
		decKey[i] = byte(0x80 + i)
	}
	bridgeSess, err := channel.New(channel.Config{
		EncryptKey: encKey, DecryptKey: decKey,
		LocalNodeID: 0x0B0B, PeerNodeID: harnessControllerNodeID,
		PeerSessionID: 0x0777, InitialCounter: 1000,
	})
	if err != nil {
		t.Fatalf("channel.New (bridge): %v", err)
	}
	peerSess, err := channel.New(channel.Config{
		EncryptKey: decKey, DecryptKey: encKey,
		LocalNodeID: harnessControllerNodeID, PeerNodeID: 0x0B0B,
		InitialCounter: 5000,
	})
	if err != nil {
		t.Fatalf("channel.New (controller): %v", err)
	}
	lookup := &harnessLookup{session: bridgeSess, fabric: fabric, subject: harnessControllerNodeID}
	b.AttachSessionLookup(lookup)

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &secureHarness{t: t, bridge: b, store: st, fabric: fabric, peer: peerSess, conn: conn, lookup: lookup, exchangeID: 0x100}
}

// asPASE turns the harness session into a PASE session that has no fabric
// yet — a second commissioner's, opened while the harness fabric already
// exists: no subject, FabricIndex 0.
func (h *secureHarness) asPASE() {
	h.lookup.mu.Lock()
	defer h.lookup.mu.Unlock()
	h.lookup.pase, h.lookup.fabric, h.lookup.subject = true, 0, 0
}

// allowAll installs a wildcard CASE Administer entry for the harness fabric.
func (h *secureHarness) allowAll() {
	h.t.Helper()
	if err := h.store.ReplaceACL(context.Background(), h.fabric, []store.ACLEntry{{
		FabricIndex: h.fabric, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{harnessControllerNodeID},
	}}); err != nil {
		h.t.Fatalf("ReplaceACL: %v", err)
	}
}

// exchange seals one IM request on a fresh exchange, dispatches it and
// returns the reply opcode and payload. ok=false means nothing came back.
func (h *secureHarness) exchange(opcode uint8, payload []byte) (replyOpcode uint8, replyPayload []byte, ok bool) {
	h.t.Helper()
	h.mu.Lock()
	h.exchangeID++
	xid := h.exchangeID
	h.mu.Unlock()

	proto := message.ProtocolHeader{
		Initiator: true, NeedsAck: true, Opcode: opcode,
		ExchangeID: xid, ProtocolID: im.InteractionModelProtocolID,
	}
	plain := append(proto.Marshal(), payload...)
	hdr := message.Header{SessionID: harnessLocalSessionID}
	sealed, err := h.peer.Encrypt(&hdr, securityFlagsByte(&hdr), plain)
	if err != nil {
		h.t.Fatalf("controller Encrypt: %v", err)
	}
	datagram := append(hdr.Marshal(), sealed.Ciphertext...)
	src, _ := h.conn.LocalAddr().(*net.UDPAddr)
	if err := h.bridge.dispatch(context.Background(), datagram, src); err != nil {
		h.t.Fatalf("dispatch: %v", err)
	}

	buf := make([]byte, 4096)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_ = h.conn.SetReadDeadline(deadline)
		n, _, err := h.conn.ReadFromUDP(buf)
		if err != nil {
			var nerr net.Error
			if errors.As(err, &nerr) && nerr.Timeout() {
				return 0, nil, false
			}
			h.t.Fatalf("ReadFromUDP: %v", err)
		}
		rhdr, hdrLen, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			h.t.Fatalf("reply header: %v", err)
		}
		body, _, err := h.peer.Decrypt(&rhdr, securityFlagsByte(&rhdr), append([]byte(nil), buf[hdrLen:n]...))
		if err != nil {
			h.t.Fatalf("controller Decrypt: %v", err)
		}
		rproto, protoLen, err := message.UnmarshalProtocolHeader(body)
		if err != nil {
			h.t.Fatalf("reply protocol header: %v", err)
		}
		if rproto.ExchangeID != xid || rproto.ProtocolID != im.InteractionModelProtocolID {
			continue // a standalone ack, or another exchange's datagram
		}
		return rproto.Opcode, body[protoLen:], true
	}
}

// invoke sends a single-command InvokeRequest and returns the decoded reply.
func (h *secureHarness) invoke(endpoint uint16, cluster, command uint32, fields func(enc *tlv.Encoder)) tlvNode {
	h.t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), false) // SuppressResponse
	enc.PutBool(tlv.ContextTag(1), false) // TimedRequest
	enc.StartArray(tlv.ContextTag(2))
	enc.StartStruct(tlv.AnonymousTag())
	im.ConcreteCommandPath{Endpoint: endpoint, Cluster: cluster, Command: command, HasEndpoint: true, HasCluster: true, HasCommand: true}.MarshalTLV(enc, tlv.ContextTag(0))
	enc.StartStruct(tlv.ContextTag(1))
	if fields != nil {
		fields(enc)
	}
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	body, err := enc.Bytes()
	if err != nil {
		h.t.Fatalf("encode InvokeRequest: %v", err)
	}
	op, payload, ok := h.exchange(im.OpcodeInvokeRequest, body)
	if !ok {
		h.t.Fatal("no reply to InvokeRequest")
	}
	if op != im.OpcodeInvokeResponse {
		h.t.Fatalf("reply opcode = 0x%02X, want InvokeResponse", op)
	}
	return decodeTLVTree(h.t, payload)
}

// invokeResult splits a single-entry InvokeResponse into either its
// CommandDataIB (command id + fields) or its CommandStatusIB status.
func invokeResult(t *testing.T, resp tlvNode) (command uint32, fields tlvNode, status im.StatusCode, isStatus bool) {
	t.Helper()
	ib := resp.mustChild(t, 1).Children
	if len(ib) != 1 {
		t.Fatalf("InvokeResponses has %d entries, want 1", len(ib))
	}
	entry := ib[0]
	if data, ok := entry.child(0); ok {
		path := data.mustChild(t, 0)
		return uint32(path.mustChild(t, 2).El.Uint), data.mustChild(t, 1), 0, false
	}
	st := entry.mustChild(t, 1)
	return 0, tlvNode{}, im.StatusCode(st.mustChild(t, 1).mustChild(t, 0).El.Uint), true
}

// tlvNode is a decoded TLV element and, for a container, its children.
type tlvNode struct {
	El       tlv.Element
	Children []tlvNode
}

// child returns the direct child carrying context tag n.
func (n tlvNode) child(tag uint64) (tlvNode, bool) {
	for _, c := range n.Children {
		if c.El.Tag.Kind == tlv.TagKindContext && uint64(c.El.Tag.Number) == tag {
			return c, true
		}
	}
	return tlvNode{}, false
}

func (n tlvNode) mustChild(t *testing.T, tag uint64) tlvNode {
	t.Helper()
	c, ok := n.child(tag)
	if !ok {
		t.Fatalf("TLV element has no context tag %d", tag)
	}
	return c
}

// decodeTLVTree decodes one top-level TLV element into a tree.
func decodeTLVTree(t *testing.T, raw []byte) tlvNode {
	t.Helper()
	dec := tlv.NewDecoder(raw)
	el, err := dec.Next()
	if err != nil {
		t.Fatalf("decode TLV: %v", err)
	}
	return decodeTLVNode(t, dec, el)
}

func decodeTLVNode(t *testing.T, dec *tlv.Decoder, el tlv.Element) tlvNode {
	t.Helper()
	n := tlvNode{El: el}
	if !el.IsContainer {
		return n
	}
	for {
		c, err := dec.Next()
		if err != nil {
			t.Fatalf("decode TLV: %v", err)
		}
		if c.IsEndContainer {
			return n
		}
		n.Children = append(n.Children, decodeTLVNode(t, dec, c))
	}
}
