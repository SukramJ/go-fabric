// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package netinfra_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

// TestCredentialChangeCallbackReadsTheServer (R2-P6-conc-1118 a): a
// change callback that reads the credentials back does not deadlock
// SetNetworkCredentials or ClearNetworkCredentials, and sees the new
// credentials.
func TestCredentialChangeCallbackReadsTheServer(t *testing.T) {
	s := newWiFi(t)
	var seen [][]byte
	unsub := s.OnAttributeChange(func(contract.Change) {
		seen = append(seen, s.SSID())
		_ = s.HasNetworkCredentials()
		_ = s.Passphrase()
	})
	defer unsub()
	// A deadlock hangs these calls (the test binary's timeout reports it).
	if err := s.SetNetworkCredentials([]byte("home"), []byte("passphrase-1")); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearNetworkCredentials(); err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 || !bytes.Equal(seen[0], []byte("home")) {
		t.Fatalf("the callback saw %q", seen)
	}
}
