// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/diag"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/internal/udpseam"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// hasEvent reports whether events hold one of kind and severity whose
// message contains part.
func hasEvent(events []diag.Event, kind diagevent.Kind, severity diagevent.Severity, part string) bool {
	return slices.ContainsFunc(events, func(e diag.Event) bool {
		return e.Kind == kind && e.Severity == severity && strings.Contains(e.Message, part)
	})
}

// TestControllerDiagnosticsOnASite: a paired node is one registered peer
// in the snapshot, referencing its CASE session; the trace holds the
// pairing, the session and the connection, newest first. With the device
// stopped the session is lost and the node reconnects — a session
// warning and the transition in the trace — and with it restarted the
// node is connected again (issue 483).
func TestControllerDiagnosticsOnASite(t *testing.T) {
	t.Parallel()
	site, dev, c, n := sitePair(t, fabrictest.Topology{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := site.Eventually(ctx, "the node is connected", func() bool { return n.ConnectionState() == client.Connected }); err != nil {
		t.Fatal(err)
	}
	addr, _ := n.PeerAddress()
	d := c.SessionDiagnostics()
	if len(d.Peers) != 1 || d.Occupancy.Capacity != 0 || d.AsOf.IsZero() {
		t.Fatalf("snapshot %+v, want one peer and untracked occupancy", d)
	}
	p := d.Peers[0]
	if !p.Registered || p.NodeID != addr.NodeID || p.FabricIndex != addr.FabricIndex || p.State != "Connected" || !p.SubscriptionActive || len(p.SessionIDs) == 0 || p.OperationalAddress == "" {
		t.Fatalf("peer %+v", p)
	}
	for _, id := range p.SessionIDs {
		i := slices.IndexFunc(d.Sessions, func(s diag.SessionInfo) bool { return s.ID == id })
		if i < 0 {
			t.Fatalf("session %d of the peer is not in Sessions %+v", id, d.Sessions)
		}
		if s := d.Sessions[i]; s.PeerNodeID != addr.NodeID || s.PASE || s.Transport != "udp" || s.CreatedAt.IsZero() || s.PeerLost {
			t.Fatalf("session %+v", s)
		}
	}
	events := c.DiagnosticEvents()
	for _, want := range []struct {
		kind diagevent.Kind
		part string
	}{
		{diagevent.KindPairing, "Pairing started."},
		{diagevent.KindPairing, " was paired."},
		{diagevent.KindSession, " was established."},
		{diagevent.KindSession, " is connected."},
	} {
		if !hasEvent(events, want.kind, diagevent.SeverityInfo, want.part) {
			t.Fatalf("no %s event %q in %+v", want.kind, want.part, events)
		}
	}
	if !slices.IsSortedFunc(events, func(a, b diag.Event) int { return b.At.Compare(a.At) }) {
		t.Fatal("the trace is not newest first")
	}

	if err := dev.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := site.Eventually(ctx, "the lost session and the reconnect in the trace", func() bool {
		events := c.DiagnosticEvents()
		return hasEvent(events, diagevent.KindSession, diagevent.SeverityWarning, " was lost; the controller reconnects.") &&
			hasEvent(events, diagevent.KindSession, diagevent.SeverityWarning, " lost its connection; the controller reconnects.")
	}); err != nil {
		t.Fatalf("%v: %+v", err, c.DiagnosticEvents())
	}
	if err := dev.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := site.Eventually(ctx, "the node is connected again", func() bool { return n.ConnectionState() == client.Connected }); err != nil {
		t.Fatal(err)
	}
	events = c.DiagnosticEvents()
	if events[0].Kind != diagevent.KindSession || !hasEvent(events[:3], diagevent.KindSession, diagevent.SeverityInfo, " is connected.") {
		t.Fatalf("the newest events %+v, want the reconnection", events[:3])
	}
	if f := c.DiagnoseMDNS(); slices.ContainsFunc(f, func(f mdns.Finding) bool { return f.Code == mdns.CodeNoOperationalNames }) {
		t.Fatalf("a connected node: findings %+v", f)
	}
}

// TestControllerPairingFailedInTrace: a pairing the default attestation
// policy rejects records its start and the failed step as an error, and
// an OnDiagnosticEvent function sees each event after the trace stored it.
func TestControllerPairingFailedInTrace(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, func(cfg *controller.Config) { cfg.Diagnostics = diag.Diagnostics{Capacity: 64} })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	seen := make(chan bool, 64)
	stop := c.OnDiagnosticEvent(func(e diag.Event) {
		got := c.DiagnosticEvents()
		seen <- len(got) > 0 && got[0].Message == e.Message
	})
	defer stop()
	_ = site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{OnAttestationFailure: attestation.Reject()})
		if err == nil {
			t.Error("Pair accepted a test attestation under Reject")
		}
		return nil
	})
	events := c.DiagnosticEvents()
	if !hasEvent(events, diagevent.KindPairing, diagevent.SeverityError, "Pairing failed at step OperationalCredentials.DeviceAttestation: ") {
		t.Fatalf("no failed attestation step in %+v", events)
	}
	if events[len(events)-1].Message != "Pairing started." {
		t.Fatalf("the oldest event %+v, want the start", events[len(events)-1])
	}
	for observed := 0; ; observed++ {
		select {
		case ok := <-seen:
			if !ok {
				t.Fatal("an observer ran before the trace held its event")
			}
			continue
		default:
		}
		if observed < 2 {
			t.Fatalf("the observer saw %d events", observed)
		}
		break
	}
}

// voidConn is a socket whose datagrams reach nobody: every write succeeds
// and nothing is ever read.
type voidConn struct {
	closed chan struct{}
	once   sync.Once
}

func newVoidConn() *voidConn { return &voidConn{closed: make(chan struct{})} }

func (v *voidConn) ReadFromUDP([]byte) (int, *net.UDPAddr, error) {
	<-v.closed
	return 0, nil, net.ErrClosed
}

func (v *voidConn) WriteToUDP(b []byte, _ *net.UDPAddr) (int, error) { return len(b), nil }

func (v *voidConn) LocalAddr() net.Addr { return &net.UDPAddr{IP: net.IPv6loopback, Port: 5540} }

func (v *voidConn) Close() error {
	v.once.Do(func() { close(v.closed) })
	return nil
}

// TestControllerDiagnoseMDNSNoOperationalNames: the database of a
// controller that paired the site's device, opened again by a controller
// whose DNS-SD browse gets no answer (a silent cache) and whose datagrams
// reach nobody: its node's CASE goes unanswered and it waits for device
// discovery, and DiagnoseMDNS reports
// no_operational_names — the cache holds none of the commissioned nodes'
// operational records. A controller that advertises nothing reports no
// other finding.
func TestControllerDiagnoseMDNSNoOperationalNames(t *testing.T) {
	t.Parallel()
	var cfg controller.Config
	site, _, first, _ := sitePair(t, fabrictest.Topology{}, func(c *controller.Config) { cfg = *c })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if f := first.DiagnoseMDNS(); len(f) != 0 {
		t.Fatalf("a paired node: findings %+v", f)
	}
	if err := site.Run(ctx, first.Close); err != nil {
		t.Fatal(err)
	}
	again := config(cfg.DB)
	clk := clocktest.New(time.Unix(2_000_000_000, 0))
	again.Clock = clk
	again.Listener = udpseam.NewListener(newVoidConn()).(*udp.Listener)
	c, err := controller.New(ctx, again)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	if len(c.Nodes()) != 1 {
		t.Fatalf("nodes %d, want the paired one", len(c.Nodes()))
	}
	n := c.Nodes()[0]
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for n.ConnectionState() != client.WaitingForDeviceDiscovery {
		select {
		case <-ctx.Done():
			t.Fatalf("the node is %v, want WaitingForDeviceDiscovery", n.ConnectionState())
		case <-tick.C:
			clk.Advance(time.Second)
		}
	}
	f := c.DiagnoseMDNS()
	if len(f) != 1 || f[0].Code != mdns.CodeNoOperationalNames || f[0].Severity != mdns.SeverityWarning || f[0].Service != "" ||
		!strings.HasPrefix(f[0].Message, "None of the 1 commissioned nodes") {
		t.Fatalf("findings %+v, want no_operational_names", f)
	}
	d := c.SessionDiagnostics()
	if len(d.Peers) != 1 || !d.Peers[0].Registered || d.Peers[0].State != "WaitingForDeviceDiscovery" || len(d.Peers[0].DiscoveredNames) != 0 || len(d.Sessions) != 0 {
		t.Fatalf("snapshot %+v", d)
	}
}
