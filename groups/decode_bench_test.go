// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// Benchmarks for [Manager.Decode] on a group-typed datagram that no key the
// node holds opens: the work a node does for every such datagram before it
// is dropped. The cost depends on how many of the node's operational group
// keys carry the datagram's group session id, since each of those is tried,
// so each key population is run with a session id no key carries and with
// one that keys carry:
//
//   - a realistic node: 5 fabrics with 3 key sets each, every key distinct;
//   - the most keys the servers admit: [maxBenchFabrics] fabrics, each with
//     the key sets GroupKeyManagement's default MaxGroupKeysPerFabric
//     leaves beside the IPK, each set holding its three epoch keys; once
//     with every key distinct and once with every key sharing one group
//     session id (equal key material under one compressed fabric id), the
//     case in which every key is a candidate.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"thingfabric.dev/go-fabric/store"
)

const (
	// maxBenchFabrics is OperationalCredentials' default SupportedFabrics
	// (cluster/core/operational_credentials.go, cfg.SupportedFabrics = 254).
	maxBenchFabrics = 254
	// maxBenchKeySets is GroupKeyManagement's default MaxGroupKeysPerFabric
	// (20, cluster/core/group_key_management.go defaultMaxGroupKeysPerFabric)
	// less the IPK key set 0 it counts (enforceKeySetBudget).
	maxBenchKeySets = 19
	// epochsPerKeySet is the epoch key slots of a key set
	// (store.GroupKeySet EpochKey0..2).
	epochsPerKeySet = 3
)

// benchEpochKey is a 16-byte epoch key unique to (fabric, key set, epoch)
// unless shared is set, when every call returns the same key.
func benchEpochKey(fabric uint8, keySet uint16, epoch int, shared bool) []byte {
	k := make([]byte, 16)
	if shared {
		copy(k, "shared-epoch-key")
		return k
	}
	k[0], k[1] = 0x5A, fabric
	binary.BigEndian.PutUint16(k[2:4], keySet)
	k[4] = byte(epoch)
	return k
}

// newBenchManager is a manager over fabrics fabrics with keySets key sets
// each, every set holding three epoch keys and mapped to a group of its
// own. It returns the manager and the group session id of the first key.
func newBenchManager(b *testing.B, fabrics, keySets int, shared bool) (m *Manager, firstSessionID uint16) {
	b.Helper()
	st := newMemStore()
	for f := 1; f <= fabrics; f++ {
		idx := uint8(f) //nolint:gosec // G115: f ≤ 254
		compressed := testCompressed
		if !shared {
			compressed[7] = idx
		}
		st.addFabric(idx, uint64(f), compressed)
		st.putKeySet(idx, store.GroupKeySet{GroupKeySetID: 0, EpochKey0: epochKeyOf(0x99)})
		for k := 1; k <= keySets; k++ {
			id := uint16(k) //nolint:gosec // G115: k ≤ 19
			st.putKeySet(idx, store.GroupKeySet{
				GroupKeySetID: id,
				EpochKey0:     benchEpochKey(idx, id, 0, shared), EpochStart0: 1,
				EpochKey1: benchEpochKey(idx, id, 1, shared), EpochStart1: 2,
				EpochKey2: benchEpochKey(idx, id, 2, shared), EpochStart2: 3,
			})
			st.mapGroup(idx, 0x0100+id, id)
		}
	}
	var err error
	if m, err = NewManager(st, nil); err != nil {
		b.Fatal(err)
	}
	first := testCompressed
	if !shared {
		first[7] = 1
	}
	op, err := OperationalKey(benchEpochKey(1, 1, 0, shared), first)
	if err != nil {
		b.Fatal(err)
	}
	if firstSessionID, err = SessionID(op); err != nil {
		b.Fatal(err)
	}
	return m, firstSessionID
}

// unusedSessionID is a group session id no epoch key of m carries.
func unusedSessionID(b *testing.B, m *Manager) uint16 {
	b.Helper()
	for sid := range uint16(0xFFFF) {
		if len(m.candidates(sid)) == 0 {
			return sid
		}
	}
	b.Fatal("every group session id is carried by a key")
	return 0
}

func BenchmarkDecodeUnopenedDatagram(b *testing.B) {
	foreignKey := benchEpochKey(0xEE, 0xEEEE, 0, false)
	cases := []struct {
		name             string
		fabrics, keySets int
		shared           bool
	}{
		{"realistic-5x3", 5, 3, false},
		{fmt.Sprintf("max-%dx%d-distinct", maxBenchFabrics, maxBenchKeySets), maxBenchFabrics, maxBenchKeySets, false},
		{fmt.Sprintf("max-%dx%d-one-session-id", maxBenchFabrics, maxBenchKeySets), maxBenchFabrics, maxBenchKeySets, true},
	}
	for _, c := range cases {
		m, carried := newBenchManager(b, c.fabrics, c.keySets, c.shared)
		ctx := context.Background()
		if err := m.ensureAllLoaded(ctx, loadForDatagram); err != nil {
			b.Fatal(err)
		}
		for _, sid := range []struct {
			name string
			id   uint16
		}{{"session-id-unused", unusedSessionID(b, m)}, {"session-id-carried", carried}} {
			b.Run(c.name+"/"+sid.name, func(b *testing.B) {
				// Sealed under a key the node does not hold, with the
				// session id under test in its header.
				datagram := sealGroup(b, foreignKey, sid.id, 0x0101, 0x77, 1, false, imPayload([]byte("toggle")))
				candidates := len(m.candidates(sid.id))
				b.ReportAllocs()
				for b.Loop() {
					_, err := m.Decode(ctx, datagram)
					var rejected *DecodeError
					if !errors.As(err, &rejected) {
						b.Fatalf("Decode: %v, want a *DecodeError", err)
					}
				}
				b.ReportMetric(float64(candidates), "candidates")
				b.ReportMetric(float64(c.fabrics*c.keySets*epochsPerKeySet), "keys")
			})
		}
	}
}
