// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"testing"

	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
)

// TestWiFiNetworkCommissioningBreadcrumbOutsideTheLock pins R2-P1-core-07:
// the Wi-Fi commands write their Breadcrumb after releasing the cluster's
// lock, so a listener on that write — GeneralCommissioning's change
// listeners reach the host's attribute hook synchronously — may read this
// cluster without deadlocking it.
func TestWiFiNetworkCommissioningBreadcrumbOutsideTheLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var w *WiFiNetworkCommissioning
	blocked := 0
	// The write runs on the command's goroutine: the cluster's lock is
	// free there exactly when the command released it before the write.
	// The probe decides at once, where a read on a second goroutine had
	// to be given time (review II, R2-P1-dr-sec-05).
	readsBack := func(uint64) {
		if !w.mu.TryLock() {
			blocked++
			return
		}
		w.mu.Unlock()
		w.MatterRead(netcommAttrNetworks)
	}
	var err error
	w, err = NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{
		Driver: &simDriver{}, IsFailSafeArmed: func() bool { return true }, SetBreadcrumb: readsBack,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		cmd  uint32
		req  any
	}{
		{"AddOrUpdateWiFiNetwork", netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("lab"), Credentials: []byte("secret12"), Breadcrumb: bc(1)}},
		{"ReorderNetwork", netcommdef.CmdReorderNetwork, netcommdef.ReorderNetworkRequest{NetworkId: []byte("lab"), NetworkIndex: 0, Breadcrumb: bc(2)}},
		{"ConnectNetwork", netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("lab"), Breadcrumb: bc(3)}},
		{"RemoveNetwork", netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: []byte("lab"), Breadcrumb: bc(4)}},
	} {
		blocked = 0
		if _, err := w.MatterInvoke(ctx, c.cmd, c.req); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if blocked != 0 {
			t.Errorf("%s wrote its Breadcrumb with the cluster's lock held: a read of the cluster from the write blocked", c.name)
		}
	}
}

// stallingWiFiDriver holds ConnectNetwork until release is closed, after
// signalling entered, and then reports success.
type stallingWiFiDriver struct {
	simDriver
	entered chan struct{}
	release chan struct{}
}

func (d *stallingWiFiDriver) ConnectNetwork(_ context.Context, _, _ []byte) (status uint8, debugText string, errorValue int32) {
	close(d.entered)
	<-d.release
	return 0, "", 0
}

// TestWiFiNetworkCommissioningConnectAfterExpiryIsDropped pins
// R2-P1-core-06: a ConnectNetwork whose driver call returns after the
// fail-safe that admitted it expired leaves the reverted state alone — no
// link on the uncommitted network, no LastNetworkID / LastNetworkingStatus
// of a network no longer listed, no Breadcrumb in a disarmed
// GeneralCommissioning. connectedhomeip drops such a late result:
// OnFailSafeTimerExpired releases the pending command handle
// (NetworkCommissioningCluster.cpp:1002 at 6170af84) and OnResult returns
// on a null handle (:850-857).
func TestWiFiNetworkCommissioningConnectAfterExpiryIsDropped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	armed := true
	var crumbs []uint64
	d := &stallingWiFiDriver{entered: make(chan struct{}), release: make(chan struct{})}
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{
		Driver: d, IsFailSafeArmed: func() bool { return armed },
		SetBreadcrumb: func(v uint64) { crumbs = append(crumbs, v) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.MatterInvoke(ctx, netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("lab"), Credentials: []byte("secret12")}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = w.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte("lab"), Breadcrumb: bc(9)})
	}()
	<-d.entered
	armed = false
	w.OnFailSafeExpired()
	close(d.release)
	<-done
	if len(crumbs) != 0 {
		t.Errorf("the late ConnectNetwork wrote Breadcrumb %v into a disarmed GeneralCommissioning", crumbs)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.link != nil || w.lastNetworkID != nil || w.lastStatus != nil {
		t.Errorf("the late ConnectNetwork applied its result to the reverted state: link=%q lastNetworkID=%q lastStatus=%v", w.link, w.lastNetworkID, w.lastStatus)
	}
}
