// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

// The committed Wi-Fi network across a restart, after connectedhomeip at
// the harness pin (6170af84): the Linux driver stores SSID and credentials
// at CommitConfiguration and loads them at Init
// (src/platform/Linux/NetworkCommissioningWiFiDriver.cpp:135-167,
// :77-128); the cluster starts with the last status, network id and
// connect error null (NetworkCommissioningCluster.cpp:200-214) and records
// the link's return as OnNetworkingStatusChange does (:283-307). The
// failsafe revert restores the committed list (:992-1014).

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// wifiOn builds a Wi-Fi server on st with an armed failsafe.
func wifiOn(t *testing.T, st store.Settings, d *simDriver) *WiFiNetworkCommissioning {
	t.Helper()
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{
		Driver: d, IsFailSafeArmed: func() bool { return true }, Store: st,
		SetBreadcrumb: func(v uint64) { t.Errorf("breadcrumb %d written outside a command", v) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// wifiNetworks reads Networks as ssid → connected, in list order.
func wifiNetworks(t *testing.T, w *WiFiNetworkCommissioning) []netcommdef.NetworkInfoStruct {
	t.Helper()
	v, _ := w.MatterRead(netcommAttrNetworks)
	return v.(spec.List[netcommdef.NetworkInfoStruct])
}

func wifiLast(w *WiFiNetworkCommissioning) (status, id, connectErr any) {
	status, _ = w.MatterRead(netcommAttrLastNetworkingStatus)
	id, _ = w.MatterRead(netcommAttrLastNetworkID)
	connectErr, _ = w.MatterRead(netcommAttrLastConnectErrorValue)
	return status, id, connectErr
}

func TestWiFiNetworkCommissioningPersistsAndReconnects(t *testing.T) {
	ctx := context.Background()
	st := memstore.New()
	first := wifiOn(t, st, &simDriver{})
	if _, err := first.MatterInvoke(ctx, netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("lab"), Credentials: []byte("secret12")}); err != nil {
		t.Fatal(err)
	}
	// Staged, not committed: a restart before CommissioningComplete keeps
	// nothing (the driver stores only at CommitConfiguration).
	if n := wifiNetworks(t, wifiOn(t, st, &simDriver{})); len(n) != 0 {
		t.Fatalf("an uncommitted network survived the restart: %v", n)
	}
	if _, err := first.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("lab")}); err != nil {
		t.Fatal(err)
	}
	first.OnCommissioningComplete(ctx)

	// The restart: the network is back, not connected, the last values
	// null (Init :200-214); Start rejoins it with the stored credentials.
	d := &simDriver{}
	w := wifiOn(t, st, d)
	if n := wifiNetworks(t, w); len(n) != 1 || string(n[0].NetworkId) != "lab" || n[0].Connected {
		t.Fatalf("Networks after the restart = %+v, want lab, not connected", n)
	}
	if s, id, ce := wifiLast(w); s != nil || id != nil || ce != nil {
		t.Fatalf("last values before Start = %v %v %v, want null", s, id, ce)
	}
	var reported [][]uint32
	w.OnAttributeChange(func(c contract.Change) { reported = append(reported, c.Attributes) })
	w.Start(ctx)
	if len(reported) != 1 || !slices.Contains(reported[0], netcommAttrNetworks) || !slices.Contains(reported[0], netcommAttrLastNetworkingStatus) {
		t.Fatalf("Start reported %v, want Networks and the last-connect attributes", reported)
	}
	if len(d.connects) != 1 || string(d.connects[0]) != "lab" {
		t.Fatalf("Start connected %q, want lab once", d.connects)
	}
	if n := wifiNetworks(t, w); !n[0].Connected {
		t.Fatal("Start left the network unconnected")
	}
	if s, id, ce := wifiLast(w); s != uint8(netcommdef.NetworkCommissioningStatusSuccess) || string(id.([]byte)) != "lab" || ce != nil {
		t.Fatalf("last values after Start = %v %v %v, want Success, lab, null", s, id, ce)
	}

	// The failsafe revert restores the loaded commit; the store is
	// untouched by an uncommitted removal.
	if _, err := w.MatterInvoke(ctx, netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: []byte("lab")}); err != nil {
		t.Fatal(err)
	}
	w.OnFailSafeExpired()
	// The link never left lab: the restored network is connected
	// (WiFiNetworkIterator::Next, :275-310), and the revert is reported.
	if n := wifiNetworks(t, w); len(n) != 1 || string(n[0].NetworkId) != "lab" || !n[0].Connected {
		t.Fatalf("Networks after the revert = %+v, want lab, connected", n)
	}
	if len(reported) != 2 || !slices.Contains(reported[1], netcommAttrNetworks) {
		t.Fatalf("the revert reported %v, want Networks", reported)
	}
	if n := wifiNetworks(t, wifiOn(t, st, &simDriver{})); len(n) != 1 {
		t.Fatalf("an uncommitted removal reached the store: %v", n)
	}

	// A committed removal empties the store; Start then joins nothing.
	if _, err := w.MatterInvoke(ctx, netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: []byte("lab")}); err != nil {
		t.Fatal(err)
	}
	w.OnCommissioningComplete(t.Context())
	empty := &simDriver{}
	none := wifiOn(t, st, empty)
	none.Start(ctx)
	if n := wifiNetworks(t, none); len(n) != 0 || len(empty.connects) != 0 {
		t.Fatalf("after a committed removal: Networks %v, connects %q", n, empty.connects)
	}
}

// TestWiFiNetworkCommissioningReconnectFails records a failed rejoin as a
// failed ConnectNetwork would: the status and connect error, the network
// not connected.
func TestWiFiNetworkCommissioningReconnectFails(t *testing.T) {
	ctx := context.Background()
	st := memstore.New()
	if err := st.SetSetting(ctx, wifiNetworksKey, `[{"ssid":"bGFi","credentials":"c2VjcmV0MTI="}]`); err != nil {
		t.Fatal(err)
	}
	w := wifiOn(t, st, &simDriver{connStatus: uint8(netcommdef.NetworkCommissioningStatusAuthFailure)})
	w.Start(ctx)
	if n := wifiNetworks(t, w); len(n) != 1 || n[0].Connected {
		t.Fatalf("Networks = %+v, want lab, not connected", n)
	}
	if s, id, ce := wifiLast(w); s != uint8(netcommdef.NetworkCommissioningStatusAuthFailure) || string(id.([]byte)) != "lab" || ce != int32(-7) {
		t.Fatalf("last values = %v %v %v, want AuthFailure, lab, -7", s, id, ce)
	}
}

// TestWiFiNetworkCommissioningStoredValueRules: a value that does not
// decode, an empty or over-long SSID and entries past MaxNetworks are not
// loaded.
func TestWiFiNetworkCommissioningStoredValueRules(t *testing.T) {
	ctx := context.Background()
	for name, raw := range map[string]string{
		"not JSON":    `{`,
		"empty SSID":  `[{"ssid":"","credentials":""}]`,
		"long SSID":   `[{"ssid":"` + "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDEy" + `","credentials":""}]`,
		"nothing set": ``,
	} {
		st := memstore.New()
		if raw != "" {
			if err := st.SetSetting(ctx, wifiNetworksKey, raw); err != nil {
				t.Fatal(err)
			}
		}
		if n := wifiNetworks(t, wifiOn(t, st, &simDriver{})); len(n) != 0 {
			t.Errorf("%s: loaded %v", name, n)
		}
	}
	st := memstore.New()
	if err := st.SetSetting(ctx, wifiNetworksKey, `[{"ssid":"YQ=="},{"ssid":"Yg=="}]`); err != nil {
		t.Fatal(err)
	}
	if n := wifiNetworks(t, wifiOn(t, st, &simDriver{})); len(n) != 1 || string(n[0].NetworkId) != "a" {
		t.Fatalf("MaxNetworks 1 loaded %v, want a", n)
	}
}
