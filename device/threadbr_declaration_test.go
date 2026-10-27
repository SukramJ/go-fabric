// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tbrmdef "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement"
	tnddef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/cluster/threadbr"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/thread/otbr"
	"github.com/SukramJ/go-fabric/thread/otbr/otbrtest"
)

func fakeBorderRouter(t *testing.T) *otbr.BorderRouter {
	t.Helper()
	srv := otbrtest.New(otbrtest.Options{})
	t.Cleanup(srv.Close)
	c, err := otbr.NewClient(otbr.Config{Host: srv.Host(), Port: srv.Port(), Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	br, err := otbr.NewBorderRouter(c, otbr.BorderRouterConfig{Name: "kit OTBR", ThreadVersion: 5})
	if err != nil {
		t.Fatal(err)
	}
	return br
}

// TestThreadBorderRouterDeclaresTheThreadbrServers: a ThreadBorderRouter
// whose ThreadBorderRouterManagement declaration carries a BorderRouter
// mounts cluster/threadbr's two servers on it, with the selected
// features; without one both clusters keep the generated server.
func TestThreadBorderRouterDeclaresTheThreadbrServers(t *testing.T) {
	t.Parallel()
	br := fakeBorderRouter(t)
	d, err := device.NewThreadBorderRouter(device.Options{StableKey: device.StringKey("tbr"), Name: "tbr"},
		tbrmdef.Declare{BorderRouter: br, Features: tbrmdef.FeaturePanChange},
		tnddef.Declare{Features: threadbr.DiagnosticsFeatures})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := d.ThreadBorderRouterManagement().Underlying().(*threadbr.ManagementServer)
	if !ok {
		t.Fatalf("ThreadBorderRouterManagement is %T", d.ThreadBorderRouterManagement().Underlying())
	}
	if m.BorderRouter() != br {
		t.Error("the management server runs on another border router")
	}
	st := d.ThreadBorderRouterManagement().Get()
	if st.BorderRouterName != "kit OTBR" || len(st.BorderAgentId) != 16 || st.ThreadVersion != 5 || st.InterfaceEnabled || st.ActiveDatasetTimestamp != nil {
		t.Errorf("management state %+v", st)
	}
	if _, ok := d.ThreadNetworkDiagnostics().Underlying().(*threadbr.DiagnosticsServer); !ok {
		t.Fatalf("ThreadNetworkDiagnostics is %T", d.ThreadNetworkDiagnostics().Underlying())
	}
	if fm, _ := d.ThreadNetworkDiagnostics().Underlying().MatterRead(spec.AttrFeatureMap); fm != uint32(threadbr.DiagnosticsFeatures) {
		t.Errorf("diagnostics FeatureMap %v", fm)
	}

	plain, err := device.NewThreadBorderRouter(device.Options{StableKey: device.StringKey("plain"), Name: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.ThreadBorderRouterManagement().Underlying().(*threadbr.ManagementServer); ok {
		t.Error("a border router without a BorderRouter got the kit's server")
	}
	if _, ok := plain.ThreadNetworkDiagnostics().Underlying().(*threadbr.DiagnosticsServer); ok {
		t.Error("diagnostics without a BorderRouter got the kit's server")
	}
}

// A declared diagnostics feature the server does not serve fails the
// device: the host asked for it.
func TestThreadBorderRouterRefusesPacketCounts(t *testing.T) {
	t.Parallel()
	_, err := device.NewThreadBorderRouter(device.Options{StableKey: device.StringKey("tbr"), Name: "tbr"},
		tbrmdef.Declare{BorderRouter: fakeBorderRouter(t)},
		tnddef.Declare{Features: tnddef.FeaturePacketCounts})
	if !errors.Is(err, device.ErrSelectionNotServed) {
		t.Errorf("PacketCounts = %v, want ErrSelectionNotServed", err)
	}
}
