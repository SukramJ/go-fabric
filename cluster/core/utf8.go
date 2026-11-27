// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import "unicode/utf8"

// truncateUTF8 is the longest prefix of s of at most maxBytes bytes that
// ends between two runes: a stored string over its bound, cut where
// matter.js cuts it — packages/general/src/util/Bytes.ts:156-170 at
// db91886c (truncateUtf8, "cut only between code points", #4995).
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := max(maxBytes, 0)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
