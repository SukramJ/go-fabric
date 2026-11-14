// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

// After a join that did not succeed the Wi-Fi server reads the network
// the radio is on from a driver that reports it, so a working link reads
// connected and is not joined again (review II, R2-P3-dr-net-04).

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
)

// TestAFailedJoinKeepsTheReportedLinkConnected: a ConnectNetwork of a
// second network that fails leaves the radio on the committed one, as
// the driver reports; Networks reads it connected, and the fail-safe's
// expiry does not join it again.
func TestAFailedJoinKeepsTheReportedLinkConnected(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	var armed atomic.Bool
	d := newLinkDriver("home")
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: d, IsFailSafeArmed: armed.Load, MaxNetworks: 2, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	commissionHome(t, w, d, &armed)

	armed.Store(true)
	if _, err := w.MatterInvoke(t.Context(), netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("away"), Credentials: []byte("secret12")}); err != nil {
		t.Fatal(err)
	}
	r, err := w.MatterInvoke(t.Context(), netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("away")})
	if err != nil || r.(netcommdef.ConnectNetworkResponse).NetworkingStatus == netcommdef.NetworkCommissioningStatusSuccess {
		t.Fatalf("precondition: the join of away failed: %+v %v", r, err)
	}
	awaitJoin(t, d)
	if d.on() != "home" {
		t.Fatalf("precondition: the radio is on %q, want home", d.on())
	}
	list := wifiNetworksOf(t, w)
	if len(list) != 2 || string(list[0].NetworkId) != "home" || !list[0].Connected || list[1].Connected {
		t.Fatalf("Networks after the failed join of away %+v, want home connected, the radio being on it", list)
	}

	armed.Store(false)
	before := d.joinCount()
	w.OnFailSafeExpired()
	clk.Advance(0)
	if n := d.joinCount() - before; n != 0 {
		t.Fatalf("the expiry joined %d times with the radio on the committed network home", n)
	}
	if list := wifiNetworksOf(t, w); len(list) != 1 || !list[0].Connected {
		t.Errorf("Networks after the expiry %+v, want home connected", list)
	}
}
