// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
)

// unsigned is the underlying type of a generated FeatureMap, enum or
// bitmap type.
type unsigned interface {
	~uint8 | ~uint16 | ~uint32 | ~uint64
}

// CheckFeatures holds a cluster package's Feature constants against its
// definition: bits lists them in the order of def.Features, and each must
// be the one bit def states for that feature. A cluster without a feature
// passes none, which holds that def states none either.
//
// The definition is held against the matter.js snapshot by
// [CheckDefinition], so the constants are held against it too: a feature
// bit is the snapshot's, never its position in the list (the bits are
// sparse — DoorLock skips 3 and 9).
func CheckFeatures[F unsigned](tb testing.TB, def *spec.Cluster, bits ...F) {
	tb.Helper()
	if len(bits) != len(def.Features) {
		tb.Errorf("%s: %d Feature constants, definition %d features", def.Name, len(bits), len(def.Features))
		return
	}
	for i, b := range bits {
		if f := &def.Features[i]; uint64(b) != uint64(f.Mask()) {
			tb.Errorf("%s: feature %s constant 0x%X, definition bit %d", def.Name, f.Name, uint64(b), f.Bit)
		}
	}
}

// CheckEnum holds an enum's Go constants against its descriptor: values
// lists them in the order of def.Values, and each must be that value and
// fit def.Bits.
func CheckEnum[E unsigned](tb testing.TB, def *spec.Enum, values ...E) {
	tb.Helper()
	if len(values) != len(def.Values) {
		tb.Errorf("%s: %d constants, descriptor %d values", def.Name, len(values), len(def.Values))
		return
	}
	for i, v := range values {
		ev := &def.Values[i]
		if uint64(v) != ev.Value {
			tb.Errorf("%s: %s constant %d, descriptor %d", def.Name, ev.Name, uint64(v), ev.Value)
		}
		if !fits(uint64(v), def.Bits) {
			tb.Errorf("%s: %s value %d does not fit %d bits", def.Name, ev.Name, uint64(v), def.Bits)
		}
	}
}

// CheckBitmap holds a bitmap's Go constants against its descriptor:
// members lists them in the order of def.Members, and each must be that
// member's mask (one bit for a flag, the whole range for a wider member)
// and fit def.Bits.
func CheckBitmap[B unsigned](tb testing.TB, def *spec.Bitmap, members ...B) {
	tb.Helper()
	if len(members) != len(def.Members) {
		tb.Errorf("%s: %d constants, descriptor %d members", def.Name, len(members), len(def.Members))
		return
	}
	for i, m := range members {
		bm := &def.Members[i]
		if uint64(m) != bm.Mask() {
			tb.Errorf("%s: %s constant 0x%X, descriptor mask 0x%X", def.Name, bm.Name, uint64(m), bm.Mask())
		}
		if !fits(uint64(m), def.Bits) {
			tb.Errorf("%s: %s mask 0x%X does not fit %d bits", def.Name, bm.Name, uint64(m), def.Bits)
		}
	}
}

// fits reports whether v is representable in bits bits.
func fits(v uint64, bits int) bool {
	return bits >= 64 || v>>uint(bits) == 0
}
