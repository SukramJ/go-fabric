// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	netcommdef "thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store/memstore"
)

// TestWiFiNetworkCommandAfterTheWindowExpired (review II,
// C5-p8-wifi-3055-sibling, the Wi-Fi side of R3-fsm-3055): a fail-safe
// that expires between the gate's check and the command's write — the
// port runs the expiry before it answers armed — fails AddOrUpdate,
// Remove and Connect with FAILSAFE_REQUIRED. Nothing stays staged, so the
// next CommissioningComplete stores the committed list unchanged, and a
// refused ConnectNetwork writes no Last… attribute.
func TestWiFiNetworkCommandAfterTheWindowExpired(t *testing.T) {
	t.Parallel()
	for _, cmd := range []struct {
		name      string
		committed bool
		id        uint32
		req       any
	}{
		{"AddOrUpdateWiFiNetwork", false, netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("lab"), Credentials: []byte("secret12")}},
		{"RemoveNetwork", true, netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: []byte("lab")}},
		{"ConnectNetwork", true, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("lab")}},
	} {
		t.Run(cmd.name, func(t *testing.T) {
			t.Parallel()
			ctx := im.WithFabricFilter(context.Background(), false, 1)
			st := memstore.New()
			var w *WiFiNetworkCommissioning
			expireNext := false
			w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{
				Driver: &simDriver{}, Store: st, Clock: clocktest.New(time.Unix(0, 0)),
				FailSafe: func() (uint8, bool) {
					if expireNext {
						expireNext = false
						w.OnFailSafeExpired()
					}
					return 1, true
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if cmd.committed {
				if _, err := w.MatterInvoke(ctx, netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("lab"), Credentials: []byte("secret12")}); err != nil {
					t.Fatal(err)
				}
				w.OnCommissioningComplete(ctx)
			}
			w.mu.Lock()
			wantStaged := len(w.committed)
			w.mu.Unlock()
			lastBefore, _, _ := wifiLast(w)

			expireNext = true
			_, err = w.MatterInvoke(ctx, cmd.id, cmd.req)
			if got := threadStatusOf(t, err); got != im.StatusFailsafeRequired {
				t.Fatalf("%s after the window expired = %v, want FAILSAFE_REQUIRED", cmd.name, err)
			}
			w.mu.Lock()
			staged := len(w.networks)
			w.mu.Unlock()
			if staged != wantStaged {
				t.Errorf("%d networks staged after the refused command, want %d", staged, wantStaged)
			}
			if last, _, _ := wifiLast(w); last != lastBefore {
				t.Errorf("LastNetworkingStatus %v after the refused command, want %v", last, lastBefore)
			}
			// The next window's completion stores what is committed, not
			// the refused command's change.
			w.OnCommissioningComplete(ctx)
			restarted := wifiOn(t, st, &simDriver{})
			n := wifiNetworks(t, restarted)
			if (len(n) == 1) != cmd.committed || cmd.committed && !bytes.Equal(n[0].NetworkId, []byte("lab")) {
				t.Errorf("stored networks after the next completion %+v, want committed %v", n, cmd.committed)
			}
		})
	}
}
