// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

// Parity against matter.js's own group cryptography. Fixture master:
// testdata/group-crypto-fixtures.json, produced by
// notes/parity/matter/generate-group-fixtures.ts (`crypto`) from
// FabricGroups.setFromGroupKeySet, KeySets.sessionIdFromKey,
// MessagePrivacy.deriveKey, Groups.multicastAddress and GroupSession.encode
// in the matter.js checkout. Every datagram below was sealed by matter.js;
// Decode has to open it.

import (
	"context"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

//go:embed testdata/group-crypto-fixtures.json
var cryptoFixturesJSON []byte

type cryptoFixture struct {
	Label              string `json:"label"`
	FabricID           string `json:"fabricId"`
	CompressedFabricID string `json:"compressedFabricId"`
	SourceNodeID       string `json:"sourceNodeId"`
	KeySetID           uint16 `json:"keySetId"`
	EpochKey           string `json:"epochKey"`
	GroupID            uint16 `json:"groupId"`
	Counter            uint32 `json:"counter"`
	ExchangeID         uint16 `json:"exchangeId"`
	Privacy            bool   `json:"privacy"`
	OperationalKey     string `json:"operationalKey"`
	GroupSessionID     uint16 `json:"groupSessionId"`
	PrivacyKey         string `json:"privacyKey"`
	MulticastAddress   string `json:"multicastAddress"`
	Plaintext          string `json:"plaintext"`
	Datagram           string `json:"datagram"`
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

func mustUint(t *testing.T, s string) uint64 {
	t.Helper()
	v, err := strconv.ParseUint(s, 0, 64)
	if err != nil {
		t.Fatalf("uint %q: %v", s, err)
	}
	return v
}

func TestGroupCryptoMatchesMatterJS(t *testing.T) {
	t.Parallel()
	var fixtures []cryptoFixture
	if err := json.Unmarshal(cryptoFixturesJSON, &fixtures); err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no fixtures")
	}
	for _, f := range fixtures {
		t.Run(f.Label, func(t *testing.T) {
			t.Parallel()
			var compressed [8]byte
			copy(compressed[:], mustHex(t, f.CompressedFabricID))
			op, err := OperationalKey(mustHex(t, f.EpochKey), compressed)
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(op); got != f.OperationalKey {
				t.Fatalf("operational key %s, matter.js %s", got, f.OperationalKey)
			}
			sid, err := SessionID(op)
			if err != nil || sid != f.GroupSessionID {
				t.Fatalf("group session id %d (%v), matter.js %d", sid, err, f.GroupSessionID)
			}
			pk, err := PrivacyKey(op)
			if err != nil || hex.EncodeToString(pk) != f.PrivacyKey {
				t.Fatalf("privacy key %x (%v), matter.js %s", pk, err, f.PrivacyKey)
			}
			fabricID := mustUint(t, f.FabricID)
			if got := MulticastAddress(fabricID, f.GroupID).String(); got != f.MulticastAddress {
				t.Fatalf("multicast address %s, matter.js %s", got, f.MulticastAddress)
			}

			st := newMemStore()
			st.addFabric(3, fabricID, compressed)
			st.putKeySet(3, store.GroupKeySet{GroupKeySetID: f.KeySetID, EpochKey0: mustHex(t, f.EpochKey), EpochStart0: 1})
			st.mapGroup(3, f.GroupID, f.KeySetID)
			_ = st.UpsertGroupTableEntry(context.Background(), store.GroupTableEntry{FabricIndex: 3, GroupID: f.GroupID, Endpoints: []uint16{2, 7}})
			m, err := NewManager(st, nil)
			if err != nil {
				t.Fatal(err)
			}
			datagram := mustHex(t, f.Datagram)
			msg, err := m.Decode(context.Background(), datagram)
			if err != nil {
				t.Fatalf("Decode of the matter.js datagram: %v", err)
			}
			if got := hex.EncodeToString(msg.Payload); got != f.Plaintext {
				t.Fatalf("plaintext %s, matter.js %s", got, f.Plaintext)
			}
			if msg.GroupID != f.GroupID || msg.SourceNodeID != mustUint(t, f.SourceNodeID) || msg.Header.MessageCounter != f.Counter ||
				msg.FabricIndex != 3 || msg.KeySetID != f.KeySetID || !msg.HasValidMapping || msg.Duplicate {
				t.Fatalf("decoded %+v", msg)
			}
			if hex.EncodeToString(datagram) != f.Datagram {
				t.Fatal("Decode modified its input")
			}
			// The same datagram again is a replay.
			again, err := m.Decode(context.Background(), datagram)
			if err != nil || !again.Duplicate {
				t.Fatalf("replayed datagram: dup=%v err=%v", again != nil && again.Duplicate, err)
			}
		})
	}
}
