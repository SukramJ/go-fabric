// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/camera"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/diag"
	"github.com/SukramJ/go-fabric/diagevent"
)

// fakeMetrics records the counters and gauges it is given.
type fakeMetrics struct {
	mu       sync.Mutex
	counters map[string]int64
	gauges   map[string]int64
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{counters: map[string]int64{}, gauges: map[string]int64{}}
}

func (m *fakeMetrics) Counter(name string, delta int64) {
	m.mu.Lock()
	m.counters[name] += delta
	m.mu.Unlock()
}

func (m *fakeMetrics) Gauge(name string, value int64) {
	m.mu.Lock()
	m.gauges[name] = value
	m.mu.Unlock()
}

func (m *fakeMetrics) counter(name string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[name]
}

// traced is a controller with only its trace and metrics, for the record
// sites a unit reaches.
func traced() (*Controller, *fakeMetrics) {
	m := newFakeMetrics()
	return &Controller{diagRing: diagevent.NewRing(diag.DefaultDiagnosticsCapacity), metrics: m}, m
}

// only returns the one event of the trace, failing on another count.
func only(t *testing.T, c *Controller) diag.Event {
	t.Helper()
	events := c.DiagnosticEvents()
	if len(events) != 1 {
		t.Fatalf("events %+v, want one", events)
	}
	return events[0]
}

// TestTracePairing: the first started step records the start once, a
// failed step an error naming it with its error and one pair.failures;
// the host's hook runs after each report, with the trace already holding
// it.
func TestTracePairing(t *testing.T) {
	t.Parallel()
	c, m := traced()
	var host []PairingStep
	progress := c.tracePairing(PairingCode{Manual: true, Discriminator: 15}, func(st PairingStep) {
		if st.Phase == PairingFailed && c.DiagnosticEvents()[0].Severity != diagevent.SeverityError {
			t.Error("the host ran before the failure was recorded")
		}
		host = append(host, st)
	})
	progress(PairingStep{Name: "Discovery", Phase: PairingStarted})
	progress(PairingStep{Name: "Discovery", Phase: PairingSucceeded})
	progress(PairingStep{Name: "PASE", Phase: PairingStarted})
	progress(PairingStep{Name: "PASE", Phase: PairingFailed, Err: errors.New("boom")})
	events := c.DiagnosticEvents()
	if len(events) != 2 || len(host) != 4 {
		t.Fatalf("events %+v, host %d reports", events, len(host))
	}
	if e := events[1]; e.Kind != diagevent.KindPairing || e.Message != "Pairing started." || e.Detail["code_kind"] != "manual" || e.Detail["discriminator"] != "15" {
		t.Fatalf("start %+v", e)
	}
	if e := events[0]; e.Severity != diagevent.SeverityError || e.Message != "Pairing failed at step PASE: boom." || e.Detail["step"] != "PASE" || e.Detail["err"] != "boom" {
		t.Fatalf("failure %+v", e)
	}
	if got := m.counter(MetricPairFailures); got != 1 {
		t.Fatalf("pair.failures %d", got)
	}
	qr := c.tracePairing(PairingCode{}, nil)
	qr(PairingStep{Name: "PASE", Phase: PairingStarted})
	if e := c.DiagnosticEvents()[0]; e.Detail["code_kind"] != "qr" {
		t.Fatalf("a QR code's start %+v", e)
	}
}

// TestDiagHooksChain: the trace's hooks record a connection-state
// transition — a lost or waiting connection as a warning — before the
// host's hook runs, and pass Commissioned and Decommissioned on.
func TestDiagHooksChain(t *testing.T) {
	t.Parallel()
	c, _ := traced()
	var states []client.ConnectionState
	var commissioned, decommissioned int
	h := c.diagHooks(func(id string) client.Hooks {
		return client.Hooks{
			ConnectionStateChanged: func(s client.ConnectionState) {
				if c.DiagnosticEvents()[0].Detail["state"] != s.String() {
					t.Errorf("the host saw %v before the trace", s)
				}
				states = append(states, s)
			},
			Commissioned:   func() { commissioned++ },
			Decommissioned: func() { decommissioned++ },
		}
	})("peer1")
	for _, tc := range []struct {
		state    client.ConnectionState
		severity diagevent.Severity
		message  string
	}{
		{client.Reconnecting, diagevent.SeverityWarning, `Node "peer1" lost its connection; the controller reconnects.`},
		{client.Connected, diagevent.SeverityInfo, `Node "peer1" is connected.`},
		{client.WaitingForDeviceDiscovery, diagevent.SeverityWarning, `Node "peer1" looks offline; the controller waits for its operational record in DNS-SD.`},
		{client.Disconnected, diagevent.SeverityInfo, `Node "peer1" is disconnected.`},
		{client.ConnectionState(9), diagevent.SeverityInfo, `Node "peer1" is ConnectionState(9).`},
	} {
		h.ConnectionStateChanged(tc.state)
		e := c.DiagnosticEvents()[0]
		if e.Kind != diagevent.KindSession || e.Severity != tc.severity || e.Message != tc.message || e.Detail["node"] != "peer1" {
			t.Fatalf("%v: event %+v", tc.state, e)
		}
	}
	h.Commissioned()
	h.Decommissioned()
	if len(states) != 5 || commissioned != 1 || decommissioned != 1 {
		t.Fatalf("host states %v, commissioned %d, decommissioned %d", states, commissioned, decommissioned)
	}
	bare := c.diagHooks(func(string) client.Hooks { return client.Hooks{} })("peer2")
	bare.ConnectionStateChanged(client.Connected)
	bare.Commissioned()
	bare.Decommissioned()
}

// TestTraceOTAStatus: a status enters the trace — Cancelled as a warning
// — and the host's hook runs after.
func TestTraceOTAStatus(t *testing.T) {
	t.Parallel()
	c, _ := traced()
	var host int
	on := c.traceOTAStatus(func(softwareupdate.Peer, softwareupdate.Status, uint32) { host++ })
	p := softwareupdate.Peer{FabricIndex: 1, NodeID: 0x55}
	on(p, mattercore.OTAUpdateDownloading, 2)
	if e := only(t, c); e.Kind != diagevent.KindOTA || e.Severity != diagevent.SeverityInfo ||
		e.Message != "Node 0000000000000055's software update is Downloading (version 2)." || e.Detail["status"] != "Downloading" || e.Detail["version"] != "2" {
		t.Fatalf("event %+v", e)
	}
	on(p, mattercore.OTAUpdateCancelled, 2)
	if e := c.DiagnosticEvents()[0]; e.Severity != diagevent.SeverityWarning {
		t.Fatalf("a cancelled update %+v", e)
	}
	c.traceOTAStatus(nil)(p, mattercore.OTAUpdateDone, 3)
	if host != 2 || len(c.DiagnosticEvents()) != 3 {
		t.Fatalf("host %d, events %d", host, len(c.DiagnosticEvents()))
	}
}

// TestCameraPortRefusalTrace: a refused Offer or Answer enters the trace
// as a warning, a refused ICECandidates or End as info — the log levels.
func TestCameraPortRefusalTrace(t *testing.T) {
	t.Parallel()
	ring := diagevent.NewRing(8)
	p := cameraPort{s: &served{listeners: map[uint64]func(CameraSignal){}}, ring: ring}
	p.Refused(context.Background(), wrdef.SignalOffer, 7, 1, 0x55)
	p.Refused(context.Background(), camera.Signal("end"), 8, 1, 0x55)
	events := ring.Snapshot()
	if len(events) != 2 {
		t.Fatalf("events %+v", events)
	}
	if e := events[1]; e.Kind != diagevent.KindCamera || e.Severity != diagevent.SeverityWarning ||
		e.Message != "Node 0000000000000055 refused a WebRTC offer for session 7." || e.Detail["webrtc_session_id"] != "7" || e.Detail["kind"] != "offer" {
		t.Fatalf("offer %+v", e)
	}
	if e := events[0]; e.Severity != diagevent.SeverityInfo {
		t.Fatalf("end %+v", e)
	}
}

// TestControllerMetricsHooks: a retransmission counts mrp.retransmits; a
// controller without a port or a peer set records nothing.
func TestControllerMetricsHooks(t *testing.T) {
	t.Parallel()
	c, m := traced()
	c.retransmitted()
	c.gaugeSessions()
	if m.counter(MetricMRPRetransmits) != 1 {
		t.Fatalf("mrp.retransmits %d", m.counter(MetricMRPRetransmits))
	}
	none := &Controller{clock: clock.Or(nil)}
	none.retransmitted()
	none.gaugeSessions()
	if got := none.OnDiagnosticEvent(nil); got == nil {
		t.Fatal("OnDiagnosticEvent(nil) returned no unsubscribe")
	}
	if d := none.SessionDiagnostics(); d.Peers != nil || d.Sessions != nil {
		t.Fatalf("a controller without peers: %+v", d)
	}
}
