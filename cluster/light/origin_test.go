// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/light"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
)

func originColorControl(tb testing.TB) contract.ClusterServer {
	tb.Helper()
	srv, err := light.NewColorControl(light.ColorControlServerConfig{
		Features:  light.ColorFeatureColorTemperature,
		MinMireds: 153, MaxMireds: 370, InitialMireds: 250,
	})
	if err != nil {
		tb.Fatal(err)
	}
	return srv
}

// TestColorControlOrigins is the origin audit of the kit's ColorControl
// server (brief BY-1 §1 and §2): the host's Set, a command and a
// controller's write of each writable attribute reach the change
// listeners — the host's OnChanged — at once, with their origin and the
// attribute that moved (Datasource.ts:1473-1476).
func TestColorControlOrigins(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, originColorControl,
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Attributes: []uint32{ccdef.AttrColorTemperatureMireds}, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			m := uint16(300)
			if err := ccdef.Wrap(srv).Set(ccdef.Patch{ColorTemperatureMireds: &m}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Attributes: []uint32{ccdef.AttrColorTemperatureMireds}, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			ctx := contract.WithOrigin(context.Background(), contract.OriginCommand)
			if _, err := srv.MatterInvoke(ctx, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 200}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.Write(ccdef.AttrOptions, uint64(1)),
		spectest.Write(ccdef.AttrStartUpColorTemperatureMireds, uint64(200)),
	)
}

// TestColorControlHostSeesEveryChangeTheWireThrottles: the host's Sets of
// the quieter ColorTemperatureMireds inside one second reach the change
// listeners each at once, while the wire (contract.AttributeReportNotifier)
// reports the first at once and the last after the interval — matter.js's
// QuietEvent throttles only its `online` view
// (packages/node/src/behavior/Events.ts:267-297).
func TestColorControlHostSeesEveryChangeTheWireThrottles(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		srv, ok := originColorControl(t).(*light.ColorControlServer)
		if !ok {
			t.Fatal("not a ColorControlServer")
		}
		count := func(subscribe func(func(contract.Change)) func()) *atomic.Int32 {
			n := new(atomic.Int32) // the quieter's deferred report runs on its timer's goroutine
			subscribe(func(c contract.Change) {
				if slices.Contains(c.Attributes, ccdef.AttrColorTemperatureMireds) {
					n.Add(1)
				}
			})
			return n
		}
		host, onWire := count(srv.OnAttributeChange), count(srv.OnAttributeReport)
		for _, m := range []uint16{260, 270, 280} {
			if err := ccdef.Wrap(srv).Set(ccdef.Patch{ColorTemperatureMireds: &m}); err != nil {
				t.Fatal(err)
			}
		}
		if host.Load() != 3 || onWire.Load() != 1 {
			t.Errorf("host %d, wire %d changes at once; want 3 and 1", host.Load(), onWire.Load())
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if onWire.Load() != 2 {
			t.Errorf("wire %d reports after the interval, want 2", onWire.Load())
		}
	})
}
