// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build unix

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAppPipeIsNotWorldWritable: the app pipe takes test commands, so the
// world has no access to it (W5-ex-14).
func TestAppPipeIsNotWorldWritable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "app.fifo")
	if err := makeFIFO(path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := st.Mode().Perm(); mode != fifoMode {
		t.Errorf("app pipe mode %o, want %o", mode, fifoMode)
	}
}
