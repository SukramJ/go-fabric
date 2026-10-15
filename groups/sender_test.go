// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// sent is one datagram a fakeTransport took.
type sent struct {
	group   net.IP
	port    int
	payload []byte
}

// fakeTransport records what a Sender sends.
type fakeTransport struct {
	mu   sync.Mutex
	out  []sent
	fail error
}

func (f *fakeTransport) SendMulticast(group net.IP, port int, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.out = append(f.out, sent{group: slices.Clone(group), port: port, payload: slices.Clone(payload)})
	return nil
}

func (f *fakeTransport) last(t *testing.T) sent {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.out) == 0 {
		t.Fatal("nothing sent")
	}
	return f.out[len(f.out)-1]
}

const (
	senderFabricID uint64 = 0x0FAB
	senderNodeID   uint64 = 0x1B669
)

var senderCompressed = [8]byte{0x87, 0xE1, 0xB0, 0x04, 0xE2, 0x35, 0xA1, 0x30}

// senderSetup is a manager on fabric 1 (node id senderNodeID) with key set
// 417 mapped to group 257, a transport and a fixed clock.
func senderSetup(t *testing.T) (*Manager, *memStore, *fakeTransport) {
	t.Helper()
	st := newMemStore()
	st.mu.Lock()
	st.fabrics[1] = store.FabricRecord{FabricIndex: 1, FabricID: senderFabricID, NodeID: senderNodeID, CompressedID: senderCompressed}
	st.mu.Unlock()
	st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 417, EpochKey0: bytes.Repeat([]byte{0xD0}, 16), EpochStart0: 1})
	st.mapGroup(1, 257, 417)
	m, err := NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	m.counterRandom = fixedStart(0x0010_0000)
	tr := &fakeTransport{}
	m.SetTransport(tr)
	return m, st, tr
}

// TestSenderKeySelection pins the current-key rules of Groups.currentKeyForId
// and KeySets.currentKeyForId (Groups.ts:122-133, KeySets.ts:108-135) and the
// order SessionManager.groupSessionForAddress fails in (:712-744).
func TestSenderKeySelection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_700_000_000_000)
	nowUs := uint64(now.UnixMilli()) * 1000
	k := func(b byte) []byte { return bytes.Repeat([]byte{b}, 16) }

	m, st, _ := senderSetup(t)
	// Slot order decides, not start order: slot 1 is in the future, slot 2
	// started most recently of the started ones, slot 0 earliest.
	st.putKeySet(1, store.GroupKeySet{
		GroupKeySetID: 5, EpochKey0: k(1), EpochStart0: 10, EpochKey1: k(2), EpochStart1: nowUs + 1,
		EpochKey2: k(3), EpochStart2: nowUs,
	})
	st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 6, EpochKey0: k(4), EpochStart0: nowUs + 1000})
	st.mapGroup(1, 10, 5)
	st.mapGroup(1, 11, 6)
	st.mapGroup(1, 12, 99)
	if err := m.Reload(ctx, 1); err != nil {
		t.Fatal(err)
	}
	s, err := m.Sender(ctx, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := OperationalKey(k(3), senderCompressed)
	wantSID, _ := SessionID(want)
	if !bytes.Equal(s.key, want) || s.SessionID() != wantSID || s.KeySetID() != 5 || s.GroupID() != 10 || s.FabricIndex() != 1 || s.NodeID() != senderNodeID {
		t.Errorf("sender %+v, want slot 2 of key set 5", s)
	}
	// The start time counts once it is not after now (KeySets.ts:127).
	m.now = func() time.Time { return now.Add(time.Millisecond) }
	if s, err := m.Sender(ctx, 1, 11); err != nil || s.KeySetID() != 6 {
		t.Errorf("a key starting now: %v", err)
	}
	m.now = func() time.Time { return now }
	if _, err := m.Sender(ctx, 1, 11); !errors.Is(err, ErrKeyNotStarted) {
		t.Errorf("a key starting in the future: %v, want ErrKeyNotStarted", err)
	}
	for name, gid := range map[string]uint16{"unmapped": 13, "mapped to an absent key set": 12} {
		if _, err := m.Sender(ctx, 1, gid); !errors.Is(err, ErrNoKey) {
			t.Errorf("%s: %v, want ErrNoKey", name, err)
		}
	}
	if _, err := m.Sender(ctx, 1, 0); !errors.Is(err, ErrInvalidGroupID) {
		t.Errorf("group 0: %v", err)
	}
	if _, err := m.Sender(ctx, 9, 257); !errors.Is(err, ErrNoFabric) {
		t.Errorf("unknown fabric: %v", err)
	}
	m.now = func() time.Time { return time.UnixMilli(-1) }
	if _, err := m.Sender(ctx, 1, 257); !errors.Is(err, ErrKeyNotStarted) {
		t.Errorf("a clock before 1970: %v", err)
	}
	m.now = func() time.Time { return now }

	// Without a transport there is no interface (GroupSession.ts:144-148);
	// without a counter store nothing can be numbered.
	m.SetTransport(nil)
	if _, err := m.Sender(ctx, 1, 257); !errors.Is(err, ErrNoInterface) {
		t.Errorf("no transport: %v", err)
	}
	bare, _ := NewManager(struct{ Store }{st}, nil)
	bare.SetTransport(&fakeTransport{})
	if _, err := bare.Sender(ctx, 1, 257); !errors.Is(err, ErrNoCounterStore) {
		t.Errorf("no counter store: %v", err)
	}
}

// collidingEpochKeys finds two epoch keys whose operational keys under
// senderCompressed derive the same group session id.
func collidingEpochKeys(t *testing.T) (a, b []byte) {
	t.Helper()
	seen := map[uint16][]byte{}
	for i := range uint32(1 << 20) {
		ek := make([]byte, 16)
		ek[0], ek[1], ek[2], ek[3] = byte(i), byte(i>>8), byte(i>>16), 0xEE
		op, _ := OperationalKey(ek, senderCompressed)
		sid, _ := SessionID(op)
		if prev, ok := seen[sid]; ok {
			return prev, ek
		}
		seen[sid] = ek
	}
	t.Fatal("no session id collision found")
	return nil, nil
}

// TestSenderCacheIsKeyedByKey: a cached outbound session is reused for the
// same fabric, group session id and operational key — and never matched by
// the session id alone (GroupSession.matches, GroupSession.ts:226-237;
// SessionManager.ts:725-732). Two key sets whose keys hash to one session
// id get two sessions.
func TestSenderCacheIsKeyedByKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st, _ := senderSetup(t)
	a, b := collidingEpochKeys(t)
	st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 20, EpochKey0: a, EpochStart0: 1})
	st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 21, EpochKey0: b, EpochStart0: 1})
	st.mapGroup(1, 30, 20)
	if err := m.Reload(ctx, 1); err != nil {
		t.Fatal(err)
	}
	sa, err := m.Sender(ctx, 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := m.Sender(ctx, 1, 30); again != sa {
		t.Error("the same key did not reuse its session")
	}
	st.mapGroup(1, 30, 21)
	if err := m.Reload(ctx, 1); err != nil {
		t.Fatal(err)
	}
	sb, err := m.Sender(ctx, 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if sb == sa || sb.SessionID() != sa.SessionID() || bytes.Equal(sb.key, sa.key) || sb.KeySetID() != 21 {
		t.Fatalf("colliding session id reused the other key's session: %p %p", sa, sb)
	}
	st.mapGroup(1, 30, 20)
	_ = m.Reload(ctx, 1)
	if back, _ := m.Sender(ctx, 1, 30); back != sa {
		t.Error("returning to the first key did not find its session")
	}
}

// TestSenderDatagramMatchesMatterJS: with the fixture's counter and exchange
// id, the Sender's datagram is the one matter.js's GroupSession.encode
// produced (testdata/group-crypto-fixtures.json) — header, payload header
// and privacy included.
func TestSenderDatagramMatchesMatterJS(t *testing.T) {
	t.Parallel()
	var fixtures []cryptoFixture
	if err := json.Unmarshal(cryptoFixturesJSON, &fixtures); err != nil {
		t.Fatal(err)
	}
	var ran int
	for _, f := range fixtures {
		if !f.Privacy {
			continue // a Sender always sets the P flag (GroupSession.ts:173-179)
		}
		ran++
		s := &Sender{
			sessionID: f.GroupSessionID, groupID: f.GroupID, nodeID: mustUint(t, f.SourceNodeID),
			key: mustHex(t, f.OperationalKey), privacyKey: mustHex(t, f.PrivacyKey),
		}
		plain := mustHex(t, f.Plaintext)
		// Plaintext = payload header (6 bytes: flags, opcode, exchange id,
		// protocol id) followed by the IM payload.
		ph, n, err := message.UnmarshalProtocolHeader(plain)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.encode(f.Counter, f.ExchangeID, plain[n:], ph.ProtocolID, ph.Opcode)
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(got) != f.Datagram {
			t.Errorf("%s:\nsender    %x\nmatter.js %s", f.Label, got, f.Datagram)
		}
	}
	if ran == 0 {
		t.Fatal("no privacy fixture")
	}
}

// TestSenderSendsTheGroupMessage: the datagram goes to the group's multicast
// address on port 5540 and decodes on the receive side as a group message
// from this node — P flag, counter of the node-global counter, a new
// initiator exchange per message, no acknowledgement requested
// (MessageExchange.ts:660, :719-749).
func TestSenderSendsTheGroupMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st, tr := senderSetup(t)
	s, err := m.Sender(ctx, 1, 257)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Address().Equal(MulticastAddress(senderFabricID, 257)) {
		t.Errorf("address %s", s.Address())
	}
	// A receiver on the same fabric and keys.
	rx, _ := NewManager(st, nil)
	exchanges := make([]uint16, 0, 3)
	for i := range 3 {
		if err := s.Send(ctx, []byte{0x15, 0x18}, 0x0001, 0x08); err != nil {
			t.Fatal(err)
		}
		out := tr.last(t)
		if !out.group.Equal(s.Address()) || out.port != 5540 {
			t.Fatalf("sent to [%s]:%d", out.group, out.port)
		}
		if out.payload[3] != 0x81 || out.payload[0] != 0x06 {
			t.Fatalf("flags %#x / security flags %#x, want S|group and P|group", out.payload[0], out.payload[3])
		}
		msg, err := rx.Decode(ctx, out.payload)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Duplicate || msg.SourceNodeID != senderNodeID || msg.GroupID != 257 || msg.Header.SessionID != s.SessionID() ||
			!msg.Header.Privacy || msg.Header.MessageCounter != 0x0010_0001+uint32(i) || msg.KeySetID != 417 {
			t.Fatalf("decoded %+v", msg)
		}
		ph, n, err := message.UnmarshalProtocolHeader(msg.Payload)
		if err != nil || !ph.Initiator || ph.NeedsAck || ph.HasAck || ph.ProtocolID != 1 || ph.Opcode != 8 || !bytes.Equal(msg.Payload[n:], []byte{0x15, 0x18}) {
			t.Fatalf("payload header %+v (%v)", ph, err)
		}
		exchanges = append(exchanges, ph.ExchangeID)
	}
	if exchanges[1] != exchanges[0]+1 || exchanges[2] != exchanges[1]+1 {
		t.Errorf("exchange ids %v do not count up", exchanges)
	}
	if v, _ := st.metaValue(GroupDataCounterKey); v != 0x0010_0000+1000 {
		t.Errorf("counter mark %d", v)
	}

	// A transport failure surfaces; a detached transport is no interface.
	tr.mu.Lock()
	tr.fail = errors.New("boom")
	tr.mu.Unlock()
	if err := s.Send(ctx, nil, 1, 8); err == nil || errors.Is(err, ErrNoInterface) {
		t.Errorf("transport failure: %v", err)
	}
	m.SetTransport(nil)
	if err := s.Send(ctx, nil, 1, 8); !errors.Is(err, ErrNoInterface) {
		t.Errorf("detached transport: %v", err)
	}
	// A failing counter persist fails the send.
	m.SetTransport(tr)
	st.mu.Lock()
	st.failMetaSet = errors.New("disk")
	st.mu.Unlock()
	m.counter.mu.Lock()
	m.counter.value = m.counter.reserved - 1
	m.counter.mu.Unlock()
	if err := s.Send(ctx, nil, 1, 8); err == nil {
		t.Error("a send past an unpersisted mark went out")
	}
	st.mu.Lock()
	st.failMetaSet = nil
	st.mu.Unlock()

	// The fabric's removal closes its sessions.
	m.ForgetFabric(1)
	if err := s.Send(ctx, nil, 1, 8); !errors.Is(err, ErrNoFabric) {
		t.Errorf("after the fabric left: %v", err)
	}
}

// TestNextExchangeIDWraps: ExchangeCounter wraps past 0xFFFF to 0
// (ExchangeManager.ts:736-741).
func TestNextExchangeIDWraps(t *testing.T) {
	t.Parallel()
	m, _ := NewManager(newMemStore(), nil)
	m.exchangeSeeded, m.exchangeID = true, 0xFFFE
	for _, want := range []uint16{0xFFFF, 0, 1} {
		if got, err := m.nextExchangeID(); err != nil || got != want {
			t.Fatalf("%#x (%v), want %#x", got, err, want)
		}
	}
}

// u8Fields is a one-field command payload.
type u8Fields uint8

func (v u8Fields) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(v))
	_ = enc.EndContainer()
}

type boolValue bool

func (v boolValue) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { enc.PutBool(tag, bool(v)) }

var (
	_ spec.Encodable = u8Fields(0)
	_ spec.Encodable = boolValue(false)
)

// decodeSent opens the last datagram and returns its IM payload and opcode.
func decodeSent(t *testing.T, rx *Manager, tr *fakeTransport) (payload []byte, opcode uint8) {
	t.Helper()
	msg, err := rx.Decode(context.Background(), tr.last(t).payload)
	if err != nil {
		t.Fatal(err)
	}
	ph, n, err := message.UnmarshalProtocolHeader(msg.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return msg.Payload[n:], ph.Opcode
}

// TestSenderInvokeAndWrite: a group invoke or write carries
// SuppressResponse, names no endpoint and is not timed; a path naming an
// endpoint, a timed request or an incomplete write path is refused
// (ClientGroupInteraction.ts:44-81).
func TestSenderInvokeAndWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st, tr := senderSetup(t)
	rx, _ := NewManager(st, nil)
	s, err := m.Sender(ctx, 1, 257)
	if err != nil {
		t.Fatal(err)
	}
	read := func(_ im.ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
		if el.IsContainer {
			return spec.ReadContainer(dec, el)
		}
		return spec.Node{El: el}, nil
	}

	for _, fields := range []spec.Encodable{u8Fields(7), nil} {
		if err := s.Invoke(ctx, im.ConcreteCommandPath{Cluster: 0x0006, Command: 0x02}, fields, RequestOptions{}); err != nil {
			t.Fatal(err)
		}
		payload, opcode := decodeSent(t, rx, tr)
		req, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(payload), read)
		if err != nil || opcode != im.OpcodeInvokeRequest {
			t.Fatalf("InvokeRequest: %v (opcode %#x)", err, opcode)
		}
		if !req.SuppressResponse || req.TimedRequest || len(req.Invokes) != 1 {
			t.Fatalf("InvokeRequest %+v", req)
		}
		p := req.Invokes[0].Path
		if p.HasEndpoint || p.Cluster != 0x0006 || p.Command != 0x02 {
			t.Errorf("path %+v", p)
		}
	}
	for name, err := range map[string]error{
		"endpoint": s.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 1, HasEndpoint: true, Cluster: 6, Command: 2}, nil, RequestOptions{}),
		"timed":    s.Invoke(ctx, im.ConcreteCommandPath{Cluster: 6, Command: 2}, nil, RequestOptions{Timed: true}),
	} {
		if !errors.Is(err, ErrInvalidGroupOperation) {
			t.Errorf("invoke %s: %v", name, err)
		}
	}

	path := im.ConcreteAttributePath{Cluster: 0x0006, Attribute: 0x4003, HasCluster: true, HasAttribute: true}
	if err := s.Write(ctx, path, boolValue(true), RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	payload, opcode := decodeSent(t, rx, tr)
	w, err := im.UnmarshalWriteRequestTLV(tlv.NewDecoder(payload), func(_ im.ConcreteAttributePath, el tlv.Element, _ *tlv.Decoder) (im.AttributeValue, error) {
		return im.AttributeValue{Value: el.Bool}, nil
	})
	if err != nil || opcode != im.OpcodeWriteRequest {
		t.Fatalf("WriteRequest: %v (opcode %#x)", err, opcode)
	}
	if !w.SuppressResponse || w.TimedRequest || len(w.Writes) != 1 || w.Writes[0].Path.HasEndpoint ||
		w.Writes[0].Path.Cluster != 6 || w.Writes[0].Path.Attribute != 0x4003 || w.Writes[0].Value.Value != true {
		t.Fatalf("WriteRequest %+v", w)
	}
	withEndpoint := path
	withEndpoint.Endpoint, withEndpoint.HasEndpoint = 1, true
	noAttr := path
	noAttr.HasAttribute = false
	noCluster := path
	noCluster.HasCluster = false
	for name, err := range map[string]error{
		"endpoint":   s.Write(ctx, withEndpoint, boolValue(true), RequestOptions{}),
		"attribute":  s.Write(ctx, noAttr, boolValue(true), RequestOptions{}),
		"cluster":    s.Write(ctx, noCluster, boolValue(true), RequestOptions{}),
		"timed":      s.Write(ctx, path, boolValue(true), RequestOptions{Timed: true}),
		"null value": s.Write(ctx, path, nil, RequestOptions{}),
	} {
		if !errors.Is(err, ErrInvalidGroupOperation) {
			t.Errorf("write %s: %v", name, err)
		}
	}
}
