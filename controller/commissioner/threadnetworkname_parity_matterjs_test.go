// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// A port of matter.js packages/protocol/test/peer/ThreadNetworkNameResolutionTest.ts at
// 862ad75f: resolving the Thread network name from the supplied name and the
// operational dataset.

import (
	"encoding/hex"
	"errors"
	"regexp"
	"testing"
)

// TestThreadNetworkNameResolution ports describe("resolveThreadNetworkName").
// matter.js's undefined is the empty string here; ThreadNetworkNameMismatchError
// is ErrThreadNetworkNameMismatch.
func TestThreadNetworkNameResolution(t *testing.T) {
	t.Parallel()
	ds := func(s string) []byte {
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var (
		datasetMyNet     = ds("03054d794e6574") // ThreadNetworkNameResolutionTest.ts:10
		datasetNoName    = ds("01021234")       // :12
		datasetMalformed = ds("0305")           // :14
	)
	resolve := func(name string, dataset []byte) (string, error) {
		return resolveThreadNetworkName(&ThreadNetwork{NetworkName: name, OperationalDataset: dataset}, discard())
	}
	expect := func(t *testing.T, name string, dataset []byte, want string) {
		t.Helper()
		got, err := resolve(name, dataset)
		if err != nil || got != want {
			t.Fatalf("resolve(%q, %x) = %q, %v; want %q", name, dataset, got, err, want)
		}
	}

	t.Run("derives the name from the dataset when none is supplied", func(t *testing.T) {
		expect(t, "", datasetMyNet, "MyNet")
	})
	t.Run("treats an empty-string name as absent and derives", func(t *testing.T) {
		expect(t, "", datasetMyNet, "MyNet")
	})
	t.Run("returns the supplied name when it matches the dataset", func(t *testing.T) {
		expect(t, "MyNet", datasetMyNet, "MyNet")
	})
	t.Run("declines with a typed error when the supplied name mismatches the dataset", func(t *testing.T) {
		_, err := resolve("OtherNet", datasetMyNet)
		if !errors.Is(err, ErrThreadNetworkNameMismatch) || !regexp.MustCompile(`OtherNet.*MyNet`).MatchString(err.Error()) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("keeps the supplied name when the dataset carries no network name", func(t *testing.T) {
		expect(t, "MyNet", datasetNoName, "MyNet")
	})
	t.Run("returns undefined when neither the params nor the dataset carry a name", func(t *testing.T) {
		expect(t, "", datasetNoName, "")
	})
	t.Run("falls back to the supplied name and does not throw when the dataset cannot be decoded", func(t *testing.T) {
		expect(t, "MyNet", datasetMalformed, "MyNet")
	})
}
