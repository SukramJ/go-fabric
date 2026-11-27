// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of the cases of matter.js packages/node/test/node/ClientNodeTest.ts
// at db91886c that came with be36b7d1d (#4901): a client write the local
// model would refuse, or could not judge, still goes to the device, and
// the device's answer decides. The Go node has one write surface,
// client.Node.Write, with no model validation on the way out and no
// write-through cache (controller/client/doc.go "Not here";
// BD-Matter-ClientNode-WriteNotThroughCache): each case is ported on its
// device-side subject — the status on the wire and the value the device
// holds — with the image checked where matter.js checks the cache.
package client_test

import (
	"reflect"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/client/interaction"
	basicinfo "thingfabric.dev/go-fabric/cluster/spec/basicinformation"
	bscdef "thingfabric.dev/go-fabric/cluster/spec/booleanstateconfiguration"
	unitdef "thingfabric.dev/go-fabric/cluster/spec/unitlocalization"
	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/node"
)

// TestClientNodeTemperatureUnitWithoutSupportedListParity ports the
// describe "writes TemperatureUnit to a peer without
// SupportedTemperatureUnits" (ClientNodeTest.ts:1281-1305 at db91886c).
// matter.js's device is a root with UnitLocalizationServer's defaults
// (Celsius of [Celsius, Fahrenheit]), the Go device root's own
// UnitLocalization (node/root.go buildRootOptional). matter.js seeds the
// peer's cache with an empty SupportedTemperatureUnits so that its local
// "in" check has nothing to accept; the Go write path judges no value
// against the image, so the write is the subject unseeded.
func TestClientNodeTemperatureUnitWithoutSupportedListParity(t *testing.T) {
	t.Parallel()
	t.Run("reaches the device", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{rootOnly: true})
		path := attributePath(0, unitdef.ClusterID, unitdef.AttrTemperatureUnit)
		if err := write(t, cp, interaction.WriteRequest{Path: path, Value: uint8Val(uint64(unitdef.TempUnitFahrenheit))}); err != nil {
			t.Fatalf("write of Fahrenheit: %v", err)
		}
		if v := deviceValue(t, cp, path); v.El.Uint != uint64(unitdef.TempUnitFahrenheit) {
			t.Fatalf("device TemperatureUnit %+v, want Fahrenheit", v.El)
		}
	})
}

// TestClientNodeSendsConstraintBreakingWriteParity ports the cases of the
// describe "sends a write that breaks a model constraint to the device"
// (ClientNodeTest.ts:1465-1558 at db91886c) whose subject is the device's
// answer. matter.js's device carries a ContactSensor whose
// BooleanStateConfiguration has SensitivityLevel with three levels, at
// level 0 (:1466-1480), the Go device the same ContactSensor, bridged as
// the pair's light is (newClientPair). The image keeps its value by
// construction (no write-through); it is checked as matter.js checks the
// cache.
func TestClientNodeSendsConstraintBreakingWriteParity(t *testing.T) {
	t.Parallel()
	// setup is newClientPair with the ContactSensor in the light's place;
	// lightEP is the sensor's endpoint.
	setup := func(t *testing.T) *clientPair {
		t.Helper()
		levels, zero := uint8(3), uint8(0)
		sensor, err := device.NewContactSensor(device.Options{StableKey: endpoint.StringKey("sensor:1"), Name: "sensor:1"}, bscdef.Declare{
			Features: bscdef.FeatureSensitivityLevel,
			Initial: bscdef.Patch{
				SupportedSensitivityLevels: &levels,
				CurrentSensitivityLevel:    &zero,
				DefaultSensitivityLevel:    &zero,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		cp := &clientPair{site: quietSite(t)}
		cp.pair = cp.site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{sensor}}})
		if cp.node, err = cp.pair.Controller.CommissionNode(parityCtx(t), cp.pair.Device, client.CommissionOptions{}); err != nil {
			t.Fatal(err)
		}
		ep, ok := cp.pair.Device.EndpointOf("sensor:1")
		if !ok {
			t.Fatal("the sensor has no endpoint")
		}
		cp.lightEP = ep
		cp.eventually(t, "the default subscription", cp.node.SubscriptionActive)
		return cp
	}

	t.Run("device rejects a value outside a range constraint and the local cache keeps its value", func(t *testing.T) {
		t.Parallel()
		cp := setup(t)
		path := cp.lightPath(bscdef.ClusterID, bscdef.AttrCurrentSensitivityLevel)
		if err := write(t, cp, interaction.WriteRequest{Path: path, Value: uint8Val(5)}); statusOf(err) != im.StatusConstraintError {
			t.Fatalf("write of CurrentSensitivityLevel 5: %v, want CONSTRAINT_ERROR from the device", err)
		}
		if v := deviceValue(t, cp, path); v.El.Uint != 0 {
			t.Fatalf("device CurrentSensitivityLevel %+v, want 0", v.El)
		}
		if v, ok := imageValue(cp.node, path); !ok || v.El.Uint != 0 {
			t.Fatalf("image CurrentSensitivityLevel %+v (%v), want 0", v.El, ok)
		}
	})

	t.Run("device rejects a string beyond a length constraint and the local cache keeps its value", func(t *testing.T) {
		t.Parallel()
		cp := setup(t)
		path := attributePath(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)
		before, _ := imageValue(cp.node, path)
		if err := write(t, cp, interaction.WriteRequest{Path: path, Value: strVal(strings.Repeat("x", 33))}); statusOf(err) != im.StatusConstraintError {
			t.Fatalf("write of a 33-character NodeLabel: %v, want CONSTRAINT_ERROR from the device", err)
		}
		if v := deviceValue(t, cp, path); !reflect.DeepEqual(v.El, before.El) {
			t.Fatalf("device NodeLabel %+v, want %+v", v.El, before.El)
		}
		if v, _ := imageValue(cp.node, path); !reflect.DeepEqual(v.El, before.El) {
			t.Fatalf("image NodeLabel %+v, want %+v", v.El, before.El)
		}
	})
}
