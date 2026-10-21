// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientStructureOffloadTest.ts
// at 862ad75f: the controller applying a multi-cluster priming report, and
// an event of the same interaction delivered to its observers. matter.js
// hands the report to a structure worker; the Go node applies it under its
// lock, so the port asserts what the worker must not lose. matter.js
// mounts the switch on the root endpoint; the Go root serves no Switch, so
// the port's switch is a bridged GenericSwitch (momentary, with release).
package client_test

import (
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

func TestClientStructureOffloadParity(t *testing.T) {
	t.Parallel()

	t.Run("applies all values from a multi-cluster subscription priming", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		if v, ok := imageValue(cp.node, cp.lightPath(onoff.ClusterID, onoff.AttrOnOff)); !ok || v.El.Bool {
			t.Fatalf("the light's OnOff %v %v, want false", v.El.Bool, ok)
		}
		v, ok := imageValue(cp.node, attributePath(0, basicinfo.ClusterID, basicinfo.AttrDataModelRevision))
		if !ok || v.El.IsContainer || v.El.IsNull {
			t.Fatalf("DataModelRevision %+v %v, want a number", v.El, ok)
		}
	})

	t.Run("delivers events emitted during the same interaction as attribute data", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		sw, err := device.NewGenericSwitch(device.Options{StableKey: endpoint.StringKey("switch:1"), Name: "switch:1"},
			swdef.Declare{Features: swdef.FeatureMomentarySwitch | swdef.FeatureMomentarySwitchRelease})
		if err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		var presses []any
		p := site.UncommissionedPair(fabrictest.PairOptions{
			Device: fabrictest.Topology{Bridged: []node.Endpoint{sw}},
			Controller: fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) {
				rc.Node.Hooks.Event = func(_ uint16, cluster, event uint32, _ interaction.EventData, v any) {
					if cluster == swdef.ClusterID && event == swdef.EventInitialPress {
						mu.Lock()
						presses = append(presses, v)
						mu.Unlock()
					}
				}
			}},
		})
		n, err := p.Controller.CommissionNode(parityCtx(t), p.Device, client.CommissionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := site.Eventually(parityCtx(t), "the default subscription", n.SubscriptionActive); err != nil {
			t.Fatal(err)
		}
		ep, _ := p.Device.EndpointOf("switch:1")
		if err := sw.Switch().EmitInitialPress(ep, swdef.InitialPressEvent{NewPosition: 1}); err != nil {
			t.Fatal(err)
		}
		if err := site.Eventually(parityCtx(t), "the InitialPress event", func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(presses) > 0
		}); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if ev, ok := presses[0].(swdef.InitialPressEvent); !ok || ev.NewPosition != 1 {
			t.Fatalf("InitialPress %#v, want NewPosition 1", presses[0])
		}
		if n.Endpoint(ep) == nil {
			t.Fatal("the switch endpoint is not in the image")
		}
	})
}
