// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package boolcfg contains the server for the Matter
// BooleanStateConfiguration cluster (0x0080), the optional companion of
// BooleanState on ContactSensor, WaterLeakDetector, RainSensor and
// WaterFreezeDetector: a sensitivity level, visual / audible alarms that a
// controller enables, disables and suppresses, and sensor faults.
//
// It is built on the generated definition
// (cluster/spec/booleanstateconfiguration, ADR 0013) and the generated
// server ([spec.Server]): ids, the feature-dependent element lists,
// FeatureMap, ClusterRevision, privileges, the codecs and the write checks
// the definition carries come from there. matter.js adds no logic to the
// generated behavior (BooleanStateConfigurationServer is an empty
// subclass), so the command rules are connectedhomeip's, at the commit of
// the harness image (CHIP_TEST_IMAGE_COMMIT 6170af84):
// src/app/clusters/boolean-state-configuration-server/
// BooleanStateConfigurationCluster.cpp, cited by line below.
package boolcfg

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	def "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ClusterID is the BooleanStateConfiguration cluster id.
const ClusterID = def.ClusterID

// Generated types the server speaks.
type (
	// Feature is a FeatureMap bit.
	Feature = def.Feature
	// AlarmMode is the AlarmModeBitmap.
	AlarmMode = def.AlarmModeBitmap
	// SensorFault is the SensorFaultBitmap.
	SensorFault = def.SensorFaultBitmap
)

// Features (every one optional; SPRS needs VIS or AUD).
const (
	FeatureVisual           = def.FeatureVisual
	FeatureAudible          = def.FeatureAudible
	FeatureAlarmSuppress    = def.FeatureAlarmSuppress
	FeatureSensitivityLevel = def.FeatureSensitivityLevel
	FeatureFaultEvents      = def.FeatureFaultEvents
)

// Alarm modes and the sensor fault bit.
const (
	AlarmVisual        = def.AlarmModeVisual
	AlarmAudible       = def.AlarmModeAudible
	SensorFaultGeneral = def.SensorFaultGeneralFault
)

// allKnownAlarmModes is every AlarmModeBitmap member (chip
// BooleanStateConfigurationCluster.cpp:39-40, kAllKnownAlarmModes).
const allKnownAlarmModes = AlarmVisual | AlarmAudible

// Sensitivity-level bounds: chip clamps SupportedSensitivityLevels into
// them (BooleanStateConfigurationCluster.h:33-34, .cpp:85-86); the
// definition's constraint is the same "2 to 10".
const (
	MinSupportedSensitivityLevels uint8 = 2
	MaxSupportedSensitivityLevels uint8 = 10
)

// Optional names the optional attributes a host serves.
type Optional uint32

// Optional attributes, the set chip's OptionalAttributesSet names
// (BooleanStateConfigurationCluster.h:36-40).
const (
	// OptionalDefaultSensitivityLevel serves DefaultSensitivityLevel
	// ("[SENSLVL]").
	OptionalDefaultSensitivityLevel Optional = 1 << iota
	// OptionalAlarmsEnabled serves AlarmsEnabled ("[VIS | AUD]").
	OptionalAlarmsEnabled
	// OptionalSensorFault serves SensorFault ("O").
	OptionalSensorFault
)

// Delegate is the host port: told about every change a controller makes
// before the server applies it. An error refuses the change, and the
// command or write answers FAILURE — chip's delegate callbacks returning
// false (BooleanStateConfigurationCluster.cpp:160, :194, :206, :329, :423).
// The device-side setters ([Server.SetAlarmsActive] and the rest) are the
// host's own changes and do not call it.
type Delegate interface {
	// CurrentSensitivityLevelChanged: a controller wrote
	// CurrentSensitivityLevel (quality N — the host persists it).
	CurrentSensitivityLevelChanged(ctx context.Context, level uint8) error
	// AlarmsEnabledChanged: EnableDisableAlarm changed AlarmsEnabled
	// (quality N — the host persists it).
	AlarmsEnabledChanged(ctx context.Context, enabled AlarmMode) error
	// AlarmsActiveChanged: EnableDisableAlarm disabled an active alarm.
	AlarmsActiveChanged(ctx context.Context, active AlarmMode) error
	// AlarmsSuppressedChanged: SuppressAlarm suppressed an alarm, or
	// EnableDisableAlarm disabled a suppressed one.
	AlarmsSuppressedChanged(ctx context.Context, suppressed AlarmMode) error
}

// Config carries the construction parameters.
type Config struct {
	Features Feature
	Optional Optional
	// SupportedSensitivityLevels is fixed (SENSLVL); clamped into
	// [MinSupportedSensitivityLevels, MaxSupportedSensitivityLevels] as
	// chip clamps it (.cpp:85-86).
	SupportedSensitivityLevels uint8
	// DefaultSensitivityLevel is fixed; capped at
	// SupportedSensitivityLevels-1 (.cpp:87).
	DefaultSensitivityLevel uint8
	// CurrentSensitivityLevel is the level the host persisted; nil starts
	// at DefaultSensitivityLevel, and a level out of range is capped at
	// SupportedSensitivityLevels-1 (.cpp:97-103).
	CurrentSensitivityLevel *uint8
	// AlarmsSupported is fixed (VIS | AUD): the alarm modes the device
	// has.
	AlarmsSupported AlarmMode
	// AlarmsEnabled is the set the host persisted; chip starts at 0 when
	// nothing is stored (.cpp:104-107).
	AlarmsEnabled AlarmMode
	// Delegate is the optional host port.
	Delegate Delegate
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// ErrInvalidConfig reports a configuration the cluster's model refuses.
var ErrInvalidConfig = errors.New("boolcfg: configuration outside the cluster's model")

// Server implements [contract.ClusterServer] for BooleanStateConfiguration.
// Reads, the data version, the change notifications, the command dispatch
// and the event priority are the generated server's; what stays here is
// chip's state machine over AlarmsActive, AlarmsSuppressed and
// AlarmsEnabled, which the server keeps whether or not the optional
// attributes are served (chip keeps mAlarmsEnabled the same way,
// BooleanStateConfigurationCluster.h:128-132).
type Server struct {
	*spec.Instance

	srv       *spec.Server
	delegate  Delegate
	supported uint8 // SupportedSensitivityLevels after the clamp
	alarmsSup AlarmMode

	mu         sync.Mutex
	endpoint   uint16
	current    uint8
	active     AlarmMode
	suppressed AlarmMode
	enabled    AlarmMode
	fault      SensorFault
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

// New builds a BooleanStateConfiguration server.
func New(cfg Config) (*Server, error) {
	features := uint32(cfg.Features)
	alarms := cfg.Features&(FeatureVisual|FeatureAudible) != 0
	// SPRS needs VIS or AUD (conformance "[VIS | AUD]"; chip refuses the
	// start-up otherwise, .cpp:109-114).
	if cfg.Features&FeatureAlarmSuppress != 0 && !alarms {
		return nil, fmt.Errorf("%w: SPRS without VIS or AUD", ErrInvalidConfig)
	}
	var opts spec.Options
	opts.Features = features
	if cfg.Optional&OptionalDefaultSensitivityLevel != 0 && cfg.Features&FeatureSensitivityLevel != 0 {
		opts.Attributes = append(opts.Attributes, def.AttrDefaultSensitivityLevel)
	}
	if cfg.Optional&OptionalAlarmsEnabled != 0 && alarms {
		opts.Attributes = append(opts.Attributes, def.AttrAlarmsEnabled)
	}
	if cfg.Optional&OptionalSensorFault != 0 {
		opts.Attributes = append(opts.Attributes, def.AttrSensorFault)
	}

	s := &Server{delegate: cfg.Delegate, alarmsSup: cfg.AlarmsSupported, enabled: cfg.AlarmsEnabled}
	s.supported = min(max(cfg.SupportedSensitivityLevels, MinSupportedSensitivityLevels), MaxSupportedSensitivityLevels)
	defaultLevel := min(cfg.DefaultSensitivityLevel, s.supported-1)
	s.current = defaultLevel
	if cfg.CurrentSensitivityLevel != nil {
		s.current = *cfg.CurrentSensitivityLevel
	}
	if s.current >= s.supported {
		s.current = s.supported - 1
	}
	// AlarmModeBitmap's Visual bit has conformance "VIS" and Audible "AUD"
	// (the generated AlarmModeBitmapDef): a mode is supported only with
	// its feature.
	var featured AlarmMode
	if cfg.Features&FeatureVisual != 0 {
		featured |= AlarmVisual
	}
	if cfg.Features&FeatureAudible != 0 {
		featured |= AlarmAudible
	}
	if cfg.AlarmsSupported&^featured != 0 || cfg.AlarmsEnabled&^cfg.AlarmsSupported != 0 {
		return nil, fmt.Errorf("%w: AlarmsSupported 0x%02X / AlarmsEnabled 0x%02X", ErrInvalidConfig, cfg.AlarmsSupported, cfg.AlarmsEnabled)
	}

	srv, err := spec.NewServer(def.Definition, opts, spec.ServerConfig{DataVersion: cfg.DataVersion, Sink: sensitivitySink{s}})
	if err != nil {
		return nil, fmt.Errorf("boolcfg: %w", err)
	}
	s.srv, s.Instance = srv, srv.Instance
	initial := s.served(map[uint32]any{
		def.AttrCurrentSensitivityLevel:    s.current,
		def.AttrSupportedSensitivityLevels: s.supported,
		def.AttrDefaultSensitivityLevel:    defaultLevel,
		def.AttrAlarmsActive:               AlarmMode(0),
		def.AttrAlarmsSuppressed:           AlarmMode(0),
		def.AttrAlarmsEnabled:              s.enabled,
		def.AttrAlarmsSupported:            s.alarmsSup,
		def.AttrSensorFault:                SensorFault(0),
	})
	if err := srv.SetAttributes(initial); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	srv.Handle(def.CmdEnableDisableAlarm, s.enableDisableAlarm)
	srv.Handle(def.CmdSuppressAlarm, s.suppressAlarm)
	return s, nil
}

// served keeps the values of the attributes the instance serves.
func (s *Server) served(values map[uint32]any) map[uint32]any {
	out := make(map[uint32]any, len(values))
	for id, v := range values {
		if s.Serves(id) {
			out[id] = v
		}
	}
	return out
}

// publish stores the served attributes among values; the generated server
// reports the ones that changed. Every value is one the model admits, so
// it cannot fail.
func (s *Server) publish(values map[uint32]any) {
	_ = s.srv.SetAttributes(s.served(values))
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *Server) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.srv.OnMatterAttributesChanged(cb)
}

// MatterRead resolves an attribute.
func (s *Server) MatterRead(attrID uint32) (any, bool) { return s.srv.MatterRead(attrID) }

// MatterWrite applies a CurrentSensitivityLevel write, the one writable
// attribute; the definition answers every other write.
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches an accepted command; the generated server
// answers any other with UNSUPPORTED_COMMAND — SuppressAlarm without SPRS
// and EnableDisableAlarm without VIS or AUD, as chip's AcceptedCommands
// (.cpp:119-134) and SuppressAlarms (.cpp:405-407) answer them.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return s.srv.MatterInvoke(ctx, cmdID, fields)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *Server) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.srv.SetMatterEventEmitter(emitter)
}

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it at reassembly.
func (s *Server) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
}

// State is a snapshot of the changing state.
type State struct {
	CurrentSensitivityLevel uint8
	AlarmsActive            AlarmMode
	AlarmsSuppressed        AlarmMode
	AlarmsEnabled           AlarmMode
	SensorFault             SensorFault
}

// State returns the current state.
func (s *Server) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return State{
		CurrentSensitivityLevel: s.current,
		AlarmsActive:            s.active,
		AlarmsSuppressed:        s.suppressed,
		AlarmsEnabled:           s.enabled,
		SensorFault:             s.fault,
	}
}

// statusErr answers a command or write with an IM status.
func statusErr(status im.StatusCode, format string, args ...any) error {
	return spec.Errorf(status, "BooleanStateConfiguration: "+format, args...)
}

// hasAlarms reports VIS or AUD.
func (s *Server) hasAlarms() bool { return s.HasFeature("VIS") || s.HasFeature("AUD") }

// sensitivitySink carries out a controller's CurrentSensitivityLevel write
// after the definition's checks: chip's SetCurrentSensitivityLevel
// (.cpp:321-337). The definition's bound "max supportedSensitivityLevels -
// 1" is an expression the generated checks do not evaluate, so it is
// held here.
type sensitivitySink struct{ s *Server }

// MatterWriteAttribute implements [spec.Sink].
func (k sensitivitySink) MatterWriteAttribute(ctx context.Context, _ uint32, value any) error {
	level, _ := value.(uint8)
	s := k.s
	if level >= s.supported { // .cpp:324
		return statusErr(im.StatusConstraintError, "CurrentSensitivityLevel %d ≥ SupportedSensitivityLevels %d", level, s.supported)
	}
	s.mu.Lock()
	same := s.current == level
	s.mu.Unlock()
	if same { // .cpp:325: no change is a success without a delegate call
		return nil
	}
	if s.delegate != nil { // .cpp:327-330
		if err := s.delegate.CurrentSensitivityLevelChanged(ctx, level); err != nil {
			return statusErr(im.StatusFailure, "CurrentSensitivityLevel refused: %v", err)
		}
	}
	s.mu.Lock()
	s.current = level
	s.mu.Unlock()
	return nil
}

// SetCurrentSensitivityLevel records a level the device changed itself;
// it is refused at or above SupportedSensitivityLevels (.cpp:324).
func (s *Server) SetCurrentSensitivityLevel(level uint8) error {
	if !s.HasFeature("SENSLVL") {
		return fmt.Errorf("%w: no SENSLVL", ErrInvalidConfig)
	}
	if level >= s.supported {
		return fmt.Errorf("%w: CurrentSensitivityLevel %d ≥ %d", ErrInvalidConfig, level, s.supported)
	}
	s.mu.Lock()
	s.current = level
	s.mu.Unlock()
	s.publish(map[uint32]any{def.AttrCurrentSensitivityLevel: level})
	return nil
}

// enableDisableAlarm carries out EnableDisableAlarm (.cpp:148-218): the
// request must name supported modes only (CONSTRAINT_ERROR otherwise);
// AlarmsEnabled becomes the request; every known mode the request leaves
// out is cleared from AlarmsActive and AlarmsSuppressed, and a cleared bit
// emits AlarmsStateChanged.
func (s *Server) enableDisableAlarm(ctx context.Context, fields any) (any, error) {
	req, ok := fields.(def.EnableDisableAlarmRequest)
	if !ok {
		return nil, statusErr(im.StatusInvalidCommand, "EnableDisableAlarm payload %T", fields)
	}
	alarms := req.AlarmsToEnableDisable
	if alarms&^s.alarmsSup != 0 { // .cpp:154
		return nil, statusErr(im.StatusConstraintError, "EnableDisableAlarm 0x%02X outside AlarmsSupported 0x%02X", alarms, s.alarmsSup)
	}
	s.mu.Lock()
	enabled, active, suppressed := s.enabled, s.active, s.suppressed
	s.mu.Unlock()

	if enabled != alarms { // .cpp:156-173
		if s.delegate != nil {
			if err := s.delegate.AlarmsEnabledChanged(ctx, alarms); err != nil {
				return nil, statusErr(im.StatusFailure, "AlarmsEnabled refused: %v", err)
			}
		}
		s.mu.Lock()
		s.enabled = alarms
		s.mu.Unlock()
		s.publish(map[uint32]any{def.AttrAlarmsEnabled: alarms})
	}

	disable := ^alarms & allKnownAlarmModes // .cpp:182-185
	event := false
	if active&disable != 0 { // .cpp:188-199
		next := active &^ disable
		if s.delegate != nil {
			if err := s.delegate.AlarmsActiveChanged(ctx, next); err != nil {
				return nil, statusErr(im.StatusFailure, "AlarmsActive refused: %v", err)
			}
		}
		s.mu.Lock()
		s.active = next
		s.mu.Unlock()
		s.publish(map[uint32]any{def.AttrAlarmsActive: next})
		event = true
	}
	if suppressed&disable != 0 { // .cpp:200-211
		next := suppressed &^ disable
		if s.delegate != nil {
			if err := s.delegate.AlarmsSuppressedChanged(ctx, next); err != nil {
				return nil, statusErr(im.StatusFailure, "AlarmsSuppressed refused: %v", err)
			}
		}
		s.mu.Lock()
		s.suppressed = next
		s.mu.Unlock()
		s.publish(map[uint32]any{def.AttrAlarmsSuppressed: next})
		event = true
	}
	if event { // .cpp:213-216
		s.alarmsStateChanged()
	}
	return nil, nil
}

// suppressAlarm carries out SuppressAlarm (.cpp:403-429): the request must
// name supported modes (CONSTRAINT_ERROR) that are active
// (INVALID_IN_STATE); suppressing what is already suppressed is a success
// that changes nothing; otherwise the modes join AlarmsSuppressed and
// AlarmsStateChanged is emitted.
func (s *Server) suppressAlarm(ctx context.Context, fields any) (any, error) {
	req, ok := fields.(def.SuppressAlarmRequest)
	if !ok {
		return nil, statusErr(im.StatusInvalidCommand, "SuppressAlarm payload %T", fields)
	}
	alarms := req.AlarmsToSuppress
	s.mu.Lock()
	active, suppressed := s.active, s.suppressed
	s.mu.Unlock()
	switch {
	case alarms&^s.alarmsSup != 0: // .cpp:410
		return nil, statusErr(im.StatusConstraintError, "SuppressAlarm 0x%02X outside AlarmsSupported 0x%02X", alarms, s.alarmsSup)
	case alarms&^active != 0: // .cpp:411
		return nil, statusErr(im.StatusInvalidInState, "SuppressAlarm 0x%02X is not active (0x%02X)", alarms, active)
	case suppressed&alarms == alarms: // .cpp:414
		return nil, nil
	}
	if s.delegate != nil { // .cpp:418-424
		if err := s.delegate.AlarmsSuppressedChanged(ctx, alarms); err != nil {
			return nil, statusErr(im.StatusFailure, "AlarmsSuppressed refused: %v", err)
		}
	}
	next := suppressed | alarms
	s.mu.Lock()
	s.suppressed = next
	s.mu.Unlock()
	s.publish(map[uint32]any{def.AttrAlarmsSuppressed: next})
	s.alarmsStateChanged()
	return nil, nil
}

// Device-side errors of the setters.
var (
	// ErrNoAlarms: the server has neither VIS nor AUD.
	ErrNoAlarms = errors.New("boolcfg: no VIS or AUD feature")
	// ErrNotEnabled: an alarm mode that is not enabled cannot go active.
	ErrNotEnabled = errors.New("boolcfg: alarm mode not enabled")
)

// SetAlarmsActive records the alarms the device raised; each must be
// enabled (chip SetAlarmsActive, .cpp:339-357). A change emits
// AlarmsStateChanged.
func (s *Server) SetAlarmsActive(alarms AlarmMode) error {
	if !s.hasAlarms() { // .cpp:341
		return ErrNoAlarms
	}
	s.mu.Lock()
	if alarms&^s.enabled != 0 { // .cpp:342
		s.mu.Unlock()
		return fmt.Errorf("%w: 0x%02X (enabled 0x%02X)", ErrNotEnabled, alarms, s.enabled)
	}
	same := s.active == alarms
	s.active = alarms
	s.mu.Unlock()
	if same { // .cpp:345
		return nil
	}
	s.publish(map[uint32]any{def.AttrAlarmsActive: alarms})
	s.alarmsStateChanged()
	return nil
}

// SetAllEnabledAlarmsActive raises every enabled alarm (chip
// SetAllEnabledAlarmsActive, .cpp:359-375).
func (s *Server) SetAllEnabledAlarmsActive() error {
	s.mu.Lock()
	enabled := s.enabled
	s.mu.Unlock()
	return s.SetAlarmsActive(enabled)
}

// ClearAllAlarms clears AlarmsActive and AlarmsSuppressed and emits
// AlarmsStateChanged when either held a bit (chip ClearAllAlarms,
// .cpp:377-401).
func (s *Server) ClearAllAlarms() {
	s.mu.Lock()
	if s.active == 0 && s.suppressed == 0 { // .cpp:379
		s.mu.Unlock()
		return
	}
	s.active, s.suppressed = 0, 0
	s.mu.Unlock()
	s.publish(map[uint32]any{def.AttrAlarmsActive: AlarmMode(0), def.AttrAlarmsSuppressed: AlarmMode(0)})
	s.alarmsStateChanged()
}

// SetSensorFault records a sensor fault (chip GenerateSensorFault,
// .cpp:302-319): the SensorFault event is emitted on every call when the
// event is in the EventList, and the SensorFault attribute, when served,
// changes with it.
func (s *Server) SetSensorFault(fault SensorFault) {
	s.mu.Lock()
	s.fault = fault
	endpoint := s.endpoint
	s.mu.Unlock()
	if s.Emits(def.EventSensorFault) {
		_ = s.srv.Emit(endpoint, def.EventSensorFault, def.SensorFaultEvent{SensorFault: fault})
	}
	s.publish(map[uint32]any{def.AttrSensorFault: fault})
}

// alarmsStateChanged emits AlarmsStateChanged with the current state
// (chip GenerateAlarmsStateChangedEvent, .cpp:287-300): AlarmsSuppressed
// is carried only with SPRS. The event is in the EventList with VIS or
// AUD, the only case anything calls this in.
func (s *Server) alarmsStateChanged() {
	s.mu.Lock()
	ev := def.AlarmsStateChangedEvent{AlarmsActive: s.active}
	if s.HasFeature("SPRS") {
		suppressed := s.suppressed
		ev.AlarmsSuppressed = &suppressed
	}
	endpoint := s.endpoint
	s.mu.Unlock()
	_ = s.srv.Emit(endpoint, def.EventAlarmsStateChanged, ev)
}
