// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	cccdef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	cddef "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	demdef "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	eemdef "github.com/SukramJ/go-fabric/cluster/spec/electricalenergymeasurement"
	epmdef "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement"
	mwocdef "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	ncdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	psdef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	ptdef "github.com/SukramJ/go-fabric/cluster/spec/powertopology"
	pumpdef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	scadef "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	switchdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	tstatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
)

// refusedDefaults are the device types whose constructor refuses its
// defaults, with the part of the error that says why. A mandatory cluster
// whose features the application must choose is not mounted by default in
// matter.js either (RequirementGenerator.ts:188-199, EndpointFile.ts:144-158);
// the rest name a cluster without a default server here.
var refusedDefaults = map[string]string{
	// Mandatory with extension: the host selects the features.
	"Closure":                "ClosureControl needs a feature selection",
	"ClosurePanel":           "ClosureDimension needs a feature selection",
	"DeviceEnergyManagement": "DeviceEnergyManagement needs a feature selection",
	"ElectricalMeter":        "ElectricalPowerMeasurement needs a feature selection",
	"ElectricalSensor":       "PowerTopology needs a feature selection",
	"GenericSwitch":          "Switch needs a feature selection",
	"AudioDoorbell":          "Switch needs a feature selection",
	"MicrowaveOven":          "MicrowaveOvenControl needs a feature selection",
	"OccupancySensor":        "OccupancySensing needs a feature selection",
	"PowerSource":            "PowerSource needs a feature selection",
	"Pump":                   "PumpConfigurationAndControl needs a feature selection",
	"RoomAirConditioner":     "Thermostat needs a feature selection",
	// NetworkCommissioning's three interface features are O.a: one of
	// them is the host's to name.
	"SecondaryNetworkInterface": "NetworkCommissioning needs a feature selection",
	"SmokeCoAlarm":              "SmokeCoAlarm needs a feature selection",
	"Thermostat":                "Thermostat needs a feature selection",
	"WindowCovering":            "WindowCovering needs a feature selection",
	// A mandatory cluster whose generated server refuses its definition
	// and that has no core default: the requestor needs the node's
	// connector.
	"OtaRequestor": "attribute DefaultOtaProviders: supply its server as the declaration's HostServer",
	// The node's root clusters have no committed definitions; the root is
	// the node facade's, not a bridged device.
	"RootNode": "requires AccessControl (0x001F, conformance \"M\") and no generated definition is committed",
}

// fixture is a device of one type as a host builds it: its own options —
// the feature selection a mandatory-with-extension cluster needs, a server
// the module has no default for — and the component parts the type
// mandates; tolerate lists the violations that remain, each with the
// reason no device-layer option can remove it.
type fixture struct {
	with     func(t *testing.T) []Option
	parts    func(t *testing.T, d *Device)
	tolerate map[string]string // "<kind> <requirement>" → reason
}

// testWith returns the options a host gives a device of the named type.
func testWith(t *testing.T, name string) []Option {
	t.Helper()
	if f, ok := fixtures[name]; ok && f.with != nil {
		return f.with(t)
	}
	return nil
}

// refusesFixture reports whether the named type refuses even the host's
// options: the types without a fixture.
func refusesFixture(name string) bool {
	_, ok := noFixture[name]
	return ok && name != "Aggregator" && name != "BridgedNode"
}

// build constructs a fixture's device with its parts.
func build(t *testing.T, ty *Type) *Device {
	t.Helper()
	d, err := New(ty, testOptions(ty.Name), testWith(t, ty.Name)...)
	if err != nil {
		t.Fatalf("%s: %v", ty.Name, err)
	}
	if f, ok := fixtures[ty.Name]; ok && f.parts != nil {
		f.parts(t, d)
	}
	return d
}

// part adds a part of the named type to owner, with a unique key.
func part(t *testing.T, owner *Device, name string, conds []string, with ...Option) {
	t.Helper()
	ty := typeByName(name)
	opts := testOptions(fmt.Sprintf("%s/%s#%d", owner.Type().Name, name, len(owner.Parts())))
	opts.Conditions = conds
	p, err := New(ty, opts, with...)
	if err != nil {
		t.Fatalf("part %s: %v", name, err)
	}
	if err := owner.AddPart(p); err != nil {
		t.Fatal(err)
	}
}

func opts(o ...Option) func(*testing.T) []Option { return func(*testing.T) []Option { return o } }

func feat[F ~uint32](clusterID uint32, f F) Option { return withFeatures(clusterID, uint32(f)) }

var (
	wired    = feat(0x002F, psdef.FeatureWired)
	nodeTopo = feat(0x009C, ptdef.FeatureNodeTopology)
	acPower  = feat(0x0090, epmdef.FeatureAlternatingCurrent)
	forecast = feat(0x0098, demdef.FeaturePowerForecastReporting)
	heating  = feat(0x0201, tstatdef.FeatureHeating)
	// The DeviceEnergyManagement a composed ESA requires of its component
	// states PA (device_energy_management requirements of the composing
	// type).
	adjustable = feat(0x0098, demdef.FeaturePowerAdjustment|demdef.FeaturePowerForecastReporting)
)

// meteredSensor is an ElectricalSensor part as an ESA's composition asks
// for it: power with Voltage and ActiveCurrent, and imported and exported
// cumulative energy.
func meteredSensor(t *testing.T, features uint32) []Option {
	t.Helper()
	return []Option{
		nodeTopo,
		withFeatures(0x0090, features),
		withElements(0x0090, spec.Options{Attributes: []uint32{epmdef.AttrVoltage, epmdef.AttrActiveCurrent}}),
		feat(0x0091, eemdef.FeatureImportedEnergy|eemdef.FeatureExportedEnergy|eemdef.FeatureCumulativeEnergy),
	}
}

// tagListReason: the Descriptor the assembler mounts has no TagList
// feature; the device type mandates it.
const tagListReason = "the assembler's Descriptor has no TagList feature and endpoint.Spec cannot select one"

var fixtures = map[string]fixture{
	"BatteryStorage": {
		parts: func(t *testing.T, d *Device) {
			t.Helper()
			part(t, d, "ElectricalSensor", nil, meteredSensor(t, uint32(epmdef.FeatureAlternatingCurrent))...)
			part(t, d, "ElectricalSensor", nil, meteredSensor(t, uint32(epmdef.FeatureDirectCurrent))...)
			part(t, d, "PowerSource", nil, wired)
			part(t, d, "PowerSource", nil, feat(0x002F, psdef.FeatureBattery|psdef.FeatureRechargeable),
				withElements(0x002F, spec.Options{Attributes: []uint32{
					psdef.AttrBatVoltage, psdef.AttrBatPercentRemaining, psdef.AttrBatTimeRemaining, psdef.AttrActiveBatFaults,
					psdef.AttrBatCapacity, psdef.AttrBatTimeToFullCharge, psdef.AttrBatChargingCurrent, psdef.AttrActiveBatChargeFaults,
				}}))
			part(t, d, "DeviceEnergyManagement", nil, adjustable)
		},
		tolerate: map[string]string{
			"instanceCount device:ElectricalSensor#1": tagListReason, "instanceCount device:ElectricalSensor#2": tagListReason,
			"instanceCount device:PowerSource#1": tagListReason, "instanceCount device:PowerSource#2": tagListReason,
			"missing device:BatteryStorage/ElectricalSensor": tagListReason, "missing device:BatteryStorage/PowerSource": tagListReason,
		},
	},
	"Closure": {
		with:     opts(feat(0x0104, cccdef.FeaturePositioning)),
		tolerate: map[string]string{"missing Descriptor.TAGLIST": tagListReason},
	},
	"ClosurePanel": {
		with:     opts(feat(0x0105, cddef.FeaturePositioning|cddef.FeatureTranslation)),
		tolerate: map[string]string{"missing Descriptor.TAGLIST": tagListReason},
	},
	"DeviceEnergyManagement": {with: opts(forecast)},
	"ElectricalEnergyTariff": {tolerate: map[string]string{"missing Descriptor.TAGLIST": tagListReason}},
	"ElectricalMeter": {
		with: opts(acPower, feat(0x0091, eemdef.FeatureImportedEnergy|eemdef.FeatureCumulativeEnergy)),
		parts: func(t *testing.T, d *Device) {
			t.Helper()
			part(t, d, "ElectricalSensor", nil, nodeTopo, acPower)
		},
	},
	"ElectricalSensor": {with: opts(nodeTopo, acPower)},
	"EnergyEvse": {
		parts: func(t *testing.T, d *Device) {
			t.Helper()
			part(t, d, "PowerSource", nil, wired)
			part(t, d, "DeviceEnergyManagement", nil, forecast)
			part(t, d, "ElectricalSensor", nil, meteredSensor(t, uint32(epmdef.FeatureAlternatingCurrent))...)
		},
	},
	"GenericSwitch": {with: opts(feat(0x003B, switchdef.FeatureLatchingSwitch))},
	// A doorbell's button is momentary.
	"AudioDoorbell": {with: opts(feat(0x003B, switchdef.FeatureMomentarySwitch))},
	// An Intercom mandates a GenericSwitch component ("deviceType
	// GenericSwitch, M, 1" in parity/schema.json).
	"Intercom": {
		parts: func(t *testing.T, d *Device) {
			t.Helper()
			part(t, d, "GenericSwitch", nil, feat(0x003B, switchdef.FeatureMomentarySwitch))
		},
	},
	"HeatPump": {
		parts: func(t *testing.T, d *Device) {
			t.Helper()
			part(t, d, "PowerSource", nil, wired)
			part(t, d, "DeviceEnergyManagement", nil, adjustable)
			part(t, d, "ElectricalSensor", nil, meteredSensor(t, uint32(epmdef.FeatureAlternatingCurrent))...)
		},
		tolerate: map[string]string{"instanceCount device:PowerSource#1": tagListReason, "missing device:HeatPump/PowerSource": tagListReason},
	},
	"IrrigationSystem": {parts: func(t *testing.T, d *Device) {
		t.Helper()
		part(t, d, "WaterValve", nil)
	}},
	"MicrowaveOven":   {with: opts(feat(0x005F, mwocdef.FeaturePowerAsNumber))},
	"OccupancySensor": {with: opts(feat(0x0406, occdef.FeaturePassiveInfrared))},
	// The composites' components are parts (brief 27-B §4): matter.js
	// counts a simple component only on an endpoint below the composite
	// (DeviceTypeConformance.ts:596-613 at 862ad75f).
	"FloodlightCamera": {parts: func(t *testing.T, d *Device) {
		t.Helper()
		part(t, d, "OnOffLight", nil)
		part(t, d, "Camera", nil)
	}},
	"VideoDoorbell": {parts: func(t *testing.T, d *Device) {
		t.Helper()
		part(t, d, "Camera", nil)
		part(t, d, "Doorbell", nil)
	}},
	"Oven": {parts: func(t *testing.T, d *Device) {
		t.Helper()
		part(t, d, "TemperatureControlledCabinet", []string{"Heater"})
	}},
	"PowerSource": {with: opts(wired)},
	"Pump":        {with: opts(feat(0x0200, pumpdef.FeatureConstantSpeed))},
	"Refrigerator": {parts: func(t *testing.T, d *Device) {
		t.Helper()
		part(t, d, "TemperatureControlledCabinet", []string{"Cooler"})
	}},
	"RoomAirConditioner": {with: opts(heating)},
	// An Ethernet secondary interface; it states no condition, so none of
	// the three diagnostics is required.
	"SecondaryNetworkInterface": {with: opts(feat(0x0031, ncdef.FeatureEthernetNetworkInterface))},
	"SmokeCoAlarm": {
		with: opts(feat(0x005C, scadef.FeatureSmokeAlarm)),
		parts: func(t *testing.T, d *Device) {
			t.Helper()
			part(t, d, "PowerSource", nil, wired)
		},
	},
	"SolarPower": {
		parts: func(t *testing.T, d *Device) {
			t.Helper()
			part(t, d, "PowerSource", nil, wired)
			part(t, d, "ElectricalSensor", nil, meteredSensor(t, uint32(epmdef.FeatureAlternatingCurrent))...)
		},
		tolerate: map[string]string{"instanceCount device:PowerSource#1": tagListReason, "missing device:SolarPower/PowerSource": tagListReason},
	},
	"Thermostat":     {with: opts(heating)},
	"WindowCovering": {with: opts(feat(0x0102, wcdef.FeatureLift|wcdef.FeaturePositionAwareLift))},
}

// noFixture are the device types the table assembles no device of, with
// the reason.
var noFixture = map[string]string{
	"RootNode":     "the root endpoint is the node's (brief Q), not a bridged device",
	"Aggregator":   "the aggregator endpoint is the node's (brief Q), not a bridged device",
	"BridgedNode":  "the assembler adds BridgedNode to every bridged endpoint; a BridgedNode device would list it twice",
	"OtaRequestor": "no OTA Software Update Requestor server in the module takes a device-layer form (the reference bridge's is host code)",
}

// TestEveryTypeConstructsAndConforms: every generated device type
// constructs with its defaults, or refuses them for the reason listed in
// refusedDefaults; built as a host would build it (fixtures), the
// assembled endpoints have zero device-type violations, or exactly the
// ones the fixture tolerates, each with its reason.
func TestEveryTypeConstructsAndConforms(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, ty := range Types() {
		seen[ty.Name] = true
		t.Run(ty.Name, func(t *testing.T) {
			t.Parallel()
			_, err := New(ty, testOptions(ty.Name))
			want, refused := refusedDefaults[ty.Name]
			switch {
			case refused && err == nil:
				t.Fatalf("constructs with defaults, but refusedDefaults says %q", want)
			case refused && !strings.Contains(err.Error(), want):
				t.Fatalf("refused with %v, want %q", err, want)
			case !refused && err != nil:
				t.Fatalf("refuses its defaults: %v", err)
			}
			if reason, ok := noFixture[ty.Name]; ok {
				t.Skip(reason)
			}
			f := fixtures[ty.Name]
			d := build(t, ty)
			matched := map[string]bool{}
			for _, v := range violationsOf(assemble(t, d)) {
				if _, ok := f.tolerate[v.Key()]; ok {
					matched[v.Key()] = true
					continue
				}
				t.Errorf("device type violation: %s", v)
			}
			for key := range f.tolerate {
				if !matched[key] {
					t.Errorf("tolerated violation %q no longer occurs", key)
				}
			}
		})
	}
	for _, m := range []map[string]string{refusedDefaults, noFixture} {
		for name := range m {
			if !seen[name] {
				t.Errorf("%s is listed but not a generated type", name)
			}
		}
	}
	for name := range fixtures {
		if !seen[name] {
			t.Errorf("fixture %s is not a generated type", name)
		}
		if _, ok := noFixture[name]; ok {
			t.Errorf("%s has a fixture and is listed in noFixture", name)
		}
	}
}

// TestRefusalsWrapTheirSentinel: a refused default is an
// ErrConfiguration or an ErrNoDefaultServer.
func TestRefusalsWrapTheirSentinel(t *testing.T) {
	t.Parallel()
	for name := range refusedDefaults {
		_, err := New(typeByName(name), testOptions(name))
		if !errors.Is(err, ErrConfiguration) && !errors.Is(err, ErrNoDefaultServer) {
			t.Errorf("%s: %v wraps neither sentinel", name, err)
		}
	}
	if !slices.ContainsFunc(Types(), func(ty *Type) bool { return ty.Name == "DimmableLight" }) {
		t.Error("Types has no DimmableLight")
	}
}
