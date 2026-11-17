// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package netinfra

import (
	"bytes"
	"testing"
)

// TestReplacedPassphraseIsZeroed (R2-P6-conc-1118 b): a passphrase that a
// new one replaces is zeroed, as ClearNetworkCredentials zeroes it.
func TestReplacedPassphraseIsZeroed(t *testing.T) {
	s, err := NewWiFiManagement(WiFiManagementConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetNetworkCredentials([]byte("home"), []byte("passphrase-1")); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	old := s.passphrase
	s.mu.Unlock()
	if err := s.SetNetworkCredentials([]byte("home"), []byte("passphrase-2")); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(old, make([]byte, len(old))) {
		t.Fatalf("the replaced passphrase is still %q", old)
	}
}
