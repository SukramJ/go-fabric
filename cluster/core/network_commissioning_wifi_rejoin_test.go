// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

// The Wi-Fi server keeps the node on its committed network: a join of it
// that fails is tried again with a backoff on the node's clock, and a
// fail-safe expiry brings the radio back to it (review II, R2-P3-net-01,
// R2-P3-net-02).

import (
	"bytes"
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// linkDriver is a contract.WiFiDriver that keeps which network the radio
// is on, as wifi/wpasupplicant's driver leaves it: a join that succeeds
// moves the radio, one that fails leaves it where it was. reachable
// decides whether a join of an SSID succeeds; failFirst fails that many
// joins first, whatever the SSID.
type linkDriver struct {
	mu        sync.Mutex
	link      []byte
	joins     [][]byte
	reachable map[string]bool
	failFirst int
	joined    chan []byte
}

func newLinkDriver(reachable ...string) *linkDriver {
	d := &linkDriver{reachable: map[string]bool{}, joined: make(chan []byte, 64)}
	for _, r := range reachable {
		d.reachable[r] = true
	}
	return d
}

func (d *linkDriver) ScanNetworks(context.Context, []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", nil
}

func (d *linkDriver) ConnectNetwork(_ context.Context, ssid, _ []byte) (status uint8, debugText string, errorValue int32) {
	d.mu.Lock()
	d.joins = append(d.joins, bytes.Clone(ssid))
	ok := d.reachable[string(ssid)] && d.failFirst == 0
	if d.failFirst > 0 {
		d.failFirst--
	}
	if ok {
		d.link = bytes.Clone(ssid)
	}
	d.mu.Unlock()
	d.joined <- bytes.Clone(ssid)
	if !ok {
		return uint8(netcommdef.NetworkCommissioningStatusUnknownError), "disconnected, reason 15", 15
	}
	return 0, "", 0
}

func (d *linkDriver) on() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return string(d.link)
}

func (d *linkDriver) joinCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.joins)
}

func (d *linkDriver) setReachable(ssid string, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reachable[ssid] = ok
}

// awaitJoin waits for the driver's next join, bounded by the test's
// deadline.
func awaitJoin(t *testing.T, d *linkDriver) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case ssid := <-d.joined:
		return string(ssid)
	case <-ctx.Done():
		d.mu.Lock()
		joins := slices.Clone(d.joins)
		d.mu.Unlock()
		t.Fatalf("no join asked of the driver; joins so far %q", joins)
		return ""
	}
}

// netcommChanged signals every change the server reports.
func netcommChanged(w *WiFiNetworkCommissioning) <-chan struct{} {
	ch := make(chan struct{}, 64)
	w.OnAttributeChange(func(contract.Change) {
		select {
		case ch <- struct{}{}:
		default:
		}
	})
	return ch
}

// awaitConnected waits until Networks reads ssid connected, on the
// server's change reports, bounded by the test's deadline.
func awaitConnected(t *testing.T, w *WiFiNetworkCommissioning, changed <-chan struct{}, ssid string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		if list := wifiNetworksOf(t, w); len(list) == 1 && string(list[0].NetworkId) == ssid && list[0].Connected {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatalf("Networks %+v, want %q connected", wifiNetworksOf(t, w), ssid)
		}
	}
}

func wifiNetworksOf(t *testing.T, w *WiFiNetworkCommissioning) spec.List[netcommdef.NetworkInfoStruct] {
	t.Helper()
	v, _ := w.MatterRead(netcommAttrNetworks)
	return v.(spec.List[netcommdef.NetworkInfoStruct])
}

// commissionHome commits "home", joined, as a first commissioning does.
func commissionHome(t *testing.T, w *WiFiNetworkCommissioning, d *linkDriver, armed *atomic.Bool) {
	t.Helper()
	armed.Store(true)
	if _, err := w.MatterInvoke(t.Context(), netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("home"), Credentials: []byte("secret12")}); err != nil {
		t.Fatal(err)
	}
	r, err := w.MatterInvoke(t.Context(), netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("home")})
	if err != nil || r.(netcommdef.ConnectNetworkResponse).NetworkingStatus != netcommdef.NetworkCommissioningStatusSuccess {
		t.Fatalf("join of home: %+v %v", r, err)
	}
	awaitJoin(t, d)
	w.OnCommissioningComplete(t.Context())
	armed.Store(false)
}

// moveTo replaces "home" by ssid under a fail-safe and joins it.
func moveTo(t *testing.T, w *WiFiNetworkCommissioning, d *linkDriver, armed *atomic.Bool, ssid string) netcommdef.ConnectNetworkResponse {
	t.Helper()
	armed.Store(true)
	for _, c := range []struct {
		cmd    uint32
		fields any
	}{
		{netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: []byte("home")}},
		{netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte(ssid), Credentials: []byte("secret12")}},
	} {
		if _, err := w.MatterInvoke(t.Context(), c.cmd, c.fields); err != nil {
			t.Fatal(err)
		}
	}
	r, err := w.MatterInvoke(t.Context(), netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte(ssid)})
	if err != nil {
		t.Fatal(err)
	}
	awaitJoin(t, d)
	return r.(netcommdef.ConnectNetworkResponse)
}

// TestFailSafeExpiryRejoinsTheCommittedNetworkAfterAFailedJoin: a join
// under the fail-safe that fails leaves no network connected, and the
// expiry brings the radio back onto the committed network.
func TestFailSafeExpiryRejoinsTheCommittedNetworkAfterAFailedJoin(t *testing.T) {
	t.Parallel()
	var armed atomic.Bool
	d := newLinkDriver("home")
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: d, IsFailSafeArmed: armed.Load})
	if err != nil {
		t.Fatal(err)
	}
	changed := netcommChanged(w)
	commissionHome(t, w, d, &armed)

	if r := moveTo(t, w, d, &armed, "new"); r.NetworkingStatus == netcommdef.NetworkCommissioningStatusSuccess {
		t.Fatalf("the join of new succeeded: %+v", r)
	}
	// The radio is on no network: the driver could not go back to "home"
	// either. After a failed join nothing reads connected.
	d.mu.Lock()
	d.link = nil
	d.mu.Unlock()
	if list := wifiNetworksOf(t, w); len(list) != 1 || list[0].Connected {
		t.Fatalf("Networks after the failed join %+v, want new not connected", list)
	}

	armed.Store(false)
	w.OnFailSafeExpired()
	if list := wifiNetworksOf(t, w); len(list) != 1 || string(list[0].NetworkId) != "home" || (list[0].Connected && d.on() != "home") {
		t.Fatalf("Networks right after the expiry %+v, want home, connected only once the radio is on it (it is on %q)", list, d.on())
	}
	if got := awaitJoin(t, d); got != "home" {
		t.Fatalf("after the fail-safe expired the driver was asked to join %q, want home", got)
	}
	awaitConnected(t, w, changed, "home")
	if d.on() != "home" {
		t.Errorf("the radio is on %q, want home", d.on())
	}
}

// TestFailSafeExpiryLeavesNoUncommittedNetworkJoined: a join under the
// fail-safe that succeeded does not outlive it — the radio goes back to
// the committed network.
func TestFailSafeExpiryLeavesNoUncommittedNetworkJoined(t *testing.T) {
	t.Parallel()
	var armed atomic.Bool
	d := newLinkDriver("home", "new")
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: d, IsFailSafeArmed: armed.Load})
	if err != nil {
		t.Fatal(err)
	}
	changed := netcommChanged(w)
	commissionHome(t, w, d, &armed)
	if r := moveTo(t, w, d, &armed, "new"); r.NetworkingStatus != netcommdef.NetworkCommissioningStatusSuccess || d.on() != "new" {
		t.Fatalf("precondition: join of new %+v, radio on %q", r, d.on())
	}

	armed.Store(false)
	w.OnFailSafeExpired()
	if got := awaitJoin(t, d); got != "home" {
		t.Fatalf("after the fail-safe expired the driver was asked to join %q, want home", got)
	}
	awaitConnected(t, w, changed, "home")
	if d.on() != "home" {
		t.Errorf("after the fail-safe expired the radio is on %q, a network that was never committed; want home", d.on())
	}
}

// TestFailedRejoinAtStartIsRetriedWithABackoff: Start's join of the
// committed network that fails is tried again on the node's clock, the
// wait doubling from chip's reconnect interval, each attempt recorded as
// the last networking status, until one succeeds.
func TestFailedRejoinAtStartIsRetriedWithABackoff(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	st := memstore.New()
	var armed atomic.Bool
	d := newLinkDriver("home")
	seed, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: d, IsFailSafeArmed: armed.Load, Store: st})
	if err != nil {
		t.Fatal(err)
	}
	commissionHome(t, seed, d, &armed)

	// The next start: "home" is out of reach for three attempts.
	d.mu.Lock()
	d.link, d.failFirst = nil, 3
	d.mu.Unlock()
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: d, IsFailSafeArmed: armed.Load, Store: st, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	life, stop := context.WithCancel(t.Context())
	defer stop()
	w.Start(life)
	awaitJoin(t, d)
	unknown := netcommdef.NetworkCommissioningStatusUnknownError
	if got, _, _ := wifiLast(w); got != uint8(unknown) {
		t.Fatalf("LastNetworkingStatus after the failed start %v, want UnknownError", got)
	}
	for _, wait := range []time.Duration{wifiRejoinFirstDelay, 2 * wifiRejoinFirstDelay, 4 * wifiRejoinFirstDelay} {
		if armedTimers := clk.Armed(); !slices.Equal(armedTimers, []time.Duration{wait}) {
			t.Fatalf("armed %v, want the next attempt in %v", armedTimers, wait)
		}
		before := d.joinCount()
		clk.Advance(wait - time.Millisecond)
		if d.joinCount() != before {
			t.Fatalf("an attempt before the backoff of %v ended", wait)
		}
		clk.Advance(time.Millisecond)
		if got := awaitJoin(t, d); got != "home" {
			t.Fatalf("the retry joined %q, want home", got)
		}
	}
	if d.on() != "home" {
		t.Fatalf("after the retries the radio is on %q", d.on())
	}
	if got, _, _ := wifiLast(w); got != uint8(netcommdef.NetworkCommissioningStatusSuccess) {
		t.Errorf("LastNetworkingStatus after the successful retry %v, want Success", got)
	}
	if list := wifiNetworksOf(t, w); len(list) != 1 || !list[0].Connected {
		t.Errorf("Networks %+v, want home connected", list)
	}
	if n := clk.Pending(); n != 0 {
		t.Errorf("%d timers armed after the successful retry, want none", n)
	}
}

// TestRejoinBackoffIsCappedWaitsForAFailSafeAndEndsWithItsLifetime: the
// wait stops doubling at wifiRejoinMaxDelay; an attempt that comes due
// while a fail-safe is armed does not touch the radio; once Start's
// context ended no attempt follows.
func TestRejoinBackoffIsCappedWaitsForAFailSafeAndEndsWithItsLifetime(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	st := memstore.New()
	var armed atomic.Bool
	d := newLinkDriver("home")
	seed, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: d, IsFailSafeArmed: armed.Load, Store: st})
	if err != nil {
		t.Fatal(err)
	}
	commissionHome(t, seed, d, &armed)
	d.mu.Lock()
	d.link = nil
	d.mu.Unlock()
	d.setReachable("home", false)

	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: d, IsFailSafeArmed: armed.Load, Store: st, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	life, stop := context.WithCancel(t.Context())
	w.Start(life)
	awaitJoin(t, d)
	for {
		next := clk.Armed()
		if len(next) != 1 {
			t.Fatalf("armed %v after a failed attempt, want the next one", next)
		}
		if next[0] >= wifiRejoinMaxDelay {
			break
		}
		clk.Advance(next[0])
		awaitJoin(t, d)
	}
	clk.Advance(wifiRejoinMaxDelay)
	awaitJoin(t, d)
	if got := clk.Armed(); !slices.Equal(got, []time.Duration{wifiRejoinMaxDelay}) {
		t.Fatalf("armed %v after the cap, want %v", got, []time.Duration{wifiRejoinMaxDelay})
	}

	armed.Store(true)
	before := d.joinCount()
	clk.Advance(wifiRejoinMaxDelay)
	if d.joinCount() != before {
		t.Fatal("an attempt joined while a fail-safe was armed")
	}
	if clk.Pending() != 1 {
		t.Fatalf("%d timers armed while the fail-safe holds the attempt, want 1", clk.Pending())
	}
	armed.Store(false)

	stop()
	clk.Advance(time.Hour)
	if d.joinCount() != before {
		t.Errorf("%d joins after Start's context ended", d.joinCount()-before)
	}
}
