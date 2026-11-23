// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"bytes"
	"testing"
	"time"
)

// TestPAALookupWaitIsBounded holds R2-C2-conc-1904: PAA — the attestation
// path, which takes no context — waits for a running update only up to
// the store's lookup bound when a stored certificate needs its self-heal,
// and then answers with the stored bytes.
func TestPAALookupWaitIsBounded(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	pki := newTestPKI(t)
	f.addRoot(pki.paa)
	s := openStore(t, CertificateOptions{Client: f.client(), Clock: newManualClock()})
	s.lookupTimeout = 50 * time.Millisecond
	id := pki.paa.skidHex()
	damaged := []byte{0x30, 0x03, 0x02, 0x01, 0x01}
	s.mu.Lock()
	e, ok := s.index[id]
	if !ok {
		s.mu.Unlock()
		t.Fatal("the root was not stored")
	}
	e.DER = damaged
	s.index[id] = e
	s.mu.Unlock()

	s.fetching <- struct{}{} // an update in flight
	released := false
	release := func() {
		if !released {
			released = true
			s.unlockFetch()
		}
	}
	defer release()
	type answer struct {
		der []byte
		ok  bool
	}
	done := make(chan answer, 1)
	go func() {
		p, ok := s.PAA(pki.paa.skid())
		done <- answer{p.DER, ok}
	}()
	// An unbounded wait hangs here to the package timeout: the update in
	// flight is released only when the test returns.
	a := <-done
	if !a.ok || !bytes.Equal(a.der, damaged) {
		t.Fatalf("PAA = %x, %v; want the stored bytes", a.der, a.ok)
	}
}
