// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"sync"
	"testing"
	"time"

	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// directions is a cover.Mover that records the lift target each command
// hands it.
type directions struct {
	mu    sync.Mutex
	lifts []uint16
	stops int
}

func (d *directions) GoTo(_ context.Context, lift, _ *uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if lift != nil {
		d.lifts = append(d.lifts, *lift)
	}
	return nil
}

func (d *directions) Stop(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stops++
	return nil
}

// TestWindowCoveringLiftAlone: a cover declaring Lift without
// PositionAwareLift builds, serves no position, and hands its Mover the
// direction of UpOrOpen (0) and DownOrClose (10000) and the stop, through
// a controller on the site.
func TestWindowCoveringLiftAlone(t *testing.T) {
	t.Parallel()
	mover := &directions{}
	wc, err := device.NewWindowCovering(device.Options{StableKey: device.StringKey("wc"), Name: "wc"},
		wcdef.Declare{Features: wcdef.FeatureLift, Mover: mover})
	if err != nil {
		t.Fatal(err)
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{wc}}})
	ep, _ := wc.EndpointNumber()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	values, err := fabrictest.ReadCluster(ctx, pair, ep, wcdef.ClusterID)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := wcdef.StateFrom(values)
	if st.CurrentPositionLiftPercent100ths != nil || st.TargetPositionLiftPercent100ths != nil {
		t.Fatalf("a Lift-only cover serves positions: %+v", st)
	}
	for _, cmd := range []uint32{wcdef.CmdUpOrOpen, wcdef.CmdDownOrClose, wcdef.CmdStopMotion} {
		if _, err := fabrictest.Invoke(ctx, pair, client.CommandAt(ep, wcdef.ClusterID, cmd), nil); err != nil {
			t.Fatalf("command 0x%02X: %v", cmd, err)
		}
	}
	mover.mu.Lock()
	defer mover.mu.Unlock()
	if len(mover.lifts) != 2 || mover.lifts[0] != 0 || mover.lifts[1] != 10000 || mover.stops != 1 {
		t.Fatalf("mover lifts %v, stops %d", mover.lifts, mover.stops)
	}
}
