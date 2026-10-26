// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/mdns"
)

// TestControllerAdvertisesItsCommissionerRecord: Config.Advertise
// publishes the `_matterd._udp` record (matter.js
// CommissionerMdnsAdvertisement.ts:20-71 at 862ad75f) on the controller's
// port, the vendor defaulting to the fabric's, and Close withdraws it.
func TestControllerAdvertisesItsCommissionerRecord(t *testing.T) {
	t.Parallel()
	adv := mdns.NewNoop()
	site, _, c := siteController(t, fabrictest.Topology{}, func(cfg *controller.Config) {
		cfg.Advertise = &controller.Advertise{Name: "go-fabric controller", DeviceType: 0x0016, ProductID: 0x8000}
		cfg.Advertiser = adv
	})
	active := adv.Active()
	if len(active) != 1 {
		t.Fatalf("%d records, want 1", len(active))
	}
	svc := active[0]
	if svc.ServiceType != mdns.ServiceTypeCommissioner || svc.Port == 0 || len(svc.InstanceName) != 16 {
		t.Errorf("record %s/%s port %d", svc.InstanceName, svc.ServiceType, svc.Port)
	}
	vendor := c.Fabric().RootVendorID
	wantTXT := []string{"DN=go-fabric controller", "DT=22", "VP=" + strconv.Itoa(int(vendor)) + "+32768"}
	if got := svc.MarshalTXT(); !slices.Equal(got, wantTXT) {
		t.Errorf("TXT %v, want %v", got, wantTXT)
	}
	if !slices.Equal(svc.Subtypes, []string{"_V" + strconv.Itoa(int(vendor)), "_T22"}) {
		t.Errorf("subtypes %v", svc.Subtypes)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := site.Run(ctx, c.Close); err != nil {
		t.Fatal(err)
	}
	if n := len(adv.Active()); n != 0 {
		t.Errorf("%d records after Close, want 0", n)
	}
}
