// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"

	matterbridge "github.com/SukramJ/go-fabric/bridge"

	"github.com/SukramJ/go-fabric/cluster/spec"
	ct "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/parity"
)

// writerEncodes reports whether the bridge's attribute value writer
// (bridge/reply.go defaultAttributeValueWriter, with encodeApplicationValue
// in bridge/application_values.go and encodeScenesResponse) encodes a
// non-null value of v's type as itself. Every other type falls to the
// writer's default arm and goes on the wire as null (reply.go, "Cluster
// server returned a Go value the writer does not handle"). The writer is
// unexported; this list transcribes its type switch, and
// TestFleetWireShapes's report asks for an exported predicate instead
// (notes/briefs/reports/O.md).
func writerEncodes(v any) bool { return matterbridge.EncodesAttributeValue(v) }

// schemaNullable maps cluster id → attribute id → the attribute's
// nullable quality ("X"), from the embedded matter.js snapshot
// (parity/schema.json, clusters[].attributes[].effective.quality).
func schemaNullable(t *testing.T) map[uint32]map[uint32]bool {
	t.Helper()
	var snap struct {
		Clusters []struct {
			ID         uint32 `json:"id"`
			Attributes []struct {
				ID        uint32 `json:"id"`
				Effective struct {
					Quality struct {
						Nullable bool `json:"nullable"`
					} `json:"quality"`
				} `json:"effective"`
			} `json:"attributes"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatal(err)
	}
	out := map[uint32]map[uint32]bool{}
	for _, c := range snap.Clusters {
		attrs := map[uint32]bool{}
		for _, a := range c.Attributes {
			attrs[a.ID] = a.Effective.Quality.Nullable
		}
		out[c.ID] = attrs
	}
	return out
}

// TestFleetWireShapes reads every attribute of every cluster on every
// endpoint of the reference daemon through the bridge's dispatcher — the
// values the bridge hands its writer — and fails on a value the writer
// would put on the wire as null: one of a type its switch does not know
// (a plain Go slice of generated structs, the defect class of the #47
// fix-up, cluster/energy/commodity_wire.go), or a null where the
// snapshot says the attribute is not nullable. It extends
// TestEnergyMarketWireShapes from the energy-market clusters to the fleet.
func TestFleetWireShapes(t *testing.T) {
	// The mechanism, on the defect class it exists for: a plain slice of
	// generated structs is not encoded, its spec.List is — the form a typed
	// Patch stores (Patch.AttributeValues) and cluster/energy's servers
	// hand out (commodity_wire.go).
	if writerEncodes([]ct.DayEntryStruct{}) || !writerEncodes(spec.List[ct.DayEntryStruct]{}) {
		t.Fatal("writerEncodes misjudges a plain slice / spec.List of generated structs")
	}
	f, br := startFleetBridge(t)
	nullable := schemaNullable(t)
	check := func(pass string) {
		results := br.Dispatcher().Read(context.Background(), im.ConcreteAttributePath{})
		if len(results) == 0 {
			t.Fatal("a wildcard read of the fleet returned nothing")
		}
		var findings []string
		read := 0
		for _, r := range results {
			if r.Status != im.StatusSuccess {
				continue
			}
			read++
			p := r.Path
			where := fmt.Sprintf("%s: endpoint %d cluster 0x%04X attribute 0x%04X", pass, p.Endpoint, p.Cluster, p.Attribute)
			v := r.Value
			switch {
			case v.IsNull || v.Value == nil:
				attrs, known := nullable[p.Cluster]
				isNullable, listed := attrs[p.Attribute]
				if known && listed && !isNullable {
					findings = append(findings, where+": null, but the attribute is not nullable (parity/schema.json)")
				}
			case !writerEncodes(v.Value):
				kind := reflect.TypeOf(v.Value).Kind()
				findings = append(findings, fmt.Sprintf("%s: %T (%s), which the bridge writer encodes as null", where, v.Value, kind))
			}
		}
		sort.Strings(findings)
		for _, s := range findings {
			t.Error(s)
		}
		t.Logf("%s: %d attributes read, %d findings", pass, read, len(findings))
	}
	check("at start")

	// A list or struct attribute that starts null or empty takes its
	// richer shape from a test event trigger: fire the ones that fill
	// them (the CHIP apps' triggers the daemon answers) and read again.
	tariff := mountedEndpoint(t, br, "demo-tariff-1")
	meter := mountedEndpoint(t, br, "demo-electricalmeter-1")
	for _, trigger := range []uint64{
		triggerTariffDataUpdated | uint64(tariff.ID)<<32, triggerPriceUpdate, triggerPriceForecastUpdate,
		triggerGridCurrentConditions, triggerGridForecastConditions,
		triggerMeteringAttributesUpdate | uint64(meter.ID)<<32, triggerMeterAttributesValueUpdate,
		triggerDemForecast, triggerDemPowerAdjustment, triggerDemUserOptOutLocalOptimization,
		triggerEvseBasicFunctionality, triggerEvseChargeDemand, triggerEvseSetSoCHigh, triggerEvseSetVehicleID,
		triggerWaterHeaterManual, triggerDrawOffHotWater,
		triggerForceSmokeWarning, triggerBoolCfgSensorTrigger,
	} {
		if err := f.testEventTrigger(context.Background(), trigger); err != nil {
			t.Fatalf("trigger 0x%016X: %v", trigger, err)
		}
	}
	check("after the triggers")
}
