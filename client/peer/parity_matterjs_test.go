// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

type (
	discoveryTXT  = discovery.TXTData
	sessionParams = sessionparams.Parameters
)

// TestParityTimingDefaults: every default of matter.js
// packages/protocol/src/peer/PeerTimingParameters.ts:184-209 at 0ceb202d.
func TestParityTimingDefaults(t *testing.T) {
	t.Parallel()
	d := DefaultTiming()
	rows := []struct {
		name string
		got  time.Duration
		want time.Duration
		line string
	}{
		{"defaultConnectionTimeout", d.DefaultConnectionTimeout, 90 * time.Second, ":188"},
		{"maxDelayBetweenInitialContactRetries", d.MaxDelayBetweenInitialContactRetries, 2 * time.Minute, ":184, :189"},
		{"delayBeforeNextAddress", d.DelayBeforeNextAddress, 45 * time.Second, ":193"},
		{"delayAfterNetworkError", d.DelayAfterNetworkError, 15 * time.Second, ":194"},
		{"delayAfterPeerError", d.DelayAfterPeerError, 5 * time.Minute, ":195"},
		{"delayAfterUnhandledError", d.DelayAfterUnhandledError, 2 * time.Minute, ":196"},
		{"kickThrottleInterval", d.KickThrottleInterval, 3 * time.Second, ":197"},
		{"kickMinRestartSaving", d.KickMinRestartSaving, time.Minute, ":199 (half of :184)"},
		{"kickRestartCooldown.addressChange", d.KickRestartCooldown.AddressChange, 30 * time.Minute, ":201"},
		{"kickRestartCooldown.connect", d.KickRestartCooldown.Connect, 10 * time.Minute, ":202"},
		{"addressChangeStabilizationDelay", d.AddressChangeStabilizationDelay, 10 * time.Second, ":204"},
		{"addressChangeProbeCooldown.minimum", d.AddressChangeProbeCooldown.Min, 10 * time.Minute, ":206"},
		{"addressChangeProbeCooldown.maximum", d.AddressChangeProbeCooldown.Max, 24 * time.Hour, ":207"},
	}
	for _, r := range rows {
		if r.got != r.want {
			t.Errorf("%s (PeerTimingParameters.ts%s) = %v, want %v", r.name, r.line, r.got, r.want)
		}
	}
	if d.KickMinRetransmissions != 2 {
		t.Errorf("kickMinRetransmissions (:198) = %d, want 2", d.KickMinRetransmissions)
	}
}

// TestParityTimingMerge: overrides apply field-wise, a group member leaves
// its sibling alone, an unset field keeps the base (PeerTimingParameters.ts:153-168).
func TestParityTimingMerge(t *testing.T) {
	t.Parallel()
	base := DefaultTiming()
	got := Merge(base, Timing{
		DelayBeforeNextAddress:     time.Second,
		KickMinRetransmissions:     4,
		KickRestartCooldown:        KickRestartCooldown{Connect: time.Minute},
		AddressChangeProbeCooldown: ProbeCooldown{Max: time.Hour},
	})
	want := base
	want.DelayBeforeNextAddress = time.Second
	want.KickMinRetransmissions = 4
	want.KickRestartCooldown.Connect = time.Minute
	want.AddressChangeProbeCooldown.Max = time.Hour
	if got != want {
		t.Fatalf("Merge = %+v, want %+v", got, want)
	}
	if Merge(base, Timing{}) != base {
		t.Fatal("an empty override changed the base")
	}
	all := Timing{
		DefaultConnectionTimeout: 1, MaxDelayBetweenInitialContactRetries: 2, DelayBeforeNextAddress: 3,
		DelayAfterNetworkError: 4, DelayAfterPeerError: 5, DelayAfterUnhandledError: 6, KickThrottleInterval: 7,
		KickMinRetransmissions: 8, KickMinRestartSaving: 9, KickRestartCooldown: KickRestartCooldown{10, 11},
		AddressChangeStabilizationDelay: 12, AddressChangeProbeCooldown: ProbeCooldown{13, 14},
	}
	if Merge(base, all) != all {
		t.Fatal("a full override did not replace every field")
	}
}

// TestParityErrorDelays: the error-to-delay table of matter.js
// PeerConnection.ts:674-696 at 0ceb202d, with busyDelay as
// SecureChannelMessenger.ts:44-50 reads it (general Busy, protocol Busy,
// two bytes little-endian).
func TestParityErrorDelays(t *testing.T) {
	t.Parallel()
	timing := DefaultTiming()
	status := func(general, code uint16, data []byte) error {
		return &exchange.StatusReportError{Report: exchange.StatusReport{GeneralCode: general, ProtocolCode: code, ProtocolData: data}}
	}
	rows := []struct {
		name      string
		err       error
		hasRecord bool
		delay     time.Duration
		hasDelay  bool
		category  string
		line      string
	}{
		{"busy 1000 ms, jitter 0.5", status(8, 4, []byte{0xE8, 0x03}), false, time.Second + 7500*time.Millisecond, true, "busy", ":678-680"},
		{"busy without protocol Busy", status(8, 2, []byte{0xE8, 0x03}), false, 5 * time.Minute, true, "peer", ":686-689"},
		{"busy with three bytes", status(8, 4, []byte{1, 2, 3}), false, 5 * time.Minute, true, "peer", ":686-689"},
		{"NoSharedTrustRoots, record deleted", status(1, 1, nil), true, 0, false, "resumption", ":681-685"},
		{"NoSharedTrustRoots, no record", status(1, 1, nil), false, 5 * time.Minute, true, "peer", ":686-689"},
		{"InvalidParam", status(1, 2, nil), false, 5 * time.Minute, true, "peer", ":686-689"},
		{"transient", &exchange.PeerMessageMissingError{}, false, 15 * time.Second, true, "network", ":690-692"},
		{"unhandled", errors.New("x"), false, 2 * time.Minute, true, "general", ":693-696"},
		{"network", exchange.ErrNetwork, false, 2 * time.Minute, true, "general", ":693-696, :700-702"},
	}
	for _, r := range rows {
		deleted := false
		delay, hasDelay, category := classifyError(r.err, timing, func() float64 { return 0.5 }, func() bool { deleted = true; return r.hasRecord })
		if delay != r.delay || hasDelay != r.hasDelay || category != r.category {
			t.Errorf("%s (PeerConnection.ts%s): %v %v %s, want %v %v %s", r.name, r.line, delay, hasDelay, category, r.delay, r.hasDelay, r.category)
		}
		if r.category == "resumption" && !deleted {
			t.Errorf("%s: the record was not deleted", r.name)
		}
	}
}

// TestParityUnresponsiveDetector: the latch fires once past
// MRP.MAX_TRANSMISSIONS (5) and re-arms at 0 (PeerConnection.ts:815-826).
func TestParityUnresponsiveDetector(t *testing.T) {
	t.Parallel()
	fired := 0
	latch := unresponsiveDetector(func() { fired++ })
	for _, n := range []int{0, 1, 5, 6, 7, 8} {
		latch(n)
	}
	if fired != 1 {
		t.Fatalf("fired %d times over one cycle, want 1", fired)
	}
	latch(0)
	latch(6)
	if fired != 2 {
		t.Fatal("the latch did not re-arm at 0")
	}
}

// TestParityProbeCooldownFibonacci: the address-change probe cooldown
// grows 10, 10, 20, 30, 50 … minutes, capped at 24 h, and resets to the
// minimum (PeerAddressMonitor.ts:269-292 with the defaults of
// PeerTimingParameters.ts:205-208).
func TestParityProbeCooldownFibonacci(t *testing.T) {
	t.Parallel()
	d := DefaultTiming()
	m := &monitor{min: d.AddressChangeProbeCooldown.Min, max: d.AddressChangeProbeCooldown.Max}
	// The cooldown before the first probe is the minimum; each answered
	// probe advances it: 10 (initial), 10, 20, 30, 50, … minutes.
	if m.cooldownLocked() != 10*time.Minute {
		t.Fatal("the initial cooldown is not the minimum")
	}
	want := []time.Duration{10, 20, 30, 50, 80, 130, 210, 340, 550, 890, 1440, 1440}
	for i, w := range want {
		if got := m.advanceBackoff(); got != w*time.Minute {
			t.Fatalf("advance %d = %v, want %v", i+1, got, w*time.Minute)
		}
	}
	m.resetBackoff()
	if m.cooldownLocked() != 10*time.Minute || m.advanceBackoff() != 10*time.Minute {
		t.Fatal("reset does not return to the minimum")
	}
}

// TestParityKnownPeerMerge: the merge rules of PeerSet.addKnownPeer
// (PeerSet.ts:315-337) — one row per field.
func TestParityKnownPeerMerge(t *testing.T) {
	t.Parallel()
	old := 800 * time.Millisecond
	nw := 1000 * time.Millisecond
	sai := 400 * time.Millisecond
	rows := []struct {
		name  string
		known Descriptor
		given Descriptor
		check func(Descriptor) bool
		line  string
	}{
		{
			"operational address replaces",
			Descriptor{OperationalAddress: udpAddr(addr1)},
			Descriptor{OperationalAddress: udpAddr(addr2)},
			func(d Descriptor) bool { return d.OperationalAddress.String() == addr2 }, ":317-319",
		},
		{
			"absent operational address keeps",
			Descriptor{OperationalAddress: udpAddr(addr1)},
			Descriptor{},
			func(d Descriptor) bool { return d.OperationalAddress.String() == addr1 }, ":317",
		},
		{
			"CATs replace",
			Descriptor{CaseAuthenticatedTags: []uint32{1}},
			Descriptor{CaseAuthenticatedTags: []uint32{2}},
			func(d Descriptor) bool { return d.CaseAuthenticatedTags[0] == 2 }, ":321-323",
		},
		{
			"discovery data: the peer's wins",
			Descriptor{DiscoveryData: &discoveryTXT{SII: &old}},
			Descriptor{DiscoveryData: &discoveryTXT{SII: &nw, SAI: &sai}},
			func(d Descriptor) bool { return *d.DiscoveryData.SII == old && *d.DiscoveryData.SAI == sai }, ":325-327",
		},
		{
			"reported parameters only when unknown",
			Descriptor{ReportedSessionParameters: &sessionParams{SessionIdleInterval: 1}},
			Descriptor{ReportedSessionParameters: &sessionParams{SessionIdleInterval: 2}},
			func(d Descriptor) bool { return d.ReportedSessionParameters.SessionIdleInterval == 1 }, ":329-331",
		},
		{
			"reported parameters fill an unknown",
			Descriptor{},
			Descriptor{ReportedSessionParameters: &sessionParams{SessionIdleInterval: 2}},
			func(d Descriptor) bool { return d.ReportedSessionParameters.SessionIdleInterval == 2 }, ":329-331",
		},
	}
	for _, r := range rows {
		p := &Peer{desc: r.known, closed: true}
		p.mergeKnown(r.given)
		if !r.check(p.desc) {
			t.Errorf("%s (PeerSet.ts%s): %+v", r.name, r.line, p.desc)
		}
	}
}
