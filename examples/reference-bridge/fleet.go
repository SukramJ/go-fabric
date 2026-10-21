// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"

	"github.com/SukramJ/go-fabric/cluster"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/modeselect"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
)

// --- the change-notification fan-out the devices share -------------------

// notifier is the host half of [contract.ChangeNotifier]: a device fires it
// once its own state has moved, and the bridge — which subscribed to the
// device's endpoint source at mount time (device.WithChangeNotifier hands
// the notifier to the device layer) — marks that endpoint's reportable
// attribute paths dirty, so the next subscription tick ships the new value.
//
// A device that changes state without firing it stays invisible to a
// subscribed controller until the controller reads again, which is what
// makes this the load-bearing half of a bridged device rather than a
// convenience.
type notifier struct {
	cbMu sync.Mutex
	cbs  map[uint64]func()
	next uint64
}

// OnMatterValueChanged implements [contract.ChangeNotifier].
func (n *notifier) OnMatterValueChanged(cb func()) (unsubscribe func()) { //nolint:unparam // the contract's signature; the bridge keeps the closure
	if cb == nil {
		return func() {}
	}
	n.cbMu.Lock()
	if n.cbs == nil {
		n.cbs = make(map[uint64]func())
	}
	id := n.next
	n.next++
	n.cbs[id] = cb
	n.cbMu.Unlock()
	return func() {
		n.cbMu.Lock()
		delete(n.cbs, id)
		n.cbMu.Unlock()
	}
}

// notify fans the change out. The callbacks run outside the lock, and every
// caller below releases its own device lock first: a callback re-enters the
// bridge, which reads the endpoint's attributes back out of the same device.
func (n *notifier) notify() {
	n.cbMu.Lock()
	cbs := make([]func(), 0, len(n.cbs))
	for _, cb := range n.cbs {
		cbs = append(cbs, cb)
	}
	n.cbMu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

// --- device 1: an on/off light ------------------------------------------

// demoLight is a hand-built stand-in for a real device: an on/off light.
// Its OnOff state lives in the device layer's default server,
// cluster/onoff.Server, which holds the Lighting state matter.js's
// OnOffServer.ts holds — GlobalSceneControl, OnTime, OffWaitTime,
// StartUpOnOff and the two countdowns. The host changes the state with
// [onoff.Server.Set] and hears a controller's change through the device's
// OnChanged hook.
type demoLight struct {
	name string

	mu  sync.Mutex
	srv *onoffdef.Server
}

func newDemoLight(name string) *demoLight { return &demoLight{name: name} }

// device builds the light: OnOffLight (on-off-light.element.ts: Identify,
// Groups and ScenesManagement are the assembler's; OnOff with LIGHTING
// :24-25 is the device layer's default server) with GlobalSceneControl
// starting true, as the daemon has always served it.
//
// FixedLabel rides along: the harness points the certification cases
// written against the CHIP all-clusters app's endpoint 1
// (TC_FLABEL_2_1.py `--endpoint 1`) at the daemon's first bridged light,
// and that app serves FixedLabel there. Base allows the cluster on any
// endpoint.
func (d *demoLight) device(opts device.Options) (*device.OnOffLight, error) {
	fixedLabel, err := mattercore.NewFixedLabel([]fixedlabeldef.LabelStruct{{Label: "room", Value: "demo"}})
	if err != nil {
		return nil, fmt.Errorf("light FixedLabel: %w", err)
	}
	dev, err := device.NewOnOffLight(opts,
		onoffdef.Declare{Initial: onoffdef.Patch{GlobalSceneControl: new(true)}},
		fixedlabeldef.Declare{HostServer: fixedLabel})
	if err != nil {
		return nil, err
	}
	srv := dev.OnOff()
	d.mu.Lock()
	d.srv = srv
	d.mu.Unlock()
	followOnOff(dev.Device, srv, func(on bool) {
		slog.Info("light.set", slog.String("device", d.name), slog.Bool("on", on))
	})
	return dev, nil
}

// server returns the light's OnOff server; nil before [demoLight.device].
func (d *demoLight) server() *onoffdef.Server {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.srv
}

// setOn is the light switched at the device, not by a controller (the demo
// actions): the state is stored and reported, as a matter.js host sets
// behavior state.
func (d *demoLight) setOn(on bool) {
	if srv := d.server(); srv != nil {
		_ = srv.Set(onoffdef.Patch{OnOff: new(on)})
	}
}

// isOn reads the light's OnOff state.
func (d *demoLight) isOn() bool {
	srv := d.server()
	return srv != nil && srv.Get().OnOff
}

// followOnOff calls changed with the new value whenever the OnOff
// attribute of srv changes — a command, a countdown that ends, a host
// Set — through the device's OnChanged hook (matter.js
// `events.onOff.onOff$Changed`). The hook runs while the server holds its
// operation lock: changed may read any server of the endpoint, but must
// not invoke or Set the OnOff server itself.
func followOnOff(dev *device.Device, srv *onoffdef.Server, changed func(on bool)) {
	dev.OnChanged(onoff.ClusterID, func(c contract.Change) {
		if slices.Contains(c.Attributes, onoff.AttrOnOff) {
			changed(srv.Get().OnOff)
		}
	})
}

// --- device 2: a temperature sensor -------------------------------------

// demoThermometer is a measurement-only device. It implements
// [contract.FloatMeasurementSource] and nothing else: the assembler picks
// the cluster (TemperatureMeasurement 0x0402) and the standalone device type
// from the declared class, so no cluster server is written on this side.
type demoThermometer struct {
	mu      sync.RWMutex
	celsius float64
}

func newDemoThermometer(celsius float64) *demoThermometer {
	return &demoThermometer{celsius: celsius}
}

// MatterMeasurementClass implements [contract.MeasurementSource].
func (t *demoThermometer) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementTemperature
}

// MatterFloatValue implements [contract.FloatMeasurementSource]. The unit is
// the model's own — °C for temperature; the cluster server converts.
func (t *demoThermometer) MatterFloatValue() (float64, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.celsius, true
}

// --- device 3: an irrigation valve --------------------------------------

// demoValve is a hand-built stand-in for a garden irrigation valve: a
// motorised head that is either open or closed, plus the timer that ends a
// timed opening.
//
// What the model does and does not claim:
//
//   - The head travels for [valveTravel]. Open and Close set TargetState
//     and report CurrentState Transitioning; when the head arrives,
//     CurrentState reads the new position and TargetState goes back to
//     null — the specification's "no target is set, the change is done"
//     (TC-VALCC-3.1 waits for exactly that pair of reports).
//   - The timer is real. A timed opening stores its deadline and arms a
//     timer for it; when it fires the valve closes itself and notifies, so
//     a subscriber learns of the self-close from a report, as from a valve
//     with its own clock (TC-VALCC-4.5 compares its subscription with a
//     read once the opening has ended).
type demoValve struct {
	name string
	notifier
	// version is held by the device model, not by the cluster server, so a
	// server built anew — the device layer builds it once, when the device
	// is (fleet.endpoints) — never starts the counter over.
	//
	// On a bridged endpoint a controller never sees this counter:
	// endpoint/dispatcher.go:38-42 answers from the ENDPOINT's version for
	// anything that is not the root or the aggregator, and that one is bumped
	// by the subscription manager (bridge/subscribe.go:997). So the tracker
	// passed here is inert on the path this daemon actually uses. It is
	// wired anyway because the server's contract asks for one and a host that
	// mounts the same server on a root endpoint would need it — but nothing
	// here depends on its value, and a reader should not go looking for the
	// effect.
	version cluster.DataVersionTracker

	mu    sync.Mutex
	state valve.State
	// openFor is the duration of the current opening; nil means the valve is
	// closed, or open until something closes it.
	openFor *uint32
	// closesAt is the deadline of a timed opening; the zero time means there
	// is none.
	closesAt    time.Time
	defaultOpen *uint32
	// closer fires at closesAt; replaced by every Open, stopped by Close.
	closer *time.Timer
	// target is the position the head is travelling to; nil when it is not
	// moving. arrive fires when it gets there.
	target *valve.State
	arrive *time.Timer
}

// valveTravel is how long the head takes to open or close.
const valveTravel = 200 * time.Millisecond

// travelLocked starts the head towards to. Caller holds v.mu.
func (v *demoValve) travelLocked(to valve.State) {
	if v.target == nil && v.state == to {
		// Already there: the head does not move, and nothing reads
		// Transitioning (TC-VALCC-3.1 closes a closed valve first).
		return
	}
	if v.arrive != nil {
		v.arrive.Stop()
	}
	v.target = &to
	v.state = valve.StateTransitioning
	v.arrive = time.AfterFunc(valveTravel, v.arrived)
}

// arrived completes the head's travel.
func (v *demoValve) arrived() {
	v.mu.Lock()
	if v.target == nil {
		v.mu.Unlock()
		return
	}
	v.state = *v.target
	v.target = nil
	v.arrive = nil
	v.mu.Unlock()
	v.notify()
}

// openingLocked reports whether the valve is open or travelling to open:
// an opening's durations hold from the Open command on (TC-VALCC-4.1 reads
// OpenDuration right after it). Caller holds v.mu.
func (v *demoValve) openingLocked() bool {
	return v.state == valve.StateOpen || (v.target != nil && *v.target == valve.StateOpen)
}

// stopTravelLocked abandons a travel in progress. Caller holds v.mu.
func (v *demoValve) stopTravelLocked() {
	if v.arrive != nil {
		v.arrive.Stop()
		v.arrive = nil
	}
	v.target = nil
}

// Compile-time assertions: the device is the endpoint source, the host port
// of the ValveConfigurationAndControl server, and its own change notifier.
var (
	_ contract.ChangeNotifier = (*demoValve)(nil)
	_ valve.StateSource       = (*demoValve)(nil)
)

// newDemoValve returns a closed valve whose default opening lasts
// defaultOpenSeconds — the duration an Open command carrying no OpenDuration
// field applies.
func newDemoValve(name string, defaultOpenSeconds uint32) *demoValve {
	seconds := defaultOpenSeconds
	return &demoValve{name: name, state: valve.StateClosed, defaultOpen: &seconds}
}

// servers are the cluster servers the device supplies. The device is
// the server's narrow port: every attribute the cluster answers is read back
// out of it, and both commands land on it.
func (v *demoValve) servers() []contract.ClusterServer {
	return []contract.ClusterServer{valve.NewServer(valve.Config{
		Source:      v,
		DataVersion: &v.version,
	})}
}

// expireLocked applies a lapsed opening deadline. Caller holds v.mu.
func (v *demoValve) expireLocked(now time.Time) {
	if v.state != valve.StateOpen || v.closesAt.IsZero() || now.Before(v.closesAt) {
		return
	}
	v.state = valve.StateClosed
	v.openFor = nil
	v.closesAt = time.Time{}
}

// CurrentState implements [valve.StateSource]. Transitioning while the head
// travels.
func (v *demoValve) CurrentState() (valve.State, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.expireLocked(time.Now())
	return v.state, true
}

// TargetState implements [valve.StateSource]: the position the head is
// travelling to, null once it has arrived.
func (v *demoValve) TargetState() (valve.State, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.target == nil {
		return valve.StateClosed, false
	}
	return *v.target, true
}

// OpenDuration implements [valve.StateSource]. Null while the valve is
// closed, and null for an indefinite opening — the spec's own meaning for
// the attribute rather than an unknown value.
func (v *demoValve) OpenDuration() (uint32, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.expireLocked(time.Now())
	if !v.openingLocked() || v.openFor == nil {
		return 0, false
	}
	return *v.openFor, true
}

// RemainingDuration implements [valve.StateSource]. Null unless a timed
// opening is running.
func (v *demoValve) RemainingDuration() (uint32, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := time.Now()
	v.expireLocked(now)
	if !v.openingLocked() || v.closesAt.IsZero() {
		return 0, false
	}
	return secondsUntil(v.closesAt.Sub(now)), true
}

// DefaultOpenDuration implements [valve.StateSource].
func (v *demoValve) DefaultOpenDuration() (uint32, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.defaultOpen == nil {
		return 0, false
	}
	return *v.defaultOpen, true
}

// SetDefaultOpenDuration implements [valve.StateSource]. Nothing is fanned
// out: DefaultOpenDuration is not a reportable attribute, and the cluster
// server bumps the DataVersion for the write itself.
func (v *demoValve) SetDefaultOpenDuration(_ context.Context, seconds *uint32) error {
	v.mu.Lock()
	v.defaultOpen = copyUint32(seconds)
	v.mu.Unlock()
	slog.Info("valve.default_open_duration", slog.String("device", v.name),
		slog.Any("seconds", seconds))
	return nil
}

// Open implements [valve.StateSource]. An absent OpenDuration field means
// "use DefaultOpenDuration"; a present null means "stay open until something
// closes me", which is why the two are kept apart rather than folded.
func (v *demoValve) Open(_ context.Context, req valve.OpenRequest) error {
	v.mu.Lock()
	duration := v.defaultOpen
	if d := req.OpenDuration; d != nil {
		duration = nil
		if !d.Null {
			duration = &d.Value
		}
	}
	v.travelLocked(valve.StateOpen)
	v.openFor = copyUint32(duration)
	if v.closer != nil {
		v.closer.Stop()
		v.closer = nil
	}
	if duration != nil {
		d := time.Duration(*duration) * time.Second
		v.closesAt = time.Now().Add(d)
		v.closer = time.AfterFunc(d, v.timedClose)
	} else {
		v.closesAt = time.Time{}
	}
	v.mu.Unlock()
	slog.Info("valve.open", slog.String("device", v.name), slog.Any("seconds", duration))
	v.notify()
	return nil
}

// timedClose is the valve's own timer ending a timed opening.
func (v *demoValve) timedClose() {
	v.mu.Lock()
	v.expireLocked(time.Now())
	v.closer = nil
	v.mu.Unlock()
	slog.Info("valve.timed_close", slog.String("device", v.name))
	v.notify()
}

// Close implements [valve.StateSource].
func (v *demoValve) Close(context.Context) error {
	v.mu.Lock()
	if v.closer != nil {
		v.closer.Stop()
		v.closer = nil
	}
	v.travelLocked(valve.StateClosed)
	v.openFor = nil
	v.closesAt = time.Time{}
	v.mu.Unlock()
	slog.Info("valve.close", slog.String("device", v.name))
	v.notify()
	return nil
}

// reportFromDevice applies a position the valve reported by itself — the
// manual lever on the head, or its own local timer — as opposed to one a
// Matter command asked for. A real host drives this from its southbound
// event stream; this example has no southbound bus, so the only caller is
// the fleet's own test, where it stands for a change that starts at the
// device rather than at a controller.
func (v *demoValve) reportFromDevice(state valve.State) {
	v.mu.Lock()
	v.stopTravelLocked()
	v.state = state
	if state != valve.StateOpen {
		v.openFor = nil
		v.closesAt = time.Time{}
	}
	v.mu.Unlock()
	slog.Info("valve.reported", slog.String("device", v.name), slog.Int("state", int(state)))
	v.notify()
}

// secondsUntil renders a remaining duration as whole seconds, rounding up so
// a valve that is still open never reports zero seconds left.
func secondsUntil(d time.Duration) uint32 {
	if d <= 0 {
		return 0
	}
	// The attribute is a uint32 count of seconds, so the ceiling is that
	// type's maximum expressed as a duration.
	const ceiling = time.Duration(^uint32(0))
	seconds := (d + time.Second - 1) / time.Second
	if seconds > ceiling {
		return ^uint32(0)
	}
	return uint32(seconds) //nolint:gosec // clamped against the uint32 maximum on the line above
}

// copyUint32 copies a nullable value so a device never aliases a pointer its
// caller still owns.
func copyUint32(v *uint32) *uint32 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

// --- device 4: a mode selector ------------------------------------------

// demoSelector is a hand-built stand-in for a coffee machine's brew-strength
// knob: a characteristic with three labelled positions and nothing in
// between them, which is the case ModeSelect exists for.
//
// The knob's position is the whole of the device state. The mode list is
// fixed for the lifetime of the device, which is what the cluster's quality
// F on Description, StandardNamespace and SupportedModes asks for.
type demoSelector struct {
	name        string
	description string
	notifier
	// version is held by the device for the reason given on [demoValve].
	version cluster.DataVersionTracker

	// modes is immutable after construction, so it needs no lock.
	modes []modeselect.ModeOptionStruct

	mu      sync.Mutex
	current uint8
}

// Compile-time assertions.
var (
	_ contract.ChangeNotifier = (*demoSelector)(nil)
	_ modeselect.ModeSource   = (*demoSelector)(nil)
)

// Brew-strength positions of [demoSelector]. The values are this host's own
// — ModeSelect leaves the numbering to the device — and they are what a
// ChangeToMode command carries.
const (
	brewMild   uint8 = 0
	brewNormal uint8 = 1
	brewStrong uint8 = 2
)

// newDemoSelector returns a selector parked on [brewNormal].
func newDemoSelector(name string) *demoSelector {
	return &demoSelector{
		name:        name,
		description: "Brew strength",
		modes: []modeselect.ModeOptionStruct{
			// SemanticTags is conformance M, so every option carries the
			// field; leaving it empty is how an option says it has no tag a
			// client could act on without reading the label. No standard
			// namespace covers brew strength, so there is nothing honest to
			// put in it here.
			{Label: "Mild", Mode: brewMild},
			{Label: "Normal", Mode: brewNormal},
			{Label: "Strong", Mode: brewStrong},
		},
		current: brewNormal,
	}
}

// servers are the cluster servers the device supplies.
func (s *demoSelector) servers() []contract.ClusterServer {
	return []contract.ClusterServer{modeselect.NewServer(modeselect.Config{
		Source:      s,
		DataVersion: &s.version,
	})}
}

// ModeDescription implements [modeselect.ModeSource].
func (s *demoSelector) ModeDescription() string { return s.description }

// ModeNamespace implements [modeselect.ModeSource]. No standard namespace
// describes brew strength, so StandardNamespace reads null.
func (s *demoSelector) ModeNamespace() (uint8, bool) { return 0, false }

// SupportedModes implements [modeselect.ModeSource]. The server deep-copies
// what it is given, so handing out the fixed slice cannot leak host state.
func (s *demoSelector) SupportedModes() []modeselect.ModeOptionStruct { return s.modes }

// CurrentMode implements [modeselect.ModeSource].
func (s *demoSelector) CurrentMode() uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// ChangeToMode implements [modeselect.ModeSource]. The server has already
// refused any mode outside SupportedModes, so what arrives here turns the
// knob.
func (s *demoSelector) ChangeToMode(_ context.Context, newMode uint8) error {
	s.mu.Lock()
	s.current = newMode
	s.mu.Unlock()
	slog.Info("selector.set", slog.String("device", s.name), slog.Int("mode", int(newMode)))
	s.notify()
	return nil
}

// reportFromDevice applies a position someone turned the knob to by hand.
// Like [demoValve.reportFromDevice] it stands in for the southbound event a
// real host would forward, and the fleet's test is its caller. An unknown
// mode is refused rather than stored: the device cannot be in a position its
// own mode list does not have.
func (s *demoSelector) reportFromDevice(mode uint8) error {
	if !s.supports(mode) {
		return fmt.Errorf("selector %s: mode %d is not one of its positions", s.name, mode)
	}
	s.mu.Lock()
	s.current = mode
	s.mu.Unlock()
	slog.Info("selector.reported", slog.String("device", s.name), slog.Int("mode", int(mode)))
	s.notify()
	return nil
}

// supports reports whether mode is one of the knob's positions.
func (s *demoSelector) supports(mode uint8) bool {
	for _, opt := range s.modes {
		if opt.Mode == mode {
			return true
		}
	}
	return false
}

// --- devices 5 and 6: a powered speaker and a ceiling light ---------------

// dimmer is a hand-built stand-in for anything with a level on a continuous
// scale plus the on/off state its LevelControl is coupled to. The fleet has
// two: a powered speaker (its volume) and a colour-temperature ceiling light
// (its brightness, see fleet_lighting.go), which differ in the device type
// they advertise and in the Lighting feature, not in how the level moves.
// Both clusters are mounted on the one endpoint, which is what makes the
// coupling real rather than declared — the "with On/Off" commands drive an
// OnOff cluster a controller can read back. The OnOff cluster is the
// device layer's default server (cluster/onoff.Server); the dimmer follows
// it through the device's OnChanged hook and sets it when a "with On/Off"
// level command switches the device ([dimmer.publishOnOff]).
//
// What the model does and does not claim:
//
//   - The device itself has no travel time. The speaker takes its commands
//     on the hand-off path, as a device that ramps natively would: a Move
//     arrives at the end of its direction and a Step lands on its target
//     inside the command, TransitionTime and Rate are validated by the
//     cluster and then not honoured, and Stop reports success over a
//     speaker already at rest. The ceiling light lets the module's
//     transition engine ramp it instead (fleet_lighting.go): its
//     LevelControl server runs the commands and sets each step through
//     [dimmer.MoveToLevel].
//   - A plain (non-On/Off) command issued while the speaker is off runs only
//     when the effective Options bitmap sets ExecuteIfOff. A gated-out
//     command changes nothing and reports success, which is what the spec
//     asks of it.
type dimmer struct {
	name string
	// kind prefixes this device's log messages ("speaker.level",
	// "ceiling.level"), so a reader of the daemon log can tell which device
	// was driven.
	kind string
	// minLevel is the lowest level the device reaches: 0 for the speaker,
	// 1 for the light, whose LevelControl carries the Lighting feature
	// (matter.js LevelControlServer.ts:89-91 minLevel, 1 with LT).
	minLevel uint8
	// lighting selects the ceiling-light surface; see servers.
	lighting *lightingSurface
	notifier
	// version is held by the device for the reason given on [demoValve].
	version cluster.DataVersionTracker

	mu      sync.Mutex
	on      bool
	level   uint8
	options uint8
	// onLevel is the level the speaker returns to when its OnOff cluster
	// turns it on; nil is the spec's "OnLevel has no effect".
	onLevel *uint8
	// onOff is the device's OnOff server; nil before [dimmer.device].
	onOff *onoffdef.Server
}

// Compile-time assertions.
var (
	_ contract.ChangeNotifier  = (*dimmer)(nil)
	_ levelcontrol.LevelSource = (*dimmer)(nil)
)

// newDemoSpeaker returns a speaker that is off, at the given volume.
func newDemoSpeaker(name string, level uint8) *dimmer {
	return &dimmer{name: name, kind: "speaker", minLevel: levelcontrol.LevelMin, level: level}
}

// servers are the cluster servers the device supplies besides OnOff,
// which is the device layer's default: the speaker's LevelControl; the
// ceiling light adds the Lighting surface (fleet_lighting.go).
func (s *dimmer) servers() []contract.ClusterServer {
	if s.lighting != nil {
		return s.lightingServers()
	}
	return []contract.ClusterServer{
		levelcontrol.NewServer(levelcontrol.Config{
			Source:      s,
			DataVersion: &s.version,
		}),
	}
}

// device builds the dimmer's device: a Speaker, a ColorTemperatureLight or
// an ExtendedColorLight (fleet_lighting.go). The light's OnOff starts with
// GlobalSceneControl true, as the daemon has always served it.
//
// Speaker (speaker.element.ts:12) requires OnOff and LevelControl, both
// conformance M and neither carrying a feature requirement (:18-19): it is
// the device type the speaker's LevelControl server, without the Lighting
// feature, fits. DimmableLight would be the more obvious host and is the
// wrong one: it requires the LT (Lighting) feature on both OnOff and
// LevelControl (dimmable-light.element.ts:19-33), a surface the speaker's
// server does not serve.
func (s *dimmer) device(opts device.Options) (*device.Device, error) {
	with := serve(s.servers(), device.WithChangeNotifier(s))
	if s.lighting != nil {
		with = append(with, onoffdef.Declare{Initial: onoffdef.Patch{GlobalSceneControl: new(true)}})
	}
	var d *device.Device
	switch {
	case s.lighting != nil && s.lighting.extended:
		l, err := device.NewExtendedColorLight(opts, with...)
		if err != nil {
			return nil, err
		}
		d = l.Device
	case s.lighting != nil:
		l, err := device.NewColorTemperatureLight(opts, with...)
		if err != nil {
			return nil, err
		}
		d = l.Device
	default:
		sp, err := device.NewSpeaker(opts, with...)
		if err != nil {
			return nil, err
		}
		d = sp.Device
	}
	srv := onoffdef.Wrap(d.Server(onoff.ClusterID))
	if srv == nil {
		return nil, fmt.Errorf("%s: no default OnOff server", s.name)
	}
	s.mu.Lock()
	s.onOff = srv
	s.mu.Unlock()
	followOnOff(d, srv, func(on bool) {
		if s.isOn() == on {
			return // the dimmer switched itself (publishOnOff)
		}
		s.setOn(on)
		slog.Info(s.kind+".set", slog.String("device", s.name), slog.Bool("on", on))
	})
	return d, nil
}

// publishOnOff stores an on/off change the dimmer made itself — a "with
// On/Off" level command — in its OnOff server, which reports it.
func (s *dimmer) publishOnOff(on bool) {
	s.mu.Lock()
	srv := s.onOff
	s.mu.Unlock()
	if srv != nil && srv.Get().OnOff != on {
		_ = srv.Set(onoffdef.Patch{OnOff: new(on)})
	}
}

// isOn reads the dimmer's on/off state.
func (s *dimmer) isOn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.on
}

// setOn switches the dimmer. Turning the speaker on restores OnLevel
// when one is configured — the coupling the LevelControl attribute
// describes, applied where the two clusters actually meet.
func (s *dimmer) setOn(on bool) {
	s.mu.Lock()
	s.on = on
	if on && s.onLevel != nil {
		s.level = *s.onLevel
	}
	s.mu.Unlock()
	s.notify()
}

// CurrentLevel implements [levelcontrol.LevelSource]. The level is always
// known: this device answers for itself rather than caching a reading taken
// somewhere else.
func (s *dimmer) CurrentLevel() (uint8, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.level, true
}

// Options implements [levelcontrol.LevelSource].
func (s *dimmer) Options() levelcontroldef.OptionsBitmap {
	s.mu.Lock()
	defer s.mu.Unlock()
	return levelcontroldef.OptionsBitmap(s.options)
}

// OnLevel implements [levelcontrol.LevelSource].
func (s *dimmer) OnLevel() (uint8, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onLevel == nil {
		return 0, false
	}
	return *s.onLevel, true
}

// SetOptions implements [levelcontrol.LevelSource]. The server has already
// refused a bitmap carrying a bit the advertised FeatureMap does not cover.
func (s *dimmer) SetOptions(_ context.Context, options levelcontroldef.OptionsBitmap) error {
	s.mu.Lock()
	s.options = uint8(options)
	s.mu.Unlock()
	slog.Info(s.kind+".options", slog.String("device", s.name), slog.Int("options", int(options)))
	return nil
}

// SetOnLevel implements [levelcontrol.LevelSource]. A nil level is the
// spec's null: the on-level has no effect.
func (s *dimmer) SetOnLevel(_ context.Context, level *uint8) error {
	s.mu.Lock()
	if level == nil {
		s.onLevel = nil
	} else {
		v := *level
		s.onLevel = &v
	}
	s.mu.Unlock()
	slog.Info(s.kind+".on_level", slog.String("device", s.name), slog.Any("level", level))
	return nil
}

// MoveToLevel implements [levelcontrol.LevelSource].
func (s *dimmer) MoveToLevel(_ context.Context, req levelcontroldef.MoveToLevelRequest) error {
	if !s.executes(uint8(req.OptionsMask), uint8(req.OptionsOverride)) {
		return nil
	}
	s.applyLevel(req.Level, false)
	return nil
}

// MoveToLevelWithOnOff implements [levelcontrol.LevelSource].
func (s *dimmer) MoveToLevelWithOnOff(_ context.Context, req levelcontroldef.MoveToLevelWithOnOffRequest) error {
	s.applyLevel(req.Level, true)
	return nil
}

// Move implements [levelcontrol.LevelSource]. With no travel time the move
// arrives at the end of its direction inside the command.
func (s *dimmer) Move(_ context.Context, req levelcontroldef.MoveRequest) error {
	if !s.executes(uint8(req.OptionsMask), uint8(req.OptionsOverride)) {
		return nil
	}
	s.applyLevel(s.moveTarget(uint8(req.MoveMode)), false)
	return nil
}

// MoveWithOnOff implements [levelcontrol.LevelSource].
func (s *dimmer) MoveWithOnOff(_ context.Context, req levelcontroldef.MoveWithOnOffRequest) error {
	s.applyLevel(s.moveTarget(uint8(req.MoveMode)), true)
	return nil
}

// Step implements [levelcontrol.LevelSource].
func (s *dimmer) Step(_ context.Context, req levelcontroldef.StepRequest) error {
	if !s.executes(uint8(req.OptionsMask), uint8(req.OptionsOverride)) {
		return nil
	}
	s.applyLevel(s.steppedLevel(req), false)
	return nil
}

// StepWithOnOff implements [levelcontrol.LevelSource].
func (s *dimmer) StepWithOnOff(_ context.Context, req levelcontroldef.StepWithOnOffRequest) error {
	s.applyLevel(s.steppedLevel(levelcontroldef.StepRequest(req)), true)
	return nil
}

// Stop implements [levelcontrol.LevelSource]. See the type doc: this model
// never has a move in flight to halt.
func (s *dimmer) Stop(context.Context, levelcontroldef.StopRequest) error { return nil }

// StopWithOnOff implements [levelcontrol.LevelSource].
func (s *dimmer) StopWithOnOff(context.Context, levelcontroldef.StopWithOnOffRequest) error {
	return nil
}

// reportFromDevice applies a volume the speaker moved to by itself — its own
// front-panel dial. Like the same method on the other two devices it stands
// in for a southbound event, and the fleet's test is its caller.
func (s *dimmer) reportFromDevice(level uint8) {
	s.mu.Lock()
	s.level = level
	s.mu.Unlock()
	slog.Info(s.kind+".reported", slog.String("device", s.name), slog.Int("level", int(level)))
	s.notify()
}

// executes applies the ExecuteIfOff gate the plain commands carry. It lives
// here rather than in the cluster server because the gate reads the OnOff
// attribute of another cluster on the endpoint, which a single-cluster
// server cannot see (matter.js LevelControlServer.ts:729-736
// #optionsAllowExecution). The bitmap arithmetic is the cluster's, so it
// comes from [levelcontrol.EffectiveOptions] rather than being restated.
func (s *dimmer) executes(mask, override uint8) bool {
	if s.isOn() {
		return true
	}
	return levelcontrol.EffectiveOptions(uint8(s.Options()), mask, override)&levelcontrol.OptionExecuteIfOff != 0
}

// applyLevel moves the speaker to level. withOnOff drives the on/off state
// along with it: the minimum level turns the speaker off, anything above it
// turns it on.
func (s *dimmer) applyLevel(level uint8, withOnOff bool) {
	level = max(level, s.minLevel)
	s.mu.Lock()
	s.level = level
	if withOnOff {
		s.on = level > s.minLevel
	}
	on := s.on
	s.mu.Unlock()
	attrs := []any{slog.String("device", s.name), slog.Int("level", int(level)), slog.Bool("on", on)}
	if s.lighting != nil {
		// The transition engine sets every step of a ramp through here.
		slog.Debug(s.kind+".level", attrs...)
	} else {
		slog.Info(s.kind+".level", attrs...)
	}
	s.notify()
	if withOnOff {
		s.publishOnOff(on)
	}
}

// steppedLevel resolves a Step command against the current level, clamped to
// the cluster's own bounds.
func (s *dimmer) steppedLevel(req levelcontroldef.StepRequest) uint8 {
	s.mu.Lock()
	current := s.level
	s.mu.Unlock()
	if uint8(req.StepMode) == levelcontrol.StepModeUp {
		if uint16(current)+uint16(req.StepSize) > uint16(levelcontrol.LevelMax) {
			return levelcontrol.LevelMax
		}
		return current + req.StepSize
	}
	if req.StepSize > current-s.minLevel {
		return s.minLevel
	}
	return current - req.StepSize
}

// moveTarget is where a Move ends on a device with no travel time.
func (s *dimmer) moveTarget(moveMode uint8) uint8 {
	if moveMode == levelcontrol.MoveModeUp {
		return levelcontrol.LevelMax
	}
	return s.minLevel
}

// --- the fleet ----------------------------------------------------------

// fleet is the hard-coded device list this daemon bridges: the host's own
// device models, which [fleet.endpoints] turns into the node's bridged
// endpoints.
type fleet struct {
	light       *demoLight
	thermometer *demoThermometer
	valve       *demoValve
	selector    *demoSelector
	speaker     *dimmer

	// One example device per surface the chip-tool suite drives
	// (internal/chiptool). Each lives in the fleet_*.go file named after
	// its kind.
	ceiling    *dimmer
	fan        *demoFan
	smoke      *demoSmokeAlarm
	pump       *demoPump
	flow       *demoReading
	washer     *demoWasher
	vacuum     *demoVacuum
	thermostat *demoThermostat
	blind      *demoBlind
	lock       *demoLock
	humidity   *demoReading
	occupancy  *demoBinary
	contact    *demoContactSensor
	button     *demoButton
	purifier   *demoAirPurifier
	garage     *demoGarage
	colorLight *dimmer
	fridge     *demoFridge
	airQuality *demoAirQualitySensor
	heater     *demoWaterHeater
	meter      *demoMeter
	dishwasher *demoDishwasher
	oven       *demoOven
	microwave  *demoMicrowave
	dryer      *demoDryer
	evse       *demoEvse
	vacuumArea *vacuumArea
	panel      *demoPanel
	soil       *demoSoilSensor
	// actions is the Aggregator's Actions host (fleet_node.go).
	actions *demoActions
	// endpointOf resolves a StableKey to its live endpoint id
	// (attachNodeProviders); nil until the node runs.
	endpointOf func(key endpoint.StringKey) (uint16, bool)
	// The energy-market devices (fleet_energy_market.go).
	energyTariff    *demoEnergyTariff
	electricalMeter *demoElectricalMeter
	// esa is the state-forecast ESA (fleet_esa.go).
	esa *demoEsa
	// wifi is the root's simulated Wi-Fi link (wifi.go), served with --wifi.
	wifi *demoWiFi
	// otaImages is the OTA provider's test image and consent
	// (ota_provider.go), served with --ota-provider; nil without.
	otaImages *testImages
	// media is the video player and content app pair (media.go), served
	// with --media; nil without.
	media *demoMedia

	// configChange raises the ConfigurationVersion of every bridged device
	// (the app pipe's SimulateConfigurationVersionChange); wired by main.
	configChange func() error
}

func newFleet() *fleet {
	vacuum := newDemoVacuum("Robot Vacuum")
	light := newDemoLight("Desk Lamp")
	return &fleet{
		wifi:        newDemoWiFi(),
		light:       light,
		thermometer: newDemoThermometer(21.5),
		valve:       newDemoValve("Garden Tap", 600),
		selector:    newDemoSelector("Coffee Machine"),
		speaker:     newDemoSpeaker("Kitchen Speaker", 120),
		ceiling:     newDemoCeilingLight("Ceiling Light"),
		fan:         newDemoFan("Ceiling Fan"),
		smoke:       newDemoSmokeAlarm("Hallway Smoke Alarm"),
		pump:        newDemoPump("Heating Pump"),
		flow:        newDemoReading("Water Meter", contract.MeasurementFlow, 0.8),
		washer:      newDemoWasher("Washing Machine"),
		vacuum:      vacuum,
		thermostat:  newDemoThermostat("Living Room Thermostat"),
		blind:       newDemoBlind("Bedroom Blind"),
		lock:        newDemoLock("Front Door"),
		humidity:    newDemoReading("Bathroom Humidity", contract.MeasurementHumidity, 48),
		occupancy:   newDemoBinary("Hall Motion", contract.MeasurementOccupancy, false),
		contact:     newDemoContactSensor("Window Contact"),
		button:      newDemoButton("Wall Button"),
		purifier:    newDemoAirPurifier("Air Purifier"),
		garage:      newDemoGarage("Garage Door"),
		colorLight:  newDemoColorLight("Color Bulb"),
		fridge:      newDemoFridge("Kitchen Fridge"),
		airQuality:  newDemoAirQualitySensor("Air Quality Sensor"),
		heater:      newDemoWaterHeater("Water Heater"),
		meter:       newDemoMeter("Electricity Meter"),
		dishwasher:  newDemoDishwasher("Dishwasher"),
		oven:        newDemoOven("Oven"),
		microwave:   newDemoMicrowave("Microwave"),
		dryer:       newDemoDryer("Tumble Dryer"),
		evse:        newDemoEvse("EV Charger"),
		vacuumArea:  newVacuumArea(vacuum),
		panel:       newDemoPanel("Patio Door Panel"),
		soil:        newDemoSoilSensor("Garden Soil"),
		actions:     newDemoActions(light),

		energyTariff:    newDemoEnergyTariff("Energy Tariff"),
		electricalMeter: newDemoElectricalMeter("Grid Meter"),
		esa:             newDemoEsa("Charger ESA"),
	}
}

// bridged are the Options of a top-level bridged device: its StableKey —
// the load-bearing field, which decides the endpoint number the device gets
// back after a restart and so must render byte-for-byte identically across
// releases — and the addresses this host derives from the same key.
func bridged(key, name string) device.Options {
	return device.Options{
		StableKey:      endpoint.StringKey("demo:" + key + ":1"),
		DeviceAddress:  "demo-" + key + "-1",
		ChannelAddress: "demo-" + key + "-1:0",
		Name:           name,
	}
}

// part are the Options of a part: its own StableKey and the conditions it
// states.
func part(key string, conditions ...string) device.Options {
	return device.Options{StableKey: endpoint.StringKey(key), Conditions: conditions}
}

// serve hands a model's servers to the device layer, each in place of the
// default the device type would mount, then the further options.
func serve(servers []contract.ClusterServer, with ...device.Option) []device.Option {
	out := make([]device.Option, 0, len(servers)+len(with))
	for _, srv := range servers {
		out = append(out, hosted(srv))
	}
	return append(out, with...)
}

// measured is the device option of a sensor whose cluster the assembler
// materialises from the host's live reading (device.WithMeasurement →
// endpoint.Spec.Measurement, cluster/measurement FromMeasurementClass).
func measured(m contract.MeasurementSource) device.Option { return device.WithMeasurement(m) }

// smokeDevice is the smoke alarm with its battery: SmokeCoAlarm requires a
// PowerSource device-type component, a part of its own whose PowerSource
// cluster the assembler materialises from the battery reading, as matter.js
// composes it (examples/device-smoke-co-alarm, parts: [PowerSourceEndpoint]).
func (f *fleet) smokeDevice() (node.Endpoint, error) {
	smoke, err := device.NewSmokeCoAlarm(bridged("smoke", f.smoke.name), serve(f.smoke.servers(), device.WithChangeNotifier(f.smoke))...)
	if err != nil {
		return nil, err
	}
	battery, err := device.NewPowerSource(part("demo:smoke:battery"), measured(f.smoke.battery))
	if err != nil {
		return nil, err
	}
	if err := smoke.AddPart(battery.Device); err != nil {
		return nil, err
	}
	return smoke, nil
}

// endpoints builds the fleet's bridged endpoints, in the order their
// endpoint numbers were first given out: the original five, then one
// device per surface the chip-tool suite drives, appended so the first
// five keep their numbers on a database written by an earlier release of
// this example. A part follows its device. Call it once: each device
// holds the servers it is built with.
func (f *fleet) endpoints() ([]node.Endpoint, error) { //nolint:funlen // the fleet's literal list, one device per line
	var (
		out  []node.Endpoint
		errs []error
	)
	add := func(ep node.Endpoint, err error) {
		if err != nil {
			errs = append(errs, err)
			return
		}
		out = append(out, ep)
	}
	notifying := func(src contract.ChangeNotifier, servers []contract.ClusterServer) []device.Option {
		return serve(servers, device.WithChangeNotifier(src))
	}

	add(f.light.device(bridged("light", f.light.name)))
	add(device.NewTemperatureSensor(bridged("thermometer", "Study Thermometer"), measured(f.thermometer)))
	add(device.NewWaterValve(bridged("valve", f.valve.name), notifying(f.valve, f.valve.servers())...))
	add(device.NewModeSelect(bridged("selector", f.selector.name), notifying(f.selector, f.selector.servers())...))
	add(f.speaker.device(bridged("speaker", f.speaker.name)))

	add(f.ceiling.device(bridged("ceiling", f.ceiling.name)))
	add(device.NewFan(bridged("fan", f.fan.name), notifying(f.fan, f.fan.servers())...))
	add(f.smokeDevice())
	add(f.pump.device(bridged("pump", f.pump.name)))
	add(device.NewFlowSensor(bridged("flow", f.flow.name), measured(f.flow)))
	add(device.NewLaundryWasher(bridged("washer", f.washer.name), serve(f.washer.servers())...))
	// The vacuum's endpoint carries its ServiceArea too
	// (fleet_servicearea.go).
	add(device.NewRoboticVacuumCleaner(bridged("vacuum", f.vacuum.name),
		serve(append(f.vacuum.servers(), f.vacuumArea.server()))...))
	add(device.NewThermostat(bridged("thermostat", f.thermostat.name), serve(f.thermostat.servers())...))
	add(device.NewWindowCovering(bridged("blind", f.blind.name), serve(f.blind.servers())...))
	add(device.NewDoorLock(bridged("lock", f.lock.name), notifying(f.lock, f.lock.servers())...))
	add(device.NewHumiditySensor(bridged("humidity", f.humidity.name), measured(f.humidity)))
	add(device.NewOccupancySensor(bridged("occupancy", f.occupancy.name), measured(f.occupancy)))
	add(device.NewContactSensor(bridged("contact", f.contact.name), notifying(f.contact, f.contact.servers())...))
	add(device.NewGenericSwitch(bridged("button", f.button.name), measured(f.button)))
	add(device.NewAirPurifier(bridged("purifier", f.purifier.name), notifying(f.purifier, f.purifier.servers())...))
	add(device.NewClosure(bridged("garage", f.garage.name), notifying(f.garage, f.garage.servers())...))
	add(f.colorLight.device(bridged("colorlight", f.colorLight.name)))
	// Refrigerator requires a TemperatureControlledCabinet part: the
	// compartment is a part of the fridge's endpoint.
	add(f.fridge.device(bridged("fridge", f.fridge.name)))
	add(device.NewAirQualitySensor(bridged("airquality", f.airQuality.name), serve(f.airQuality.servers())...))
	add(device.NewWaterHeater(bridged("waterheater", f.heater.name), serve(f.heater.servers())...))
	add(device.NewElectricalUtilityMeter(bridged("meter", f.meter.name), serve(f.meter.servers())...))
	add(device.NewDishwasher(bridged("dishwasher", f.dishwasher.name), serve(f.dishwasher.servers())...))
	// Oven requires a TemperatureControlledCabinet part with the Heater
	// condition: the cavity is a part of the oven's endpoint.
	add(f.oven.device(bridged("oven", f.oven.name)))
	add(device.NewMicrowaveOven(bridged("microwave", f.microwave.name), serve(f.microwave.servers())...))
	add(device.NewLaundryDryer(bridged("dryer", f.dryer.name), serve(f.dryer.servers())...))
	// EnergyEvse requires PowerSource, DeviceEnergyManagement and
	// ElectricalSensor device types: the charger's parts (fleet_evse.go).
	add(f.evse.device(bridged("evse", f.evse.name)))
	add(device.NewClosurePanel(bridged("panel", f.panel.name), serve(f.panel.servers())...))
	add(device.NewSoilSensor(bridged("soil", f.soil.name), measured(f.soil)))
	add(device.NewElectricalEnergyTariff(bridged("tariff", f.energyTariff.name), serve(f.energyTariff.servers())...))
	add(f.electricalMeter.device(bridged("electricalmeter", f.electricalMeter.name)))
	add(f.esa.device(bridged("esa", f.esa.name)))
	// The media pair (--media, TESTING ONLY) comes last, so every device
	// above keeps its number with or without it.
	if f.media != nil {
		pair, err := f.media.devices()
		if err != nil {
			errs = append(errs, err)
		}
		out = append(out, pair...)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("fleet: %w", err)
	}
	return out, nil
}
