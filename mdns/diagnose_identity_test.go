// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"bytes"
	"net"
	"testing"
)

// TestDiagnoseIgnoresANilAddressEntry pins the skip in the address walk.
// A Service assembled from partly-parsed input can carry a nil entry —
// net.ParseIP returns nil for anything it cannot read — and the walk has
// to step over it. Reading through a nil net.IP would panic inside the
// diagnostic, which is the one place that must stay usable when the
// advertisement is already malformed.
//
// The routable address alongside it must still be seen, so the record
// does not additionally report as address-less.
func TestDiagnoseIgnoresANilAddressEntry(t *testing.T) {
	t.Parallel()

	findings := Diagnose([]Service{{
		InstanceName: "x",
		ServiceType:  ServiceTypeOperational,
		Port:         5540,
		Addresses:    []net.IP{nil, net.ParseIP("192.168.1.40")},
	}})
	for _, code := range []string{"no_addresses", "container_internal_address"} {
		if hasCode(findings, code) {
			t.Errorf("a nil address entry produced %q; findings = %v", code, codes(findings))
		}
	}
}

// TestDiagnoseReportsARecordWithOnlyNilAddresses covers the other side of
// the same skip: when every entry is unreadable the record really does
// tell a controller nothing to connect to, and the diagnostic must say
// so rather than pass the record as healthy.
func TestDiagnoseReportsARecordWithOnlyNilAddresses(t *testing.T) {
	t.Parallel()

	findings := Diagnose([]Service{{
		InstanceName: "x",
		ServiceType:  ServiceTypeOperational,
		Port:         5540,
		Addresses:    []net.IP{nil},
	}})
	// The slice is non-empty, so the "no_addresses" arm does not fire;
	// what must fire is the missing-IPv6 finding, since a record with no
	// readable address certainly has no AAAA.
	if len(findings) == 0 {
		t.Fatal("a record whose only address is unreadable produced no findings")
	}
}

// TestDeriveUniqueIDFromIdentityIsStableAndDistinct pins the two
// properties the rotating-device-id derivation depends on. The UniqueID
// feeds [GenerateRotatingID], whose output ships in the `RI` TXT key of
// the commissionable record (Matter §4.3.1.6): if it moved between
// restarts a controller would see a different device each boot, and if
// two bridges collided they would be indistinguishable in the same
// commissioning browse.
//
// The length is the one the spec fixes — chip
// `setup_payload/AdditionalDataPayloadGenerator.h`
// kRotatingDeviceIDUniqueIDLength, mirrored here as
// [RotatingIDUniqueIDLength].
func TestDeriveUniqueIDFromIdentityIsStableAndDistinct(t *testing.T) {
	t.Parallel()

	base := DeriveUniqueIDFromIdentity(0xFFF1, 0x8000, "SN-0001", "Kitchen")
	if len(base) != RotatingIDUniqueIDLength {
		t.Fatalf("UniqueID length = %d, want %d", len(base), RotatingIDUniqueIDLength)
	}

	// Stable: the same identity tuple derives the same UniqueID, which
	// is what makes the RI key survive a restart without persistence.
	again := DeriveUniqueIDFromIdentity(0xFFF1, 0x8000, "SN-0001", "Kitchen")
	if !bytes.Equal(base, again) {
		t.Error("the same identity tuple derived two different UniqueIDs")
	}

	// Distinct: every field of the tuple must reach the digest. A field
	// that did not would let two bridges differing only in that field
	// advertise the same rotating id.
	variants := map[string][]byte{
		"vendor":  DeriveUniqueIDFromIdentity(0xFFF2, 0x8000, "SN-0001", "Kitchen"),
		"product": DeriveUniqueIDFromIdentity(0xFFF1, 0x8001, "SN-0001", "Kitchen"),
		"serial":  DeriveUniqueIDFromIdentity(0xFFF1, 0x8000, "SN-0002", "Kitchen"),
		"label":   DeriveUniqueIDFromIdentity(0xFFF1, 0x8000, "SN-0001", "Hallway"),
	}
	for field, got := range variants {
		if bytes.Equal(got, base) {
			t.Errorf("changing %s left the UniqueID unchanged; that field does not reach the digest", field)
		}
	}

	// The serial / label boundary is separated by a delimiter byte, so
	// splitting the same characters differently must not collide.
	if a, b := DeriveUniqueIDFromIdentity(0xFFF1, 0x8000, "AB", "C"),
		DeriveUniqueIDFromIdentity(0xFFF1, 0x8000, "A", "BC"); bytes.Equal(a, b) {
		t.Error(`("AB","C") and ("A","BC") derived the same UniqueID; the field delimiter is missing`)
	}
}

// TestMustGenerateRotatingIDMatchesGenerateRotatingID pins the
// non-panicking path of the Must wrapper. It exists so first-boot wiring
// fails loudly on a short UniqueID; on a valid one it must return
// exactly what [GenerateRotatingID] returns, not a second derivation.
func TestMustGenerateRotatingIDMatchesGenerateRotatingID(t *testing.T) {
	t.Parallel()

	uniqueID := DeriveUniqueIDFromIdentity(0xFFF1, 0x8000, "SN-0001", "Kitchen")
	const counter uint16 = 7

	got := MustGenerateRotatingID(uniqueID, counter)
	if want := GenerateRotatingID(uniqueID, counter); got != want {
		t.Fatalf("MustGenerateRotatingID = %q, want %q", got, want)
	}
	// 2-byte counter prefix + 16-byte hash suffix, hex-encoded: 36
	// characters (chip AdditionalDataPayloadGenerator.cpp:113).
	if len(got) != 36 {
		t.Errorf("rotating id length = %d, want 36 hex characters", len(got))
	}
}
