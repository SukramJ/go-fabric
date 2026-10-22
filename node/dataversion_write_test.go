// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"testing"
	"time"

	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

// uint16Value and uint8Value are scalar write values.
type (
	uint16Value uint16
	uint8Value  uint8
)

func (v uint16Value) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { enc.PutUint(tag, uint64(v)) }
func (v uint8Value) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag)  { enc.PutUint(tag, uint64(v)) }

// TestWriteAdvancesDataVersionOnce answers BY-1's first question: a
// controller's write advances the cluster's DataVersion once — the
// dispatcher's advance (endpoint/dispatcher.go WriteAuthorized); the
// bridge's change listener does not advance it again for the change the
// write itself reports (OriginControllerWrite). matter.js advances the
// version once per commit (packages/node/src/behavior/state/managed/
// Datasource.ts:1440-1448, advanceVersion), and a write is one commit.
// Checked for a spec.Server store (OnOff's OnTime), a self-held server
// (LevelControl's OnLevel) and a write that moves companions with it (the
// thermostat's heating setpoint).
func TestWriteAdvancesDataVersionOnce(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	site := quietSite(t)
	light, err := device.NewDimmableLight(device.Options{StableKey: endpoint.StringKey("light"), Name: "Hall light"})
	if err != nil {
		t.Fatal(err)
	}
	thermo, err := device.NewThermostat(device.Options{StableKey: endpoint.StringKey("thermo"), Name: "Living room"},
		thermostatdef.Declare{
			Features: thermostatdef.FeatureHeating,
			Initial:  thermostatdef.Patch{LocalTemperature: new(int16(1950)), OccupiedHeatingSetpoint: new(int16(2000))},
			Sink:     &heating{},
		})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light, thermo}}})
	lightEP, _ := light.EndpointNumber()
	thermoEP, _ := thermo.EndpointNumber()
	for _, tc := range []struct {
		name  string
		path  im.ConcreteAttributePath
		value interface{ EncodeTLV(*tlv.Encoder, tlv.Tag) }
	}{
		{"OnOff OnTime (spec.Server store)", lifecycleAttr(lightEP, onoffdef.ClusterID, onoffdef.AttrOnTime), uint16Value(30)},
		{"LevelControl OnLevel (self-held)", lifecycleAttr(lightEP, levelcontroldef.ClusterID, levelcontroldef.AttrOnLevel), uint8Value(77)},
		{"Thermostat OccupiedHeatingSetpoint (with companions)", lifecycleAttr(thermoEP, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint), int16Value(2250)},
	} {
		before, err := fabrictest.Read(ctx, pair, tc.path)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if err := fabrictest.Write(ctx, pair, tc.path, tc.value); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if err := site.Settle(ctx); err != nil {
			t.Fatal(err)
		}
		after, err := fabrictest.Read(ctx, pair, tc.path)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if after.DataVersion != before.DataVersion+1 {
			t.Errorf("%s: DataVersion %d → %d, want one advance", tc.name, before.DataVersion, after.DataVersion)
		}
	}
}
