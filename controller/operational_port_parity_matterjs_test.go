// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/mdns"
)

// offlineConn is a DNS-SD connection that sends nowhere and hears
// nothing: the port cases need no discovery, and the default opener
// (discovery.Open) would join the host's multicast group, where a Thread
// border router announcing itself on the LAN reaches the controller's
// border-router registry — a local run then covers the diagnostics
// watch's border-router arm (diagnostics.go watchDiscovery) and CI never
// does, which moved the package's coverage by half a point.
type offlineConn struct{ out chan mdns.QueryResponse }

func (c offlineConn) Send([]byte) error                    { return nil }
func (c offlineConn) Responses() <-chan mdns.QueryResponse { return c.out }
func (c offlineConn) Close() error                         { return nil }

func offlineNames(log *slog.Logger) (*discovery.Names, error) {
	return discovery.NewNames(offlineConn{out: make(chan mdns.QueryResponse)}, log), nil
}

// TestOperationalPortParityMatterJS ports the controller-node cases of
// matter.js packages/node/test/node/ServerNodeTest.ts:339-376 at 862ad75f:
// a controller without an explicit port takes an ephemeral one, not the
// standard 5540, and an explicit port is honoured. The Go controller role
// is controller.Controller ([Config].Listen, "" is "[::]:0").
func TestOperationalPortParityMatterJS(t *testing.T) {
	t.Parallel()
	open := func(t *testing.T, listen string) int {
		t.Helper()
		db, err := OpenDB(filepath.Join(t.TempDir(), "c.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		c, err := New(t.Context(), Config{DB: db, Attestation: attestation.TestStore(), Listen: listen, ManualStart: true, Names: offlineNames})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close(t.Context()) })
		return c.udp.LocalAddr().Port
	}
	t.Run("uses an ephemeral operational port for a controller node without an explicit port", func(t *testing.T) {
		t.Parallel()
		if port := open(t, ""); port <= 0 || port == 5540 {
			t.Fatalf("port %d, want an ephemeral one", port)
		}
	})
	t.Run("honors an explicit port on a controller node", func(t *testing.T) {
		t.Parallel()
		// A port known to be free: one the OS handed out and took back.
		l, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback})
		if err != nil {
			t.Skip("no IPv6 loopback")
		}
		want := l.LocalAddr().(*net.UDPAddr).Port
		_ = l.Close()
		if port := open(t, "[::1]:"+strconv.Itoa(want)); port != want {
			t.Fatalf("port %d, want %d", port, want)
		}
	})
}
