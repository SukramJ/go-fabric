// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import "testing"

// TestICDVerificationKeyComparedInConstantTime pins the comparison of
// assertMayModify (review 2026-11 W2-sec-04): the same accept/refuse
// outcome as matter.js IcdManagementServer.ts:761 (Bytes.areEqual), taken
// by a length check and crypto/subtle.ConstantTimeCompare
// (notes/parity/by_design.md BD-Matter-ICDVerificationKeyConstantTime).
func TestICDVerificationKeyComparedInConstantTime(t *testing.T) {
	t.Parallel()
	key := []byte("0123456789abcdef")
	cases := []struct {
		name      string
		presented []byte
		want      bool
	}{
		{"equal", []byte("0123456789abcdef"), true},
		{"last byte differs", []byte("0123456789abcdeX"), false},
		{"shorter", []byte("0123456789abcde"), false},
		{"longer", []byte("0123456789abcdef0"), false},
		{"empty", []byte{}, false},
	}
	for _, tc := range cases {
		if got := verificationKeyMatches(tc.presented, key); got != tc.want {
			t.Errorf("%s: verificationKeyMatches = %v, want %v", tc.name, got, tc.want)
		}
	}
}
