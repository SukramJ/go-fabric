// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
)

// TestControllerAdvertiser: a record published again under its instance
// and type replaces the first; Withdraw removes one and ignores a miss;
// Active is a copy; the operational record answers for its fabric and
// node; Close drops everything.
func TestControllerAdvertiser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := &Site{}
	addr := &net.UDPAddr{IP: net.ParseIP("fd00::7"), Port: 5540}
	a := s.controllerAdvertiser(1, addr)
	if got := s.controllerAdvertisers(); len(got) != 1 || got[0] != a {
		t.Fatalf("site advertisers %v", got)
	}

	cfid := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	operational := mdns.Service{InstanceName: strings.ToLower("0102030405060708-0000000000000042"), ServiceType: mdns.ServiceTypeOperational, Port: 5540}
	commissioner := mdns.Service{InstanceName: "ctrl", ServiceType: mdns.ServiceTypeCommissioner, Port: 5540}
	for _, svc := range []mdns.Service{operational, commissioner, {InstanceName: "ctrl", ServiceType: mdns.ServiceTypeCommissioner, Port: 5541}} {
		if err := a.Publish(ctx, svc); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}
	active := a.Active()
	if len(active) != 2 || active[1].Port != 5541 {
		t.Fatalf("active %+v, want the commissioner record replaced", active)
	}
	active[0].InstanceName = "changed"
	if a.Active()[0].InstanceName == "changed" {
		t.Fatal("Active is not a copy")
	}

	if !a.answersOperational(cfid, 0x42) || a.answersOperational(cfid, 0x43) {
		t.Fatal("answersOperational")
	}
	got, err := siteResolver{s: s}.ResolveOperational(ctx, cfid, 0x42)
	if err != nil || len(got) != 1 || got[0] != addr {
		t.Fatalf("ResolveOperational = %v, %v; want the advertiser's address", got, err)
	}
	if _, err := (siteResolver{s: s}).ResolveOperational(ctx, cfid, 0x43); err == nil {
		t.Fatal("ResolveOperational of an unknown node: want an error")
	}

	if err := a.Withdraw(ctx, "missing", mdns.ServiceTypeCommissioner); err != nil || len(a.Active()) != 2 {
		t.Fatalf("Withdraw of a missing record: %v, %d left", err, len(a.Active()))
	}
	if err := a.Withdraw(ctx, operational.InstanceName, mdns.ServiceTypeOperational); err != nil || len(a.Active()) != 1 {
		t.Fatalf("Withdraw: %v, %d left", err, len(a.Active()))
	}
	if a.answersOperational(cfid, 0x42) {
		t.Fatal("a withdrawn record still answers")
	}
	if err := a.Close(); err != nil || len(a.Active()) != 0 {
		t.Fatalf("Close: %v, %d left", err, len(a.Active()))
	}
}

// TestEventStatusError names the event path and unwraps to the
// *StatusError.
func TestEventStatusError(t *testing.T) {
	t.Parallel()
	err := error(&eventStatusError{StatusError: StatusError{Status: im.StatusUnsupportedEvent}, path: EventPath{Endpoint: 1, Cluster: 0x28, Event: 2}})
	if !strings.HasSuffix(err.Error(), " for event 1/0x0028/0x02") {
		t.Fatalf("Error() = %q", err.Error())
	}
	var st *StatusError
	if !errors.As(err, &st) || st.Status != im.StatusUnsupportedEvent {
		t.Fatalf("errors.As: %v", st)
	}
}
