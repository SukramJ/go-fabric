// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// TestPeerTransportFlags: the transport preference, the TCP-unsupported
// flag (Peer.ts:346-353) and the TCP validation of a session's reported
// parameters (PeerConnection.ts:596-621).
func TestPeerTransportFlags(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, err := h.set.AddKnownPeer(Descriptor{Address: testAddr})
	if err != nil {
		t.Fatal(err)
	}
	p.SetTransportPreference(exchange.TransportTCP)
	if p.TransportPreference() != exchange.TransportTCP {
		t.Fatal("no TCP preference")
	}
	p.SetTransportPreference(exchange.TransportUDP)
	if p.TransportPreference() != exchange.TransportUDP {
		t.Fatal("the TCP preference stayed")
	}
	p.MarkTCPUnsupported()
	p.MarkTCPUnsupported()
	if !p.Descriptor().TCPUnsupported {
		t.Fatal("not marked")
	}
	if p.tcpDenied(nil) {
		t.Error("no parameters deny TCP")
	}
	server := sessionparams.Parameters{SupportedTransports: sessionparams.TransportTCPServer, SpecificationVersion: sessionparams.MinTCPSpecificationVersion}
	if p.tcpDenied(&server) {
		t.Error("a TCP server denied")
	}
	absent := sessionparams.Parameters{SpecificationVersion: sessionparams.MinTCPSpecificationVersion}
	if p.tcpDenied(&absent) {
		t.Error("an absent bitmap denied")
	}
	cleared := sessionparams.Parameters{SupportedTransports: sessionparams.TransportTCPClient, SpecificationVersion: sessionparams.MinTCPSpecificationVersion}
	if !p.tcpDenied(&cleared) {
		t.Error("a cleared TCP server bit without a T record allowed")
	}
	if p.Networks() == nil || p.Exchanges() == nil {
		t.Error("no networks or exchanges")
	}
	p.SetMedium(func() Medium { return Medium{} })
	p.SetMedium(nil)
	tcp := exchange.TransportTCP
	if p.SessionFor(&tcp, nil) != nil {
		t.Error("a TCP session without one")
	}
}

// TestSemaphoreGrantedAndClear: the grant counter and Clear failing the
// waiting requests (Semaphore.ts:224-231).
func TestSemaphoreGrantedAndClear(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	s := NewSemaphore(clk, "test", 1, 0, 0)
	first, err := s.Obtain(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if s.Granted() != 1 {
		t.Fatalf("granted %d", s.Granted())
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.Obtain(t.Context())
		done <- err
	}()
	for s.Count() == 0 {
		time.Sleep(time.Millisecond)
	}
	s.Clear()
	if err := <-done; err == nil {
		t.Fatal("a cleared request was granted")
	}
	first.Close()
}
