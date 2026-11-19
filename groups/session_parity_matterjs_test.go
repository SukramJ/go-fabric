// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// A port of matter.js packages/protocol/test/session/SecureSessionTest.ts at
// 862ad75f: the GroupSession cases (describe "group privacy") — the
// group-message round trips, the unmapped-key report, the send address,
// the cached-session key and the privacy flag. The NodeSession cases live
// in secure/channel/session_parity_matterjs_test.go. The failure of a
// message whose key set is unmapped, and the describe "GroupKeyMap check"
// (TestSecureSessionGroupKeyMapCheck), follow the file at db021da0.

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"net"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/message"
)

// secureSessionGroupFabric is groupFabric(), SecureSessionTest.ts:122-166:
// fabric 1 with fabric id 0x456789ABCDEF1234 and node id 1, key set 1 with
// epoch key 000102…0f from start time 1, and group 2 mapped to key set 1.
func secureSessionGroupFabric(t *testing.T) (*Manager, *memStore, *fakeTransport) {
	t.Helper()
	rootPub := mustHex(t, "044a9f42b1ca4840d37292bbc7f6a7e11e22200c976fc900dbc98a7a383a641cb8254a2e56d4e295a847943b4e3897c4a773e930277b4d9fbede8a052686bfacfa")
	const fabricID uint64 = 0x456789ABCDEF1234
	compressed, err := ca.CompressedFabricID(rootPub, fabricID)
	if err != nil {
		t.Fatal(err)
	}
	st := newMemStore()
	st.mu.Lock()
	st.fabrics[1] = store.FabricRecord{FabricIndex: 1, FabricID: fabricID, NodeID: 1, CompressedID: compressed}
	st.mu.Unlock()
	st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 1, EpochKey0: mustHex(t, "000102030405060708090a0b0c0d0e0f"), EpochStart0: 1})
	st.mapGroup(1, 2, 1)
	m, err := NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.clock = clocktest.New(time.UnixMilli(1_700_000_000_000))
	m.counterRandom = fixedStart(0x0010_0000)
	tr := &fakeTransport{}
	m.SetTransport(tr)
	return m, st, tr
}

// secureSessionGroupEncode is GroupSession.encode for the test's message
// (SecureSessionTest.ts:190-215): counter, source node id, group 2, P flag
// as given, an initiator payload header with exchange 0x1234, protocol 1,
// opcode 5, and the payload 00112233.
func secureSessionGroupEncode(t *testing.T, s *Sender, counter uint32, source uint64, privacy bool) []byte {
	t.Helper()
	hdr := message.Header{
		SessionID: s.sessionID, MessageCounter: counter,
		HasSourceNodeID: true, SourceNodeID: source,
		DestSize: message.DestGroup, DestGroupID: 2,
		SessionType: message.SessionGroup, Privacy: privacy,
	}
	ph := message.ProtocolHeader{Initiator: true, Opcode: 0x05, ExchangeID: 0x1234, ProtocolID: 0x0001}
	raw, ct, err := channel.EncryptGroup(s.key, source, hdr, append(ph.Marshal(), 0x00, 0x11, 0x22, 0x33))
	if err != nil {
		t.Fatal(err)
	}
	if privacy {
		if raw, err = channel.ObfuscateGroupHeader(s.privacyKey, s.sessionID, raw, ct); err != nil {
			t.Fatal(err)
		}
	}
	return append(bytes.Clone(raw), ct...)
}

// secureSessionPayload strips the payload header off a decoded message.
func secureSessionPayload(t *testing.T, msg *Message) string {
	t.Helper()
	_, n, err := message.UnmarshalProtocolHeader(msg.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(msg.Payload[n:])
}

// secureSessionUnmap empties GroupKeyMap (groupKeyIdMap = new Map()).
func secureSessionUnmap(t *testing.T, m *Manager, st *memStore) {
	t.Helper()
	st.mu.Lock()
	st.maps[1] = nil
	st.mu.Unlock()
	if err := m.Reload(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
}

// secureSessionWantNoCandidate asserts the failure of a message whose
// key set GroupKeyMap maps to nothing: the key set is never tried, so
// there is no candidate fabric, and the group is the plain header's id
// without a fabric — none behind privacy (SecureSessionTest.ts:431-436,
// :488-493 at db021da0).
func secureSessionWantNoCandidate(t *testing.T, err error, group uint16, hasGroup bool) {
	t.Helper()
	var rejected *DecodeError
	if !errors.As(err, &rejected) || !errors.Is(err, ErrNoKey) || len(rejected.CandidateFabrics) != 0 ||
		rejected.HasGroupID != hasGroup || rejected.GroupID != group || rejected.GroupFabricIndex != 0 {
		t.Fatalf("err = %v (%+v), want no candidate and group %v=%d without a fabric", err, rejected, hasGroup, group)
	}
}

func TestSecureSessionGroupPrivacy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("derives the nonce from the source node ID in the header", func(t *testing.T) {
		t.Parallel()
		m, _, _ := secureSessionGroupFabric(t)
		s, err := m.Sender(ctx, 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		// The header names node 7, not the fabric's node id 1.
		msg, err := m.Decode(ctx, secureSessionGroupEncode(t, s, 0x12345679, 7, true))
		if err != nil || msg.SourceNodeID != 7 {
			t.Fatalf("Decode: %+v, %v; want source node 7", msg, err)
		}
		// The Go encode names the sender beside the header; one that
		// disagrees with the header cannot pick the nonce.
		hdr := message.Header{
			SessionID: s.sessionID, MessageCounter: 1, HasSourceNodeID: true, SourceNodeID: 7,
			DestSize: message.DestGroup, DestGroupID: 2, SessionType: message.SessionGroup,
		}
		if _, _, err := channel.EncryptGroup(s.key, 1, hdr, []byte{0}); !errors.Is(err, channel.ErrSourceNodeMismatch) {
			t.Fatalf("EncryptGroup with sender 1 and header source 7: %v, want ErrSourceNodeMismatch", err)
		}
	})

	t.Run("round-trips a group message with privacy enabled", func(t *testing.T) {
		t.Parallel()
		m, st, _ := secureSessionGroupFabric(t)
		s, err := m.Sender(ctx, 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		wire := secureSessionGroupEncode(t, s, 0x12345679, 1, true)
		if wire[3]&0x80 != 0x80 {
			t.Fatalf("security flags %#x without the P flag", wire[3])
		}
		msg, err := m.Decode(ctx, wire)
		if err != nil {
			t.Fatal(err)
		}
		if p := secureSessionPayload(t, msg); p != "00112233" || msg.SourceNodeID != 1 || msg.GroupID != 2 ||
			msg.Header.DestGroupID != 2 || msg.Header.MessageCounter != 0x12345679 {
			t.Fatalf("decoded payload %s, %+v", p, msg)
		}
		// A key set without a GroupKeyMap link is never tried, so the
		// obfuscated header reveals no group.
		secureSessionUnmap(t, m, st)
		_, err = m.Decode(ctx, wire)
		secureSessionWantNoCandidate(t, err, 0, false)
	})

	// The rejection's event is DecodeError.Event; the bridge reports it
	// (bridge:TestGroupMessageOutcomes, decodeFailureEvent).
	t.Run("round-trips a group message without privacy and reports the plain header group id", func(t *testing.T) {
		t.Parallel()
		m, st, _ := secureSessionGroupFabric(t)
		s, err := m.Sender(ctx, 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		wire := secureSessionGroupEncode(t, s, 0x1234567a, 1, false)
		if wire[3]&0x80 != 0 {
			t.Fatalf("security flags %#x carry the P flag", wire[3])
		}
		msg, err := m.Decode(ctx, wire)
		if err != nil {
			t.Fatal(err)
		}
		if p := secureSessionPayload(t, msg); p != "00112233" || msg.Header.DestGroupID != 2 {
			t.Fatalf("decoded payload %s, %+v", p, msg)
		}
		// A key set without a GroupKeyMap link is never tried; the plain
		// header names the group without a fabric.
		secureSessionUnmap(t, m, st)
		_, err = m.Decode(ctx, wire)
		secureSessionWantNoCandidate(t, err, 2, true)
		// SecureSessionTest.ts:506-514: emitted as unauthenticated with no
		// candidate fabric, the group without a fabric, no source address.
		var rejected *DecodeError
		errors.As(err, &rejected)
		if ev := rejected.Event(nil); ev.FabricIndex != 0 || len(ev.CandidateFabrics) != 0 || !ev.HasHeaderGroupID ||
			ev.HeaderGroupID != 2 || ev.HeaderGroupFabricIndex != 0 || ev.HasGroupID || ev.SourceIP != nil {
			t.Errorf("event %+v", ev)
		}
	})

	t.Run("names where it sends, address and port together", func(t *testing.T) {
		t.Parallel()
		m, _, tr := secureSessionGroupFabric(t)
		s, err := m.Sender(ctx, 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Send(ctx, []byte{0x15, 0x18}, 0x0001, 0x08); err != nil {
			t.Fatal(err)
		}
		out := tr.last(t)
		const want = "[ff35:40:fd45:6789:abcd:ef12:3400:2]:5540" // SecureSessionTest.ts:432
		if got := net.JoinHostPort(out.group.String(), strconv.Itoa(out.port)); got != want {
			t.Fatalf("sent to %s, want %s", got, want)
		}
		if got := net.JoinHostPort(s.Address().String(), strconv.Itoa(groupPort)); got != want {
			t.Fatalf("Address() with the group port = %s, want %s", got, want)
		}
	})

	// GroupSession.matches is the cache key of a Go Sender: senderKey.
	t.Run("matches a cached session by fabric, session id and operational key, not by id alone", func(t *testing.T) {
		t.Parallel()
		m, _, _ := secureSessionGroupFabric(t)
		s, err := m.Sender(ctx, 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		k := senderKey{fabricIndex: 1, groupID: 2, sessionID: s.sessionID, key: hex.EncodeToString(s.key)}
		otherKey, otherFabric, otherID := k, k, k
		otherKey.key = hex.EncodeToString(bytes.Repeat([]byte{0xff}, 16))
		otherFabric.fabricIndex = 99
		otherID.sessionID ^= 0x1
		m.senderMu.Lock()
		defer m.senderMu.Unlock()
		if m.senders[k] != s {
			t.Fatal("the sender is not cached under its fabric, session id and key")
		}
		for name, miss := range map[string]senderKey{"key": otherKey, "fabric": otherFabric, "session id": otherID} {
			if m.senders[miss] != nil {
				t.Errorf("a different %s matches the cached sender", name)
			}
		}
	})

	t.Run("always sends group messages with the privacy flag set", func(t *testing.T) {
		t.Parallel()
		m, _, tr := secureSessionGroupFabric(t)
		s, err := m.Sender(ctx, 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Send(ctx, []byte{0x15, 0x18}, 0x0001, 0x08); err != nil {
			t.Fatal(err)
		}
		if p := tr.last(t).payload; p[3]&0x80 != 0x80 {
			t.Fatalf("security flags %#x without the P flag", p[3])
		}
	})
}

// secureSessionKeyMap replaces fabric idx's GroupKeyMap with the given
// group → key set pairs and reloads it (groupKeyIdMap = new Map(...)).
func secureSessionKeyMap(t *testing.T, m *Manager, st *memStore, idx uint8, pairs ...[2]uint16) {
	t.Helper()
	st.mu.Lock()
	st.maps[idx] = nil
	st.mu.Unlock()
	for _, p := range pairs {
		st.mapGroup(idx, p[0], p[1])
	}
	if err := m.Reload(context.Background(), idx); err != nil {
		t.Fatal(err)
	}
}

// secureSessionDatagram is groupDatagram(fabric, { keySetId: 1, groupId })
// of packages/protocol/test/session/group-datagrams.ts:37-89 at b25ed960:
// the current key of key set 1 (epoch key 000102…0f), node 1, counter
// 0x1234567b, privacy, an initiator payload header for exchange 0x1234,
// protocol 1, opcode 5 and the payload 00112233.
func secureSessionDatagram(t *testing.T, st *memStore, groupID uint16) []byte {
	t.Helper()
	st.mu.Lock()
	compressed := st.fabrics[1].CompressedID
	st.mu.Unlock()
	op, err := OperationalKey(mustHex(t, "000102030405060708090a0b0c0d0e0f"), compressed)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := SessionID(op)
	if err != nil {
		t.Fatal(err)
	}
	ph := message.ProtocolHeader{Initiator: true, Opcode: 0x05, ExchangeID: 0x1234, ProtocolID: 0x0001}
	return sealGroup(t, op, sid, groupID, 1, 0x1234567b, true, append(ph.Marshal(), 0x00, 0x11, 0x22, 0x33))
}

// secureSessionAddTwin is addTwinFabric (SecureSessionTest.ts:565-588 at
// db021da0): fabric 2 with the same identity, so its key set 1 with the
// same epoch key derives the same operational key.
func secureSessionAddTwin(t *testing.T, m *Manager, st *memStore) {
	t.Helper()
	st.mu.Lock()
	rec := st.fabrics[1]
	rec.FabricIndex, rec.FabricID = 2, 0x456789ABCDEF1235
	st.fabrics[2] = rec
	st.mu.Unlock()
	st.putKeySet(2, store.GroupKeySet{GroupKeySetID: 1, EpochKey0: mustHex(t, "000102030405060708090a0b0c0d0e0f"), EpochStart0: 1})
	if err := m.Reload(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
}

func secureSessionRejection(t *testing.T, m *Manager, datagram []byte) *DecodeError {
	t.Helper()
	msg, err := m.Decode(context.Background(), datagram)
	var rejected *DecodeError
	if msg != nil || !errors.As(err, &rejected) {
		t.Fatalf("Decode = %+v, %v; want a DecodeError", msg, err)
	}
	return rejected
}

// TestSecureSessionGroupKeyMapCheck ports the describe "GroupKeyMap check"
// of packages/protocol/test/session/SecureSessionTest.ts:560-880 at
// db021da0, the cases that do not reach into the payload codec or the
// crypto backend (Decode does not parse the payload: the bridge does).
// A candidate that opens the message still fails when GroupKeyMap does
// not map its group to a key set holding its key (GroupSession.ts:431-433).
func TestSecureSessionGroupKeyMapCheck(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wantGroup := func(t *testing.T, r *DecodeError, fabrics []uint8, group uint16, owner uint8) {
		t.Helper()
		if !slices.Equal(r.CandidateFabrics, fabrics) || !r.HasGroupID || r.GroupID != group || r.GroupFabricIndex != owner {
			t.Errorf("rejection %+v, want candidates %v and group %d of fabric %d", r, fabrics, group, owner)
		}
	}

	t.Run("rejects a key whose key set GroupKeyMap maps only to another group", func(t *testing.T) {
		t.Parallel()
		m, st, _ := secureSessionGroupFabric(t)
		secureSessionKeyMap(t, m, st, 1, [2]uint16{3, 1})
		wantGroup(t, secureSessionRejection(t, m, secureSessionDatagram(t, st, 2)), []uint8{1}, 2, 1)
	})

	t.Run("reports a key that GroupKeyMap maps only to another group as failed authentication", func(t *testing.T) {
		t.Parallel()
		m, st, _ := secureSessionGroupFabric(t)
		secureSessionKeyMap(t, m, st, 1, [2]uint16{3, 1})
		var got []GroupMessageEvent
		m.OnGroupMessage(func(ev GroupMessageEvent) { got = append(got, ev) })
		m.ReportGroupMessage(secureSessionRejection(t, m, secureSessionDatagram(t, st, 2)).Event(net.ParseIP("fd00::1")))
		if len(got) != 1 {
			t.Fatalf("events %+v, want one", got)
		}
		ev := got[0]
		if ev.FabricIndex != 0 || !slices.Equal(ev.CandidateFabrics, []uint8{1}) || ev.HeaderGroupID != 2 ||
			ev.HeaderGroupFabricIndex != 1 || !ev.SourceIP.Equal(net.ParseIP("fd00::1")) {
			t.Errorf("event %+v", ev)
		}
		if ev.ResultFor(1) != TestResultFailedAuth || ev.ResultFor(2) != TestResultNoAvailableKey {
			t.Errorf("resultFor 1 = %d, 2 = %d; want FailedAuth, NoAvailableKey", ev.ResultFor(1), ev.ResultFor(2))
		}
	})

	t.Run("names every fabric whose key was a candidate when no key passes", func(t *testing.T) {
		t.Parallel()
		m, st, _ := secureSessionGroupFabric(t)
		secureSessionKeyMap(t, m, st, 1, [2]uint16{3, 1})
		secureSessionAddTwin(t, m, st)
		secureSessionKeyMap(t, m, st, 2, [2]uint16{4, 1})
		wantGroup(t, secureSessionRejection(t, m, secureSessionDatagram(t, st, 2)), []uint8{1, 2}, 2, 1)
	})

	t.Run("tries the next candidate after one fails the GroupKeyMap check", func(t *testing.T) {
		t.Parallel()
		m, st, _ := secureSessionGroupFabric(t)
		secureSessionKeyMap(t, m, st, 1, [2]uint16{3, 1})
		secureSessionAddTwin(t, m, st)
		secureSessionKeyMap(t, m, st, 2, [2]uint16{2, 1})
		msg, err := m.Decode(ctx, secureSessionDatagram(t, st, 2))
		if err != nil || msg.FabricIndex != 2 || msg.GroupID != 2 {
			t.Fatalf("Decode = %+v, %v; want group 2 of the twin fabric", msg, err)
		}
	})

	t.Run("names a fabric once when several of its keys were candidates", func(t *testing.T) {
		t.Parallel()
		m, st, _ := secureSessionGroupFabric(t)
		st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 2, EpochKey0: mustHex(t, "000102030405060708090a0b0c0d0e0f"), EpochStart0: 1})
		secureSessionKeyMap(t, m, st, 1, [2]uint16{3, 1}, [2]uint16{4, 2})
		if r := secureSessionRejection(t, m, secureSessionDatagram(t, st, 2)); !slices.Equal(r.CandidateFabrics, []uint8{1}) {
			t.Errorf("candidate fabrics %v, want [1]", r.CandidateFabrics)
		}
	})

	t.Run("does not name a fabric whose key set is not mapped as a candidate", func(t *testing.T) {
		t.Parallel()
		m, st, _ := secureSessionGroupFabric(t)
		secureSessionKeyMap(t, m, st, 1, [2]uint16{3, 1})
		secureSessionAddTwin(t, m, st)
		secureSessionKeyMap(t, m, st, 2)
		if r := secureSessionRejection(t, m, secureSessionDatagram(t, st, 2)); !slices.Equal(r.CandidateFabrics, []uint8{1}) {
			t.Errorf("candidate fabrics %v, want [1]", r.CandidateFabrics)
		}
	})

	t.Run("rejects a key whose key set holds other key material than the key set of the destination group", func(t *testing.T) {
		t.Parallel()
		m, st, _ := secureSessionGroupFabric(t)
		st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 2, EpochKey0: mustHex(t, "f0f1f2f3f4f5f6f7f8f9fafbfcfdfeff"), EpochStart0: 1})
		secureSessionKeyMap(t, m, st, 1, [2]uint16{3, 1}, [2]uint16{2, 2})
		if r := secureSessionRejection(t, m, secureSessionDatagram(t, st, 2)); !slices.Equal(r.CandidateFabrics, []uint8{1}) {
			t.Errorf("candidate fabrics %v, want [1]", r.CandidateFabrics)
		}
	})

	t.Run("names the plain header group id without a fabric when no key authenticates", func(t *testing.T) {
		t.Parallel()
		m, st, _ := secureSessionGroupFabric(t)
		st.mu.Lock()
		compressed := st.fabrics[1].CompressedID
		st.mu.Unlock()
		op, err := OperationalKey(mustHex(t, "000102030405060708090a0b0c0d0e0f"), compressed)
		if err != nil {
			t.Fatal(err)
		}
		sid, err := SessionID(op)
		if err != nil {
			t.Fatal(err)
		}
		wire := sealGroup(t, op, sid, 2, 1, 0x1234567b, false, []byte{0x00})
		// matter.js removes key set 1 (removeGroupKeySet).
		st.mu.Lock()
		st.sets[1] = nil
		st.mu.Unlock()
		if err := m.Reload(ctx, 1); err != nil {
			t.Fatal(err)
		}
		r := secureSessionRejection(t, m, wire)
		if len(r.CandidateFabrics) != 0 || !r.HasGroupID || r.GroupID != 2 || r.GroupFabricIndex != 0 {
			t.Errorf("rejection %+v, want no candidate and group 2 without a fabric", r)
		}
	})

	t.Run("accepts a key whose key set GroupKeyMap maps the destination group to", func(t *testing.T) {
		t.Parallel()
		m, st, _ := secureSessionGroupFabric(t)
		secureSessionKeyMap(t, m, st, 1, [2]uint16{3, 1}, [2]uint16{2, 1})
		msg, err := m.Decode(ctx, secureSessionDatagram(t, st, 2))
		if err != nil || msg.GroupID != 2 || msg.KeySetID != 1 || secureSessionPayload(t, msg) != "00112233" {
			t.Fatalf("Decode = %+v, %v", msg, err)
		}
	})

	t.Run("accepts a key that the destination group's key set holds although its own key set maps another group", func(t *testing.T) {
		t.Parallel()
		m, st, _ := secureSessionGroupFabric(t)
		st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 2, EpochKey0: mustHex(t, "000102030405060708090a0b0c0d0e0f"), EpochStart0: 1})
		secureSessionKeyMap(t, m, st, 1, [2]uint16{3, 1}, [2]uint16{2, 2})
		msg, err := m.Decode(ctx, secureSessionDatagram(t, st, 2))
		if err != nil || msg.GroupID != 2 || !msg.HasValidMapping {
			t.Fatalf("Decode = %+v, %v", msg, err)
		}
	})
}
