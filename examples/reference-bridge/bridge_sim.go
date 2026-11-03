// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	descriptordef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	psdef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	tmdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/schema"
)

// --bridge-sim (TESTING ONLY): the bridge the bridge-client certification
// case TC-BR-4 runs the module's controller against, after matter.js's
// bridge test instance (support/chip-testing/src/BridgeTestInstance.ts at
// 862ad75f), which lays its devices out as chip's bridge-app does: a
// light, two temperature sensors, a battery-powered composed device
// carrying two more temperature sensors as its own parts, and four further
// lights (:148-172); a second light the app pipe adds, and the first one
// it removes — "not inverses of one another" (:27-28).
//
// The composition is matter.js's by meaning, not by number: an endpoint
// number here is the node's persisted allocation (endpoint/identity.go),
// which a host cannot choose, so the case finds each device by its
// NodeLabel — the labels are the test instance's. The devices come after
// every other device of the daemon, so the default composition — and the
// PICS slices and goldens the chip-tool leg holds it to — does not move.
//
// The app pipe's five commands are the test instance's backchannel
// (:177-203; packages/testing/src/device/backchannel.ts:134-150):
// toggleBridgedLights, warmBridgedTemperatureSensors, renameBridgedLights,
// addBridgedLight and removeBridgedLight, each without further fields.

// Bridge-simulation constants (BridgeTestInstance.ts).
const (
	// bridgeSimOneDegree is how far one warming moves a sensor, in the
	// hundredths of a degree the cluster reports (ONE_DEGREE, :46).
	bridgeSimOneDegree = 100
	// bridgeSimInitialCelsius is every sensor's starting reading:
	// INITIAL_MEASURED_VALUE 100 (:50) in hundredths of a degree; the
	// declaration's bounds are MIN_MEASURED_VALUE and MAX_MEASURED_VALUE
	// (:48-49), temperatureDeclaration's.
	bridgeSimInitialCelsius = 1
)

// The NodeLabels of the test instance's devices (:152-172, :251).
const (
	bridgeSimLight1Label   = "Light 1"
	bridgeSimLight2Label   = "Light 2"
	bridgeSimComposedLabel = "Composed Device"
)

// bridgeSimRenames are the labels renameBridgedLights gives the two lights
// (:234-237).
var bridgeSimRenames = [2]string{"Light 1b", "Light 2b"}

// demoBridgeSim is the simulated bridge: the device handles the app pipe
// acts on, and the Aggregator it adds and removes lights through.
type demoBridgeSim struct {
	// light1 and light2 are the two lights the commands name (:207);
	// light2 is built up front and added only by addBridgedLight.
	light1, light2 *bridgeSimLight
	// actionLights are "Action Light 1"–"4" (:169-172), which no command
	// touches.
	actionLights []*device.OnOffLight
	// sensors are "TempSensor 1", "TempSensor 2" and the composed device's
	// two parts, in the order #warmTemperatureSensors names them
	// (:218-223).
	sensors  []*device.TemperatureSensor
	composed *device.BridgedNode

	mu  sync.Mutex
	agg *node.Aggregator
}

// bridgeSimLight is a bridged On/Off Light and whether the node holds it.
type bridgeSimLight struct {
	light *demoLight
	dev   *device.OnOffLight
	added bool
}

// bridgeSimNumberTag is the CommonNumberTag (namespace 0x07) tag n with
// its label, as matter.js's SemanticNamespace builds it
// (packages/node/src/tags/CommonNumberTag.ts, SemanticNamespace.ts:21-31).
func bridgeSimNumberTag(n uint8, label string) device.SemanticTag {
	return device.SemanticTag{
		MfgCode: spec.Nullable[uint16]{Null: true}, NamespaceId: descriptordef.NamespaceCommonNumber,
		Tag: n, Label: &spec.Nullable[string]{Value: label},
	}
}

func newDemoBridgeSim(f *fleet) (*demoBridgeSim, error) {
	s := &demoBridgeSim{}
	var errs []error
	light := func(key, label string) *bridgeSimLight {
		l := newDemoLight(label)
		dev, err := l.device(f.bridged(key, label))
		errs = append(errs, err)
		return &bridgeSimLight{light: l, dev: dev}
	}
	sensor := func(opts device.Options) *device.TemperatureSensor {
		d, err := device.NewTemperatureSensor(opts, temperatureDeclaration(bridgeSimInitialCelsius))
		errs = append(errs, err)
		return d
	}
	// The composed device's sensors share a device type, so each carries a
	// number tag that tells them apart (#temperatureSensor, :306-317).
	tagged := func(opts device.Options, tag device.SemanticTag) device.Options {
		opts.Tags = []device.SemanticTag{tag}
		return opts
	}
	s.light1 = light("bridgesim-light1", bridgeSimLight1Label)
	s.light2 = light("bridgesim-light2", bridgeSimLight2Label)
	s.sensors = []*device.TemperatureSensor{
		sensor(f.bridged("bridgesim-temp1", "TempSensor 1")),
		sensor(f.bridged("bridgesim-temp2", "TempSensor 2")),
		// The composed device's sensors are its parts and carry no
		// BridgedDeviceBasicInformation of their own (:159-167).
		sensor(tagged(f.part("demo:bridgesim-composed:temp1"), bridgeSimNumberTag(1, "One"))),
		sensor(tagged(f.part("demo:bridgesim-composed:temp2"), bridgeSimNumberTag(2, "Two"))),
	}
	for i := range 4 {
		label := fmt.Sprintf("Action Light %d", i+1)
		l := light(fmt.Sprintf("bridgesim-actionlight%d", i+1), label)
		s.actionLights = append(s.actionLights, l.dev)
	}
	// The composed device: a Bridged Node with the PowerSource server's
	// Battery feature and two Temperature Sensors as its own parts, listing
	// Power Source beside Bridged Node (#composedDevice, :323-351;
	// deviceTypeList :330-335).
	composedOpts := f.bridged("bridgesim-composed", bridgeSimComposedLabel)
	composedOpts.DeviceTypes = []schema.DeviceTypeID{device.PowerSourceID}
	composed, err := device.NewBridgedNode(composedOpts, bridgeSimBattery())
	errs = append(errs, err)
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("bridge simulation: %w", err)
	}
	for _, part := range s.sensors[2:] {
		if err := composed.AddPart(part.Device); err != nil {
			return nil, fmt.Errorf("bridge simulation: %w", err)
		}
	}
	s.composed = composed
	return s, nil
}

// bridgeSimBattery is the composed device's PowerSource (:337-349): the
// Battery feature, Active, order 0, "Battery", charge level Ok
// (BAT_CHARGE_LEVEL, :55-62), no replacement needed, not replaceable. Its
// EndpointList names the composed endpoint and its two parts once the node
// numbered them ([demoBridgeSim.numberPowerSource]).
//
// Bridged Node lists PowerSource under the condition
// BridgedPowerSourceInfo (device/bridged_node_gen.go), so the declaration
// mounts it as a declared optional server; the condition itself is not
// stated — the kit states it only for a device type that does not list
// PowerSource (device/base.go withBridgedPowerSource) — and stating it
// would make PowerSourceConfiguration mandatory, which matter.js's
// composed device does not serve either.
func bridgeSimBattery() device.Declaration {
	status, order, description := psdef.PowerSourceStatusActive, uint8(0), "Battery"
	level, needed, replace := psdef.BatChargeLevelOk, false, psdef.BatReplaceabilityNotReplaceable
	return psdef.Declare{Features: psdef.FeatureBattery, Initial: psdef.Patch{
		Status: &status, Order: &order, Description: &description, BatChargeLevel: &level,
		BatReplacementNeeded: &needed, BatReplaceability: &replace, EndpointList: &[]uint16{},
	}}
}

// endpoints are the simulated bridge's devices in the order the test
// instance adds them (:152-172): "Light 1", the two sensors, the composed
// device with its parts, the four action lights. "Light 2" is not among
// them until addBridgedLight.
func (s *demoBridgeSim) endpoints() []node.Endpoint {
	s.light1.added = true
	out := make([]node.Endpoint, 0, 4+len(s.actionLights))
	out = append(out, s.light1.dev, s.sensors[0], s.sensors[1], s.composed)
	for _, l := range s.actionLights {
		out = append(out, l)
	}
	return out
}

// attach hands the simulation the Aggregator its add and remove act on.
func (s *demoBridgeSim) attach(agg *node.Aggregator) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.agg = agg
	s.mu.Unlock()
}

// numberPowerSource sets the composed device's EndpointList to the
// endpoints the node serves it and its parts under (:344-348), once the
// node has numbered them.
func (s *demoBridgeSim) numberPowerSource() error {
	if s == nil {
		return nil
	}
	devices := append([]*device.Device{s.composed.Device}, s.composed.Parts()...)
	list := make([]uint16, 0, len(devices))
	for _, d := range devices {
		n, ok := d.EndpointNumber()
		if !ok {
			return errors.New("the composed device has no endpoint number yet")
		}
		list = append(list, n)
	}
	return s.composed.PowerSource().Set(psdef.Patch{EndpointList: &list})
}

// errNoBridgeSim is a bridge-simulation command without the flag.
func errNoBridgeSim(name string) error { return fmt.Errorf("%s needs --bridge-sim", name) }

// toggle is #toggleLights (:205-214): OnOff toggled on the two lights the
// command names, a light the bridge does not hold skipped; the action
// lights are left alone.
func (s *demoBridgeSim) toggle() error {
	if s == nil {
		return errNoBridgeSim("toggleBridgedLights")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range []*bridgeSimLight{s.light1, s.light2} {
		if l.added {
			l.light.setOn(!l.light.isOn())
		}
	}
	return nil
}

// warm is #warmTemperatureSensors (:216-230): every sensor's MeasuredValue
// one degree up, from the initial value where it holds none.
func (s *demoBridgeSim) warm() error {
	if s == nil {
		return errNoBridgeSim("warmBridgedTemperatureSensors")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for _, d := range s.sensors {
		srv := d.TemperatureMeasurement()
		v := int16(bridgeSimInitialCelsius * 100)
		if cur := srv.Get().MeasuredValue; cur != nil {
			v = *cur
		}
		v += bridgeSimOneDegree
		errs = append(errs, srv.Set(tmdef.Patch{MeasuredValue: &v}))
	}
	return errors.Join(errs...)
}

// rename is #renameLights (:232-244): "Light 1b" and "Light 2b" on
// whichever of the two lights the bridge holds — the device's Rename,
// which reports the NodeLabel to a subscribed controller
// (device/lifecycle.go Rename, node/lifecycle.go NotifyEndpointNodeLabel).
func (s *demoBridgeSim) rename() error {
	if s == nil {
		return errNoBridgeSim("renameBridgedLights")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, l := range []*bridgeSimLight{s.light1, s.light2} {
		if l.added {
			l.dev.Rename(context.Background(), bridgeSimRenames[i])
		}
	}
	return nil
}

// add is #addLight2 (:246-252): the second light under the Aggregator, a
// no-op when it is there. Its StableKey is its own, so the node gives it a
// number no device had (node/aggregator.go Add); the running node
// reassembles and reports the root's and the Aggregator's PartsList and
// every attribute of the new endpoint (bridge/bridge.go Reassemble).
func (s *demoBridgeSim) add() error {
	if s == nil {
		return errNoBridgeSim("addBridgedLight")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.light2.added {
		return nil
	}
	if s.agg == nil {
		return errors.New("addBridgedLight: the bridge simulation has no Aggregator")
	}
	if err := s.agg.Add(context.Background(), s.light2.dev); err != nil {
		return fmt.Errorf("addBridgedLight: %w", err)
	}
	s.light2.added = true
	return nil
}

// remove is #removeLight1 (:254-261): the first light taken off the
// Aggregator, a no-op when it is gone. Its key keeps its number reserved
// (node/aggregator.go Remove), so the second light never takes it.
func (s *demoBridgeSim) remove() error {
	if s == nil {
		return errNoBridgeSim("removeBridgedLight")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.light1.added {
		return nil
	}
	if s.agg == nil {
		return errors.New("removeBridgedLight: the bridge simulation has no Aggregator")
	}
	if err := s.agg.Remove(context.Background(), s.light1.dev.Spec().StableKey); err != nil {
		return fmt.Errorf("removeBridgedLight: %w", err)
	}
	s.light1.added = false
	return nil
}
