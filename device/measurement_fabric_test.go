// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/cluster/spec"
	bsdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	epmdef "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement"
	osdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	psdef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	soildef "github.com/SukramJ/go-fabric/cluster/spec/soilmeasurement"
	switchdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	tmdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/schema"
)

// floatReading, boolReading, electricalReading, soilReading and
// switchReading are a host's classified data points: one measurement
// class each, read on every dispatch.
type floatReading struct {
	class contract.MeasurementClass
	value float64
}

func (r floatReading) MatterMeasurementClass() contract.MeasurementClass { return r.class }
func (r floatReading) MatterFloatValue() (float64, bool)                 { return r.value, true }

type boolReading struct {
	class contract.MeasurementClass
	value bool
}

func (r boolReading) MatterMeasurementClass() contract.MeasurementClass { return r.class }
func (r boolReading) MatterBoolValue() (value, observed bool)           { return r.value, true }

type electricalReading struct{ floatReading }

func (electricalReading) ActivePower() (float64, bool) { return 42.5, true }
func (electricalReading) Voltage() (float64, bool)     { return 230, true }
func (electricalReading) Current() (float64, bool)     { return 0.2, true }
func (electricalReading) Frequency() (float64, bool)   { return 50, true }
func (electricalReading) Energy() (float64, bool)      { return 1.5, true }
func (electricalReading) HasEnergy() bool              { return true }

type soilReading struct{ floatReading }

func (soilReading) MatterSoilMoistureLimits() soildef.MeasurementAccuracyStruct {
	percentMax := uint16(10)
	return soildef.MeasurementAccuracyStruct{
		MeasurementType: soildef.MeasurementTypeSoilMoisture,
		Measured:        true, MinMeasuredValue: 0, MaxMeasuredValue: 100,
		AccuracyRanges: []soildef.MeasurementAccuracyRangeStruct{{RangeMin: 0, RangeMax: 100, PercentMax: &percentMax}},
	}
}

type switchReading struct{}

func (switchReading) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementMomentarySwitch
}
func (switchReading) MatterSwitchPositions() uint8        { return 2 }
func (switchReading) MatterSwitchSupportsLongPress() bool { return false }

// measurementCase is one built-in class over the kit: the source, and
// the attribute that carries its reading.
type measurementCase struct {
	name    string
	src     contract.MeasurementSource
	cluster uint32
	attr    uint32
}

func measurementCases() []measurementCase {
	f := func(class contract.MeasurementClass, v float64) floatReading { return floatReading{class, v} }
	headline := func(class contract.MeasurementClass) uint32 {
		k, _ := contract.MeasurementKindFor(class)
		return k.ClusterID
	}
	cases := make([]measurementCase, 0, 21)
	for _, c := range []struct {
		name  string
		class contract.MeasurementClass
		v     float64
	}{
		{"Temperature", contract.MeasurementTemperature, 21.5},
		{"Humidity", contract.MeasurementHumidity, 45},
		{"Illuminance", contract.MeasurementIlluminance, 120},
		{"Pressure", contract.MeasurementPressure, 101.3},
		{"Flow", contract.MeasurementFlow, 3},
		{"CO2", contract.MeasurementCO2, 650},
		{"PM25", contract.MeasurementPM25, 12},
		{"PM10", contract.MeasurementPM10, 20},
		{"CO", contract.MeasurementCO, 2},
		{"NO2", contract.MeasurementNO2, 30},
		{"Ozone", contract.MeasurementOzone, 40},
		{"Formaldehyde", contract.MeasurementFormaldehyde, 0.05},
		{"PM1", contract.MeasurementPM1, 8},
		{"TVOC", contract.MeasurementTVOC, 150},
		{"Radon", contract.MeasurementRadon, 90},
	} {
		// MeasuredValue is attribute 0 of every float measurement and
		// concentration cluster.
		cases = append(cases, measurementCase{c.name, f(c.class, c.v), headline(c.class), 0x0000})
	}
	cases = append(cases,
		measurementCase{"Contact", boolReading{contract.MeasurementContact, true}, bsdef.ClusterID, bsdef.AttrStateValue},
		measurementCase{"Leak", boolReading{contract.MeasurementLeak, true}, bsdef.ClusterID, bsdef.AttrStateValue},
		measurementCase{"Occupancy", boolReading{contract.MeasurementOccupancy, true}, osdef.ClusterID, osdef.AttrOccupancy},
		measurementCase{"Electrical", electricalReading{f(contract.MeasurementElectrical, 42.5)}, epmdef.ClusterID, epmdef.AttrActivePower},
		measurementCase{"SoilMoisture", soilReading{f(contract.MeasurementSoilMoisture, 37)}, soildef.ClusterID, soildef.AttrSoilMoistureMeasuredValue},
		measurementCase{"MomentarySwitch", switchReading{}, switchdef.ClusterID, switchdef.AttrNumberOfPositions},
	)
	return cases
}

// TestMeasurementServesEveryBuiltinClassOverTheWire (#221): a host whose
// sensors are classified data declares each with device.Measurement on
// the class's device type; a controller reads the class's attribute as
// cluster/measurement's materialiser serves it on the device's endpoint,
// and the device conforms to its type. The battery rides on a sensor as
// its PowerSource, its EndpointList naming the device's endpoint.
func TestMeasurementServesEveryBuiltinClassOverTheWire(t *testing.T) {
	cases := measurementCases()
	bridged := make([]node.Endpoint, 0, len(cases)+1)
	devices := make([]*device.Device, len(cases))
	for i, c := range cases {
		typ, ok := device.TypeByID(schema.DeviceTypeID(contract.MeasurementClassDeviceType(c.src.MatterMeasurementClass())))
		if !ok {
			t.Fatalf("%s: no device type", c.name)
		}
		d, err := device.New(typ, device.Options{StableKey: endpoint.StringKey("sensor:" + c.name), Name: c.name}, device.Measurement(c.src))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if d.Server(c.cluster) != nil {
			t.Errorf("%s: the device mounts a server for the measured cluster 0x%04X", c.name, c.cluster)
		}
		devices[i] = d
		bridged = append(bridged, d)
	}
	battery := boolReading{contract.MeasurementBattery, false}
	thermo, err := device.NewTemperatureSensor(device.Options{StableKey: endpoint.StringKey("sensor:battery"), Name: "battery"},
		device.Measurement(floatReading{contract.MeasurementTemperature, 19}), device.Measurement(battery))
	if err != nil {
		t.Fatal(err)
	}
	bridged = append(bridged, thermo)

	// The kit's devices conform to their types with the measured
	// clusters the assembler mounts (endpoint.ValidateDeviceTypes).
	a, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	specs := make([]endpoint.Spec, 0, len(bridged))
	for _, d := range append(slices.Clone(devices), thermo.Device) {
		specs = append(specs, d.Spec())
	}
	topo, err := a.Assemble(context.Background(), []endpoint.Snapshot{{Scope: "test", Endpoints: specs, ModelComplete: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range endpoint.ValidateDeviceTypes(topo) {
		if v.Endpoint > 1 {
			t.Errorf("device-type violation: %v", v)
		}
	}

	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: bridged}})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	read := func(ep uint16, cluster, attr uint32) (spec.Node, error) {
		got, err := fabrictest.Read(ctx, pair, im.ConcreteAttributePath{
			Endpoint: ep, Cluster: cluster, Attribute: attr,
			HasEndpoint: true, HasCluster: true, HasAttribute: true,
		})
		return got.Value, err
	}

	for i, c := range cases {
		ep, ok := devices[i].EndpointNumber()
		if !ok {
			t.Fatalf("%s: not assembled", c.name)
		}
		got, err := read(ep, c.cluster, c.attr)
		if err != nil {
			t.Errorf("%s: read 0x%04X/0x%04X: %v", c.name, c.cluster, c.attr, err)
			continue
		}
		want := servedValue(t, c, ep)
		if err := sameScalar(want, got); err != nil {
			t.Errorf("%s: 0x%04X/0x%04X: %v", c.name, c.cluster, c.attr, err)
		}
		if c.name == "Temperature" && got.El.Int != 2150 {
			t.Errorf("Temperature MeasuredValue %d, want 2150", got.El.Int)
		}
	}

	ep, _ := thermo.EndpointNumber()
	if got, err := read(ep, tmdef.ClusterID, tmdef.AttrMeasuredValue); err != nil || got.El.Int != 1900 {
		t.Errorf("battery-powered sensor's MeasuredValue %+v, %v, want 1900", got.El, err)
	}
	if got, err := read(ep, psdef.ClusterID, psdef.AttrBatChargeLevel); err != nil || got.El.Uint != uint64(psdef.BatChargeLevelOk) {
		t.Errorf("BatChargeLevel %+v, %v, want Ok", got.El, err)
	}
	got, err := read(ep, psdef.ClusterID, psdef.AttrEndpointList)
	if err != nil {
		t.Fatalf("EndpointList: %v", err)
	}
	list := make([]uint64, 0, len(got.Children))
	for _, n := range got.Children {
		list = append(list, n.El.Uint)
	}
	if !slices.Equal(list, []uint64{uint64(ep)}) {
		t.Errorf("EndpointList %v, want [%d]", list, ep)
	}
}

// servedValue is what cluster/measurement's materialiser serves for c
// on endpoint ep — the server the assembler builds.
func servedValue(t *testing.T, c measurementCase, ep uint16) any {
	t.Helper()
	for _, srv := range measurement.FromMeasurementClass(c.src.MatterMeasurementClass(), c.src, contract.MeasurementContext{EndpointID: ep}) {
		if srv.MatterClusterID() == c.cluster {
			v, ok := srv.MatterRead(c.attr)
			if !ok {
				t.Fatalf("%s: the materialised server does not serve 0x%04X", c.name, c.attr)
			}
			return v
		}
	}
	t.Fatalf("%s: the materialiser builds no 0x%04X server", c.name, c.cluster)
	return nil
}

// sameScalar compares a server's Go value with the scalar a controller
// read.
func sameScalar(want any, got spec.Node) error {
	rv := reflect.ValueOf(want)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			if got.El.IsNull {
				return nil
			}
			return fmt.Errorf("read %+v, want null", got.El)
		}
		rv = rv.Elem()
	}
	var ok bool
	switch rv.Kind() {
	case reflect.Bool:
		ok = got.El.Bool == rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		ok = got.El.Int == rv.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		ok = got.El.Uint == rv.Uint()
	case reflect.Float32, reflect.Float64:
		ok = float32(got.El.Float) == float32(rv.Float())
	default:
		return fmt.Errorf("unsupported %T", want)
	}
	if !ok {
		return fmt.Errorf("read %+v, served %v", got.El, want)
	}
	if got.El.IsNull {
		return errors.New("read null, served a value")
	}
	return nil
}

// TestMeasurementRefusals: nil, a source of no materialisable shape, a
// declaration for a measured cluster, a second source of one kind.
func TestMeasurementRefusals(t *testing.T) {
	t.Parallel()
	src := floatReading{contract.MeasurementTemperature, 1}
	opts := device.Options{StableKey: endpoint.StringKey("r"), Name: "r"}
	for name, decls := range map[string][]device.Declaration{
		"nil":           {device.Measurement(nil)},
		"no cluster":    {device.Measurement(floatReading{class: contract.MeasurementNone})},
		"wrong shape":   {device.Measurement(boolReading{class: contract.MeasurementTemperature})},
		"declared too":  {device.Measurement(src), tmdef.Declare{}},
		"two sources":   {device.Measurement(src), device.Measurement(floatReading{contract.MeasurementHumidity, 1})},
		"two batteries": {device.Measurement(boolReading{class: contract.MeasurementBattery}), device.Measurement(boolReading{class: contract.MeasurementBattery})},
		"a host's too":  {device.Measurement(src), tmdef.Declare{HostServer: measurement.NewTemperatureServer(src)}},
		"opting it in":  {device.Measurement(src), device.HostServer(measurement.NewTemperatureServer(src))},
	} {
		if _, err := device.NewTemperatureSensor(opts, decls...); !errors.Is(err, device.ErrConfiguration) {
			t.Errorf("%s: %v, want ErrConfiguration", name, err)
		}
	}
	// Without the declaration the type is unchanged: a default server.
	plain, err := device.NewTemperatureSensor(opts)
	if err != nil {
		t.Fatal(err)
	}
	if plain.TemperatureMeasurement() == nil || plain.Spec().Measurement != nil || plain.Spec().PowerSource != nil {
		t.Error("a TemperatureSensor without Measurement changed")
	}
}
