// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestGroupEncodeMatchesMatterJS: the send half reproduces matter.js's
// GroupSession.encode byte for byte. From each fixture's header fields
// (session id, counter, source node id, group id, P flag) and plaintext,
// channel.EncryptGroup and channel.ObfuscateGroupHeader produce the very
// datagram matter.js sealed (testdata/group-crypto-fixtures.json,
// GroupSession.ts:251-287), and Decode opens what they produce.
func TestGroupEncodeMatchesMatterJS(t *testing.T) {
	t.Parallel()
	var fixtures []cryptoFixture
	if err := json.Unmarshal(cryptoFixturesJSON, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Label, func(t *testing.T) {
			t.Parallel()
			op, pk := mustHex(t, f.OperationalKey), mustHex(t, f.PrivacyKey)
			source := mustUint(t, f.SourceNodeID)
			hdr := message.Header{
				SessionID: f.GroupSessionID, MessageCounter: f.Counter,
				HasSourceNodeID: true, SourceNodeID: source,
				DestSize: message.DestGroup, DestGroupID: f.GroupID,
				SessionType: message.SessionGroup, Privacy: f.Privacy,
			}
			raw, ct, err := channel.EncryptGroup(op, source, hdr, mustHex(t, f.Plaintext))
			if err != nil {
				t.Fatal(err)
			}
			wireHeader := raw
			if f.Privacy {
				if wireHeader, err = channel.ObfuscateGroupHeader(pk, f.GroupSessionID, raw, ct); err != nil {
					t.Fatal(err)
				}
			}
			datagram := append(bytes.Clone(wireHeader), ct...)
			if got := hex.EncodeToString(datagram); got != f.Datagram {
				t.Fatalf("encoded %s\nmatter.js %s", got, f.Datagram)
			}

			var compressed [8]byte
			copy(compressed[:], mustHex(t, f.CompressedFabricID))
			st := newMemStore()
			st.addFabric(1, mustUint(t, f.FabricID), compressed)
			st.putKeySet(1, store.GroupKeySet{GroupKeySetID: f.KeySetID, EpochKey0: mustHex(t, f.EpochKey), EpochStart0: 1})
			st.mapGroup(1, f.GroupID, f.KeySetID)
			m, err := NewManager(st, nil)
			if err != nil {
				t.Fatal(err)
			}
			msg, err := m.Decode(context.Background(), datagram)
			if err != nil || hex.EncodeToString(msg.Payload) != f.Plaintext || msg.SourceNodeID != source {
				t.Fatalf("Decode of the encoded datagram: %+v, %v", msg, err)
			}
		})
	}
}
