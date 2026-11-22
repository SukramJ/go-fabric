// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	netcommdef "thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
	"thingfabric.dev/go-fabric/contract"
)

// capableDriver is a linkDriver with the two optional capabilities: it
// hands the server's link report out, and it counts DisconnectNetwork.
type capableDriver struct {
	*linkDriver
	report       chan func(contract.WiFiLinkChange)
	disconnected chan struct{}
	mu           sync.Mutex
	disconnects  int
}

var (
	_ contract.WiFiLinkWatcher  = (*capableDriver)(nil)
	_ contract.WiFiDisconnector = (*capableDriver)(nil)
)

func newCapableDriver(reachable ...string) *capableDriver {
	return &capableDriver{linkDriver: newLinkDriver(reachable...), report: make(chan func(contract.WiFiLinkChange), 1), disconnected: make(chan struct{}, 8)}
}

func (d *capableDriver) WatchLink(ctx context.Context, report func(contract.WiFiLinkChange)) {
	d.report <- report
	<-ctx.Done()
}

func (d *capableDriver) DisconnectNetwork(context.Context) error {
	d.linkDriver.mu.Lock()
	d.link = nil
	d.linkDriver.mu.Unlock()
	d.mu.Lock()
	d.disconnects++
	d.mu.Unlock()
	d.disconnected <- struct{}{}
	return nil
}

func (d *capableDriver) disconnectCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.disconnects
}

func awaitDisconnect(t *testing.T, d *capableDriver) {
	t.Helper()
	select {
	case <-d.disconnected:
	case <-t.Context().Done():
		t.Fatal("no DisconnectNetwork asked of the driver")
	}
}

// TestWiFiLinkWatcherMovesTheNetworkState (R2-P5-port-09): a link the
// driver reports lost leaves the committed network disconnected with the
// reported status and error, and starts no rejoin; the link the radio
// regains reads connected again.
func TestWiFiLinkWatcherMovesTheNetworkState(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	var armed atomic.Bool
	d := newCapableDriver("home")
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: d, IsFailSafeArmed: armed.Load, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	commissionHome(t, w, d.linkDriver, &armed)
	w.Start(t.Context())
	clk.Advance(0)
	report := <-d.report
	joins := d.joinCount()

	reason := int32(4)
	report(contract.WiFiLinkChange{SSID: []byte("home"), Status: uint8(netcommdef.NetworkCommissioningStatusAuthFailure), ErrorValue: &reason})
	if list := wifiNetworksOf(t, w); len(list) != 1 || list[0].Connected {
		t.Fatalf("Networks after a reported loss %+v, want home disconnected", list)
	}
	if v, _ := w.MatterRead(netcommAttrLastNetworkingStatus); v != uint8(netcommdef.NetworkCommissioningStatusAuthFailure) {
		t.Errorf("LastNetworkingStatus %v, want AuthFailure", v)
	}
	if v, _ := w.MatterRead(netcommAttrLastConnectErrorValue); v != int32(4) {
		t.Errorf("LastConnectErrorValue %v, want 4", v)
	}
	if v, _ := w.MatterRead(netcommAttrLastNetworkID); !bytes.Equal(v.([]byte), []byte("home")) {
		t.Errorf("LastNetworkID %v, want home", v)
	}
	clk.Advance(time.Minute)
	if n := d.joinCount() - joins; n != 0 {
		t.Errorf("a reported loss started %d joins, want none: the driver's radio rejoins by itself", n)
	}

	report(contract.WiFiLinkChange{SSID: []byte("home"), Connected: true})
	if list := wifiNetworksOf(t, w); len(list) != 1 || !list[0].Connected {
		t.Errorf("Networks after the link came back %+v, want home connected", list)
	}
}

// TestFailSafeExpiryDisconnectsWithoutACommittedNetwork (R2-P5-port-10):
// a first commissioning whose fail-safe expires after ConnectNetwork joined
// leaves no committed network; the radio leaves the network the
// commissioning joined.
func TestFailSafeExpiryDisconnectsWithoutACommittedNetwork(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	var armed atomic.Bool
	d := newCapableDriver("away")
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: d, IsFailSafeArmed: armed.Load, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	armed.Store(true)
	if _, err := w.MatterInvoke(t.Context(), netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("away"), Credentials: []byte("secret12")}); err != nil {
		t.Fatal(err)
	}
	if r, err := w.MatterInvoke(t.Context(), netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("away")}); err != nil || r.(netcommdef.ConnectNetworkResponse).NetworkingStatus != netcommdef.NetworkCommissioningStatusSuccess {
		t.Fatalf("join of away: %+v %v", r, err)
	}
	awaitJoin(t, d.linkDriver)
	armed.Store(false)
	w.OnFailSafeExpired()
	clk.Advance(0)
	awaitDisconnect(t, d)
	if d.on() != "" || d.disconnectCount() != 1 {
		t.Errorf("after the expiry the radio is on %q, %d disconnects; want none, one", d.on(), d.disconnectCount())
	}
}

// TestFailSafeExpiryDisconnectsAfterAFailedRejoin (R2-P5-port-10): the
// expiry finds the radio on a network the revert removed, and the rejoin
// of the committed network fails: the radio leaves the removed network.
func TestFailSafeExpiryDisconnectsAfterAFailedRejoin(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	var armed atomic.Bool
	d := newCapableDriver("home", "new")
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: d, IsFailSafeArmed: armed.Load, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	commissionHome(t, w, d.linkDriver, &armed)
	if r := moveTo(t, w, d.linkDriver, &armed, "new"); r.NetworkingStatus != netcommdef.NetworkCommissioningStatusSuccess {
		t.Fatalf("precondition: the join of new succeeded: %+v", r)
	}
	d.linkDriver.mu.Lock()
	d.reachable["home"] = false
	d.linkDriver.mu.Unlock()
	armed.Store(false)
	w.OnFailSafeExpired()
	clk.Advance(0)
	if got := awaitJoin(t, d.linkDriver); got != "home" {
		t.Fatalf("the expiry rejoined %q, want home", got)
	}
	clk.Advance(0)
	awaitDisconnect(t, d)
	if d.on() != "" {
		t.Errorf("after the failed rejoin the radio is on %q, want none", d.on())
	}
}

// TestWiFiOnConnectedRunsAfterASuccessfulJoin (review II, R2-P6-api-2450):
// the OnConnected hook runs once after a ConnectNetwork the driver
// answers successfully, and not after one it refuses.
func TestWiFiOnConnectedRunsAfterASuccessfulJoin(t *testing.T) {
	t.Parallel()
	var armed atomic.Bool
	var joined atomic.Int32
	d := newCapableDriver("home")
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{
		Driver: d, IsFailSafeArmed: armed.Load, Clock: clocktest.New(time.Unix(1_700_000_000, 0)),
		OnConnected: func(context.Context) { joined.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	armed.Store(true)
	for _, ssid := range []string{"home", "away"} {
		if _, err := w.MatterInvoke(t.Context(), netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte(ssid), Credentials: []byte("secret12")}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.MatterInvoke(t.Context(), netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("away")}); err != nil {
		t.Fatal(err)
	}
	if got := joined.Load(); got != 0 {
		t.Fatalf("OnConnected ran %d times after a refused join, want 0", got)
	}
	if r, err := w.MatterInvoke(t.Context(), netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("home")}); err != nil || r.(netcommdef.ConnectNetworkResponse).NetworkingStatus != netcommdef.NetworkCommissioningStatusSuccess {
		t.Fatalf("join of home: %+v %v", r, err)
	}
	if got := joined.Load(); got != 1 {
		t.Errorf("OnConnected ran %d times after a successful join, want 1", got)
	}
}
