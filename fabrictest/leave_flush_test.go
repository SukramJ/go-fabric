// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// TestLeaveFlushIssue473: a factory reset flushes what the controller's
// subscription owes — the Leave event and an attribute changed inside the
// subscription's floor — as one report, and waits at most 500 ms for its
// answer. Answered, the reset returns before the 500 ms bound; with the
// controller no longer answering, it returns once the clock has moved
// 500 ms, never the MRP-derived bound of a report (issue 473, the S9 log
// of go-fabric-server #54). Mirrors matter.js Fabric.leave
// (packages/protocol/src/fabric/Fabric.ts:447-457 at db021da0) → one
// #flush per subscription (ServerSubscription.ts:753-763), its DataReport
// sent with waitForAck false (:934) and answered within Millis(500)
// (InteractionMessenger.ts:844).
func TestLeaveFlushIssue473(t *testing.T) {
	t.Parallel()
	for _, answered := range []bool{true, false} {
		name := "answered"
		if !answered {
			name = "unanswered"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			site, p, pl, ep := pairWithPlug(t, fabrictest.Options{})
			ctx := testContext(t)
			reports := make(chan *interaction.Report, 16)
			sub, err := fabrictest.Subscribe(ctx, p, interaction.SubscribeRequest{
				Attributes: []im.ConcreteAttributePath{onOffPath(ep)},
				Events: []im.ConcreteEventPath{{
					Endpoint: 0, Cluster: basicinfo.ClusterID, Event: basicinfo.EventLeave,
					HasEndpoint: true, HasCluster: true, HasEvent: true,
				}},
				MinIntervalFloor:   interaction.Interval(time.Hour),
				MaxIntervalCeiling: interaction.Interval(2 * time.Hour),
				Updated:            func(r *interaction.Report) error { reports <- r; return nil },
			})
			if err != nil {
				t.Fatalf("Subscribe: %v", err)
			}
			defer sub.Close()
			if err := p.Device.Act(func(*node.Node) error { pl.srv.Set(true); return nil }); err != nil {
				t.Fatal(err)
			}
			if err := site.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			for len(reports) > 0 {
				<-reports // the priming report
			}
			if !answered {
				device := p.Device.Addr().String()
				site.Net().Filter(func(d fabrictest.Datagram) bool { return d.To.String() != device })
			}

			before := site.Clock().Now()
			if answered {
				if err := p.Device.FactoryReset(ctx); err != nil {
					t.Fatalf("FactoryReset: %v", err)
				}
			} else if err := site.Run(ctx, p.Device.FactoryReset); err != nil {
				t.Fatalf("FactoryReset: %v", err)
			}
			moved := site.Clock().Now().Sub(before)
			switch {
			case answered && moved >= 500*time.Millisecond:
				t.Fatalf("the answered reset ran into the 500 ms bound, moved %v: the answer should end the wait", moved)
			case !answered && (moved < 500*time.Millisecond || moved >= time.Second):
				t.Fatalf("the unanswered reset returned after %v on the clock, want 500 ms", moved)
			}

			if len(reports) != 1 {
				t.Fatalf("the controller received %d reports, want one carrying the Leave event and OnOff", len(reports))
			}
			r := <-reports
			if v, ok := r.Value(ep, onoff.ClusterID, onoff.AttrOnOff); !ok || !v.El.Bool {
				t.Fatalf("the flushed report carries OnOff %v (%v), want true", v, ok)
			}
			leave := 0
			for _, ev := range r.Events {
				if ev.Path.Endpoint == 0 && ev.Path.Cluster == basicinfo.ClusterID && ev.Path.Event == basicinfo.EventLeave {
					leave++
				}
			}
			if leave != 1 || len(r.Events) != 1 {
				t.Fatalf("the flushed report carries %d events, %d Leave, want the Leave alone", len(r.Events), leave)
			}
		})
	}
}
