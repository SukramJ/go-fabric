// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/internal/netwatch"
)

// The cases of matter.js packages/node/test/behaviors/general-diagnostics/
// GeneralDiagnosticsNetworkInterfacesTest.ts at db021da0 (#4842), ported by
// meaning: matter.js's node is online while its Network is observed; the Go
// server compares the interfaces while its attribute changes are observed
// (the bridge's listener on the root endpoint), on the fake clock.

type fakeInterfaces struct {
	mu   sync.Mutex
	list []NetworkInterfaceStruct
}

func (f *fakeInterfaces) get() []NetworkInterfaceStruct {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]NetworkInterfaceStruct, len(f.list))
	for i, e := range f.list {
		e.IPv6Addresses = append([][]byte(nil), e.IPv6Addresses...)
		out[i] = e
	}
	return out
}

func (f *fakeInterfaces) addIPv6(addr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.list[0].IPv6Addresses = append(f.list[0].IPv6Addresses, net.ParseIP(addr).To16())
}

func networkInterfacesDevice(t *testing.T) (*GeneralDiagnostics, *fakeInterfaces, *clocktest.Fake) {
	t.Helper()
	clk := clocktest.New(time.Unix(1_000_000, 0))
	g := NewGeneralDiagnostics(BootReasonPowerOnReboot)
	g.SetClock(clk)
	ifaces := &fakeInterfaces{list: []NetworkInterfaceStruct{{
		Name: "eth0", IsOperational: true, InterfaceType: InterfaceTypeEthernet,
		HardwareAddress: []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01},
		IPv4Addresses:   [][]byte{net.ParseIP("10.0.0.2").To4()},
		IPv6Addresses:   [][]byte{net.ParseIP("fe80::1").To16()},
	}}}
	g.interfaces = ifaces.get
	return g, ifaces, clk
}

func ipv6Count(t *testing.T, g *GeneralDiagnostics) int {
	t.Helper()
	v, ok := g.MatterRead(gendiagAttrNetworkInterfaces)
	if !ok {
		t.Fatal("NetworkInterfaces is not served")
	}
	return len(v.([]NetworkInterfaceStruct)[0].IPv6Addresses)
}

// TestGeneralDiagnosticsNetworkInterfaces: an address change reaches a
// subscriber of NetworkInterfaces within one interface check, with the
// DataVersion advanced first (GeneralDiagnosticsServer.ts
// #interfacesChanged :447-457 at db021da0); while nobody observes, no
// check runs, and the attribute reads the current interfaces when read.
func TestGeneralDiagnosticsNetworkInterfaces(t *testing.T) {
	t.Parallel()

	t.Run("follows an address change while online", func(t *testing.T) {
		t.Parallel()
		g, ifaces, clk := networkInterfacesDevice(t)
		var mu sync.Mutex
		var changes []contract.Change
		unsub := g.OnAttributeChange(func(c contract.Change) {
			mu.Lock()
			changes = append(changes, c)
			mu.Unlock()
		})
		defer unsub()
		before, version := ipv6Count(t, g), g.MatterDataVersion()

		ifaces.addIPv6("2001:db8::99")
		clk.Advance(netwatch.DefaultInterval)

		mu.Lock()
		defer mu.Unlock()
		if len(changes) != 1 || len(changes[0].Attributes) != 1 || changes[0].Attributes[0] != gendiagAttrNetworkInterfaces {
			t.Fatalf("changes %+v, want one of NetworkInterfaces", changes)
		}
		if g.MatterDataVersion() == version {
			t.Error("DataVersion did not advance")
		}
		if got := ipv6Count(t, g); got != before+1 {
			t.Errorf("IPv6 addresses %d, want %d", got, before+1)
		}
	})

	t.Run("skips address changes while offline and reads the interfaces again when the node comes online", func(t *testing.T) {
		t.Parallel()
		g, ifaces, clk := networkInterfacesDevice(t)
		var mu sync.Mutex
		fired := 0
		unsub := g.OnAttributeChange(func(contract.Change) {
			mu.Lock()
			fired++
			mu.Unlock()
		})
		before := ipv6Count(t, g)
		unsub()
		unsub() // idempotent

		ifaces.addIPv6("2001:db8::99")
		clk.Advance(2 * netwatch.DefaultInterval)
		mu.Lock()
		if fired != 0 {
			t.Errorf("%d changes reported while unobserved", fired)
		}
		mu.Unlock()
		if g.ifWatch.Running() {
			t.Error("the interface check runs without an observer")
		}

		// Coming online reads the interfaces again.
		defer g.OnAttributeChange(func(contract.Change) {})()
		if got := ipv6Count(t, g); got != before+1 {
			t.Errorf("IPv6 addresses %d, want %d", got, before+1)
		}
	})

	t.Run("the host disables the interface check", func(t *testing.T) {
		t.Parallel()
		g, ifaces, clk := networkInterfacesDevice(t)
		g.SetInterfaceCheckInterval(0)
		fired := false
		defer g.OnAttributeChange(func(contract.Change) { fired = true })()
		ifaces.addIPv6("2001:db8::99")
		clk.Advance(2 * netwatch.DefaultInterval)
		if fired {
			t.Error("a disabled check reported a change")
		}
	})
}
