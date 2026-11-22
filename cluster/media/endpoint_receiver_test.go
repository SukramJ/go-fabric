// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media_test

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/media"
	targetdef "thingfabric.dev/go-fabric/cluster/spec/targetnavigator"
	"thingfabric.dev/go-fabric/contract"
)

// TestMediaServersStayEndpointReceivers: the servers that embed a
// *spec.Server beside the package's events declare SetEndpoint, so they
// stay contract.EndpointReceivers now that spec.Server has a SetEndpoint
// of its own (an ambiguous promotion would drop it), and the store learns
// the endpoint too.
func TestMediaServersStayEndpointReceivers(t *testing.T) {
	t.Parallel()
	login, err := media.NewAccountLogin(media.AccountLoginConfig{Port: &login{}})
	if err != nil {
		t.Fatal(err)
	}
	control, err := media.NewContentControl(media.ContentControlConfig{Port: &onOffParental{}})
	if err != nil {
		t.Fatal(err)
	}
	targets, err := media.NewTargets(media.TargetsConfig{
		Port: targets{}, Initial: media.TargetState{TargetList: []targetdef.TargetInfoStruct{{Identifier: 1, Name: "home"}}, CurrentTarget: 0xFF},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, srv := range []any{login, control, targets} {
		r, ok := srv.(contract.EndpointReceiver)
		if !ok {
			t.Fatalf("%T is no contract.EndpointReceiver", srv)
		}
		r.SetEndpoint(3)
	}
}
