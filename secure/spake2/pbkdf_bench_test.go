// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2_test

// Benchmarks for the PBKDF2 derivation a PASE initiator runs once per
// handshake, after the responder's PBKDFParamResponse has named the salt
// and the iteration count. The iteration count is the responder's choice
// within [spake2.PBKDFMinIterations, spake2.PBKDFMaxIterations], which
// DecodePBKDFParamResponse enforces, so the cost of one derivation is
// measured at the lower bound, at a mid value and at the upper bound.

import (
	"strconv"
	"testing"

	"thingfabric.dev/go-fabric/secure/spake2"
)

// benchPBKDFIterations are the iteration counts the benchmarks derive at:
// the accepted minimum, a mid value and the accepted maximum.
var benchPBKDFIterations = []uint32{
	spake2.PBKDFMinIterations,
	10_000,
	spake2.PBKDFMaxIterations,
}

// benchPBKDFSalt is a salt of the largest accepted length; the salt
// length moves the cost of the first HMAC block only.
var benchPBKDFSalt = make([]byte, spake2.PBKDFMaxSaltSize)

// BenchmarkPBKDF measures the (w0, w1) derivation alone.
func BenchmarkPBKDF(b *testing.B) {
	for _, it := range benchPBKDFIterations {
		b.Run("iterations="+strconv.FormatUint(uint64(it), 10), func(b *testing.B) {
			for b.Loop() {
				if _, _, err := spake2.PBKDF(20202021, benchPBKDFSalt, int(it)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkNewProver measures what the PASE initiator calls once the
// response is decoded: the derivation plus the prover's construction.
func BenchmarkNewProver(b *testing.B) {
	for _, it := range benchPBKDFIterations {
		b.Run("iterations="+strconv.FormatUint(uint64(it), 10), func(b *testing.B) {
			for b.Loop() {
				p, err := spake2.NewProver(20202021, benchPBKDFSalt, int(it), nil, nil, nil)
				if err != nil {
					b.Fatal(err)
				}
				p.Close()
			}
		})
	}
}

// TestPBKDFParamResponseIterationBounds pins the bounds the benchmarks
// above measure at: a response one outside either bound does not decode,
// one at either bound does.
func TestPBKDFParamResponseIterationBounds(t *testing.T) {
	for _, tc := range []struct {
		iterations uint32
		ok         bool
	}{
		{spake2.PBKDFMinIterations - 1, false},
		{spake2.PBKDFMinIterations, true},
		{spake2.PBKDFMaxIterations, true},
		{spake2.PBKDFMaxIterations + 1, false},
		{0xFFFFFFFF, false},
	} {
		resp := spake2.PBKDFParamResponse{
			InitiatorRandom:    make([]byte, 32),
			ResponderRandom:    make([]byte, 32),
			ResponderSessionID: 1,
			Parameters:         &spake2.PBKDFParameters{Iterations: tc.iterations, Salt: make([]byte, spake2.PBKDFMinSaltSize)},
		}
		_, err := spake2.DecodePBKDFParamResponse(resp.Marshal())
		if (err == nil) != tc.ok {
			t.Errorf("iterations %d: decode error %v, want accepted=%v", tc.iterations, err, tc.ok)
		}
	}
}
