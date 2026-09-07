// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"log/slog"
	"net"
	"sync"
	"testing"

	"golang.org/x/net/ipv4"
)

// TestSubtypeResponderAnnounceDuringCloseIsSynchronised drives the
// interleaving [Zeroconf.announceSubtypes] produces at shutdown: the
// deferred second Announce (time.AfterFunc, untracked by the WaitGroup)
// fires while the advertiser is closing the responder. Announce reads
// pc4 / pc6 to fan the PTRs out; Close nils them. Without a common lock
// that is an unsynchronised read/write of the conn pointers, and in the
// worst interleaving a WriteTo on a handle Close already pulled.
//
// The responder is socket-backed (a loopback UDP conn behind the ipv4
// wrapper) so the fan-out really dereferences pc4, but it never joins a
// multicast group and never starts the receive loops, so the upstream
// grandcat/zeroconf race that keeps this package out of -race runs is
// not in play. Run with
//
//	GO_FABRIC_MDNS_RACE=1 go test -race -run AnnounceDuringClose ./mdns/
//
// to have the detector arbitrate; without -race the test still exercises
// the path and fails on a panic.
func TestSubtypeResponderAnnounceDuringCloseIsSynchronised(t *testing.T) {
	t.Parallel()

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skipf("loopback UDP socket unavailable: %v", err)
	}
	r := &SubtypeResponder{
		logger:   slog.New(slog.DiscardHandler),
		mappings: make(map[string]string),
		pc4:      ipv4.NewPacketConn(conn),
	}
	r.AddSubtype("_L3840._sub._matterc._udp.local", "inst._matterc._udp.local")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.Announce()
	}()
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wg.Wait()

	// After Close the handles are gone; a late Announce must be a no-op
	// rather than a write on a released conn.
	r.Announce()
}
