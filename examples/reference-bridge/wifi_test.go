// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	wifidiagdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkdiagnostics"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/node"
)

// wifiEventLog records the WiFiNetworkDiagnostics events.
type wifiEventLog struct{ events []any }

func (l *wifiEventLog) MatterEmitEvent(_ uint16, clusterID, _ uint32, data any, _ contract.EventPriority) {
	if clusterID == wifidiagdef.ClusterID {
		l.events = append(l.events, data)
	}
}

// TestWiFiFlag: with --wifi the root serves the simulated link with PKTCNT,
// ERRCNT and CurrentMaxRate; the app pipe steps the counters and raises the
// three events, ResetCounts zeroes the counters, and CHIP's Wi-Fi
// TestEventTrigger codes raise the all-clusters app's events. Without
// --wifi the root has no such cluster and the commands are refused.
func TestWiFiFlag(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	plain := newFleet()
	n := testNode(t, plain, filepath.Join(t.TempDir(), "plain.db"))
	if n.Root().WiFiNetworkDiagnostics != nil {
		t.Fatal("WiFiNetworkDiagnostics mounted without --wifi")
	}
	if err := plain.applyPipeCommand(pipeCommand{Name: "WiFiDiagnostics", Action: "StepCounters"}); !errors.Is(err, errNoWiFi) {
		t.Errorf("WiFiDiagnostics without --wifi: %v", err)
	}
	if err := plain.testEventTrigger(ctx, triggerWiFiDisconnection); err == nil {
		t.Error("a Wi-Fi trigger was accepted without --wifi")
	}

	f := newFleet()
	n = testNodeWith(t, f, filepath.Join(t.TempDir(), "wifi.db"), func(cfg *node.Config) { cfg.Network.WiFi = f.wifi.nodeConfig() })
	srv := n.Root().WiFiNetworkDiagnostics
	if srv == nil {
		t.Fatal("no WiFiNetworkDiagnostics with --wifi")
	}
	if fm, _ := srv.MatterRead(0xFFFC); fm != uint32(wifidiagdef.FeaturePacketCounts|wifidiagdef.FeatureErrorCounts) {
		t.Errorf("FeatureMap %v", fm)
	}
	if v, ok := srv.MatterRead(wifidiagdef.AttrCurrentMaxRate); !ok || v == nil {
		t.Errorf("CurrentMaxRate %v (%v)", v, ok)
	}
	if v, _ := srv.MatterRead(wifidiagdef.AttrRssi); v.(int8) < -120 || v.(int8) > 0 {
		t.Errorf("RSSI %v outside -120..0", v)
	}
	rx := func() uint32 {
		v, _ := srv.MatterRead(wifidiagdef.AttrPacketUnicastRxCount)
		return v.(uint32)
	}
	before := rx()
	if err := f.applyPipeCommand(pipeCommand{Name: "WiFiDiagnostics", Action: "StepCounters"}); err != nil {
		t.Fatal(err)
	}
	if rx() <= before {
		t.Errorf("StepCounters left PacketUnicastRxCount at %d", rx())
	}
	if _, err := srv.MatterInvoke(ctx, wifidiagdef.CmdResetCounts, wifidiagdef.ResetCountsRequest{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint32{
		wifidiagdef.AttrBeaconLostCount, wifidiagdef.AttrBeaconRxCount, wifidiagdef.AttrPacketMulticastRxCount,
		wifidiagdef.AttrPacketMulticastTxCount, wifidiagdef.AttrPacketUnicastRxCount, wifidiagdef.AttrPacketUnicastTxCount,
	} {
		if v, _ := srv.MatterRead(id); v != uint32(0) {
			t.Errorf("attribute 0x%04X = %v after ResetCounts", id, v)
		}
	}
	if v, _ := srv.MatterRead(wifidiagdef.AttrOverrunCount); v != uint64(0) {
		t.Errorf("OverrunCount = %v after ResetCounts", v)
	}

	log := &wifiEventLog{}
	srv.SetMatterEventEmitter(log)
	reason, cause, status, conn := uint16(7), uint8(3), uint16(17), uint8(0)
	for _, cmd := range []pipeCommand{
		{Name: "WiFiDiagnostics", Action: "Disconnection", ReasonCode: &reason},
		{Name: "WiFiDiagnostics", Action: "AssociationFailure", AssociationFailureCause: &cause, Status: &status},
		{Name: "WiFiDiagnostics", Action: "ConnectionStatus", ConnectionStatus: &conn},
	} {
		if err := f.applyPipeCommand(cmd); err != nil {
			t.Fatalf("%s: %v", cmd.Action, err)
		}
	}
	for _, trigger := range []uint64{
		triggerWiFiDisconnection, triggerWiFiAssociationFailure,
		triggerWiFiConnectionStatus | 0x0000_0001_0000_0000, // CHIP clears the endpoint bits
	} {
		if err := f.testEventTrigger(ctx, trigger); err != nil {
			t.Fatalf("trigger 0x%016X: %v", trigger, err)
		}
	}
	want := []any{
		wifidiagdef.DisconnectionEvent{ReasonCode: 7},
		wifidiagdef.AssociationFailureEvent{AssociationFailureCause: wifidiagdef.AssociationFailureCauseSsidNotFound, Status: 17},
		wifidiagdef.ConnectionStatusEvent{ConnectionStatus: wifidiagdef.ConnectionStatusConnected},
		// CHIP's all-clusters app values (wifi-diagnostics-stub.cpp :42, :55-57, :71).
		wifidiagdef.DisconnectionEvent{ReasonCode: 3},
		wifidiagdef.AssociationFailureEvent{AssociationFailureCause: wifidiagdef.AssociationFailureCauseAuthenticationFailed, Status: 4},
		wifidiagdef.ConnectionStatusEvent{ConnectionStatus: wifidiagdef.ConnectionStatusNotConnected},
	}
	if !slices.Equal(log.events, want) {
		t.Errorf("events %+v, want %+v", log.events, want)
	}
	if err := f.applyPipeCommand(pipeCommand{Name: "WiFiDiagnostics", Action: "Roam"}); !errors.Is(err, errUnknownPipeCommand) {
		t.Errorf("unknown action: %v", err)
	}
}
