// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"testing"
	"time"

	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// TestInvokeBatch: Node.InvokeBatch sends On to two lights and Toggle to
// an endpoint the device does not have; the results are in request
// order, the third an UnsupportedEndpoint status and no error, and the
// lights are on. Against a device of MaxPathsPerInvoke 1 the same batch
// is split into one exchange per command with the same results.
func TestInvokeBatch(t *testing.T) {
	t.Parallel()
	for _, maxPaths := range []uint16{0, 1} {
		t.Run("MaxPathsPerInvoke "+map[uint16]string{0: "default", 1: "1"}[maxPaths], func(t *testing.T) {
			t.Parallel()
			lights := make([]*device.OnOffLight, 2)
			eps := make([]node.Endpoint, 2)
			for i := range lights {
				l, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light" + string(rune('a'+i))), Name: "light"})
				if err != nil {
					t.Fatal(err)
				}
				lights[i], eps[i] = l, l
			}
			site := fabrictest.NewSite(t, fabrictest.Options{})
			pair := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
				Bridged:   eps,
				Configure: func(cfg *node.Config) { cfg.MaxPathsPerInvoke = maxPaths },
			}})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			n, err := pair.Controller.CommissionNode(ctx, pair.Device, client.CommissionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			want := int(maxPaths)
			if want == 0 {
				want = im.DefaultMaxPathsPerInvoke
			}
			if got := n.MaxPathsPerInvoke(); got != want {
				t.Fatalf("MaxPathsPerInvoke %d, want %d", got, want)
			}
			ep0, _ := lights[0].EndpointNumber()
			ep1, _ := lights[1].EndpointNumber()
			cmds := []client.CommandRequest{
				{Path: client.CommandAt(ep0, onoffdef.ClusterID, onoffdef.CmdOn), Fields: onoffdef.OnRequest{}},
				{Path: client.CommandAt(ep1, onoffdef.ClusterID, onoffdef.CmdOn), Fields: onoffdef.OnRequest{}},
				{Path: client.CommandAt(0xFFF0, onoffdef.ClusterID, onoffdef.CmdToggle)},
			}
			var res []client.Result
			if err := site.Run(ctx, func(ctx context.Context) (err error) {
				res, err = n.InvokeBatch(ctx, cmds, client.InvokeOptions{})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if len(res) != 3 || res[0].Status != im.StatusSuccess || res[1].Status != im.StatusSuccess || res[2].Status != im.StatusUnsupportedEndpoint {
				t.Fatalf("results %+v, want Success, Success, UnsupportedEndpoint", res)
			}
			for i, l := range lights {
				if !l.OnOff().Get().OnOff {
					t.Errorf("light %d is off", i)
				}
			}
		})
	}
}
