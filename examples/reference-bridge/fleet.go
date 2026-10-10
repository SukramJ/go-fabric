// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/modeselect"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// scope names this host's single partition. The assembler garbage-collects
// persisted endpoint identities one scope at a time; a host with one
// partition may use any non-empty constant.
const scope = "demo"

// Matter device types the fleet's command-driven devices advertise. Only
// the ids are written here — the revision each endpoint publishes is read
// from the generated matter.js snapshot by the assembler, so a schema
// regeneration moves it without an edit on this side.
const (
	// deviceTypeWaterValve is WaterValve (matter.js
	// packages/model/src/standard/elements/water-valve.element.ts:13). Its
	// mandatory server clusters are Identify — which the assembler mounts on
	// every bridged endpoint — and ValveConfigurationAndControl (:18-19),
	// which is what [demoValve] serves.
	deviceTypeWaterValve uint16 = 0x0042
	// deviceTypeModeSelect is ModeSelect (mode-select-device.element.ts:12),
	// whose one required server cluster is the ModeSelect cluster itself
	// (:18).
	deviceTypeModeSelect uint16 = 0x0027
	// deviceTypeSpeaker is Speaker (speaker.element.ts:12): OnOff and
	// LevelControl, both conformance M and neither carrying a feature
	// requirement (:18-19).
	//
	// It is the device type this module's LevelControl server fits.
	// DimmableLight would be the more obvious host and is the wrong one: it
	// requires the LT (Lighting) feature on both OnOff and LevelControl
	// (dimmable-light.element.ts:19-33), and cluster/levelcontrol
	// deliberately implements neither LT-gated attribute — advertising that
	// device type would promise a surface the endpoint does not serve.
	deviceTypeSpeaker uint16 = 0x0022
)

// --- the change-notification fan-out the devices share -------------------

// notifier is the host half of [contract.ChangeNotifier]: a device fires it
// once its own state has moved, and the bridge — which subscribed to every
// endpoint source at mount time — marks that endpoint's reportable
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

// demoLight is a hand-built stand-in for a real device: an on/off light
// whose state lives in this process. It implements
// [contract.EndpointSource], which is all the assembler needs to give it a
// bridged endpoint, and it hands out its own OnOff cluster server.
type demoLight struct {
	name string

	mu sync.RWMutex
	on bool

	// servers is built once: the bridge asks for the cluster servers on
	// every dispatch, and the OnOff server holds the LT state (OnTime,
	// OffWaitTime, the countdowns) and the change listeners the bridge
	// subscribed at reassembly. Rebuilt per call, a written OnTime was
	// gone by the next read (TC-OO-2.1).
	serversOnce sync.Once
	servers     []contract.ClusterServer
}

func newDemoLight(name string) *demoLight { return &demoLight{name: name} }

// MatterDeviceType implements [contract.EndpointSource].
func (d *demoLight) MatterDeviceType() uint16 { return onoff.DeviceTypeOnOffLight }

// MatterClusterServers implements [contract.EndpointSource]. The assembler
// adds Descriptor and BridgedDeviceBasicInformation itself; only the
// device-specific surface comes from here.
//
// OnOffLight mandates three clusters besides Identify (on-off-light.element.ts:
// Groups :22, OnOff with LIGHTING :24-25, ScenesManagement :36). Groups is not
// the device's to supply: group membership is stack state, and the assembler
// mounts the real Groups server on every endpoint whose device type mandates
// it (endpoint.Config.Groups). ScenesManagement stays the module's stub; the
// light has no scene table, and the stub advertises exactly that.
func (d *demoLight) MatterClusterServers() []contract.ClusterServer {
	d.serversOnce.Do(func() {
		d.servers = []contract.ClusterServer{
			&onOffServer{dev: d, logMessage: "light.set", lt: newLightingState()},
			wire.ScenesManagement{},
		}
		// FixedLabel on the first light as well: the harness points the
		// certification cases written against the CHIP all-clusters app's
		// endpoint 1 (TC_FLABEL_2_1.py `--endpoint 1`) at the daemon's first
		// bridged light, and that app serves FixedLabel there. Base allows
		// the cluster on any endpoint.
		if fixedLabel, err := mattercore.NewFixedLabel([]fixedlabeldef.LabelStruct{
			{Label: "room", Value: "demo"},
		}); err == nil {
			d.servers = append(d.servers, fixedLabel)
		}
	})
	return d.servers
}

// deviceName implements [onOffDevice].
func (d *demoLight) deviceName() string { return d.name }

// setOn implements [onOffDevice].
func (d *demoLight) setOn(on bool) {
	d.mu.Lock()
	d.on = on
	d.mu.Unlock()
}

// isOn implements [onOffDevice].
func (d *demoLight) isOn() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.on
}

// onOffDevice is the narrow port [onOffServer] drives: a device with a
// boolean on/off state that can name itself in a log line. Two devices in
// this fleet have one — the light, and the speaker whose LevelControl is
// coupled to it — so the server takes the port rather than either of them.
type onOffDevice interface {
	deviceName() string
	isOn() bool
	setOn(on bool)
}

// onOffServer projects an [onOffDevice] onto the OnOff cluster (0x0006).
//
// Ids, the feature bit and the revision all come from the module's onoff
// package rather than being written out here — the revision in particular is
// read from the generated matter.js snapshot, so a schema regeneration moves
// it without an edit on this side.
//
// With a non-nil [lightingState] the server advertises the LT (Lighting)
// feature and serves what LT makes mandatory: GlobalSceneControl, OnTime,
// OffWaitTime, StartUpOnOff and the OffWithEffect / OnWithRecallGlobalScene
// / OnWithTimedOff commands (on-off.element.ts:30-36, :41-51). The light
// needs it — OnOffLight marks LIGHTING "M" (on-off-light.element.ts:24-25);
// the speaker does not (speaker.element.ts:18 requires OnOff, no feature)
// and runs without.
type onOffServer struct {
	dev onOffDevice
	// logMessage is the slog message [onOffServer.apply] emits. Each device
	// kind names its own, so a reader of the daemon log can tell which
	// device was driven when more than one serves this cluster.
	logMessage string
	version    contract.DataVersionTracker
	// lt is nil for a server without the Lighting feature.
	lt *lightingState
	// changes reports what the two LT countdowns change on their own. A
	// command's or a write's changes reach subscribers through the bridge
	// (reportInvokeChanges, reportWrittenAttributes); a timer tick has no
	// request behind it, so without this a subscriber never learned that
	// OnTime ran down or that the timed-on phase switched the light off
	// (found by TC-OO-2.8). matter.js OnOffServer.ts assigns the ticked
	// values to its state, whose Datasource reports every change.
	changes cluster.AttributeChanges
}

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *onOffServer) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) { //nolint:unparam // the contract's signature; the bridge keeps the closure
	return s.changes.OnMatterAttributesChanged(cb)
}

// MatterClusterID implements [contract.ClusterServer].
func (s *onOffServer) MatterClusterID() uint32 { return onoff.ClusterID }

// MatterRead implements [contract.ClusterServer]. FeatureMap and
// ClusterRevision are answered here on purpose: the IM dispatcher
// synthesises the three list globals for a server that does not implement
// them, but never those two — a cluster that stays silent on 0xFFFC/0xFFFD
// answers a controller's very first read with UnsupportedAttribute.
func (s *onOffServer) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case onoff.AttrOnOff:
		return s.dev.isOn(), true
	case cluster.AttrGlobalFeatureMap:
		if s.lt != nil {
			return onoff.FeatureLighting, true
		}
		// No LT: plain On/Off/Toggle and none of the LT-gated timing
		// attributes.
		return uint32(0), true
	case cluster.AttrGlobalClusterRevision:
		return uint32(onoff.Revision()), true
	}
	if s.lt == nil {
		return nil, false
	}
	s.lt.mu.Lock()
	defer s.lt.mu.Unlock()
	switch attrID {
	case onoff.AttrGlobalSceneControl:
		return s.lt.globalSceneControl, true
	case onoff.AttrOnTime:
		return s.lt.onTime, true
	case onoff.AttrOffWaitTime:
		return s.lt.offWaitTime, true
	case onoff.AttrStartUpOnOff:
		// Nullable; null is "keep the last state". (nil, true) encodes the
		// TLV null. The value is stored and reported but never applied:
		// matter.js OnOffServer.ts:33-36 skips the start-up transition on
		// an endpoint owned by an Aggregator, which every endpoint here is.
		if s.lt.startUpOnOff == nil {
			return nil, true
		}
		return *s.lt.startUpOnOff, true
	default:
		return nil, false
	}
}

// MatterWrite implements [contract.ClusterServer]. OnOff itself is
// command-driven and read-only per Matter §1.5.6; the three LT attributes
// are writable (on-off.element.ts:31-36).
func (s *onOffServer) MatterWrite(_ context.Context, attrID uint32, value any) error {
	if s.lt == nil {
		return errors.New("onoff: attribute is read-only")
	}
	switch attrID {
	case onoff.AttrOnTime, onoff.AttrOffWaitTime:
		v, ok := asUint16(value)
		if !ok {
			return fmt.Errorf("onoff: attribute %#06x expects uint16, got %T", attrID, value)
		}
		s.lt.mu.Lock()
		if attrID == onoff.AttrOnTime {
			s.lt.onTime = v
		} else {
			s.lt.offWaitTime = v
		}
		// A write only ends an active countdown parked at 0 or the 0xFFFF
		// hold; it never starts one — matter.js OnOffServer.ts
		// #stopHeldTimer.
		if s.dev.isOn() {
			if s.lt.timedOn != nil && (s.lt.onTime == 0 || s.lt.onTime == 0xFFFF) {
				s.lt.stopTimedOn()
			}
		} else if s.lt.delayedOff != nil && (s.lt.offWaitTime == 0 || s.lt.offWaitTime == 0xFFFF) {
			s.lt.stopDelayedOff()
		}
		s.lt.mu.Unlock()
		s.version.Bump()
		return nil
	case onoff.AttrStartUpOnOff:
		var next *uint8
		if value != nil {
			v, ok := cluster.AsUint8(value)
			if !ok || v > onoffStartUpOnOffToggle {
				return fmt.Errorf("onoff: StartUpOnOff expects 0..2 or null, got %v", value)
			}
			next = &v
		}
		s.lt.mu.Lock()
		s.lt.startUpOnOff = next
		s.lt.mu.Unlock()
		s.version.Bump()
		return nil
	default:
		return errors.New("onoff: attribute is read-only")
	}
}

// onoffStartUpOnOffToggle is the largest StartUpOnOffEnum value
// (on-off.element.ts: Off=0, On=1, Toggle=2).
const onoffStartUpOnOffToggle uint8 = 2

// MatterInvoke implements [contract.ClusterServer]. Every command is
// status-only, so the response is nil.
func (s *onOffServer) MatterInvoke(_ context.Context, cmdID uint32, fields any) (any, error) {
	if s.lt == nil {
		switch cmdID {
		case onoff.CmdOn:
			s.apply(true)
		case onoff.CmdOff:
			s.apply(false)
		case onoff.CmdToggle:
			s.apply(!s.dev.isOn())
		default:
			return nil, fmt.Errorf("onoff: unsupported command %#x", cmdID)
		}
		return nil, nil
	}
	s.lt.mu.Lock()
	err := s.invokeLighting(cmdID, fields)
	switched := s.lt.takeSwitch()
	s.lt.mu.Unlock()
	if switched != nil {
		s.apply(*switched)
	}
	return nil, err
}

// invokeLighting runs one command against the LT state. Caller holds
// s.lt.mu; the on/off switch it decides is applied once the lock is
// released ([lightingState.takeSwitch]).
func (s *onOffServer) invokeLighting(cmdID uint32, fields any) error {
	switch cmdID {
	case onoff.CmdOn:
		s.on()
	case onoff.CmdOff:
		s.off()
	case onoff.CmdToggle:
		if s.dev.isOn() {
			s.off()
		} else {
			s.on()
		}
	case onoff.CmdOffWithEffect:
		// The effect is ignored, as matter.js OnOffServer.ts offWithEffect
		// does; there is no scene table here to store the global scene in.
		s.lt.globalSceneControl = false
		s.off()
	case onoff.CmdOnWithRecallGlobalScene:
		if s.lt.globalSceneControl {
			return nil
		}
		s.lt.globalSceneControl = true
		if s.lt.onTime == 0 {
			s.lt.offWaitTime = 0
		}
		s.on()
	case onoff.CmdOnWithTimedOff:
		control, onTime, offWaitTime, err := onWithTimedOffFields(fields)
		if err != nil {
			return err
		}
		s.onWithTimedOff(control, onTime, offWaitTime)
	default:
		return fmt.Errorf("onoff: unsupported command %#x", cmdID)
	}
	return nil
}

// on is the LT-aware On. Caller holds s.lt.mu. Mirrors matter.js
// OnOffServer.ts on(): GlobalSceneControl is set, and OffWaitTime is kept
// through a timed-on phase but cleared when no OnTime runs.
//
// The device is switched after s.lt.mu is released, not here: switching it
// fires the device's change notification, whose subscriber reads this
// server back — under s.lt.mu that read would deadlock.
func (s *onOffServer) on() {
	s.lt.switchTo(true)
	s.lt.globalSceneControl = true
	if s.lt.onTime == 0 {
		s.lt.stopDelayedOff()
		s.lt.offWaitTime = 0
	}
}

// off is the LT-aware Off. Caller holds s.lt.mu. Mirrors matter.js
// OnOffServer.ts off(): the timed-on countdown ends, and an OffWaitTime
// above zero (and below the 0xFFFF hold) enters the delayed-off guard
// period of spec §1.5.7.6.4.
func (s *onOffServer) off() {
	s.lt.switchTo(false)
	s.lt.stopTimedOn()
	s.lt.onTime = 0
	if s.lt.offWaitTime > 0 && s.lt.offWaitTime != 0xFFFF && s.lt.delayedOff == nil {
		s.lt.startDelayedOff(s.delayedOffTick)
	}
}

// onWithTimedOff is matter.js OnOffServer.ts onWithTimedOff. Caller holds
// s.lt.mu.
func (s *onOffServer) onWithTimedOff(control uint8, onTime, offWaitTime uint16) {
	const acceptOnlyWhenOn = 0x01
	on := s.dev.isOn()
	if control&acceptOnlyWhenOn != 0 && !on {
		return
	}
	if s.lt.offWaitTime > 0 && !on {
		// Delayed-off guard: the device stays off; the request may only
		// shorten the remaining wait.
		s.lt.offWaitTime = min(offWaitTime, s.lt.offWaitTime)
		if s.lt.delayedOff == nil && s.lt.offWaitTime > 0 && s.lt.offWaitTime != 0xFFFF {
			s.lt.startDelayedOff(s.delayedOffTick)
		}
		return
	}
	s.lt.onTime = max(onTime, s.lt.onTime)
	s.lt.offWaitTime = offWaitTime
	// 0xFFFF holds indefinitely (spec §1.5.8): no countdown.
	if s.lt.onTime != 0 && s.lt.onTime != 0xFFFF {
		s.lt.startTimedOn(s.timedOnTick)
	} else {
		s.lt.stopTimedOn()
	}
	s.on()
}

// timedOnTick runs every 100 ms while a timed-on phase counts down —
// matter.js OnOffServer.ts #timedOnTick. OnTime is in tenths of a second.
func (s *onOffServer) timedOnTick() {
	changed, switched := s.timedOnStep()
	if switched != nil {
		s.apply(*switched)
	}
	s.changes.Notify(changed...)
}

// timedOnStep is one countdown step of [onOffServer.timedOnTick]; it
// returns the attributes it changed and the switch it decided, which the
// caller applies and reports once the lock is released.
func (s *onOffServer) timedOnStep() (changed []uint32, switched *bool) {
	s.lt.mu.Lock()
	defer s.lt.mu.Unlock()
	if s.lt.timedOn == nil {
		return nil, nil // stopped between the fire and the lock
	}
	if s.lt.onTime == 0xFFFF {
		s.lt.stopTimedOn()
		return nil, nil
	}
	if s.lt.onTime <= 1 {
		changed := []uint32{onoff.AttrOnOff, onoff.AttrOnTime}
		if s.lt.offWaitTime != 0 {
			changed = append(changed, onoff.AttrOffWaitTime)
		}
		s.lt.onTime = 0
		s.lt.stopTimedOn()
		s.lt.offWaitTime = 0
		s.off()
		return changed, s.lt.takeSwitch()
	}
	s.lt.onTime--
	s.version.Bump()
	s.lt.timedOn.Reset(lightingTick)
	return []uint32{onoff.AttrOnTime}, nil
}

// delayedOffTick runs every 100 ms through the delayed-off guard —
// matter.js OnOffServer.ts #delayedOffTick.
func (s *onOffServer) delayedOffTick() {
	s.changes.Notify(s.delayedOffStep()...)
}

// delayedOffStep is one step of [onOffServer.delayedOffTick], returning
// what it changed.
func (s *onOffServer) delayedOffStep() []uint32 {
	s.lt.mu.Lock()
	defer s.lt.mu.Unlock()
	if s.lt.delayedOff == nil {
		return nil
	}
	if s.lt.offWaitTime == 0xFFFF {
		s.lt.stopDelayedOff()
		return nil
	}
	if s.lt.offWaitTime <= 1 {
		s.lt.offWaitTime = 0
		s.lt.stopDelayedOff()
	} else {
		s.lt.offWaitTime--
		s.lt.delayedOff.Reset(lightingTick)
	}
	s.version.Bump()
	return []uint32{onoff.AttrOffWaitTime}
}

// apply drives the device and bumps the cluster's DataVersion, so a
// controller's DataVersionFilter misses and the next report carries the new
// value.
func (s *onOffServer) apply(on bool) {
	s.dev.setOn(on)
	s.version.Bump()
	slog.Info(s.logMessage, slog.String("device", s.dev.deviceName()), slog.Bool("on", on))
}

// MatterReportable implements [contract.ClusterServer].
func (s *onOffServer) MatterReportable() []uint32 {
	if s.lt != nil {
		return []uint32{onoff.AttrOnOff, onoff.AttrGlobalSceneControl, onoff.AttrOnTime, onoff.AttrOffWaitTime}
	}
	return []uint32{onoff.AttrOnOff}
}

// MatterAttributes implements [contract.ClusterAttributeLister] so a
// wildcard read enumerates OnOff rather than only the globals.
func (s *onOffServer) MatterAttributes() []uint32 {
	if s.lt != nil {
		return onoff.LightingAttributes()
	}
	return []uint32{onoff.AttrOnOff}
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (s *onOffServer) MatterAcceptedCommands() []uint32 {
	if s.lt != nil {
		return onoff.LightingCommands()
	}
	return []uint32{onoff.CmdOff, onoff.CmdOn, onoff.CmdToggle}
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister]. OnOff
// emits no response commands.
func (s *onOffServer) MatterGeneratedCommands() []uint32 { return []uint32{} }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *onOffServer) MatterDataVersion() uint32 { return s.version.Current() }

// lightingTick is the countdown period of the two LT timers: OnTime and
// OffWaitTime count tenths of a second (matter.js OnOffServer.ts
// `Time.getPeriodicTimer("Timed on", Millis(100), …)`).
const lightingTick = 100 * time.Millisecond

// lightingState is the OnOff cluster's LT-feature state plus the two
// countdowns of matter.js OnOffServer.ts. An idle light owns no goroutine:
// each countdown is a time.AfterFunc that re-arms itself per tick and is
// nil when not running.
type lightingState struct {
	mu                 sync.Mutex
	globalSceneControl bool
	onTime             uint16
	offWaitTime        uint16
	startUpOnOff       *uint8
	timedOn            *time.Timer
	delayedOff         *time.Timer
	// pendingSwitch is the on/off state a command or tick decided while
	// holding mu, applied to the device after mu is released.
	pendingSwitch *bool
}

// switchTo records the on/off state to apply once mu is released.
func (l *lightingState) switchTo(on bool) { l.pendingSwitch = &on }

// takeSwitch returns and clears the recorded switch.
func (l *lightingState) takeSwitch() *bool {
	p := l.pendingSwitch
	l.pendingSwitch = nil
	return p
}

// newLightingState returns the LT defaults: GlobalSceneControl true, no
// countdown, StartUpOnOff null (on-off.element.ts:30-36 defaults).
func newLightingState() *lightingState { return &lightingState{globalSceneControl: true} }

func (l *lightingState) startTimedOn(tick func()) {
	if l.timedOn == nil {
		l.timedOn = time.AfterFunc(lightingTick, tick)
	}
}

func (l *lightingState) stopTimedOn() {
	if l.timedOn != nil {
		l.timedOn.Stop()
		l.timedOn = nil
	}
}

func (l *lightingState) startDelayedOff(tick func()) {
	if l.delayedOff == nil {
		l.delayedOff = time.AfterFunc(lightingTick, tick)
	}
}

func (l *lightingState) stopDelayedOff() {
	if l.delayedOff != nil {
		l.delayedOff.Stop()
		l.delayedOff = nil
	}
}

// onWithTimedOffFields pulls OnWithTimedOff's three fields out of the
// bridge-decoded payload, tags per on-off.element.ts:52-55: [0]
// OnOffControl (bitmap8), [1] OnTime (uint16), [2] OffWaitTime (uint16).
// The bridge decodes the payload through the generated OnOff definition
// into an [onoffdef.OnWithTimedOffRequest], already checked against
// matter.js's request schema; an in-process caller may still hand over a
// tag-keyed map, in which an absent field reads as 0.
func onWithTimedOffFields(fields any) (control uint8, onTime, offWaitTime uint16, err error) {
	switch r := fields.(type) {
	case onoffdef.OnWithTimedOffRequest:
		return uint8(r.OnOffControl), r.OnTime, r.OffWaitTime, nil
	case *onoffdef.OnWithTimedOffRequest:
		if r != nil {
			return uint8(r.OnOffControl), r.OnTime, r.OffWaitTime, nil
		}
	}
	m, ok := fields.(map[uint8]any)
	if !ok {
		if fields == nil {
			return 0, 0, 0, nil
		}
		return 0, 0, 0, fmt.Errorf("onoff: OnWithTimedOff expects tagged fields, got %T", fields)
	}
	if raw, present := m[0]; present {
		if control, ok = cluster.AsUint8(raw); !ok {
			return 0, 0, 0, fmt.Errorf("onoff: OnWithTimedOff OnOffControl expects bitmap8, got %T", raw)
		}
	}
	if raw, present := m[1]; present {
		if onTime, ok = asUint16(raw); !ok {
			return 0, 0, 0, fmt.Errorf("onoff: OnWithTimedOff OnTime expects uint16, got %T", raw)
		}
	}
	if raw, present := m[2]; present {
		if offWaitTime, ok = asUint16(raw); !ok {
			return 0, 0, 0, fmt.Errorf("onoff: OnWithTimedOff OffWaitTime expects uint16, got %T", raw)
		}
	}
	return control, onTime, offWaitTime, nil
}

// asUint16 accepts the integer widths the TLV decoder produces for a
// uint16 field.
func asUint16(v any) (uint16, bool) {
	switch n := v.(type) {
	case uint16:
		return n, true
	case uint8:
		return uint16(n), true
	case uint32:
		if n > 0xFFFF {
			return 0, false
		}
		return uint16(n), true
	case uint64:
		if n > 0xFFFF {
			return 0, false
		}
		return uint16(n), true
	case int:
		if n < 0 || n > 0xFFFF {
			return 0, false
		}
		return uint16(n), true
	case int64:
		if n < 0 || n > 0xFFFF {
			return 0, false
		}
		return uint16(n), true
	default:
		return 0, false
	}
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
	// version is held by the device, not by the cluster server, because the
	// assembler rebuilds the server on every reassembly and a version that
	// restarted with it would go backwards.
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
	_ contract.EndpointSource = (*demoValve)(nil)
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

// MatterDeviceType implements [contract.EndpointSource].
func (v *demoValve) MatterDeviceType() uint16 { return deviceTypeWaterValve }

// MatterClusterServers implements [contract.EndpointSource]. The device is
// the server's narrow port: every attribute the cluster answers is read back
// out of it, and both commands land on it.
func (v *demoValve) MatterClusterServers() []contract.ClusterServer {
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
	if req.HasOpenDuration {
		duration = req.OpenDuration
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
	_ contract.EndpointSource = (*demoSelector)(nil)
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

// MatterDeviceType implements [contract.EndpointSource].
func (s *demoSelector) MatterDeviceType() uint16 { return deviceTypeModeSelect }

// MatterClusterServers implements [contract.EndpointSource].
func (s *demoSelector) MatterClusterServers() []contract.ClusterServer {
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
// OnOff cluster a controller can read back.
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
	// lighting selects the ceiling-light surface; see MatterClusterServers.
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
}

// Compile-time assertions.
var (
	_ contract.EndpointSource  = (*dimmer)(nil)
	_ contract.ChangeNotifier  = (*dimmer)(nil)
	_ levelcontrol.LevelSource = (*dimmer)(nil)
	_ onOffDevice              = (*dimmer)(nil)
)

// newDemoSpeaker returns a speaker that is off, at the given volume.
func newDemoSpeaker(name string, level uint8) *dimmer {
	return &dimmer{name: name, kind: "speaker", minLevel: levelcontrol.LevelMin, level: level}
}

// MatterDeviceType implements [contract.EndpointSource].
func (s *dimmer) MatterDeviceType() uint16 {
	if s.lighting != nil && s.lighting.extended {
		return deviceTypeExtendedColorLight
	}
	if s.lighting != nil {
		return deviceTypeColorTemperatureLight
	}
	return deviceTypeSpeaker
}

// MatterClusterServers implements [contract.EndpointSource]. Speaker
// requires both clusters, and both are served from this one device; the
// ceiling light adds the Lighting surface (fleet_lighting.go).
func (s *dimmer) MatterClusterServers() []contract.ClusterServer {
	if s.lighting != nil {
		return s.lightingServers()
	}
	return []contract.ClusterServer{
		&onOffServer{dev: s, logMessage: "speaker.set"},
		levelcontrol.NewServer(levelcontrol.Config{
			Source:      s,
			DataVersion: &s.version,
		}),
	}
}

// deviceName implements [onOffDevice].
func (s *dimmer) deviceName() string { return s.name }

// isOn implements [onOffDevice].
func (s *dimmer) isOn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.on
}

// setOn implements [onOffDevice]. Turning the speaker on restores OnLevel
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
func (s *dimmer) Options() uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.options
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
func (s *dimmer) SetOptions(_ context.Context, options uint8) error {
	s.mu.Lock()
	s.options = options
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
func (s *dimmer) MoveToLevel(_ context.Context, req levelcontrol.MoveToLevelRequest) error {
	if !s.executes(req.OptionsMask, req.OptionsOverride) {
		return nil
	}
	s.applyLevel(req.Level, false)
	return nil
}

// MoveToLevelWithOnOff implements [levelcontrol.LevelSource].
func (s *dimmer) MoveToLevelWithOnOff(_ context.Context, req levelcontrol.MoveToLevelRequest) error {
	s.applyLevel(req.Level, true)
	return nil
}

// Move implements [levelcontrol.LevelSource]. With no travel time the move
// arrives at the end of its direction inside the command.
func (s *dimmer) Move(_ context.Context, req levelcontrol.MoveRequest) error {
	if !s.executes(req.OptionsMask, req.OptionsOverride) {
		return nil
	}
	s.applyLevel(s.moveTarget(req.MoveMode), false)
	return nil
}

// MoveWithOnOff implements [levelcontrol.LevelSource].
func (s *dimmer) MoveWithOnOff(_ context.Context, req levelcontrol.MoveRequest) error {
	s.applyLevel(s.moveTarget(req.MoveMode), true)
	return nil
}

// Step implements [levelcontrol.LevelSource].
func (s *dimmer) Step(_ context.Context, req levelcontrol.StepRequest) error {
	if !s.executes(req.OptionsMask, req.OptionsOverride) {
		return nil
	}
	s.applyLevel(s.steppedLevel(req), false)
	return nil
}

// StepWithOnOff implements [levelcontrol.LevelSource].
func (s *dimmer) StepWithOnOff(_ context.Context, req levelcontrol.StepRequest) error {
	s.applyLevel(s.steppedLevel(req), true)
	return nil
}

// Stop implements [levelcontrol.LevelSource]. See the type doc: this model
// never has a move in flight to halt.
func (s *dimmer) Stop(context.Context, levelcontrol.StopRequest) error { return nil }

// StopWithOnOff implements [levelcontrol.LevelSource].
func (s *dimmer) StopWithOnOff(context.Context, levelcontrol.StopRequest) error { return nil }

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
	return levelcontrol.EffectiveOptions(s.Options(), mask, override)&levelcontrol.OptionExecuteIfOff != 0
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
}

// steppedLevel resolves a Step command against the current level, clamped to
// the cluster's own bounds.
func (s *dimmer) steppedLevel(req levelcontrol.StepRequest) uint8 {
	s.mu.Lock()
	current := s.level
	s.mu.Unlock()
	if req.StepMode == levelcontrol.StepModeUp {
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

// fleet is the hard-coded device list this daemon bridges, plus the
// assembler that turns it into a topology.
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

	assembler *endpoint.Assembler
	// labels returns the NodeLabel a controller wrote for a bridged
	// endpoint in an earlier run (persist.go); nil restores nothing.
	labels func(ctx context.Context, key endpoint.StringKey) string
	// configVersions returns the ConfigurationVersion persisted for a
	// bridged endpoint (persist.go); nil restores nothing.
	configVersions func(ctx context.Context, key endpoint.StringKey) uint32
	// configChange raises the ConfigurationVersion of every bridged device
	// (the app pipe's SimulateConfigurationVersionChange); wired by main.
	configChange func() error
}

func newFleet(store endpoint.Store, cfg endpoint.Config, logger *slog.Logger) (*fleet, error) {
	asm, err := endpoint.New(store, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("endpoint assembler: %w", err)
	}
	return &fleet{
		light:       newDemoLight("Desk Lamp"),
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
		vacuum:      newDemoVacuum("Robot Vacuum"),
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
		assembler:   asm,
	}, nil
}

// snapshotter is what the bridge calls at Start and on every Reassemble. It
// walks this host's model — here, a hard-coded fleet — describes each
// as a flat [endpoint.Spec], and hands the assembled topology back.
//
// StableKey is the load-bearing field: it decides which endpoint number the
// device gets back after a restart, so it must render byte-for-byte
// identically for the same device across releases.
func (f *fleet) snapshotter(ctx context.Context) (*endpoint.Topology, error) {
	specs := []endpoint.Spec{ //nolint:prealloc // the literal is the readable part; the surface devices are appended below
		{
			StableKey:      endpoint.StringKey("demo:light:1"),
			DeviceAddress:  "demo-light-1",
			ChannelAddress: "demo-light-1:0",
			DeviceType:     onoff.DeviceTypeOnOffLight,
			FriendlyName:   f.light.name,
			Source:         f.light,
		},
		{
			StableKey:      endpoint.StringKey("demo:thermometer:1"),
			DeviceAddress:  "demo-thermometer-1",
			ChannelAddress: "demo-thermometer-1:0",
			DeviceType:     contract.MeasurementClassDeviceType(contract.MeasurementTemperature),
			FriendlyName:   "Study Thermometer",
			Measurement:    f.thermometer,
		},
		{
			StableKey:      endpoint.StringKey("demo:valve:1"),
			DeviceAddress:  "demo-valve-1",
			ChannelAddress: "demo-valve-1:0",
			DeviceType:     deviceTypeWaterValve,
			FriendlyName:   f.valve.name,
			Source:         f.valve,
		},
		{
			StableKey:      endpoint.StringKey("demo:selector:1"),
			DeviceAddress:  "demo-selector-1",
			ChannelAddress: "demo-selector-1:0",
			DeviceType:     deviceTypeModeSelect,
			FriendlyName:   f.selector.name,
			Source:         f.selector,
		},
		{
			StableKey:      endpoint.StringKey("demo:speaker:1"),
			DeviceAddress:  "demo-speaker-1",
			ChannelAddress: "demo-speaker-1:0",
			DeviceType:     deviceTypeSpeaker,
			FriendlyName:   f.speaker.name,
			Source:         f.speaker,
		},
	}
	specs = append(specs, f.surfaceSpecs()...)
	if f.labels != nil {
		for i := range specs {
			if k, ok := specs[i].StableKey.(endpoint.StringKey); ok {
				specs[i].NodeLabel = f.labels(ctx, k)
			}
		}
	}
	if f.configVersions != nil {
		for i := range specs {
			if k, ok := specs[i].StableKey.(endpoint.StringKey); ok {
				specs[i].ConfigurationVersion = f.configVersions(ctx, k)
			}
		}
	}
	return f.assembler.Assemble(ctx, []endpoint.Snapshot{{
		Scope:     scope,
		Endpoints: specs,
		// The fleet is hard-coded, so it is authoritative from the first
		// call. A host whose model loads asynchronously must report false
		// until the load finishes, or the assembler's vanished-source
		// collection wipes every persisted endpoint number at boot.
		ModelComplete: true,
	}})
}

// surfaceSpecs describes the devices that give the chip-tool suite one
// endpoint per surface it drives. They are appended after the original five
// so those keep their StableKeys — and therefore their endpoint numbers — on
// a database written by an earlier release of this example.
func (f *fleet) surfaceSpecs() []endpoint.Spec {
	spec := func(key, name string, deviceType uint16, src contract.EndpointSource) endpoint.Spec {
		return endpoint.Spec{
			StableKey:      endpoint.StringKey("demo:" + key + ":1"),
			DeviceAddress:  "demo-" + key + "-1",
			ChannelAddress: "demo-" + key + "-1:0",
			DeviceType:     deviceType,
			FriendlyName:   name,
			Source:         src,
		}
	}
	sensor := func(key, name string, m contract.MeasurementSource) endpoint.Spec {
		return endpoint.Spec{
			StableKey:      endpoint.StringKey("demo:" + key + ":1"),
			DeviceAddress:  "demo-" + key + "-1",
			ChannelAddress: "demo-" + key + "-1:0",
			DeviceType:     contract.MeasurementClassDeviceType(m.MatterMeasurementClass()),
			FriendlyName:   name,
			Measurement:    m,
		}
	}
	smoke := spec("smoke", f.smoke.name, f.smoke.MatterDeviceType(), f.smoke)
	// SmokeCoAlarm requires a PowerSource device-type component: the
	// alarm's battery is a part of its own, as matter.js composes it
	// (examples/device-smoke-co-alarm, parts: [PowerSourceEndpoint]).
	smoke.Parts = []endpoint.Spec{{
		StableKey:   endpoint.StringKey("demo:smoke:battery"),
		DeviceType:  deviceTypePowerSource,
		PowerSource: f.smoke.battery,
	}}
	// Refrigerator requires a TemperatureControlledCabinet part: the
	// compartment is a part of the fridge's endpoint.
	fridge := spec("fridge", f.fridge.name, f.fridge.MatterDeviceType(), f.fridge)
	fridge.Parts = f.fridge.parts()
	return []endpoint.Spec{
		spec("ceiling", f.ceiling.name, f.ceiling.MatterDeviceType(), f.ceiling),
		spec("fan", f.fan.name, f.fan.MatterDeviceType(), f.fan),
		smoke,
		spec("pump", f.pump.name, f.pump.MatterDeviceType(), f.pump),
		sensor("flow", f.flow.name, f.flow),
		spec("washer", f.washer.name, f.washer.MatterDeviceType(), f.washer),
		spec("vacuum", f.vacuum.name, f.vacuum.MatterDeviceType(), f.vacuum),
		spec("thermostat", f.thermostat.name, f.thermostat.MatterDeviceType(), f.thermostat),
		spec("blind", f.blind.name, f.blind.MatterDeviceType(), f.blind),
		spec("lock", f.lock.name, f.lock.MatterDeviceType(), f.lock),
		sensor("humidity", f.humidity.name, f.humidity),
		sensor("occupancy", f.occupancy.name, f.occupancy),
		spec("contact", f.contact.name, f.contact.MatterDeviceType(), f.contact),
		sensor("button", f.button.name, f.button),
		spec("purifier", f.purifier.name, f.purifier.MatterDeviceType(), f.purifier),
		spec("garage", f.garage.name, f.garage.MatterDeviceType(), f.garage),
		spec("colorlight", f.colorLight.name, f.colorLight.MatterDeviceType(), f.colorLight),
		fridge,
		spec("airquality", f.airQuality.name, f.airQuality.MatterDeviceType(), f.airQuality),
		spec("waterheater", f.heater.name, f.heater.MatterDeviceType(), f.heater),
		spec("meter", f.meter.name, f.meter.MatterDeviceType(), f.meter),
	}
}
