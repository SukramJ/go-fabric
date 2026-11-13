// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"fmt"
	"net"
	"slices"
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

// TestGeneralDiagnosticsNetworkInterfacesSelection: matter.js filters the
// unassigned MAC, puts the operational interfaces first, serves at most 8
// and adds no entry of its own (GeneralDiagnosticsServer.ts:505-518 at
// db021da0; the model's "max 8", general-diagnostics.element.ts:27).
func TestGeneralDiagnosticsNetworkInterfacesSelection(t *testing.T) {
	t.Parallel()

	iface := func(name string, operational bool, lastMACByte byte) NetworkInterfaceStruct {
		return NetworkInterfaceStruct{
			Name: name, IsOperational: operational, InterfaceType: InterfaceTypeEthernet,
			HardwareAddress: []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, lastMACByte},
			IPv6Addresses:   [][]byte{net.ParseIP("fe80::1").To16()},
		}
	}
	read := func(t *testing.T, candidates []NetworkInterfaceStruct) []string {
		t.Helper()
		g := NewGeneralDiagnostics(BootReasonPowerOnReboot)
		g.interfaces = func() []NetworkInterfaceStruct { return candidates }
		v, ok := g.MatterRead(gendiagAttrNetworkInterfaces)
		if !ok {
			t.Fatal("NetworkInterfaces is not served")
		}
		list, ok := v.([]NetworkInterfaceStruct)
		if !ok || list == nil {
			t.Fatalf("NetworkInterfaces = %#v, want a non-nil []NetworkInterfaceStruct", v)
		}
		names := make([]string, 0, len(list))
		for _, e := range list {
			names = append(names, e.Name)
		}
		return names
	}

	t.Run("five interfaces are five entries", func(t *testing.T) {
		t.Parallel()
		var in []NetworkInterfaceStruct
		for i := range 5 {
			in = append(in, iface(fmt.Sprintf("eth%d", i), true, byte(i+1)))
		}
		if got, want := read(t, in), []string{"eth0", "eth1", "eth2", "eth3", "eth4"}; !slices.Equal(got, want) {
			t.Errorf("NetworkInterfaces %v, want %v", got, want)
		}
	})

	t.Run("the list ends after eight entries", func(t *testing.T) {
		t.Parallel()
		var in []NetworkInterfaceStruct
		for i := range 10 {
			in = append(in, iface(fmt.Sprintf("eth%d", i), true, byte(i+1)))
		}
		if got := read(t, in); len(got) != 8 || got[7] != "eth7" {
			t.Errorf("NetworkInterfaces %v, want the first eight", got)
		}
	})

	t.Run("operational interfaces come first, in their listed order", func(t *testing.T) {
		t.Parallel()
		in := []NetworkInterfaceStruct{
			iface("down0", false, 1), iface("up0", true, 2), iface("down1", false, 3), iface("up1", true, 4),
		}
		if got, want := read(t, in), []string{"up0", "up1", "down0", "down1"}; !slices.Equal(got, want) {
			t.Errorf("NetworkInterfaces %v, want %v", got, want)
		}
		// The cut comes after the order: a ninth, operational interface
		// displaces one that is not.
		in = in[:0]
		in = append(in, iface("down", false, 1))
		for i := range 8 {
			in = append(in, iface(fmt.Sprintf("up%d", i), true, byte(i+2)))
		}
		if got := read(t, in); len(got) != 8 || slices.Contains(got, "down") {
			t.Errorf("NetworkInterfaces %v, want the eight operational ones", got)
		}
	})

	t.Run("the unassigned MAC is not listed", func(t *testing.T) {
		t.Parallel()
		unassigned := iface("tun0", true, 0)
		unassigned.HardwareAddress = make([]byte, 6)
		if got, want := read(t, []NetworkInterfaceStruct{unassigned, iface("eth0", true, 1)}), []string{"eth0"}; !slices.Equal(got, want) {
			t.Errorf("NetworkInterfaces %v, want %v", got, want)
		}
	})

	t.Run("no qualifying interface is an empty list", func(t *testing.T) {
		t.Parallel()
		unassigned := iface("tun0", true, 0)
		unassigned.HardwareAddress = make([]byte, 6)
		for name, in := range map[string][]NetworkInterfaceStruct{
			"none": nil, "only the unassigned MAC": {unassigned},
		} {
			if got := read(t, in); len(got) != 0 {
				t.Errorf("%s: NetworkInterfaces %v, want an empty list", name, got)
			}
		}
	})
}
