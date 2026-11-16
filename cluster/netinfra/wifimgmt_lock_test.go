// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package netinfra_test

import (
	"bytes"
	"testing"
	"time"

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
	done := make(chan error, 2)
	go func() {
		done <- s.SetNetworkCredentials([]byte("home"), []byte("passphrase-1"))
		done <- s.ClearNetworkCredentials()
	}()
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a change callback that reads the server deadlocks")
		}
	}
	if len(seen) == 0 || !bytes.Equal(seen[0], []byte("home")) {
		t.Fatalf("the callback saw %q", seen)
	}
}
