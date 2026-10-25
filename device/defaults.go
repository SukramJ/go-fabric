// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/alarmbase"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/filter"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	acfdef "github.com/SukramJ/go-fabric/cluster/spec/activatedcarbonfiltermonitoring"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	boolcfgdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	meteringdef "github.com/SukramJ/go-fabric/cluster/spec/commoditymetering"
	pricedef "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	dwadef "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
	doorlockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	ecodef "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
	griddef "github.com/SukramJ/go-fabric/cluster/spec/electricalgridconditions"
	evsedef "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	hepadef "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	washerdef "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	rfadef "github.com/SukramJ/go-fabric/cluster/spec/refrigeratoralarm"
	switchdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	tadef "github.com/SukramJ/go-fabric/cluster/spec/temperaturealarm"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	tuidef "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	switchserver "github.com/SukramJ/go-fabric/cluster/switch"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
)

// defaultBuild is what a cluster's default server is built from: the
// definition, the feature and element selection the device type and the
// host make, the initial values, the device's clock, and the servers of the
// device built before it (the endpoint's other clusters a server couples
// to).
type defaultBuild struct {
	def     *spec.Cluster
	opts    spec.Options
	cfg     spec.ServerConfig
	clock   clock.Clock
	sibling func(clusterID uint32) contract.ClusterServer
	// deviceType is the device's type id; key its StableKey, which names
	// what a server persists (a lock's records, a keypad lockout).
	deviceType uint16
	key        string
	// ports is the cluster's typed declaration (<cluster>def.Declare),
	// nil without one; transitions is its Transitions.
	ports       any
	transitions bool
	// initial are the values the server starts with; a factory deletes
	// the ones it consumes as configuration, and the rest are set through
	// the server's SetAttributes.
	initial map[uint32]any
}

// serverFactory builds a cluster's default server.
type serverFactory func(b defaultBuild) (contract.ClusterServer, error)

// ErrSelectionNotServed is a selection the cluster's self-held server does
// not serve, wrapping the server's reason. A cluster the host declared —
// a Declare with a port, or with features the kit's server takes without
// one — fails the device's construction with it: the host asked for the
// self-held server. For the device type's default selection the cluster
// keeps the generated server and the kit logs one Info line
// (device.default_server.generated).
var ErrSelectionNotServed = errors.New("device: the self-held server does not serve the selection")

// Logger receives the device kit's diagnostics (a cluster that keeps the
// generated server); replace it once, before building devices.
var Logger = slog.Default()

// notServed wraps a self-held server's refusal of the selection.
func notServed(err error) error { return fmt.Errorf("%w: %w", ErrSelectionNotServed, err) }

// selfHeldDefaults is the device kit's list of self-held defaults: every
// cluster for which the module has a server with its own command logic
// that runs on its own state, built here with the device type's feature
// selection and the device's clock — matter.js's default behaviors of the
// generated device files (packages/node/src/devices/dimmable-light.ts:
// 103-116 mounts LevelControlServer, OnOffServer, …; ADR 0018 decision 2:
// the self-held servers are the defaults once they hold their state). Every
// other cluster gets the generated [spec.Server], its commands
// unimplemented until the host registers them; a self-held server that
// needs a host port to run (a mover, an actuator, a measurement source) is
// a default once the device's declaration carries the port
// (<cluster>def.Declare), and the generated server until then.
//
// A self-held server is the default where it serves the selection the
// device type and the host make — its FeatureMap and every attribute,
// command and event the selection mandates; a selection it does not serve
// (WindowCovering's AbsolutePosition, a LevelControl with Frequency)
// keeps the generated server ([ErrSelectionNotServed]), so no selection
// that built a device before is refused now.
//
// The table is hand-written: each entry maps the selection onto its
// server's own configuration, which no generator knows.
//
// The fabric-scoped clusters, whose generated server [spec.NewServer]
// refuses ([spec.ErrFabricScoped]), take the core server here where it
// holds its own state: Binding and EcosystemInformation.
// OtaSoftwareUpdateRequestor has none: its server needs the node's
// connector to reach a provider (node/ota.go), so a device that mounts it —
// OtaRequestor — answers [ErrNoDefaultServer] and the host supplies it
// as the cluster's declared HostServer.
var selfHeldDefaults = map[uint32]serverFactory{
	onoffdef.ClusterID:      newOnOffServer,                // OnOff: cluster/onoff, matter.js OnOffServer.ts.
	lvldef.ClusterID:        newLevelControlServer,         // LevelControl: cluster/levelcontrol.New, matter.js LevelControlServer.ts.
	bindingdef.ClusterID:    newBindingServer,              // Binding: cluster/core.Binding, matter.js BindingServer.ts.
	switchdef.ClusterID:     newSwitchServer,               // Switch: cluster/switch, matter.js SwitchServer.ts.
	doorlockdef.ClusterID:   newDoorLockServer,             // DoorLock: cluster/lock, matter.js DoorLockServer.ts.
	wcdef.ClusterID:         newWindowCoveringServer,       // WindowCovering: cluster/cover, matter.js WindowCoveringServer.ts.
	thermostatdef.ClusterID: newThermostatServer,           // Thermostat: cluster/thermo, matter.js ThermostatServer.ts.
	ccdef.ClusterID:         newColorControlServer,         // ColorControl: cluster/light, matter.js ColorControlServer.ts.
	ecodef.ClusterID:        newEcosystemInformationServer, // EcosystemInformation: cluster/core.EcosystemInformation.
	fixedlabeldef.ClusterID: newFixedLabelServer,           // FixedLabel: cluster/core.NewFixedLabel, matter.js FixedLabelServer.ts.
	userlabeldef.ClusterID:  newUserLabelServer,            // UserLabel: cluster/core.NewUserLabel, matter.js UserLabelServer.ts.

	// The servers that run on a host port the declaration carries
	// (defaults_ports.go).
	fandef.ClusterID: newFanControlServer, // FanControl: cluster/fan, matter.js FanControlServer.ts.

	// The appliance, sensor-configuration and energy servers
	// (defaults_appliance.go).
	washerdef.ClusterID:   newLaundryWasherControlsServer,                          // LaundryWasherControls: cluster/appliance.
	rfadef.ClusterID:      alarmServer(alarmbase.NewRefrigeratorAlarm),             // RefrigeratorAlarm: cluster/alarmbase.
	dwadef.ClusterID:      alarmServer(alarmbase.NewDishwasherAlarm),               // DishwasherAlarm: cluster/alarmbase.
	tadef.ClusterID:       alarmServer(alarmbase.NewTemperatureAlarm),              // TemperatureAlarm: cluster/alarmbase.
	hepadef.ClusterID:     filterServer(filter.NewHepaFilterMonitoring),            // HepaFilterMonitoring: cluster/filter.
	acfdef.ClusterID:      filterServer(filter.NewActivatedCarbonFilterMonitoring), // ActivatedCarbonFilterMonitoring: cluster/filter.
	boolcfgdef.ClusterID:  newBooleanStateConfigurationServer,                      // BooleanStateConfiguration: cluster/boolcfg.
	pricedef.ClusterID:    newCommodityPriceServer,                                 // CommodityPrice: cluster/energy.
	evsedef.ClusterID:     newEnergyEvseServer,                                     // EnergyEvse: cluster/energy.
	griddef.ClusterID:     newGridConditionsServer,                                 // ElectricalGridConditions: cluster/energy.
	tuidef.ClusterID:      newThermostatUIServer,                                   // ThermostatUserInterfaceConfiguration: cluster/thermo.
	meteringdef.ClusterID: newCommodityMeteringServer,                              // CommodityMetering: cluster/energy.
}

// coupledLast are the defaults built after the device's other servers,
// because they couple to them: LevelControl follows OnOff and moves
// ColorControl's colour temperature.
var coupledLast = []uint32{lvldef.ClusterID}

// buildOrder returns the indexes of reqs in the order their defaults are
// built: the type's order, the coupled clusters last.
func buildOrder(reqs []ClusterRequirement) []int {
	order := make([]int, 0, len(reqs))
	var last []int
	for i := range reqs {
		if slices.Contains(coupledLast, reqs[i].ID) {
			last = append(last, i)
			continue
		}
		order = append(order, i)
	}
	return append(order, last...)
}

// newBindingServer is the module's Binding server ([core.NewBinding]), the
// default matter.js mounts where the device type — or Base, for a type
// that would miss it — mandates Binding
// (support/codegen/src/endpoints/RequirementGenerator.ts:143-155, 187-188;
// packages/node/src/devices/on-off-sensor.ts:91 `mandatory: { Identify:
// IdentifyServer, Binding: BindingServer }`; packages/node/src/endpoints/
// base.ts:15-19, 39 BindingServer as the default implementation). It keeps
// its own list and data version.
func newBindingServer(defaultBuild) (contract.ClusterServer, error) {
	return core.NewBinding(), nil
}

// newFixedLabelServer is the module's FixedLabel server
// ([core.NewFixedLabel]), empty; the declaration's Initial.LabelList is
// set on it as its labels. Base-optional on every device type (base.go).
func newFixedLabelServer(defaultBuild) (contract.ClusterServer, error) {
	return core.NewFixedLabel(nil)
}

// newUserLabelServer is the module's UserLabel server
// ([core.NewUserLabel]): a controller's write is held to the label limit
// (UserLabelServer.ts), the declaration's Initial.LabelList is its first
// list, and a node persists the list under the device's key
// (node/persist.go, persistUserLabels). Base-optional on every device type.
func newUserLabelServer(defaultBuild) (contract.ClusterServer, error) {
	return core.NewUserLabel(core.UserLabelConfig{})
}

// newEcosystemInformationServer is the module's EcosystemInformation
// server ([core.NewEcosystemInformation]) over empty directories, the state
// matter.js's default server starts with
// (packages/node/src/behaviors/ecosystem-information/
// EcosystemInformationServer.ts:14, no server logic; both attributes are
// lists). A host whose bridged device belongs to an ecosystem supplies its
// own server over its directory as the declared HostServer.
func newEcosystemInformationServer(defaultBuild) (contract.ClusterServer, error) {
	return core.NewEcosystemInformation(emptyEcosystem{}), nil
}

// emptyEcosystem is an ecosystem directory with no devices and no
// locations.
type emptyEcosystem struct{}

func (emptyEcosystem) EcosystemDevices(context.Context) []ecodef.EcosystemDeviceStruct { return nil }

func (emptyEcosystem) EcosystemLocations(context.Context) []ecodef.EcosystemLocationStruct {
	return nil
}

// newOnOffServer is the module's OnOff server ([onoff.New]). A device of
// this layer sits under the Aggregator, so StartUpOnOff is not applied
// (OnOffServer.ts:37, `!this.endpoint.ownerOfType(AggregatorEndpoint)`).
// Its countdown runs on the device's clock, the node's once it is added.
// A declared Sink (onoffdef.Declare) switches the device.
func newOnOffServer(b defaultBuild) (contract.ClusterServer, error) {
	cfg := onoff.Config{
		Features:    onoff.Feature(b.opts.Features),
		DataVersion: b.cfg.DataVersion,
		Initial:     b.cfg.Initial,
		Bridged:     true,
		TimeSource:  b.clock,
	}
	if d, ok := b.ports.(onoffdef.Declare); ok && d.Sink != nil {
		cfg.Sink = d.Sink
	}
	return onoff.New(cfg)
}

// newLevelControlServer is the module's LevelControl server that holds its
// own level ([levelcontrol.New]), with the endpoint's OnOff and
// ColorControl servers to couple to: its "with On/Off" commands switch
// OnOff, OnOff turning on moves to OnLevel, and the CoupleColorTempToLevel
// option moves the colour temperature — matter.js LevelControlServer's
// couplings (LevelControlServer.ts:141-143, :556-617, :658-670). The level
// changes at once, matter.js's default (managedTransitionTimeHandling
// false, :782-790), unless the declaration asks for Transitions. A
// declared LevelSource (levelcontroldef.Declare) is the device's level:
// the server drives it instead of holding one. A declared Ramp is a device
// that ramps by itself: the server drives the kit's NativeSource on it.
func newLevelControlServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(lvldef.Declare)
	if d.Ramp != nil && d.LevelSource != nil {
		return nil, fmt.Errorf("%w: levelcontrol: Ramp and LevelSource are exclusive", ErrConfiguration)
	}
	if d.Ramp != nil && d.Transitions {
		return nil, fmt.Errorf("%w: levelcontrol: Ramp and Transitions are exclusive", ErrConfiguration)
	}
	if d.Ramp != nil {
		d.LevelSource = nativeSource(b, d.Ramp)
	}
	if d.LevelSource != nil {
		return newLevelControlPortServer(b, d)
	}
	cfg := levelcontrol.HeldConfig{Features: b.opts.Features, DataVersion: b.cfg.DataVersion, Clock: b.clock, ManageTransitions: b.transitions}
	if srv := b.sibling(onoffdef.ClusterID); srv != nil {
		cfg.OnOff = onOffOf(srv)
	}
	if ct, ok := b.sibling(ccdef.ClusterID).(levelcontrol.ColorTemperatureCoupling); ok {
		cfg.ColorTemperature = ct
	}
	srv, err := levelcontrol.New(cfg)
	if errors.Is(err, levelcontrol.ErrFeatures) {
		return nil, notServed(err)
	}
	return srv, err
}

// newColorControlServer is the module's ColorControl server
// ([light.NewColorControl]), gated on the endpoint's OnOff for the
// ExecuteIfOff option (ColorControlServer.ts #optionsAllowExecution). Its
// commands apply their target at once, matter.js's default
// (managedTransitionTimeHandling false).
func newColorControlServer(b defaultBuild) (contract.ClusterServer, error) {
	cfg := light.ColorControlServerConfig{Features: light.ColorFeature(b.opts.Features), Clock: b.clock, ManageTransitions: b.transitions}
	if srv := b.sibling(onoffdef.ClusterID); srv != nil {
		cfg.OnOff = onOffOf(srv)
	}
	colorConfigFrom(&cfg, b.initial)
	srv, err := light.NewColorControl(cfg)
	if errors.Is(err, light.ErrColorFeatures) {
		return nil, notServed(err)
	}
	if err != nil {
		return nil, err
	}
	if d, ok := b.ports.(ccdef.Declare); ok {
		if d.ColorTemperatureWriter != nil {
			srv.SetWriter(d.ColorTemperatureWriter)
		}
		if d.ColorWriter != nil {
			srv.SetColorWriter(d.ColorWriter)
		}
	}
	return srv, nil
}

// colorConfigFrom takes the ColorControl configuration the declaration's
// Initial states — the physical colour-temperature range, the coupling
// floor, the colour the light starts with and its mode — into cfg, whose
// fields name them otherwise.
func colorConfigFrom(cfg *light.ColorControlServerConfig, initial map[uint32]any) {
	for id, dst := range map[uint32]*uint16{
		ccdef.AttrColorTempPhysicalMinMireds: &cfg.MinMireds, ccdef.AttrColorTempPhysicalMaxMireds: &cfg.MaxMireds,
		ccdef.AttrColorTemperatureMireds: &cfg.InitialMireds, ccdef.AttrCoupleColorTempToLevelMinMireds: &cfg.CoupleColorTempToLevelMinMireds,
		ccdef.AttrCurrentX: &cfg.InitialX, ccdef.AttrCurrentY: &cfg.InitialY, ccdef.AttrEnhancedCurrentHue: &cfg.InitialEnhancedHue,
	} {
		if v, ok := consume(initial, id); ok && v <= math.MaxUint16 {
			*dst = uint16(v)
		}
	}
	for id, dst := range map[uint32]*uint8{ccdef.AttrCurrentHue: &cfg.InitialHue, ccdef.AttrCurrentSaturation: &cfg.InitialSaturation} {
		if v, ok := consume8(initial, id); ok {
			*dst = v
		}
	}
	// The mode the light starts in: EnhancedColorMode, else ColorMode (the
	// two read the same but for the enhanced hue mode).
	mode, ok := consume8(initial, ccdef.AttrEnhancedColorMode)
	if m, has := consume8(initial, ccdef.AttrColorMode); has && !ok {
		mode, ok = m, true
	}
	if ok {
		m := light.ColorMode(mode)
		cfg.InitialColorMode = &m
	}
}

// newWindowCoveringServer is the module's WindowCovering server
// ([cover.New]): without a mover a command moves the covering at once,
// matter.js's WindowCoveringServer without a handleMovement of its own; a
// declared Mover (windowcoveringdef.Declare) moves the device.
func newWindowCoveringServer(b defaultBuild) (contract.ClusterServer, error) {
	cfg := cover.Config{FeatureMap: b.opts.Features, Clock: b.clock}
	if d, ok := b.ports.(wcdef.Declare); ok && d.Mover != nil {
		cfg.Mover = d.Mover
	}
	if b.transitions {
		cfg.MoveStep = coverMoveStep
	}
	// Type and EndProductType are the configuration's; the position the
	// covering starts at is the current and the target position.
	configFrom(&cfg, b.def, b.initial)
	if v, ok := consume(b.initial, wcdef.AttrCurrentPositionLiftPercent100ths); ok && v <= math.MaxUint16 {
		cfg.InitialPositionPercent100ths = uint16(v)
		delete(b.initial, wcdef.AttrTargetPositionLiftPercent100ths)
	}
	if v, ok := consume(b.initial, wcdef.AttrCurrentPositionTiltPercent100ths); ok && v <= math.MaxUint16 {
		cfg.InitialTiltPositionPercent100ths = uint16(v)
		delete(b.initial, wcdef.AttrTargetPositionTiltPercent100ths)
	}
	srv, err := cover.New(cfg)
	if errors.Is(err, cover.ErrFeatureMap) {
		return nil, notServed(err)
	}
	return srv, err
}

// coverMoveStep is a travelling movement's step: matter.js's CHIP test
// node moves in six steps of 950 ms (support/chip-testing/src/cluster/
// TestWindowCoveringServer.ts).
const coverMoveStep = 950 * time.Millisecond

// newThermostatServer is the module's Thermostat server ([thermo.New]),
// at its default limits and setpoints; a declared Sink
// (thermostatdef.Declare) is told every change it admits.
func newThermostatServer(b defaultBuild) (contract.ClusterServer, error) {
	// The element's defaults: the absolute limits 7 / 30 °C (heat) and
	// 16 / 32 °C (cool), the occupied setpoints 20 and 26 °C (matter.js
	// packages/model/src/standard/elements/thermostat-cluster.element.ts:
	// 44-72) — thermo.DefaultThermostatConfig's.
	cfg := thermo.DefaultThermostatConfig()
	cfg.Features, cfg.Clock = b.opts.Features, b.clock
	if d, ok := b.ports.(thermostatdef.Declare); ok {
		if d.Sink != nil {
			cfg.Sink = d.Sink
		}
		if d.PresetStore != nil {
			cfg.PresetStore = d.PresetStore
		}
	}
	// The limits and the presets the device ships with (NumberOfPresets,
	// PresetTypes, Presets, ActivePresetHandle) are the server's
	// configuration; the setpoints it starts with are too, under their
	// Config names.
	configFrom(&cfg, b.def, b.initial)
	for id, dst := range map[uint32]*int16{
		thermostatdef.AttrOccupiedHeatingSetpoint: &cfg.InitialHeatingSetpoint,
		thermostatdef.AttrOccupiedCoolingSetpoint: &cfg.InitialCoolingSetpoint,
	} {
		if v, ok := b.initial[id].(int16); ok {
			*dst = v
			delete(b.initial, id)
		}
	}
	for id, dst := range map[uint32]**int16{
		thermostatdef.AttrUnoccupiedHeatingSetpoint: &cfg.InitialUnoccupiedHeatingSetpoint,
		thermostatdef.AttrUnoccupiedCoolingSetpoint: &cfg.InitialUnoccupiedCoolingSetpoint,
	} {
		if v, ok := b.initial[id].(int16); ok {
			*dst = &v
			delete(b.initial, id)
		}
	}
	srv, err := thermo.New(cfg)
	if errors.Is(err, thermo.ErrFeatures) {
		return nil, notServed(err)
	}
	return srv, err
}

// newDoorLockServer is the module's DoorLock server ([lock.New]): with a
// declared StateSource (doorlockdef.Declare) the lock's actuator moves the
// bolt and reports it, without one the server keeps its own lock state —
// matter.js's DoorLockServer, whose default handleLockOperation moves
// nothing. An empty selection is none, as matter.js's.
func newDoorLockServer(b defaultBuild) (contract.ClusterServer, error) {
	cfg := lock.DoorLockConfig{Features: lock.Feature(b.opts.Features), DataVersion: b.cfg.DataVersion, Clock: b.clock}
	if d, ok := b.ports.(doorlockdef.Declare); ok {
		if d.StateSource != nil {
			cfg.Source = d.StateSource
		}
		if d.AuthStore != nil {
			cfg.Auth, cfg.AuthKey = d.AuthStore, b.key
		}
	}
	// The lock's capacities and policies (the user and credential counts,
	// the PIN lengths, WrongCodeEntryLimit, AutoRelockTime, …) are its
	// configuration.
	configFrom(&cfg, b.def, b.initial)
	srv, err := lock.New(cfg)
	if errors.Is(err, lock.ErrConfig) {
		return nil, notServed(err)
	}
	return srv, err
}

// newSwitchServer is the module's Switch server ([switchserver.New]).
// NumberOfPositions, MultiPressMax and the position it starts at come from
// the declaration's Initial.
func newSwitchServer(b defaultBuild) (contract.ClusterServer, error) {
	cfg := switchserver.Config{Features: switchserver.Feature(b.opts.Features), Clock: b.clock, DataVersion: b.cfg.DataVersion}
	configFrom(&cfg, b.def, b.initial)
	return switchserver.New(cfg)
}

// onOffPort is the endpoint's OnOff server as the On/Off state a
// LevelControl or ColorControl server couples to: read through its
// attribute, set through its [spec.AttributeSetter] (the state write
// matter.js's coupling makes, `onOff.state.onOff = …`,
// LevelControlServer.ts:597-617), and followed through its change
// notification. It works over any OnOff server the host mounts.
type onOffPort struct{ srv contract.ClusterServer }

func onOffOf(srv contract.ClusterServer) onOffPort { return onOffPort{srv: srv} }

// OnOff implements [levelcontrol.OnOff] and [light.OnOffState].
func (p onOffPort) OnOff() bool {
	v, _ := p.srv.MatterRead(onoff.AttrOnOff)
	on, _ := v.(bool)
	return on
}

// SetOnOff implements [levelcontrol.OnOff].
func (p onOffPort) SetOnOff(_ context.Context, on bool) error {
	setter, ok := p.srv.(spec.AttributeSetter)
	if !ok {
		return fmt.Errorf("%w: %T", spec.ErrNotSettable, p.srv)
	}
	return setter.SetAttributes(map[uint32]any{onoff.AttrOnOff: on})
}

// OnAttributeChange implements [contract.AttributeChangeNotifier]
// over the OnOff server's own notification; a server without one reports
// nothing.
func (p onOffPort) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	if n, ok := p.srv.(contract.AttributeChangeNotifier); ok {
		return n.OnAttributeChange(cb)
	}
	return func() {}
}

// applyInitial sets the initial values on a self-held default through its
// [spec.AttributeSetter] — the device type's defaults and the host's
// declaration's Initial — as a matter.js endpoint starts its behaviors' state with
// them.
func applyInitial(name string, srv contract.ClusterServer, initial map[uint32]any) error {
	if len(initial) == 0 {
		return nil
	}
	setter, ok := srv.(spec.AttributeSetter)
	if !ok {
		return fmt.Errorf("%s: %w: %T", name, spec.ErrNotSettable, srv)
	}
	if err := setter.SetAttributes(initial); err != nil {
		return fmt.Errorf("%s: initial values: %w", name, err)
	}
	return nil
}

// checkSelection reports a self-held default that does not advertise the
// selection the device type and the host made: its FeatureMap, the
// attributes, commands and events the selection mandates.
func checkSelection(name string, srv contract.ClusterServer, opts spec.Options) error {
	if fm, ok := srv.MatterRead(spec.AttrFeatureMap); ok {
		if got, _ := fm.(uint32); got != opts.Features {
			return notServed(fmt.Errorf("device: %s: the server advertises FeatureMap 0x%X, the selection is 0x%X", name, got, opts.Features))
		}
	}
	if l, ok := srv.(contract.ClusterAttributeLister); ok {
		if missing := absent(opts.Attributes, l.MatterAttributes()); len(missing) > 0 {
			return notServed(fmt.Errorf("device: %s: the server does not serve attributes %v the selection mandates", name, missing))
		}
	}
	if l, ok := srv.(contract.ClusterCommandLister); ok {
		if missing := absent(opts.Commands, l.MatterAcceptedCommands()); len(missing) > 0 {
			return notServed(fmt.Errorf("device: %s: the server does not accept commands %v the selection mandates", name, missing))
		}
	}
	if l, ok := srv.(contract.ClusterEventLister); ok {
		if missing := absent(opts.Events, l.MatterEvents()); len(missing) > 0 {
			return notServed(fmt.Errorf("device: %s: the server does not emit events %v the selection mandates", name, missing))
		}
	}
	return nil
}

// absent are the ids of want that have does not hold.
func absent(want, have []uint32) []uint32 {
	var out []uint32
	for _, id := range want {
		if !slices.Contains(have, id) {
			out = append(out, id)
		}
	}
	return out
}
