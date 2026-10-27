// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread

// A port of matter.js packages/protocol/test/thread/MeshCopTlvTypesTest.ts
// at 862ad75f: the MeshCoP TLV type table and its reverse lookup.

import "testing"

func TestMeshCopTlvTypesType(t *testing.T) {
	t.Run("exposes the canonical numeric values", func(t *testing.T) {
		for _, c := range []struct {
			got  TLVType
			want uint8
		}{
			{TLVChannel, 0},
			{TLVPANID, 1},
			{TLVExtPANID, 2},
			{TLVNetworkName, 3},
			{TLVPSKc, 4},
			{TLVNetworkKey, 5},
			{TLVMeshLocalPrefix, 7},
			{TLVSecurityPolicy, 12},
			{TLVActiveTimestamp, 14},
			{TLVPendingTimestamp, 51},
			{TLVDelayTimer, 52},
			{TLVChannelMask, 53},
			{TLVJoinerAdvertisement, 241},
		} {
			if uint8(c.got) != c.want {
				t.Errorf("type = %d, want %d", c.got, c.want)
			}
		}
	})

	// Object.values / Object.keys of MeshCopTlvType are the package's table.
	t.Run("has no duplicate numeric values", func(t *testing.T) {
		seen := map[TLVType]bool{}
		for _, e := range tlvTypes {
			if seen[e.typ] {
				t.Errorf("type %d listed twice", e.typ)
			}
			seen[e.typ] = true
		}
	})

	t.Run("contains the 43 entries known to Agners' reference parser", func(t *testing.T) {
		if len(tlvTypes) != 43 {
			t.Fatalf("%d entries, want 43", len(tlvTypes))
		}
	})

	t.Run("includes the post-spec extensions DURATION, THREAD_DOMAIN_NAME, WAKEUP_CHANNEL", func(t *testing.T) {
		if TLVDuration != 23 || TLVThreadDomainName != 59 || TLVWakeupChannel != 74 {
			t.Fatalf("DURATION=%d THREAD_DOMAIN_NAME=%d WAKEUP_CHANNEL=%d", TLVDuration, TLVThreadDomainName, TLVWakeupChannel)
		}
	})
}

func TestMeshCopTlvTypesName(t *testing.T) {
	t.Run("maps numeric type back to canonical name", func(t *testing.T) {
		for typ, want := range map[TLVType]string{0: "CHANNEL", 3: "NETWORK_NAME", 12: "SECURITY_POLICY", 241: "JOINER_ADVERTISEMENT"} {
			if got, ok := typ.Name(); !ok || got != want {
				t.Errorf("Name(%d) = %q, %v; want %q", typ, got, ok, want)
			}
		}
	})

	t.Run("returns undefined for an unknown type", func(t *testing.T) {
		if got, ok := TLVType(200).Name(); ok || got != "" {
			t.Fatalf("Name(200) = %q, %v; want none", got, ok)
		}
	})
}
