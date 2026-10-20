// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/bridge"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// The facade's re-exported errors are the bridge's own values.
func TestFacadeErrorsAreTheBridges(t *testing.T) {
	for _, c := range []struct {
		name         string
		facade, base error
	}{
		{"AdministratorWindowOpen", node.ErrAdministratorWindowOpen, bridge.ErrAdministratorWindowOpen},
		{"CommissioningBlocked", node.ErrCommissioningBlocked, bridge.ErrCommissioningBlocked},
		{"CommissioningWindowAlreadyOpen", node.ErrCommissioningWindowAlreadyOpen, bridge.ErrCommissioningWindowAlreadyOpen},
		{"CommissioningWindowNotConfigured", node.ErrCommissioningWindowNotConfigured, bridge.ErrCommissioningWindowNotConfigured},
	} {
		if c.facade == nil || !errors.Is(c.facade, c.base) || !errors.Is(c.base, c.facade) {
			t.Errorf("%s: node's %v is not bridge's %v", c.name, c.facade, c.base)
		}
	}
}

// The commissioning bounds come from secure/setup and secure/spake2.
func TestFacadeCommissioningBounds(t *testing.T) {
	if node.MaxDiscriminator != setup.MaxDiscriminator ||
		node.PBKDFIterationsMin != spake2.IterationsMin || node.PBKDFIterationsMax != spake2.IterationsMax ||
		node.PBKDFSaltMin != spake2.PBKDFMinSaltSize || node.PBKDFSaltMax != spake2.PBKDFMaxSaltSize {
		t.Fatal("a node commissioning bound differs from its source")
	}
	if node.RegulatoryIndoor != mattercore.RegulatoryIndoor || node.RegulatoryOutdoor != mattercore.RegulatoryOutdoor ||
		node.RegulatoryIndoorOutdoor != mattercore.RegulatoryIndoorOutdoor {
		t.Fatal("a regulatory location differs from cluster/core's")
	}
	if got, want := node.SoftwareVersionFromString("v1.2.3"), mattercore.SoftwareVersionFromString("v1.2.3"); got != want {
		t.Fatalf("SoftwareVersionFromString %d, want %d", got, want)
	}
	for _, c := range []struct {
		pin uint32
		ok  bool
	}{
		{testPasscode, true},
		{1, true},
		{99999998, true},
		{0, false},
		{12345678, false},
		{11111111, false},
		{99999999, false},
		{100000000, false},
	} {
		err := node.ValidateSetupPIN(c.pin)
		if (err == nil) != c.ok {
			t.Errorf("ValidateSetupPIN(%d) = %v, want ok %v", c.pin, err, c.ok)
		}
		if err != nil && !errors.Is(err, node.ErrInvalidConfig) {
			t.Errorf("ValidateSetupPIN(%d) = %v, not ErrInvalidConfig", c.pin, err)
		}
	}
}

// The aliases are the lower packages' types: the accessors' values and the
// Config fields pass to code written against the originals unconverted.
func TestFacadeAliases(t *testing.T) {
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "n.db"))))
	if !bridgeWindow(n.CommissioningWindow()) || !operationalManager(n.Sessions()) || !subscriptionManager(n.Subscriptions()) {
		t.Fatal("an accessor returned nil")
	}
	var snap bridge.Snapshotter
	cfg := node.Config{Topology: snap, Network: node.Network{TCP: bridge.TCPConfig{Incoming: true}}}
	if cfg.Topology != nil || !cfg.Network.TCP.Incoming {
		t.Fatal("Config fields did not take the bridge's types")
	}
	if endpointTopology(&node.Topology{}) == nil || len(diagEvent(node.DiagnosticEvent{Message: "m"}).Message) != 1 {
		t.Fatal("Topology or DiagnosticEvent did not pass as the original")
	}
	var probe endpoint.AvailabilityProbe = func() bool { return true }
	opts := device.Options{StableKey: endpoint.StringKey("plug"), Reachable: probe}
	if deviceKey(opts.StableKey).String() != "plug" || !deviceProbe(opts.Reachable)() {
		t.Fatal("device.Options did not take the endpoint types")
	}
}

func bridgeWindow(w *bridge.CommissioningWindow) bool          { return w != nil }
func operationalManager(m *operational.Manager) bool           { return m != nil }
func subscriptionManager(m *subscription.Manager) bool         { return m != nil }
func endpointTopology(t *endpoint.Topology) *endpoint.Topology { return t }
func diagEvent(e diagevent.Event) diagevent.Event              { return e }
func deviceKey(k device.SourceKey) endpoint.SourceKey          { return k }
func deviceProbe(p device.AvailabilityProbe) func() bool       { return p }
