// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/diag"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/internal/udpseam"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// hasEvent reports whether events hold one of kind and severity whose
// message contains part.
func hasEvent(events []diag.Event, kind diagevent.Kind, severity diagevent.Severity, part string) bool {
	return slices.ContainsFunc(events, func(e diag.Event) bool {
		return e.Kind == kind && e.Severity == severity && strings.Contains(e.Message, part)
	})
}

// connectedEvents counts the session events of a transition to Connected.
func connectedEvents(events []diag.Event) int {
	n := 0
	for _, e := range events {
		if e.Kind == diagevent.KindSession && e.Detail["state"] == "Connected" {
			n++
		}
	}
	return n
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
	if err := site.Eventually(ctx, "the node is connected, in the trace", func() bool {
		return n.ConnectionState() == client.Connected && connectedEvents(c.DiagnosticEvents()) > 0
	}); err != nil {
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

	before := connectedEvents(c.DiagnosticEvents())
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
	if err := site.Eventually(ctx, "the node is connected again, in the trace", func() bool {
		return n.ConnectionState() == client.Connected && connectedEvents(c.DiagnosticEvents()) > before
	}); err != nil {
		t.Fatalf("%v: %+v", err, c.DiagnosticEvents())
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

// TestControllerPairingRaceStepFailure: in a race over DNS-SD and a BLE
// central that cannot scan, the BLE transport's failed discovery step is
// a warning in the trace and no pair.failures: the IP transport wins and
// the pairing succeeds (pair_race.go reports the failed transport).
func TestControllerPairingRaceStepFailure(t *testing.T) {
	t.Parallel()
	drv := &wifiDriver{}
	m := &countingMetrics{}
	site, dev, c := siteController(t, fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.Network.WiFi = &node.WiFi{Driver: drv}
	}}, func(cfg *controller.Config) { cfg.Metrics = m })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{
			BLE: unscannable{bletest.NewCentral()}, WiFi: &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"},
			OnAttestationFailure: attestation.Accept(),
		})
		return err
	}); err != nil {
		t.Fatalf("Pair: %v", err)
	}
	events := c.DiagnosticEvents()
	warnings := 0
	for _, e := range events {
		if e.Kind == diagevent.KindPairing && e.Severity == diagevent.SeverityWarning {
			warnings++
			if e.Detail["step"] != "Discovery.BLE" {
				t.Fatalf("failed step %+v, want Discovery.BLE", e)
			}
		}
		if e.Kind == diagevent.KindPairing && e.Severity == diagevent.SeverityError {
			t.Fatalf("an error in a pairing that succeeded: %+v", e)
		}
	}
	if warnings != 1 || !hasEvent(events, diagevent.KindPairing, diagevent.SeverityInfo, " was paired.") {
		t.Fatalf("%d warnings, events %+v", warnings, events)
	}
	if got := m.counter(controller.MetricPairFailures); got != 0 {
		t.Fatalf("pair.failures %d for a pairing that succeeded", got)
	}
}

// unscannable is a BLE central whose scan fails at once.
type unscannable struct{ *bletest.Central }

func (unscannable) StartScanning(context.Context) error { return errors.New("bletest: adapter off") }

// countingMetrics counts the counters it is given.
type countingMetrics struct {
	mu       sync.Mutex
	counters map[string]int64
	gauges   map[string]int64
}

func (m *countingMetrics) Counter(name string, delta int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.counters == nil {
		m.counters = map[string]int64{}
	}
	m.counters[name] += delta
}

func (m *countingMetrics) Gauge(name string, value int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gauges == nil {
		m.gauges = map[string]int64{}
	}
	m.gauges[name] = value
}

func (m *countingMetrics) counter(name string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[name]
}

func (m *countingMetrics) gauge(name string) (int64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.gauges[name]
	return v, ok
}

// TestControllerICDWatchAndGaugeLifecycle: a paired node's ICD Check-Ins
// are watched; decommissioned, the watch goes and the disconnect that
// follows does not bring it back. sessions.open counts the peer's session
// while it runs and reads 0 once the controller closed.
func TestControllerICDWatchAndGaugeLifecycle(t *testing.T) {
	t.Parallel()
	m := &countingMetrics{}
	site, _, c, n := sitePair(t, fabrictest.Topology{}, func(cfg *controller.Config) { cfg.Metrics = m })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if !controller.WatchingICD(c, n) {
		t.Fatal("a commissioned node's ICD Check-Ins are not watched")
	}
	if v, ok := m.gauge(controller.MetricSessionsOpen); !ok || v < 1 {
		t.Fatalf("sessions.open %d (%v) with a paired node", v, ok)
	}
	if err := site.Run(ctx, func(ctx context.Context) error { return c.Registry().Decommission(ctx, n) }); err != nil {
		t.Fatalf("Decommission: %v", err)
	}
	if err := site.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if controller.WatchingICD(c, n) {
		t.Fatal("a decommissioned node is still watched")
	}
	if err := site.Run(ctx, c.Close); err != nil {
		t.Fatal(err)
	}
	if v, _ := m.gauge(controller.MetricSessionsOpen); v != 0 {
		t.Fatalf("sessions.open %d after Close", v)
	}
	if controller.WatchingICD(c, n) || c.DiagnoseMDNS() != nil {
		t.Fatal("a closed controller watches or diagnoses")
	}
}

// TestControllerICDWatchEndsWithTheNodesDeletion (review II, R2-P1-ctl-05):
// a commissioned node deleted through the registry without a
// decommission — Forget, or Erase — is no longer watched. The watch is
// the controller's only per-node state of the trace: two observers on the
// node's ICD client, no timer and no goroutine; it goes with the node.
// Before, only the Decommissioned hook ended it, which neither path fires,
// so the controller kept the node and its observers until Close.
func TestControllerICDWatchEndsWithTheNodesDeletion(t *testing.T) {
	t.Parallel()
	for name, remove := range map[string]func(context.Context, *controller.Controller, *client.Node) error{
		"Forget": func(ctx context.Context, c *controller.Controller, n *client.Node) error {
			return c.Registry().Forget(ctx, n)
		},
		"Erase": func(ctx context.Context, c *controller.Controller, _ *client.Node) error {
			c.Registry().Erase(ctx)
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			site, _, c, n := sitePair(t, fabrictest.Topology{}, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			if !controller.WatchingICD(c, n) {
				t.Fatal("a commissioned node's ICD Check-Ins are not watched")
			}
			if err := site.Run(ctx, func(ctx context.Context) error { return remove(ctx, c, n) }); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if err := site.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if len(c.Registry().Nodes()) != 0 {
				t.Fatalf("the registry still holds %d nodes", len(c.Registry().Nodes()))
			}
			if controller.WatchingICD(c, n) {
				t.Fatal("a deleted node is still watched")
			}
			if err := site.Run(ctx, c.Close); err != nil {
				t.Fatal(err)
			}
		})
	}
}
