// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns_test

// The commissioner cases of matter.js packages/protocol/test/mdns/
// MdnsTest.ts at 862ad75f (:546, :637, :714): the `_matterd._udp` record
// BuildCommissionerService yields carries the instance, the PTR subtypes
// and the TXT values CommissionerMdnsAdvertisement broadcasts. The packet
// itself (record order, cache-flush bits, the _services._dns-sd PTRs) is
// grandcat/zeroconf's, as for the other two record kinds.

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/mdns"
)

// testCommissioner is MdnsTest.ts:549-554: ServiceDescription.Commissioner
// with name "Test Commissioner", device type 1, vendor 1, product 0x8000,
// on the instance 8080808080808080 the test's mock crypto yields.
func testCommissioner(masked bool) mdns.CommissionerServiceConfig {
	return mdns.CommissionerServiceConfig{
		InstanceID:    [8]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80},
		DeviceName:    "Test Commissioner",
		DeviceTypeID:  1,
		VendorID:      1,
		ProductID:     0x8000,
		PrivacyMasked: masked,
		Port:          5540,
		HostName:      "00B0D063C2260000",
	}
}

func TestCommissionerAdvertisementParity(t *testing.T) {
	t.Parallel()
	t.Run("it broadcasts the controller commissioner on one port", func(t *testing.T) {
		t.Parallel()
		svc := mdns.BuildCommissionerService(testCommissioner(false))
		if err := svc.Validate(); err != nil {
			t.Fatal(err)
		}
		// :563-565, :586-594: the SRV/TXT owner and the service PTR.
		if got := svc.FQDN(); got != "8080808080808080._matterd._udp.local." {
			t.Errorf("FQDN %q", got)
		}
		if svc.Port != 5540 || svc.HostName != "00B0D063C2260000" {
			t.Errorf("SRV port %d target %q", svc.Port, svc.HostName)
		}
		// :576: ["VP=1+32768", "DT=1", "DN=Test Commissioner"]; the order
		// of TXT strings carries no meaning (MarshalTXT sorts).
		want := []string{"DN=Test Commissioner", "DT=1", "VP=1+32768"}
		if got := svc.MarshalTXT(); !slices.Equal(got, want) {
			t.Errorf("TXT %v, want %v", got, want)
		}
		// :595-626: the _V1 and _T1 subtypes.
		if !slices.Equal(svc.Subtypes, []string{"_V1", "_T1"}) {
			t.Errorf("subtypes %v, want [_V1 _T1]", svc.Subtypes)
		}
	})
	t.Run("it omits vendor details from commissioner broadcasts when privacy masked", func(t *testing.T) {
		t.Parallel()
		svc := mdns.BuildCommissionerService(testCommissioner(true))
		// :667: ["DT=1", "DN=Test Commissioner"]; :686-701: _T1 only.
		if got, want := svc.MarshalTXT(), []string{"DN=Test Commissioner", "DT=1"}; !slices.Equal(got, want) {
			t.Errorf("TXT %v, want %v", got, want)
		}
		if !slices.Equal(svc.Subtypes, []string{"_T1"}) {
			t.Errorf("subtypes %v, want [_T1]", svc.Subtypes)
		}
	})
	t.Run("omits DT and the _T subtype without a device type", func(t *testing.T) {
		t.Parallel()
		// CommissionerMdnsAdvertisement.ts:47-52 (deviceType undefined)
		// and the undefined filter of MdnsAdvertisement.ts:160-163.
		cfg := testCommissioner(false)
		cfg.DeviceTypeID = 0
		svc := mdns.BuildCommissionerService(cfg)
		if got, want := svc.MarshalTXT(), []string{"DN=Test Commissioner", "VP=1+32768"}; !slices.Equal(got, want) {
			t.Errorf("TXT %v, want %v", got, want)
		}
		if !slices.Equal(svc.Subtypes, []string{"_V1"}) {
			t.Errorf("subtypes %v, want [_V1]", svc.Subtypes)
		}
	})
	t.Run("it allows announcements of multiple devices on different ports", func(t *testing.T) {
		t.Parallel()
		// :714-737: an operational, a commissionable and a commissioner
		// record side by side, each under its own service type.
		adv := mdns.NewNoop()
		ctx := context.Background()
		op := mdns.BuildOperationalService(mdns.OperationalServiceConfig{Port: 5540, HostName: "00B0D063C2260000"})
		com := mdns.BuildCommissionableService(mdns.CommissionableServiceConfig{Port: 5541, HostName: "00B0D063C2260000", VendorID: 1, ProductID: 0x8000, DeviceTypeID: 1, Discriminator: 0xf00, CommissioningMode: 1})
		cfg := testCommissioner(false)
		cfg.Port = 5542
		ctl := mdns.BuildCommissionerService(cfg)
		for _, svc := range []mdns.Service{op, com, ctl} {
			if err := adv.Publish(ctx, svc); err != nil {
				t.Fatal(err)
			}
		}
		types := map[string]uint16{}
		for _, svc := range adv.Active() {
			types[svc.ServiceType] = svc.Port
		}
		want := map[string]uint16{mdns.ServiceTypeOperational: 5540, mdns.ServiceTypeCommissionable: 5541, mdns.ServiceTypeCommissioner: 5542}
		for k, v := range want {
			if types[k] != v {
				t.Errorf("%s on port %d, want %d (active %v)", k, types[k], v, types)
			}
		}
		if err := adv.Withdraw(ctx, ctl.InstanceName, mdns.ServiceTypeCommissioner); err != nil {
			t.Fatal(err)
		}
		if n := len(adv.Active()); n != 2 {
			t.Errorf("%d records after withdrawing the commissioner, want 2", n)
		}
	})
}
