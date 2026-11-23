// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

// Benchmarks for the node's CASE responder fabric lookup:
// caseIdentities.resolveSigma1Destination computes a destination id for
// every candidate key of every installed fabric and returns at the first
// equal one. Measured with the default and the largest fabric table and
// four candidate keys per fabric (three keys of key set 0 and the AddNOC
// key), for a Sigma1 that addresses no fabric and for one that addresses
// an installed fabric. The fabrics sit in a map, so the matched fabric's
// position in the iteration differs from run to run.

import (
	"crypto/rand"
	"strconv"
	"testing"

	"thingfabric.dev/go-fabric/secure/sigma"
)

// benchCaseIPKsPerFabric is the candidate key count per fabric.
const benchCaseIPKsPerFabric = 4

func benchCaseIdentities(tb testing.TB, fabrics int) *caseIdentities {
	tb.Helper()
	c := newCaseIdentities(nil, nil)
	for i := range fabrics {
		idx := uint8(i + 1)
		id := &sigma.Identity{FabricID: uint64(i + 1), NodeID: 0x2000 + uint64(i), FabricIndex: idx}
		ipks := make([][16]byte, benchCaseIPKsPerFabric)
		for k := range ipks {
			_, _ = rand.Read(ipks[k][:])
		}
		id.IPK = ipks[0]
		root := make([]byte, 65)
		root[0] = 0x04
		_, _ = rand.Read(root[1:])
		c.byIdx[idx] = &caseFabric{identity: id, rootPublicKey: root, addNOCIPK: ipks[benchCaseIPKsPerFabric-1], ipks: ipks}
	}
	return c
}

func BenchmarkCaseResolveSigma1Destination(b *testing.B) {
	var random [sigma.RandomSize]byte
	_, _ = rand.Read(random[:])
	for _, n := range []int{defaultSupportedFabrics, maxSupportedFabrics} {
		c := benchCaseIdentities(b, n)
		var none [32]byte
		_, _ = rand.Read(none[:])
		target := c.byIdx[uint8(n)]
		hit := sigma.ComputeDestinationID(target.ipks[benchCaseIPKsPerFabric-1], random, target.rootPublicKey, target.identity.FabricID, target.identity.NodeID)
		for _, tc := range []struct {
			name string
			dest [32]byte
			want bool
		}{{"none", none, false}, {"installed", hit, true}} {
			b.Run("fabrics="+strconv.Itoa(n)+"/match="+tc.name, func(b *testing.B) {
				for b.Loop() {
					if _, _, _, ok := c.resolveSigma1Destination(tc.dest, random); ok != tc.want {
						b.Fatalf("found=%v, want %v", ok, tc.want)
					}
				}
			})
		}
	}
}
