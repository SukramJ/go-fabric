// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/spec"
	fandef "thingfabric.dev/go-fabric/cluster/spec/fancontrol"
	occdef "thingfabric.dev/go-fabric/cluster/spec/occupancysensing"
	smokedef "thingfabric.dev/go-fabric/cluster/spec/smokecoalarm"
	tempdef "thingfabric.dev/go-fabric/cluster/spec/temperaturemeasurement"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/node"
)

// unserved lists, for every committed device type the kit builds with its
// defaults and the values New requires (device.InitialFor, decision 17),
// the attributes a server lists in its AttributeList that a read answers
// UnsupportedAttribute for, as "Cluster/Attribute".
func unserved(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, typ := range device.Types() {
		d, err := device.New(typ, device.Options{StableKey: device.StringKey("k")}, device.InitialFor(typ.Name)...)
		if err != nil {
			continue // a type whose default selection the host must make
		}
		for _, srv := range d.Servers() {
			l, ok := srv.(contract.ClusterAttributeLister)
			if !ok {
				continue
			}
			def := spec.Lookup(srv.MatterClusterID())
			for _, a := range l.MatterAttributes() {
				if _, ok := srv.MatterRead(a); ok {
					continue
				}
				name, cname := fmt.Sprintf("0x%04X", a), fmt.Sprintf("0x%04X", srv.MatterClusterID())
				if def != nil {
					cname = def.Name
					if at := def.Attribute(a); at != nil {
						name = at.Name
					}
				}
				out = append(out, cname+"/"+name)
			}
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// hostSupplied are the listed attributes no default fills once the
// required values are stated: none since the generator composes a struct
// default from its children's (OperationalError, wave A).
var hostSupplied = []string{}

// TestEveryListedAttributeHasAValue: with its defaults alone, every
// committed device type serves every attribute its servers list — the
// SelectDefaultValue fallbacks and the default servers' initialize
// values of matter.js, beside the values New requires ([hostSupplied]
// is empty). A new gap fails here.
func TestEveryListedAttributeHasAValue(t *testing.T) {
	t.Parallel()
	got := unserved(t)
	if !slices.Equal(got, hostSupplied) {
		t.Fatalf("attributes listed but not served:\n%q\nwant\n%q", got, hostSupplied)
	}
}

// TestMandatoryDefaultsOnTheWire: the attributes H1 found unserved answer
// a controller's wildcard read — MeasuredValue null, FanMode Off,
// OccupancySensorType PIR from the PIR feature, SmokeCoAlarm's BatteryAlert
// Normal.
func TestMandatoryDefaultsOnTheWire(t *testing.T) {
	t.Parallel()
	temp, err := device.NewTemperatureSensor(device.Options{StableKey: device.StringKey("t"), Name: "t"})
	if err != nil {
		t.Fatal(err)
	}
	fan, err := device.NewFan(device.Options{StableKey: device.StringKey("f"), Name: "f"}, required("Fan",
		fandef.Declare{Initial: fandef.Patch{FanModeSequence: new(fandef.FanModeSequenceOffLowMedHigh)}})...)
	if err != nil {
		t.Fatal(err)
	}
	occ, err := device.NewOccupancySensor(device.Options{StableKey: device.StringKey("o"), Name: "o"},
		occdef.Declare{Features: occdef.FeaturePassiveInfrared})
	if err != nil {
		t.Fatal(err)
	}
	smoke, err := device.NewSmokeCoAlarm(device.Options{StableKey: device.StringKey("s"), Name: "s"},
		smokedef.Declare{Features: smokedef.FeatureSmokeAlarm})
	if err != nil {
		t.Fatal(err)
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{temp, fan, occ, smoke}}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	read := func(d interface{ EndpointNumber() (uint16, bool) }, cluster uint32) spec.Values {
		ep, _ := d.EndpointNumber()
		v, err := fabrictest.ReadCluster(ctx, pair, ep, cluster)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ts, absent := tempdef.StateFrom(read(temp, tempdef.ClusterID))
	if slices.Contains(absent, tempdef.AttrMeasuredValue) || ts.MeasuredValue != nil {
		t.Fatalf("TemperatureMeasurement %+v, absent %v: want MeasuredValue null", ts, absent)
	}
	fs, absent := fandef.StateFrom(read(fan, fandef.ClusterID))
	if slices.Contains(absent, fandef.AttrFanMode) || fs.FanMode != fandef.FanModeOff {
		t.Fatalf("FanControl FanMode %v, absent %v", fs.FanMode, absent)
	}
	os, absent := occdef.StateFrom(read(occ, occdef.ClusterID))
	if slices.Contains(absent, occdef.AttrOccupancySensorType) || os.OccupancySensorType != occdef.OccupancySensorTypeEnumPir {
		t.Fatalf("OccupancySensing type %v, absent %v", os.OccupancySensorType, absent)
	}
	ss, absent := smokedef.StateFrom(read(smoke, smokedef.ClusterID))
	if slices.Contains(absent, smokedef.AttrBatteryAlert) || ss.BatteryAlert != smokedef.AlarmStateNormal {
		t.Fatalf("SmokeCoAlarm BatteryAlert %v, absent %v", ss.BatteryAlert, absent)
	}
}
