// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx

import (
	"path/filepath"
	"testing"
)

// TestFileSinkWrittenDoesNotWaitForTheDisk pins G1-16 (ADR 0026 P6): the
// file I/O lock is held across a write or the close's sync and rename,
// and a progress read does not take it.
func TestFileSinkWrittenDoesNotWaitForTheDisk(t *testing.T) {
	t.Parallel()
	s := NewFileSink(filepath.Join(t.TempDir(), "image.ota"))
	if _, err := s.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	s.io.Lock() // a write or close in the middle of its I/O
	n := s.Written()
	s.io.Unlock()
	if n != 3 {
		t.Fatalf("Written() = %d, want 3", n)
	}
	if err := s.Close(nil); err != nil {
		t.Fatal(err)
	}
}
