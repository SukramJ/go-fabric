// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behavior/system/commissioning/RemoteDescriptorTest.ts
// at 862ad75f.
//
// matter.js's RemoteDescriptor converts between the short form a scanner
// reports (CommissionableDevice, DiscoveryData) and the long form
// CommissioningClient stores. The Go long form is the Record; toLongForm
// over a commissionable device is Record.applyDevice, over bare discovery
// data Record.applyTXT, and fromLongForm is Record.device (the
// commissionable device) and Record.txt (its TXT keys). A Go discovery
// device holds the discriminator and commissioning mode as numbers, so
// "absent" is their zero.
package client

import (
	"net/netip"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

func rdAddr(ip string) discovery.Address {
	return discovery.Address{IP: netip.MustParseAddr(ip), Port: 5540}
}

func rdMs(ms float64) *time.Duration {
	d := time.Duration(ms * float64(time.Millisecond))
	return &d
}

func rdString(s string) *string { return &s }

// toLongForm is RemoteDescriptor.toLongForm of a commissionable device onto
// r (a fresh record when nil).
func toLongForm(d discovery.Device, r *Record) *Record {
	if r == nil {
		r = &Record{}
	}
	r.applyDevice(&d, time.Unix(0, 0))
	return r
}

func ips(d *discovery.Device) []string {
	out := make([]string, 0, len(d.Addresses))
	for _, a := range d.Addresses {
		out = append(out, a.IP.String())
	}
	slices.Sort(out)
	return out
}

// TestRemoteDescriptorRoundtripParityMatterJS ports the describe
// "toLongForm / fromLongForm roundtrip" (RemoteDescriptorTest.ts:17-159).
func TestRemoteDescriptorRoundtripParityMatterJS(t *testing.T) {
	t.Parallel()

	// :18-32.
	t.Run("preserves addresses", func(t *testing.T) {
		t.Parallel()
		back := toLongForm(discovery.Device{InstanceID: "abc", Addresses: []discovery.Address{rdAddr("fd00::1"), rdAddr("fd00::2")}, D: 1234, CM: 1}, nil).device()
		if got := ips(back); !slices.Equal(got, []string{"fd00::1", "fd00::2"}) {
			t.Errorf("addresses %v", got)
		}
	})

	// :34-46.
	t.Run("preserves deviceIdentifier", func(t *testing.T) {
		t.Parallel()
		back := toLongForm(discovery.Device{InstanceID: "test-device-123", Addresses: []discovery.Address{rdAddr("fd00::1")}, D: 100, CM: 1}, nil).device()
		if back.InstanceID != "test-device-123" {
			t.Errorf("deviceIdentifier %q", back.InstanceID)
		}
	})

	// :48-62.
	t.Run("preserves hostname", func(t *testing.T) {
		t.Parallel()
		long := toLongForm(discovery.Device{InstanceID: "d", Hostname: "0011223344550000", Addresses: []discovery.Address{rdAddr("fd00::1")}, D: 100, CM: 1}, nil)
		if long.Hostname != "0011223344550000" || long.device().Hostname != "0011223344550000" {
			t.Errorf("hostname %q, back %q", long.Hostname, long.device().Hostname)
		}
	})

	// :64-81. A device whose SRV expired reports no host.
	t.Run("clears a hostname the device no longer names, rather than keeping the last one", func(t *testing.T) {
		t.Parallel()
		long := toLongForm(discovery.Device{InstanceID: "d", Hostname: "0011223344550000", Addresses: []discovery.Address{rdAddr("fd00::1")}, D: 100, CM: 1}, nil)
		toLongForm(discovery.Device{InstanceID: "d", Addresses: []discovery.Address{rdAddr("fd00::1")}, D: 100, CM: 1}, long)
		if long.Hostname != "" {
			t.Errorf("hostname %q, want none", long.Hostname)
		}
	})

	// :83-97. Bare discovery data names no host.
	t.Run("leaves a known host alone for a caller that carries no host at all", func(t *testing.T) {
		t.Parallel()
		long := toLongForm(discovery.Device{InstanceID: "d", Hostname: "0011223344550000", Addresses: []discovery.Address{rdAddr("fd00::1")}, D: 100, CM: 1}, nil)
		long.applyTXT(discovery.TXTData{DN: rdString("a device")})
		if long.Hostname != "0011223344550000" {
			t.Errorf("hostname %q, want the known one", long.Hostname)
		}
	})

	// :99-112.
	t.Run("preserves discriminator (D field)", func(t *testing.T) {
		t.Parallel()
		back := toLongForm(discovery.Device{InstanceID: "d", Addresses: []discovery.Address{rdAddr("fd00::1")}, D: 3840, CM: 2}, nil).device()
		if back.D != 3840 || back.CM != 2 {
			t.Errorf("D %d CM %d", back.D, back.CM)
		}
	})

	// :114-127.
	t.Run("preserves vendor/product from VP field", func(t *testing.T) {
		t.Parallel()
		d := discovery.Device{InstanceID: "vp", Addresses: []discovery.Address{rdAddr("fd00::1")}, D: 0, CM: 1}
		d.VP = rdString("4631+24576")
		back := toLongForm(d, nil).device()
		if back.VP == nil || *back.VP != "4631+24576" {
			t.Errorf("VP %v", back.VP)
		}
	})

	// :129-132.
	t.Run("returns empty object for all-undefined input", func(t *testing.T) {
		t.Parallel()
		var r Record
		if txt := r.txt(); !reflect.DeepEqual(txt, discovery.TXTData{}) {
			t.Errorf("TXT %+v, want none", txt)
		}
		if back := r.device(); back.InstanceID != "" || back.Hostname != "" || len(back.Addresses) != 0 || back.D != 0 || back.CM != 0 {
			t.Errorf("device %+v, want nothing", back)
		}
	})

	// :134-158.
	t.Run("minimal CommissionableDevice with only required fields roundtrips correctly", func(t *testing.T) {
		t.Parallel()
		long := toLongForm(discovery.Device{InstanceID: "minimal", Addresses: []discovery.Address{rdAddr("10.0.0.1")}, D: 1000, CM: 1}, nil)
		if len(long.Addresses) != 1 || long.Addresses[0].IP != netip.MustParseAddr("10.0.0.1") || long.Addresses[0].Port != 5540 {
			t.Errorf("addresses %+v", long.Addresses)
		}
		if long.DeviceIdentifier != "minimal" || long.Discriminator == nil || *long.Discriminator != 1000 || long.CommissioningMode != 1 {
			t.Errorf("long form %+v", long)
		}
		back := long.device()
		if got := ips(back); !slices.Equal(got, []string{"10.0.0.1"}) || back.InstanceID != "minimal" || back.D != 1000 || back.CM != 1 {
			t.Errorf("back %+v", back)
		}
	})
}

// TestRemoteDescriptorSessionIntervalsParityMatterJS ports the describe
// "session intervals" (RemoteDescriptorTest.ts:161-228).
func TestRemoteDescriptorSessionIntervalsParityMatterJS(t *testing.T) {
	t.Parallel()
	reported := sessionparams.Parameters{SessionIdleInterval: uint32(2 * time.Hour / time.Millisecond), SessionActiveInterval: 400, SessionActiveThreshold: 5000}

	// :162-184.
	t.Run("stores advertised intervals apart from the session parameters", func(t *testing.T) {
		t.Parallel()
		p := reported
		long := &Record{PeerAddress: &handshake.PeerAddress{FabricIndex: 1, NodeID: 1}, ReportedSessionParameters: &p}
		long.applyTXT(discovery.TXTData{SII: rdMs(500), SAI: rdMs(300), SAT: rdMs(4000)})
		want := &AdvertisedIntervals{IdleInterval: rdMs(500), ActiveInterval: rdMs(300), ActiveThreshold: rdMs(4000)}
		if !reflect.DeepEqual(long.AdvertisedIntervals, want) {
			t.Errorf("advertised %+v", long.AdvertisedIntervals)
		}
		if *long.ReportedSessionParameters != reported {
			t.Errorf("reported %+v, want %+v", *long.ReportedSessionParameters, reported)
		}
	})

	// :186-199.
	t.Run("reports only advertised intervals as SII/SAI/SAT", func(t *testing.T) {
		t.Parallel()
		p := reported
		dd := (&Record{ReportedSessionParameters: &p, AdvertisedIntervals: &AdvertisedIntervals{IdleInterval: rdMs(500)}}).txt()
		if dd.SII == nil || *dd.SII != 500*time.Millisecond || dd.SAI != nil || dd.SAT != nil {
			t.Errorf("SII %v SAI %v SAT %v", dd.SII, dd.SAI, dd.SAT)
		}
	})

	// :201-205.
	t.Run("ignores intervals beyond what DNS-SD can advertise", func(t *testing.T) {
		t.Parallel()
		var long Record
		long.applyTXT(discovery.TXTData{SII: rdMs(3_602_000), SAI: rdMs(300), SAT: rdMs(70_000)})
		if want := (&AdvertisedIntervals{ActiveInterval: rdMs(300)}); !reflect.DeepEqual(long.AdvertisedIntervals, want) {
			t.Errorf("advertised %+v, want the active interval alone", long.AdvertisedIntervals)
		}
	})

	// :207-219.
	t.Run("drops stored intervals DNS-SD cannot advertise", func(t *testing.T) {
		t.Parallel()
		dd := (&Record{AdvertisedIntervals: &AdvertisedIntervals{IdleInterval: rdMs(500.5), ActiveInterval: rdMs(300), ActiveThreshold: rdMs(0)}}).txt()
		if dd.SAI == nil || *dd.SAI != 300*time.Millisecond || dd.SII != nil || dd.SAT != nil {
			t.Errorf("SII %v SAI %v SAT %v", dd.SII, dd.SAI, dd.SAT)
		}
	})

	// :221-227.
	t.Run("clears advertised intervals a node no longer advertises", func(t *testing.T) {
		t.Parallel()
		var long Record
		long.applyTXT(discovery.TXTData{SII: rdMs(500), SAI: rdMs(300), SAT: rdMs(4000)})
		long.applyTXT(discovery.TXTData{})
		if long.AdvertisedIntervals != nil {
			t.Errorf("advertised %+v, want none", long.AdvertisedIntervals)
		}
	})
}
