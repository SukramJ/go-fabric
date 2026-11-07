// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"sync"
	"testing"
	"time"

	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestStandaloneAckBeatsTheControllersRetransmit: on an in-memory site the
// device acknowledges each StatusResponse the controller sends for a
// report — a message it answers with nothing, so with a standalone ACK
// from its pump — before the controller's MRP retransmission is due: no
// datagram reaches the device twice. The pump is still ticking for the
// report it tracked when the StatusResponse arrives; the idle pump's wake
// is held by the bridge's TestAckPump_* tests (brief FP2 review, M3).
func TestStandaloneAckBeatsTheControllersRetransmit(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair, light := observedPair(t, site, fabrictest.NewMetrics(), nil)
	ctx := observeContext(t)
	ep, _ := light.EndpointNumber()
	dev := pair.Device.Addr().String()
	var (
		mu   sync.Mutex
		seen = map[string]bool{}
		dups int
	)
	site.Net().Filter(func(d fabrictest.Datagram) bool {
		if d.To.String() == dev {
			mu.Lock()
			if seen[string(d.Payload)] {
				dups++
			}
			seen[string(d.Payload)] = true
			mu.Unlock()
		}
		return true
	})
	reports := make(chan bool, 16)
	sub, err := fabrictest.Subscribe(ctx, pair, fabrictest.SubscribeRequest{
		Attributes: []fabrictest.AttributePath{client.AttributeAt(ep, onoffdef.ClusterID, onoffdef.AttrOnOff)},
		Updated: func(r *fabrictest.Report) error {
			if _, ok := r.Value(ep, onoffdef.ClusterID, onoffdef.AttrOnOff); ok {
				state, _ := onoffdef.StateFrom(r.Values(ep, onoffdef.ClusterID))
				reports <- state.OnOff
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	for i := range 4 {
		want := i%2 == 0
		if err := pair.Device.Act(func(*node.Node) error { return light.OnOff().Set(onoffdef.Patch{OnOff: &want}) }); err != nil {
			t.Fatal(err)
		}
		if err := site.Eventually(ctx, "the report", func() bool {
			select {
			case got := <-reports:
				return got == want
			default:
				return false
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Past every retransmission the controller could still have due.
	if err := site.Step(ctx, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if dups != 0 {
		t.Fatalf("%d datagrams reached the device twice: a standalone ACK came after the controller's retransmission", dups)
	}
}
