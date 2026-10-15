// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgetest

// The commissioner's seeded randomness: one stream per label, each a
// function of [Options.Seed] alone, so two commissioners with one seed
// draw the same bytes in the same order.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"sync"
)

// seededStream is an io.Reader whose bytes are the blocks
// deriveBytes(seed, label, 0), deriveBytes(seed, label, 1), … It is safe
// for concurrent use; concurrent readers share the one stream.
type seededStream struct {
	seed  uint64
	label string

	mu    sync.Mutex
	block uint64
	buf   []byte
}

func newSeededStream(seed uint64, label string) *seededStream {
	return &seededStream{seed: seed, label: label}
}

// Read fills p from the stream; it never fails.
func (r *seededStream) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(r.buf) < len(p) {
		r.buf = append(r.buf, deriveBytes(r.seed, r.label, r.block, sha256.Size)...)
		r.block++
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// Float64 draws a value in [0, 1) from the stream — the MRP jitter of
// the exchange manager.
func (r *seededStream) Float64() float64 {
	var b [8]byte
	_, _ = r.Read(b[:])
	return float64(binary.BigEndian.Uint64(b[:])>>11) / (1 << 53)
}

// key draws a P-256 private key from the stream: a scalar outside
// [1, N-1] is rejected and the next 32 bytes drawn (probability about
// 2^-32 per draw).
func (r *seededStream) key() *ecdsa.PrivateKey {
	for {
		var b [32]byte
		_, _ = r.Read(b[:])
		if k, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), b[:]); err == nil {
			return k
		}
	}
}

// deriveBytes returns size bytes named by (seed, label, n): HKDF-SHA256
// over the seed and n, the label as info.
func deriveBytes(seed uint64, label string, n uint64, size int) []byte {
	var in [16]byte
	binary.BigEndian.PutUint64(in[:8], seed)
	binary.BigEndian.PutUint64(in[8:], n)
	out, err := hkdf.Key(sha256.New, in[:], nil, "go-fabric bridgetest "+label, size)
	mustNot(err)
	return out
}

// mustNot panics on an error that only a defect in this package can
// produce (an encoder or a derivation over constant inputs).
func mustNot(err error) {
	if err != nil {
		panic("bridgetest: " + err.Error())
	}
}
