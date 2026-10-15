// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"errors"
	"fmt"
	"sync"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	wifidiagdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkdiagnostics"
	"github.com/SukramJ/go-fabric/node"
)

// --wifi (TESTING ONLY): the root serves WiFiNetworkDiagnostics from a
// simulated Wi-Fi link, so the certification harness can run the DGWIFI
// family against a daemon that has no radio. The link's values are fixed
// and plausible; its counters move only when the app pipe steps them
// (WiFiDiagnostics, Action StepCounters) and return to zero on ResetCounts.
// The three events are raised by the app pipe (WiFiDiagnostics, Action
// Disconnection | AssociationFailure | ConnectionStatus) and by CHIP's
// Wi-Fi TestEventTrigger codes (src/app/clusters/
// wifi-network-diagnostics-server/WiFiDiagnosticsTestEventTriggerHandler.h
// :44-56 at the harness pin 6170af8461b10b1766044122ac83332c6d00ab20), with
// the values CHIP's all-clusters app raises
// (all-clusters-common/src/wifi-diagnostics-stub.cpp :37-74).
//
// A real Wi-Fi host reads the values from the kernel instead — nl80211, or
// `iw dev <if> link` and `iw dev <if> station dump` — behind its own
// mattercore.WiFiReporter; NetworkCommissioning stays Ethernet-only either
// way (concept v2 §B.4).

// CHIP's Wi-Fi diagnostics test event triggers
// (WiFiDiagnosticsTestEventTriggerHandler.h :44-56); the handler clears the
// endpoint bits first (:69, clearEndpointInEventTrigger).
const (
	triggerWiFiDisconnection      uint64 = 0x0036_0000_0000_0000
	triggerWiFiAssociationFailure uint64 = 0x0036_0000_0000_0001
	triggerWiFiConnectionStatus   uint64 = 0x0036_0000_0000_0002
)

// The event values CHIP's all-clusters app raises (wifi-diagnostics-stub.cpp):
// reason 3, "deauthenticated because sending STA is leaving" (:42);
// AuthenticationFailed with 802.11 status 4 (:55-57); NotConnected (:71).
const (
	wifiStubReasonCode  uint16 = 3
	wifiStubAssocCause         = wifidiagdef.AssociationFailureCauseAuthenticationFailed
	wifiStubAssocStatus uint16 = 4
	wifiStubConnStatus         = wifidiagdef.ConnectionStatusNotConnected
)

// demoWiFiBSSID is a locally administered unicast address (bit 1 of the
// first octet set), so it names no real access point.
var demoWiFiBSSID = []byte{0x02, 0x00, 0x5E, 0x10, 0x20, 0x01}

// demoWiFi is the simulated link: a mattercore.WiFiReporter with fixed
// association values and counters, and the server it feeds once mounted.
type demoWiFi struct {
	mu                       sync.Mutex
	beaconLost, beaconRx     uint32
	multicastRx, multicastTx uint32
	unicastRx, unicastTx     uint32
	overrun                  uint64
	resets                   int
	srv                      *mattercore.WiFiNetworkDiagnostics
}

var _ mattercore.WiFiReporter = (*demoWiFi)(nil)

func newDemoWiFi() *demoWiFi {
	w := &demoWiFi{}
	w.seed()
	return w
}

// seed sets the counters a link that has been up a while shows.
func (w *demoWiFi) seed() {
	w.beaconLost, w.beaconRx = 2, 1200
	w.multicastRx, w.multicastTx = 340, 25
	w.unicastRx, w.unicastTx = 5600, 4100
	w.overrun = 0
}

// nodeConfig is the node's Network.WiFi for the simulated link: PKTCNT and
// ERRCNT, and CurrentMaxRate.
func (w *demoWiFi) nodeConfig() node.WiFi {
	return node.WiFi{
		Reporter:   w,
		Features:   wifidiagdef.FeaturePacketCounts | wifidiagdef.FeatureErrorCounts,
		Attributes: []uint32{wifidiagdef.AttrCurrentMaxRate},
	}
}

// attach hands the link the server the node mounted (nil without --wifi).
func (w *demoWiFi) attach(srv *mattercore.WiFiNetworkDiagnostics) {
	w.mu.Lock()
	w.srv = srv
	w.mu.Unlock()
}

func (w *demoWiFi) count32(p *uint32) (uint32, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return *p, true
}

// BSSID implements mattercore.WiFiReporter.
func (*demoWiFi) BSSID() ([]byte, bool) { return demoWiFiBSSID, true }

// SecurityType implements mattercore.WiFiReporter: WPA2.
func (*demoWiFi) SecurityType() (wifidiagdef.SecurityTypeEnum, bool) {
	return wifidiagdef.SecurityTypeWpa2, true
}

// WiFiVersion implements mattercore.WiFiReporter: 802.11ax.
func (*demoWiFi) WiFiVersion() (wifidiagdef.WiFiVersionEnum, bool) {
	return wifidiagdef.WiFiVersionAx, true
}

// ChannelNumber implements mattercore.WiFiReporter: channel 36 (5 GHz).
func (*demoWiFi) ChannelNumber() (uint16, bool) { return 36, true }

// RSSI implements mattercore.WiFiReporter.
func (*demoWiFi) RSSI() (int8, bool) { return -52, true }

// CurrentMaxRate implements mattercore.WiFiReporter: 2x2 HE80, in bit/s.
func (*demoWiFi) CurrentMaxRate() (uint64, bool) { return 1_201_000_000, true }

// BeaconLostCount implements mattercore.WiFiReporter.
func (w *demoWiFi) BeaconLostCount() (uint32, bool) { return w.count32(&w.beaconLost) }

// BeaconRxCount implements mattercore.WiFiReporter.
func (w *demoWiFi) BeaconRxCount() (uint32, bool) { return w.count32(&w.beaconRx) }

// PacketMulticastRxCount implements mattercore.WiFiReporter.
func (w *demoWiFi) PacketMulticastRxCount() (uint32, bool) { return w.count32(&w.multicastRx) }

// PacketMulticastTxCount implements mattercore.WiFiReporter.
func (w *demoWiFi) PacketMulticastTxCount() (uint32, bool) { return w.count32(&w.multicastTx) }

// PacketUnicastRxCount implements mattercore.WiFiReporter.
func (w *demoWiFi) PacketUnicastRxCount() (uint32, bool) { return w.count32(&w.unicastRx) }

// PacketUnicastTxCount implements mattercore.WiFiReporter.
func (w *demoWiFi) PacketUnicastTxCount() (uint32, bool) { return w.count32(&w.unicastTx) }

// OverrunCount implements mattercore.WiFiReporter.
func (w *demoWiFi) OverrunCount() (uint64, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.overrun, true
}

// ResetCounts implements mattercore.WiFiReporter: every counter to zero.
func (w *demoWiFi) ResetCounts() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.beaconLost, w.beaconRx = 0, 0
	w.multicastRx, w.multicastTx = 0, 0
	w.unicastRx, w.unicastTx = 0, 0
	w.overrun = 0
	w.resets++
}

// step moves every counter as some traffic would.
func (w *demoWiFi) step() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.beaconLost++
	w.beaconRx += 10
	w.multicastRx += 3
	w.multicastTx++
	w.unicastRx += 40
	w.unicastTx += 30
	w.overrun++
}

// errNoWiFi: a Wi-Fi command for a daemon started without --wifi.
var errNoWiFi = errors.New("WiFiNetworkDiagnostics is not mounted (start the daemon with --wifi)")

func (w *demoWiFi) server() (*mattercore.WiFiNetworkDiagnostics, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.srv == nil {
		return nil, errNoWiFi
	}
	return w.srv, nil
}

// pipeCommand applies the app pipe's WiFiDiagnostics command: Action
// Disconnection (ReasonCode), AssociationFailure (AssociationFailureCause,
// Status), ConnectionStatus (ConnectionStatus) — each defaulting to the
// value CHIP's all-clusters app raises — or StepCounters.
func (w *demoWiFi) pipeCommand(cmd pipeCommand) error {
	srv, err := w.server()
	if err != nil {
		return err
	}
	switch cmd.Action {
	case "Disconnection":
		reason := wifiStubReasonCode
		if cmd.ReasonCode != nil {
			reason = *cmd.ReasonCode
		}
		return srv.OnDisconnectionDetected(reason)
	case "AssociationFailure":
		cause, status := wifiStubAssocCause, wifiStubAssocStatus
		if cmd.AssociationFailureCause != nil {
			cause = wifidiagdef.AssociationFailureCauseEnum(*cmd.AssociationFailureCause)
		}
		if cmd.Status != nil {
			status = *cmd.Status
		}
		return srv.OnAssociationFailureDetected(cause, status)
	case "ConnectionStatus":
		status := wifiStubConnStatus
		if cmd.ConnectionStatus != nil {
			status = wifidiagdef.ConnectionStatusEnum(*cmd.ConnectionStatus)
		}
		return srv.OnConnectionStatusChanged(status)
	case "StepCounters":
		w.step()
		return nil
	}
	return fmt.Errorf("WiFiDiagnostics action %q: %w", cmd.Action, errUnknownPipeCommand)
}

// testEventTrigger raises the event of one of CHIP's Wi-Fi triggers with
// the all-clusters app's values; false for any other trigger, or without
// --wifi, as CHIP's handler declines a trigger it does not know
// (WiFiDiagnosticsTestEventTriggerHandler.h :67-75).
func (w *demoWiFi) testEventTrigger(trigger uint64) (bool, error) {
	var action string
	switch trigger & smokeTriggerMask {
	case triggerWiFiDisconnection:
		action = "Disconnection"
	case triggerWiFiAssociationFailure:
		action = "AssociationFailure"
	case triggerWiFiConnectionStatus:
		action = "ConnectionStatus"
	default:
		return false, nil
	}
	if _, err := w.server(); err != nil {
		return false, nil //nolint:nilerr // without --wifi the trigger is not this handler's
	}
	return true, w.pipeCommand(pipeCommand{Action: action})
}
