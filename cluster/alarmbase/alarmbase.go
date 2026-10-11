// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package alarmbase contains one server for the Matter AlarmBase cluster
// family, instantiated for its three derivations: DishwasherAlarm
// (0x005D), RefrigeratorAlarm (0x0057) and TemperatureAlarm (0x0064).
//
// The server is built on the generated definitions
// (cluster/spec/dishwasheralarm, cluster/spec/refrigeratoralarm,
// cluster/spec/temperaturealarm; ADR 0013): ids, the AlarmBitmap of each
// derivation, the feature-dependent attribute, command and event lists,
// FeatureMap, ClusterRevision, the privileges and the write answers come
// from them. matter.js adds no logic to the generated behaviors —
// DishwasherAlarmServer, RefrigeratorAlarmServer and TemperatureAlarmServer
// (packages/node/src/behaviors/<name>/<Name>Server.ts) are empty
// subclasses — so the rules this server adds are connectedhomeip's
// AlarmBase server at the harness pin 6170af8461b10b1766044122ac83332c6d00ab20
// (src/app/clusters/alarm-base-server/AlarmBaseCluster.cpp):
//
//   - Supported and Latch are fixed: they are configured once and never
//     change (alarm-base-server/README.md "Fixed attributes");
//   - a Mask that is not a subset of Supported is refused; a Mask that no
//     longer covers State clears the masked-off bits of State, ignoring the
//     latch (SetMask :34-46);
//   - a State that is not a subset of Supported or of Mask is refused; with
//     the Reset feature, the latched bits of the current State stay set
//     (SetStateInternal :58-71);
//   - a State change emits Notify with the bits that became active, those
//     that became inactive, the new State and the Mask; an unchanged State
//     emits nothing (:73-81, DishwasherAlarmCluster.cpp:25-37,
//     RefrigeratorAlarmCluster.cpp:25-37);
//   - Reset: alarms not a subset of Supported answer INVALID_COMMAND, a
//     host refusal FAILURE; otherwise the alarms are cleared from State,
//     latch or not (HandleReset :162-175, ResetLatchedAlarms :84-91);
//   - ModifyEnabledAlarms: a mask not a subset of Supported answers
//     INVALID_COMMAND, a host refusal FAILURE; otherwise the mask is set as
//     SetMask sets it (HandleModifyEnabledAlarms :177-190);
//   - the initial Mask is set before the initial State
//     (dishwasher-alarm-server/CodegenIntegration.cpp:158-167).
package alarmbase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	dwa "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
	rfa "github.com/SukramJ/go-fabric/cluster/spec/refrigeratoralarm"
	tma "github.com/SukramJ/go-fabric/cluster/spec/temperaturealarm"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// Cluster ids.
const (
	ClusterIDDishwasherAlarm   = dwa.ClusterID
	ClusterIDRefrigeratorAlarm = rfa.ClusterID
	ClusterIDTemperatureAlarm  = tma.ClusterID
)

// FeatureReset is the Reset feature (RESET), bit 0 in every derivation.
const FeatureReset = uint32(dwa.FeatureReset)

// The AlarmBase element ids every derivation inherits unchanged.
const (
	AttrMask               = dwa.AttrMask
	AttrLatch              = dwa.AttrLatch
	AttrState              = dwa.AttrState
	AttrSupported          = dwa.AttrSupported
	CmdReset               = dwa.CmdReset
	CmdModifyEnabledAlarms = dwa.CmdModifyEnabledAlarms
	EventNotify            = dwa.EventNotify
)

// Delegate is the host port chip's AlarmBase::Delegate is
// (alarm-base-server/Delegate.h): it is asked before a command changes
// Mask or State. An error refuses the command — the server answers
// FAILURE and changes nothing. A nil Delegate accepts every command, as
// chip's default implementation does (Delegate.h:63, :82).
type Delegate interface {
	// ModifyEnabledAlarms is asked before Mask becomes mask.
	ModifyEnabledAlarms(ctx context.Context, mask uint32) error
	// ResetAlarms is asked before the alarms are cleared from State.
	ResetAlarms(ctx context.Context, alarms uint32) error
}

// Config carries the construction parameters.
type Config struct {
	// Features is the FeatureMap: FeatureReset, and for TemperatureAlarm
	// its threshold features.
	Features uint32
	// Supported (fixed) is the set of alarms the device has.
	Supported uint32
	// Latch (fixed, with Reset) is the set of alarms that stay set until a
	// Reset clears them.
	Latch uint32
	// Mask is the initial set of enabled alarms, a subset of Supported.
	Mask uint32
	// State is the initial set of active alarms, a subset of Mask.
	State uint32
	// ModifyEnabledAlarms accepts the optional ModifyEnabledAlarms command.
	ModifyEnabledAlarms bool
	// Delegate is asked before a command changes Mask or State; optional.
	Delegate Delegate
	// Thresholds holds the TemperatureAlarm threshold attributes (in
	// 0.01 °C) the feature selection serves, by attribute id; every served
	// one is required. Other derivations leave it nil.
	Thresholds map[uint32]int16
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Configuration and setter errors.
var (
	// ErrNotSupported: a Mask, State or Reset set with an alarm outside
	// Supported, or a State with an alarm outside Mask — what chip answers
	// with Status::Failure (AlarmBaseCluster.cpp:36, :62-63, :86).
	ErrNotSupported = errors.New("alarmbase: alarms outside Supported or Mask")
	// ErrThresholds: a served TemperatureAlarm threshold missing, not
	// served, or outside its type.
	ErrThresholds = errors.New("alarmbase: temperature thresholds")
	// ErrAdjustableThresholds: a TemperatureAlarm *ADJ feature, which
	// mandates SetTemperatureAlarmThresholds. Neither matter.js
	// (TemperatureAlarmServer is empty) nor chip at the pin (no
	// temperature-alarm-server) gives that command a rule, so the server
	// does not accept it.
	ErrAdjustableThresholds = errors.New("alarmbase: SetTemperatureAlarmThresholds has no source rule; the *ADJ features are not supported")
)

// variant is what distinguishes the derivations.
type variant struct {
	def    *spec.Cluster
	notify func(active, inactive, state, mask uint32) any
	// alarmsOf extracts the bitmap of a Reset / ModifyEnabledAlarms
	// request; false for a payload of another type.
	resetOf, maskOf func(fields any) (uint32, bool)
}

var (
	dishwasher = variant{
		def: dwa.Definition,
		notify: func(a, i, s, m uint32) any {
			return dwa.NotifyEvent{Active: dwa.AlarmBitmap(a), Inactive: dwa.AlarmBitmap(i), State: dwa.AlarmBitmap(s), Mask: dwa.AlarmBitmap(m)}
		},
		resetOf: func(f any) (uint32, bool) { r, ok := f.(dwa.ResetRequest); return uint32(r.Alarms), ok },
		maskOf:  func(f any) (uint32, bool) { r, ok := f.(dwa.ModifyEnabledAlarmsRequest); return uint32(r.Mask), ok },
	}
	refrigerator = variant{
		def: rfa.Definition,
		notify: func(a, i, s, m uint32) any {
			return rfa.NotifyEvent{Active: rfa.AlarmBitmap(a), Inactive: rfa.AlarmBitmap(i), State: rfa.AlarmBitmap(s), Mask: rfa.AlarmBitmap(m)}
		},
		resetOf: func(f any) (uint32, bool) { r, ok := f.(rfa.ResetRequest); return uint32(r.Alarms), ok },
		maskOf:  func(f any) (uint32, bool) { r, ok := f.(rfa.ModifyEnabledAlarmsRequest); return uint32(r.Mask), ok },
	}
	temperature = variant{
		def: tma.Definition,
		notify: func(a, i, s, m uint32) any {
			return tma.NotifyEvent{Active: tma.AlarmBitmap(a), Inactive: tma.AlarmBitmap(i), State: tma.AlarmBitmap(s), Mask: tma.AlarmBitmap(m)}
		},
		resetOf: func(f any) (uint32, bool) { r, ok := f.(tma.ResetRequest); return uint32(r.Alarms), ok },
		maskOf:  func(f any) (uint32, bool) { r, ok := f.(tma.ModifyEnabledAlarmsRequest); return uint32(r.Mask), ok },
	}
)

// adjustable is the TemperatureAlarm *ADJ feature set.
const adjustable = uint32(tma.FeatureOverCriticalAdjustable | tma.FeatureOverMajorAdjustable |
	tma.FeatureOverMinorAdjustable | tma.FeatureUnderMinorAdjustable |
	tma.FeatureUnderMajorAdjustable | tma.FeatureUnderCriticalAdjustable)

// thresholdAttrs are the TemperatureAlarm threshold attributes.
var thresholdAttrs = []uint32{
	tma.AttrCriticalOverTemperatureThreshold, tma.AttrMajorOverTemperatureThreshold,
	tma.AttrMinorOverTemperatureThreshold, tma.AttrMinorUnderTemperatureThreshold,
	tma.AttrMajorUnderTemperatureThreshold, tma.AttrCriticalUnderTemperatureThreshold,
}

// Server implements [contract.ClusterServer] for one AlarmBase
// derivation. Reads, the read-only write answers, the command dispatch,
// the data version, the change notifications and the event priority are
// the generated server's ([spec.Server]); the lists, globals and
// privileges its embedded [spec.Instance]'s. What stays here are chip's
// AlarmBase rules.
type Server struct {
	*spec.Instance

	v         variant
	srv       *spec.Server
	supported uint32
	latch     uint32
	delegate  Delegate

	// op serialises the read-modify-write of Mask and State and the Notify
	// that reports it.
	op       sync.Mutex
	mu       sync.Mutex
	endpoint uint16
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*Server)(nil)
	_ contract.ClusterDataVersion             = (*Server)(nil)
	_ contract.ClusterAttributeLister         = (*Server)(nil)
	_ contract.ClusterCommandLister           = (*Server)(nil)
	_ contract.ClusterEventLister             = (*Server)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*Server)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*Server)(nil)
	_ contract.AttributeChangeNotifier        = (*Server)(nil)
	_ contract.EventReceiver                  = (*Server)(nil)
)

// NewDishwasherAlarm builds a DishwasherAlarm (0x005D) server.
func NewDishwasherAlarm(cfg Config) (*Server, error) { return newServer(dishwasher, cfg) }

// NewRefrigeratorAlarm builds a RefrigeratorAlarm (0x0057) server. Its
// Reset feature and ModifyEnabledAlarms are disallowed ("X").
func NewRefrigeratorAlarm(cfg Config) (*Server, error) { return newServer(refrigerator, cfg) }

// NewTemperatureAlarm builds a TemperatureAlarm (0x0064) server. The
// threshold attributes the feature selection serves come from
// Config.Thresholds; the *ADJ features are refused
// ([ErrAdjustableThresholds]).
func NewTemperatureAlarm(cfg Config) (*Server, error) {
	if cfg.Features&adjustable != 0 {
		return nil, ErrAdjustableThresholds
	}
	return newServer(temperature, cfg)
}

func newServer(v variant, cfg Config) (*Server, error) {
	opts := spec.Options{Features: cfg.Features}
	if cfg.ModifyEnabledAlarms {
		opts.Commands = []uint32{CmdModifyEnabledAlarms}
	}
	initial := map[uint32]any{
		AttrSupported: cfg.Supported,
		AttrMask:      uint32(0),
		AttrState:     uint32(0),
	}
	if v.def.ID == tma.ClusterID {
		inst, err := spec.New(v.def, opts)
		if err != nil {
			return nil, fmt.Errorf("alarmbase: %w", err)
		}
		for _, id := range thresholdAttrs {
			if !inst.Serves(id) {
				continue
			}
			t, ok := cfg.Thresholds[id]
			if !ok {
				return nil, fmt.Errorf("%w: %s missing", ErrThresholds, v.def.Attribute(id).Name)
			}
			initial[id] = t
		}
	}
	if cfg.Features&FeatureReset != 0 {
		initial[AttrLatch] = cfg.Latch
	}
	srv, err := spec.NewServer(v.def, opts, spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("alarmbase: %w", err)
	}
	if err := srv.SetAttributes(initial); err != nil {
		return nil, fmt.Errorf("alarmbase: %w", err)
	}
	s := &Server{Instance: srv.Instance, v: v, srv: srv, supported: cfg.Supported, latch: cfg.Latch, delegate: cfg.Delegate}
	// The initial Mask, then the initial State, as chip's integration sets
	// them (dishwasher-alarm-server/CodegenIntegration.cpp:158-167). No
	// emitter is attached yet, so the State's Notify goes nowhere, as
	// chip's goes nowhere before Startup (DishwasherAlarmCluster.cpp:28).
	if err := s.SetMask(cfg.Mask); err != nil {
		return nil, err
	}
	if err := s.SetState(cfg.State); err != nil {
		return nil, err
	}
	srv.Handle(CmdReset, s.handleReset)
	srv.Handle(CmdModifyEnabledAlarms, s.handleModifyEnabledAlarms)
	return s, nil
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *Server) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.srv.OnMatterAttributesChanged(cb)
}

// MatterRead resolves an attribute.
func (s *Server) MatterRead(attrID uint32) (any, bool) { return s.srv.MatterRead(attrID) }

// MatterWrite answers every write: every attribute is read-only.
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches an accepted command; the generated server
// answers any other with UNSUPPORTED_COMMAND.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return s.srv.MatterInvoke(ctx, cmdID, fields)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *Server) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.srv.SetMatterEventEmitter(emitter)
}

// SetEndpoint stamps the endpoint Notify is addressed to; the bridge calls
// it when it attaches the emitter.
func (s *Server) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
}

// Supported returns the fixed Supported bitmap.
func (s *Server) Supported() uint32 { return s.supported }

// Latch returns the fixed Latch bitmap (served with the Reset feature).
func (s *Server) Latch() uint32 { return s.latch }

// Mask returns the current Mask.
func (s *Server) Mask() uint32 { return s.bitmap(AttrMask) }

// State returns the current State.
func (s *Server) State() uint32 { return s.bitmap(AttrState) }

func (s *Server) bitmap(attrID uint32) uint32 {
	v, _ := s.srv.Value(attrID)
	n, _ := v.(uint32)
	return n
}

// SetMask records the alarms the device has enabled — chip's SetMask
// (AlarmBaseCluster.cpp:34-46). A mask with an alarm outside Supported is
// refused ([ErrNotSupported]); an active alarm the new mask disables is
// cleared from State, latched or not, and Notify reports it.
func (s *Server) SetMask(mask uint32) error {
	s.op.Lock()
	defer s.op.Unlock()
	return s.setMask(mask)
}

func (s *Server) setMask(mask uint32) error {
	if mask&^s.supported != 0 {
		return fmt.Errorf("%w: Mask 0x%X, Supported 0x%X", ErrNotSupported, mask, s.supported)
	}
	if mask == s.Mask() {
		// SetAttributeValue reports no change: chip returns before it
		// looks at State (AlarmBaseCluster.cpp:37).
		return nil
	}
	if err := s.srv.Set(AttrMask, mask); err != nil {
		return fmt.Errorf("alarmbase: %w", err)
	}
	if state := s.State(); state&^mask != 0 {
		return s.setState(state&mask, true)
	}
	return nil
}

// SetState records the alarms that are active — chip's SetState
// (AlarmBaseCluster.cpp:48-82). A state with an alarm outside Supported or
// Mask is refused ([ErrNotSupported]). With the Reset feature, a latched
// alarm that is active stays active until a Reset clears it. A change
// emits Notify.
func (s *Server) SetState(state uint32) error {
	s.op.Lock()
	defer s.op.Unlock()
	return s.setState(state, false)
}

// ResetLatchedAlarms clears alarms from State, latched or not — chip's
// ResetLatchedAlarms (AlarmBaseCluster.cpp:84-91). Alarms outside
// Supported are refused ([ErrNotSupported]).
func (s *Server) ResetLatchedAlarms(alarms uint32) error {
	s.op.Lock()
	defer s.op.Unlock()
	return s.resetLatched(alarms)
}

func (s *Server) resetLatched(alarms uint32) error {
	if alarms&^s.supported != 0 {
		return fmt.Errorf("%w: alarms 0x%X, Supported 0x%X", ErrNotSupported, alarms, s.supported)
	}
	return s.setState(s.State()&^alarms, true)
}

// setState is chip's SetStateInternal.
func (s *Server) setState(state uint32, ignoreLatch bool) error {
	mask := s.Mask()
	if state&^s.supported != 0 || state&^mask != 0 {
		return fmt.Errorf("%w: State 0x%X, Supported 0x%X, Mask 0x%X", ErrNotSupported, state, s.supported, mask)
	}
	current := s.State()
	if !ignoreLatch && s.FeatureMap()&FeatureReset != 0 {
		state |= s.latch & current
	}
	if state == current {
		// An unchanged State is no change and no Notify
		// (AlarmBaseCluster.cpp:73).
		return nil
	}
	if err := s.srv.Set(AttrState, state); err != nil {
		return fmt.Errorf("alarmbase: %w", err)
	}
	s.mu.Lock()
	endpoint := s.endpoint
	s.mu.Unlock()
	// Notify: became active, became inactive, the new State, the Mask
	// (AlarmBaseCluster.cpp:75-80).
	return s.srv.Emit(endpoint, EventNotify, s.v.notify(state&^current, current&^state, state, mask))
}

// handleReset is chip's HandleReset (AlarmBaseCluster.cpp:162-175).
func (s *Server) handleReset(ctx context.Context, fields any) (any, error) {
	alarms, ok := s.v.resetOf(fields)
	if !ok {
		return nil, statusError{im.StatusInvalidCommand, fmt.Sprintf("alarmbase: Reset fields %T", fields)}
	}
	if alarms&^s.supported != 0 {
		return nil, statusError{im.StatusInvalidCommand, fmt.Sprintf("alarmbase: Reset 0x%X outside Supported 0x%X", alarms, s.supported)}
	}
	if s.delegate != nil {
		if err := s.delegate.ResetAlarms(ctx, alarms); err != nil {
			return nil, statusError{im.StatusFailure, fmt.Sprintf("alarmbase: Reset refused: %v", err)}
		}
	}
	s.op.Lock()
	defer s.op.Unlock()
	return nil, s.resetLatched(alarms)
}

// handleModifyEnabledAlarms is chip's HandleModifyEnabledAlarms
// (AlarmBaseCluster.cpp:177-190).
func (s *Server) handleModifyEnabledAlarms(ctx context.Context, fields any) (any, error) {
	mask, ok := s.v.maskOf(fields)
	if !ok {
		return nil, statusError{im.StatusInvalidCommand, fmt.Sprintf("alarmbase: ModifyEnabledAlarms fields %T", fields)}
	}
	if mask&^s.supported != 0 {
		return nil, statusError{im.StatusInvalidCommand, fmt.Sprintf("alarmbase: ModifyEnabledAlarms 0x%X outside Supported 0x%X", mask, s.supported)}
	}
	if s.delegate != nil {
		if err := s.delegate.ModifyEnabledAlarms(ctx, mask); err != nil {
			return nil, statusError{im.StatusFailure, fmt.Sprintf("alarmbase: ModifyEnabledAlarms refused: %v", err)}
		}
	}
	s.op.Lock()
	defer s.op.Unlock()
	return nil, s.setMask(mask)
}

// Thresholds returns the served TemperatureAlarm thresholds by attribute
// id; empty for the other derivations.
func (s *Server) Thresholds() map[uint32]int16 {
	out := map[uint32]int16{}
	for _, id := range thresholdAttrs {
		if v, ok := s.srv.Value(id); ok {
			if t, isT := v.(int16); isT {
				out[id] = t
			}
		}
	}
	return out
}

// SetThresholds records TemperatureAlarm thresholds the device changed, by
// attribute id. Every one must be a served threshold and is checked as
// the definition checks it; nothing is stored when one is refused.
func (s *Server) SetThresholds(thresholds map[uint32]int16) error {
	values := make(map[uint32]any, len(thresholds))
	for id, t := range thresholds {
		if !slices.Contains(thresholdAttrs, id) || !s.Serves(id) {
			return fmt.Errorf("%w: attribute 0x%04X is not a served threshold", ErrThresholds, id)
		}
		values[id] = t
	}
	if err := s.srv.SetAttributes(values); err != nil {
		return fmt.Errorf("%w: %w", ErrThresholds, err)
	}
	return nil
}

// statusError carries an exact IM status to the dispatcher.
type statusError struct {
	status im.StatusCode
	msg    string
}

func (e statusError) Error() string                   { return e.msg }
func (e statusError) MatterStatusCode() im.StatusCode { return e.status }
