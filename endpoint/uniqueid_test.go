// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"testing"
)

// TestUniqueIDFor_DistinctAcrossSourceKeys is the regression guard for
// the Apple Home pair-abort root cause — `uniqueIDFor` once fell back to
// one constant literal string for the concrete key type the assembler
// actually carried, because the type switch only matched `Stringer` /
// `Central()/Address()` interfaces and that type satisfied neither. The
// result was every bridged endpoint sharing
// `BridgedDeviceBasicInformation.UniqueID` and Apple's HAP service
// mapper collapsing all five into a single HMAccessory. We assert here
// that five distinct [SourceKey] values each hash to a distinct
// 32-character hex value.
//
// The keys below are shaped the way a Homematic owner renders them; this
// package never parses that shape, and the guard holds for any set of
// distinct strings.
func TestUniqueIDFor_DistinctAcrossSourceKeys(t *testing.T) {
	t.Parallel()
	keys := []StringKey{
		"GoOtto|000C9709AC8CAF|1|generic|",
		"GoOtto|000C9709AC8CB1|1|generic|",
		"GoOtto|000C9709AC8CD4|2|generic|",
		"GoOtto|00091569A38F49|1|measurement|ILLUMINATION",
		"GoOtto|000A1709AF4FC9|1|generic|",
	}
	seen := make(map[string]StringKey, len(keys))
	for _, k := range keys {
		uid := uniqueIDFor(k)
		if len(uid) != 32 {
			t.Errorf("uniqueIDFor(%q) returned %d hex chars, want 32: %q", k, len(uid), uid)
		}
		if prev, dup := seen[uid]; dup {
			t.Errorf("collision: uniqueIDFor(%q) and uniqueIDFor(%q) both = %q", k, prev, uid)
		}
		seen[uid] = k
	}
	if len(seen) != len(keys) {
		t.Fatalf("only %d unique IDs from %d distinct SourceKeys", len(seen), len(keys))
	}
}

// TestUniqueIDFor_StableAcrossInvocations asserts the hash is purely
// a function of the key — Matter §9.13.5.20 mandates a persistent
// per-device identifier, and Apple Home pins HMAccessory state by it.
func TestUniqueIDFor_StableAcrossInvocations(t *testing.T) {
	t.Parallel()
	const key = StringKey("GoOtto|000C9709AC8CAF|7|generic|")
	first := uniqueIDFor(key)
	for i := range 16 {
		got := uniqueIDFor(key)
		if got != first {
			t.Fatalf("uniqueIDFor returned different values for the same key: first=%q got=%q (iteration %d)", first, got, i)
		}
	}
}

// TestUniqueIDFor_PointerAndValueAgree guards the *SourceKey vs
// SourceKey path through renderSourceKey — both shapes must produce
// the same fingerprint or the bridge would expose two different
// UniqueIDs depending on whether the assembler hands over a value or
// a pointer.
func TestUniqueIDFor_PointerAndValueAgree(t *testing.T) {
	t.Parallel()
	k := StringKey("GoOtto|000C9709AC8CAF|1|generic|")
	if uniqueIDFor(k) != uniqueIDFor(&k) {
		t.Fatalf("uniqueIDFor(value) != uniqueIDFor(pointer): %q vs %q", uniqueIDFor(k), uniqueIDFor(&k))
	}
}

// TestUniqueIDFor_UsesTheWholeKey ensures the hash reads the key end to
// end. A renderer that truncated it — or hashed only a prefix — would
// regress to the duplicate-fingerprint state this guard set was written
// for, because owners render their coordinates in a fixed order and the
// last one would stop counting.
//
// Whether the OWNER's key carries every coordinate of its own model is
// the owner's guard to keep; this one only pins that nothing is dropped
// on the way into the hash.
func TestUniqueIDFor_UsesTheWholeKey(t *testing.T) {
	t.Parallel()
	const base = StringKey("GoOtto|000C9709AC8CAF|1|generic|STATE")
	baseHash := uniqueIDFor(base)
	for _, variant := range []StringKey{
		"OtherCcu|000C9709AC8CAF|1|generic|STATE",
		"GoOtto|000C9709AC8CB1|1|generic|STATE",
		"GoOtto|000C9709AC8CAF|2|generic|STATE",
		"GoOtto|000C9709AC8CAF|1|measurement|STATE",
		"GoOtto|000C9709AC8CAF|1|generic|DIFFERENT",
	} {
		if uniqueIDFor(variant) == baseHash {
			t.Errorf("%q hashes the same as %q — part of the key is not reaching the hash", variant, base)
		}
	}
}

// TestUniqueIDFor_NilKey returns a deterministic non-empty string so
// the BridgedDeviceBasicInformation cluster can still publish UniqueID;
// the value is not required to be unique across nil-key endpoints
// (those should not exist in production), only to be a valid 32-char
// hex string.
func TestUniqueIDFor_NilKey(t *testing.T) {
	t.Parallel()
	if got := uniqueIDFor(nil); len(got) != 32 {
		t.Fatalf("uniqueIDFor(nil) = %q; want 32-char hex string", got)
	}
}
