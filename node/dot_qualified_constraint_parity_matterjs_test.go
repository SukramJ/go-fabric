// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/DotQualifiedConstraintTest.ts at
// 862ad75f: constraints that name a field of a sibling attribute
// ("holdTimeLimits.holdTimeMin to holdTimeLimits.holdTimeMax",
// "soilMoistureMeasurementLimits.minMeasuredValue to …maxMeasuredValue") —
// written over the wire, set locally, and checked when the server is built.
//
// matter.js writes on a MockServerNode as administrator; the port writes
// over the wire from a commissioned fabrictest pair, the servers being the
// module's generated ones on bridged endpoints. Where matter.js sets a
// behavior's state and expects a ConstraintError naming the constraint, the
// Go server's Set returns a spec.StatusError of CONSTRAINT_ERROR whose
// message names it; where matter.js fails to add the endpoint, the Go
// server refuses to be built (spec.NewServer, through the device layer).
package node_test

import (
	"errors"
	"fmt"
	"regexp"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ambient "github.com/SukramJ/go-fabric/cluster/spec/ambientcontextsensing"
	occ "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	soil "github.com/SukramJ/go-fabric/cluster/spec/soilmeasurement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// holdTimeLimits is HOLD_TIME_LIMITS (DotQualifiedConstraintTest.ts:26).
var holdTimeLimits = occ.HoldTimeLimitsStruct{HoldTimeMin: 10, HoldTimeMax: 100, HoldTimeDefault: 50}

// occupancyDevice is OccupancyDevice (:28-34): PassiveInfrared, PIR, HoldTime 50.
func occupancyDevice(site *fabrictest.Site, holdTime uint64) (node.Endpoint, error) {
	return device.NewOccupancySensor(device.Options{StableKey: endpoint.StringKey("occupancy:1"), Name: "occupancy"},
		device.WithFeatures(occ.ClusterID, uint32(occ.FeaturePassiveInfrared)),                                           //nolint:staticcheck // the parity case states matter.js's untyped initial state
		device.WithElements(occ.ClusterID, spec.Options{Attributes: []uint32{occ.AttrHoldTime, occ.AttrHoldTimeLimits}}), //nolint:staticcheck // the parity case states matter.js's untyped initial state
		device.WithInitial(occ.ClusterID, map[uint32]any{ //nolint:staticcheck // the parity case states matter.js's untyped initial state
			occ.AttrOccupancySensorType: uint64(occ.OccupancySensorTypeEnumPir),
			occ.AttrHoldTime:            holdTime,
			occ.AttrHoldTimeLimits:      holdTimeLimits,
		}))
}

// ambientEndpoint is AmbientContextSensingEndpoint (:36-45): device type
// 0x0015, HumanActivity.
func ambientEndpoint(*fabrictest.Site) (node.Endpoint, error) {
	srv, err := spec.NewServer(ambient.Definition, spec.Options{Features: uint32(ambient.FeatureHumanActivity), Attributes: []uint32{ambient.AttrHoldTime, ambient.AttrHoldTimeLimits}}, spec.ServerConfig{Initial: map[uint32]any{
		ambient.AttrHoldTime:       uint64(50),
		ambient.AttrHoldTimeLimits: ambient.HoldTimeLimitsStruct{HoldTimeMin: new(uint16(10)), HoldTimeMax: new(uint16(100)), HoldTimeDefault: new(uint16(50))},
	}})
	if err != nil {
		return nil, err
	}
	return &customEndpoint{key: "ambient:1", deviceType: 0x0015, servers: []contract.ClusterServer{srv}}, nil
}

func TestDotQualifiedConstraintParity(t *testing.T) {
	t.Parallel()

	holdTimeCases := []struct {
		name  string
		value uint64
		want  im.StatusCode
	}{
		{"accepts a value within the declared limits", 50, im.StatusSuccess},
		{"accepts the lower bound", 10, im.StatusSuccess},
		{"accepts the upper bound", 100, im.StatusSuccess},
		{"rejects a value below the declared limits", 9, im.StatusConstraintError},
		{"rejects a value above the declared limits", 101, im.StatusConstraintError},
		{"rejects the far end of the type range", 65535, im.StatusConstraintError},
		{"rejects zero", 0, im.StatusConstraintError},
	}
	for _, group := range []struct {
		name    string
		key     string
		cluster uint32
		attr    uint32
		build   func(*fabrictest.Site) (node.Endpoint, error)
	}{
		{"OccupancySensing.HoldTime", "occupancy:1", occ.ClusterID, occ.AttrHoldTime, func(s *fabrictest.Site) (node.Endpoint, error) { return occupancyDevice(s, 50) }},
		{"AmbientContextSensing.HoldTime", "ambient:1", ambient.ClusterID, ambient.AttrHoldTime, ambientEndpoint},
	} {
		t.Run(group.name, func(t *testing.T) {
			t.Parallel()
			for _, c := range holdTimeCases {
				t.Run(c.name, func(t *testing.T) {
					t.Parallel()
					h := newInvokeHostOn(t, quietSite(t), 5, endpointSpec{key: group.key, build: group.build})
					path := attrPath(h.ep(group.key), group.cluster, group.attr)
					got := writeStatuses(parityCtx(t), t, &mockPair{site: h.site, pair: h.pair}, interaction.WriteRequest{Path: path, Value: uintVal(c.value)})
					assertWriteStatuses(t, got, []interaction.WriteStatus{{Path: path, Status: c.want}})
				})
			}
		})
	}

	// SoilMoistureMeasuredValue is "R V": the bound guards the host.
	t.Run("SoilMeasurement.SoilMoistureMeasuredValue", func(t *testing.T) {
		t.Parallel()
		newSoil := func(t *testing.T) *spec.Server {
			t.Helper()
			pct := uint16(1000)
			d, err := device.NewSoilSensor(device.Options{StableKey: endpoint.StringKey("soil:1"), Name: "soil"},
				device.WithInitial(soil.ClusterID, map[uint32]any{ //nolint:staticcheck // the parity case states matter.js's untyped initial state
					soil.AttrSoilMoistureMeasurementLimits: soil.MeasurementAccuracyStruct{
						MeasurementType: soil.MeasurementTypeSoilMoisture, Measured: true, MinMeasuredValue: 20, MaxMeasuredValue: 80,
						AccuracyRanges: []soil.MeasurementAccuracyRangeStruct{{RangeMin: 20, RangeMax: 80, PercentMax: &pct}},
					},
					soil.AttrSoilMoistureMeasuredValue: uint64(50),
				}))
			if err != nil {
				t.Fatal(err)
			}
			srv, _ := d.SoilMeasurement().Underlying().(*spec.Server)
			return srv
		}
		set := func(t *testing.T, v any) (any, error) {
			t.Helper()
			srv := newSoil(t)
			if err := srv.Set(soil.AttrSoilMoistureMeasuredValue, v); err != nil {
				return nil, err
			}
			got, _ := srv.Value(soil.AttrSoilMoistureMeasuredValue)
			return got, nil
		}
		bound := regexp.MustCompile(`soilMoistureMeasurementLimits\.minMeasuredValue to soilMoistureMeasurementLimits\.maxMeasuredValue`)
		t.Run("accepts a value within the declared limits", func(t *testing.T) {
			t.Parallel()
			if got, err := set(t, uint64(80)); err != nil || fmt.Sprint(got) != "80" {
				t.Fatalf("got %v, %v, want 80", got, err)
			}
		})
		t.Run("accepts null", func(t *testing.T) {
			t.Parallel()
			if got, err := set(t, nil); err != nil || got != nil {
				t.Fatalf("got %v, %v, want null", got, err)
			}
		})
		for _, c := range []struct {
			name string
			v    uint64
		}{{"rejects a value below the declared limits", 19}, {"rejects a value above the declared limits", 81}} {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				_, err := set(t, c.v)
				var se *spec.StatusError
				if !errors.As(err, &se) || se.Status != im.StatusConstraintError || !bound.MatchString(se.Msg) {
					t.Fatalf("Set(%d): %v, want a CONSTRAINT_ERROR naming the bound", c.v, err)
				}
			})
		}
	})

	t.Run("validation at behavior initialization", func(t *testing.T) {
		t.Parallel()
		t.Run("accepts a configured value within the declared limits", func(t *testing.T) {
			t.Parallel()
			newInvokeHostOn(t, quietSite(t), 5, endpointSpec{key: "occupancy:1", build: func(s *fabrictest.Site) (node.Endpoint, error) { return occupancyDevice(s, 50) }})
		})
		t.Run("refuses to initialize with a configured value outside the declared limits", func(t *testing.T) {
			t.Parallel()
			_, err := occupancyDevice(quietSite(t), 5)
			var se *spec.StatusError
			if !errors.As(err, &se) || se.Status != im.StatusConstraintError ||
				!regexp.MustCompile(`holdTimeLimits\.holdTimeMin to holdTimeLimits\.holdTimeMax`).MatchString(se.Msg) {
				t.Fatalf("a HoldTime of 5 built: %v, want a CONSTRAINT_ERROR naming the bound", err)
			}
		})
	})
}
