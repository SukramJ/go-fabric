// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valueenc_test

import (
	"fmt"
	"os"
	"testing"

	"thingfabric.dev/go-fabric/internal/valueenc"
)

// TestMain fails the run when an encoder panicked and the panic was
// contained (internal/valueenc) without a test declaring it: a coding
// mistake in this module's own encoders must still fail a test (#625).
func TestMain(m *testing.M) {
	code := m.Run()
	if n, last := valueenc.UnexpectedPanics(); n != 0 {
		fmt.Fprintf(os.Stderr, "FAIL: %d contained encoder panic(s) no test declared (internal/valueenc tripwire); last:\n%s\n", n, last)
		code = 1
	}
	os.Exit(code)
}
