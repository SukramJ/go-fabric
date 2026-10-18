// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// A port of matter.js packages/protocol/test/session/SecureSessionTest.ts at
// 862ad75f: the GroupSession cases (describe "group privacy") — the
// group-message round trips, the unmapped-key report, the send address,
// the cached-session key and the privacy flag. The NodeSession cases live
// in secure/channel/session_parity_matterjs_test.go.

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"net"
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

func secureSessionWantNoKey(t *testing.T, err error) {
	t.Helper()
	var noKey *NoKeyError
	if !errors.As(err, &noKey) || !noKey.Authenticated || noKey.GroupID != 2 || noKey.FabricIndex != 1 {
		t.Fatalf("err = %v, want a NoKeyError naming group 2 of fabric 1", err)
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
		// An unmapped key set still authenticates, so the group id is reported.
		secureSessionUnmap(t, m, st)
		_, err = m.Decode(ctx, wire)
		secureSessionWantNoKey(t, err)
	})

	// The testing event (onGroupMessage with NoAvailableKey and the fabric)
	// is the bridge's: bridge:TestGroupMessageOutcomes turns this NoKeyError
	// into it (decodeFailureEvent).
	t.Run("round-trips a group message without privacy and reports the unmapped group id", func(t *testing.T) {
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
		secureSessionUnmap(t, m, st)
		_, err = m.Decode(ctx, wire)
		secureSessionWantNoKey(t, err)
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
