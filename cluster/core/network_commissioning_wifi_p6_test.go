// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/contract"
)

// A fail-safe that expires during a ConnectNetwork's join cancels that
// join; the command keeps the driver until it returned, and the rejoin of
// the committed network starts after it: two joins never run on the
// driver at once (review II, R2-P6-conc-1021, N3-finnet-2).
func TestWiFiExpiryAbandonsTheCommandsJoinBeforeTheRejoin(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		clk := clocktest.New(time.Unix(1_700_000_000, 0))
		var armed atomic.Bool
		d := newHeldDriver()
		w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: d, IsFailSafeArmed: armed.Load, Clock: clk, MaxNetworks: 2})
		if err != nil {
			t.Fatal(err)
		}
		w.Start(t.Context())
		armed.Store(true)
		if _, err := w.MatterInvoke(t.Context(), netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("home"), Credentials: []byte("secret12")}); err != nil {
			t.Fatal(err)
		}
		w.OnCommissioningComplete(t.Context())
		if _, err := w.MatterInvoke(t.Context(), netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("new"), Credentials: []byte("secret12")}); err != nil {
			t.Fatal(err)
		}
		answered := make(chan struct{})
		go func() {
			defer close(answered)
			_, _ = w.MatterInvoke(t.Context(), netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("new")})
		}()
		if got := awaitStarted(t, d); got != "new" {
			t.Fatalf("the command joined %q, want new", got)
		}
		armed.Store(false)
		w.OnFailSafeExpired()
		select {
		case <-answered:
		case <-time.After(5 * time.Second):
			close(d.release)
			t.Fatal("the ConnectNetwork the expiry abandoned did not return: its join was not cancelled")
		}
		go clk.Advance(0)
		if got := awaitStarted(t, d); got != "home" {
			t.Fatalf("the rejoin joined %q, want home", got)
		}
		close(d.release)
		if most, _ := d.stats(); most > 1 {
			t.Errorf("%d joins ran on the driver at once, want 1", most)
		}
	})
}

// slowScanDriver's scan returns only when its ctx ends.
type slowScanDriver struct{ *heldDriver }

func (slowScanDriver) ScanNetworks(ctx context.Context, _ []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	<-ctx.Done()
	return uint8(netcommdef.NetworkCommissioningStatusUnknownError), "scan timed out", nil
}

// The driver's scan and join are bounded by the ScanMaxTimeSeconds and
// ConnectMaxTimeSeconds the server advertises: a driver that waits on its
// ctx returns, and the next command is not Busy (review II,
// R2-P6-bound-1012).
func TestWiFiDriverCallsAreBoundedByTheAdvertisedTimes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		d := slowScanDriver{newHeldDriver()}
		w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{
			Driver: d, IsFailSafeArmed: func() bool { return true }, ScanMaxTimeSeconds: 1, ConnectMaxTimeSeconds: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		scanned := make(chan error, 1)
		go func() {
			_, err := w.MatterInvoke(ctx, netcommdef.CmdScanNetworks, netcommdef.ScanNetworksRequest{})
			scanned <- err
		}()
		select {
		case <-scanned:
		case <-time.After(10 * time.Second):
			t.Fatal("ScanNetworks did not return within the advertised ScanMaxTimeSeconds")
		}
		if _, err := w.MatterInvoke(ctx, netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("home"), Credentials: []byte("secret12")}); err != nil {
			t.Fatal(err)
		}
		connected := make(chan any, 1)
		go func() {
			resp, _ := w.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("home")})
			connected <- resp
		}()
		select {
		case resp := <-connected:
			if r, ok := resp.(netcommdef.ConnectNetworkResponse); !ok || r.NetworkingStatus == netcommdef.NetworkCommissioningStatusSuccess {
				t.Errorf("ConnectNetwork past ConnectMaxTimeSeconds = %#v, want a failure status", resp)
			}
		case <-time.After(10 * time.Second):
			close(d.release)
			t.Fatal("ConnectNetwork did not return within the advertised ConnectMaxTimeSeconds")
		}
	})
}
