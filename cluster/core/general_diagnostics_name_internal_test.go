// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestInterfaceNameCutToTheConstraint: NetworkInterfaces serves an OS
// interface name cut to the field's "max 32" in bytes, never inside a
// rune (R2-P5-apispec-11, S-3).
func TestInterfaceNameCutToTheConstraint(t *testing.T) {
	t.Parallel()
	if maxInterfaceName != 32 {
		t.Fatalf("NetworkInterface.Name maximum %d, want the snapshot's 32", maxInterfaceName)
	}
	for _, c := range []struct{ in, want string }{
		{"en0", "en0"},
		{strings.Repeat("a", 32), strings.Repeat("a", 32)},
		{strings.Repeat("b", 40), strings.Repeat("b", 32)},
		{strings.Repeat("c", 31) + "äx", strings.Repeat("c", 31)}, // "ä" would end at byte 33
		{strings.Repeat("d", 30) + "äx", strings.Repeat("d", 30) + "ä"},
	} {
		got := interfaceName(c.in)
		if got != c.want || len(got) > 32 || !utf8.ValidString(got) {
			t.Errorf("interfaceName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
