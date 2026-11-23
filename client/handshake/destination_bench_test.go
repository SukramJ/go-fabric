// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake_test

// Benchmarks for the CASE responder's fabric lookup: FabricList computes
// one destination id per fabric for the Sigma1's initiator random and
// returns at the first equal one. The cost of a Sigma1 that addresses no
// fabric, of one that addresses the first fabric and of one that
// addresses the last are measured over fabric lists of several lengths,
// so the time a lookup takes can be read against the list's length and
// the matched fabric's position.

import (
	"crypto/rand"
	"strconv"
	"testing"

	"thingfabric.dev/go-fabric/client/handshake"
	"thingfabric.dev/go-fabric/secure/sigma"
)

// benchDestinationFabricCounts are the fabric list lengths measured.
var benchDestinationFabricCounts = []int{1, 5, 16, 254}

// benchFabricList builds n fabrics, each with its own IPK and root key.
func benchFabricList(tb testing.TB, n int) handshake.FabricList {
	tb.Helper()
	l := make(handshake.FabricList, n)
	for i := range l {
		id := &sigma.Identity{FabricID: uint64(i + 1), NodeID: 0x1000 + uint64(i), FabricIndex: uint8(i%254 + 1)}
		_, _ = rand.Read(id.IPK[:])
		root := make([]byte, 65)
		root[0] = 0x04
		_, _ = rand.Read(root[1:])
		l[i] = handshake.Fabric{Identity: id, RootPublicKey: root}
	}
	return l
}

// destinationFor is the destination id an initiator addressing f sends.
func destinationFor(f handshake.Fabric, random [sigma.RandomSize]byte) [32]byte {
	return sigma.ComputeDestinationID(f.Identity.IPK, random, f.RootPublicKey, f.Identity.FabricID, f.Identity.NodeID)
}

func BenchmarkFabricListDestinationLookup(b *testing.B) {
	var random [sigma.RandomSize]byte
	_, _ = rand.Read(random[:])
	for _, n := range benchDestinationFabricCounts {
		l := benchFabricList(b, n)
		var none [32]byte
		_, _ = rand.Read(none[:])
		cases := []struct {
			name string
			dest [32]byte
			want bool
		}{
			{"none", none, false},
			{"first", destinationFor(l[0], random), true},
			{"last", destinationFor(l[n-1], random), true},
		}
		for _, c := range cases {
			b.Run("fabrics="+strconv.Itoa(n)+"/match="+c.name, func(b *testing.B) {
				for b.Loop() {
					if _, ok := l.FabricForDestination(c.dest, random); ok != c.want {
						b.Fatalf("found=%v, want %v", ok, c.want)
					}
				}
			})
		}
	}
}

// benchDestinationEqualSink keeps the comparison below from being
// eliminated.
var benchDestinationEqualSink bool

// BenchmarkDestinationIDEquality measures the 32-byte comparison the
// lookup applies to each candidate, for an equal id and for ids that
// first differ in the first and in the last byte.
func BenchmarkDestinationIDEquality(b *testing.B) {
	var a [32]byte
	_, _ = rand.Read(a[:])
	firstDiffers, lastDiffers := a, a
	firstDiffers[0] ^= 1
	lastDiffers[31] ^= 1
	for _, c := range []struct {
		name  string
		other [32]byte
	}{{"equal", a}, {"differs-at-0", firstDiffers}, {"differs-at-31", lastDiffers}} {
		b.Run(c.name, func(b *testing.B) {
			x, y := &a, &c.other
			for b.Loop() {
				benchDestinationEqualSink = *x == *y
			}
		})
	}
}
