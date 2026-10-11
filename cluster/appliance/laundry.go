// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package appliance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ldc "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols"
	lwc "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// Cluster ids.
const (
	ClusterIDLaundryWasherControls = lwc.ClusterID
	ClusterIDLaundryDryerControls  = ldc.ClusterID
)

// LaundryWasherControls features.
const (
	WasherFeatureSpin  = uint32(lwc.FeatureSpin)  // SPIN
	WasherFeatureRinse = uint32(lwc.FeatureRinse) // RINSE
)

// NumberOfRinses is the NumberOfRinsesEnum.
type NumberOfRinses = lwc.NumberOfRinsesEnum

// DrynessLevel is the DrynessLevelEnum.
type DrynessLevel = ldc.DrynessLevelEnum

// ErrLaundryValue reports a configured or device-set value outside the cluster's
// model.
var ErrLaundryValue = errors.New("appliance: laundry value outside the cluster's model")

// WasherListener is told about a controller's write that changed a
// LaundryWasherControls attribute, after it is stored — chip's
// OnSpinSpeedCurrentChanged / OnNumberOfRinsesChanged
// (laundry-washer-controls-delegate.h; LaundryWasherControlsCluster.cpp:92-94,
// :108-110). spin is nil for null.
type WasherListener interface {
	SpinSpeedCurrentChanged(spin *uint8)
	NumberOfRinsesChanged(rinses NumberOfRinses)
}

// WasherConfig carries the construction parameters.
type WasherConfig struct {
	// Features: WasherFeatureSpin, WasherFeatureRinse, or both.
	Features uint32
	// SpinSpeeds (SPIN) names the spin speeds: at most 16, each at most
	// 64 characters.
	SpinSpeeds []string
	// SpinSpeedCurrent (SPIN) is the initial index into SpinSpeeds; nil
	// is null.
	SpinSpeedCurrent *uint8
	// SupportedRinses (RINSE) lists the rinse counts the device offers,
	// at most 4.
	SupportedRinses []NumberOfRinses
	// NumberOfRinses (RINSE) is the initial rinse count, one of
	// SupportedRinses.
	NumberOfRinses NumberOfRinses
	// Listener, optional, is told about a controller's changes.
	Listener WasherListener
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// WasherServer implements [contract.ClusterServer] for
// LaundryWasherControls.
type WasherServer struct {
	*spec.Instance

	srv      *spec.Server
	speeds   []string
	rinses   []NumberOfRinses
	listener WasherListener
}

// DryerListener is told about a controller's write that changed
// SelectedDrynessLevel, after it is stored, so the host can persist it as
// chip does (LaundryDryerControlsCluster.cpp:61-70); level is nil for
// null.
type DryerListener interface {
	SelectedDrynessLevelChanged(level *DrynessLevel)
}

// DryerConfig carries the construction parameters.
type DryerConfig struct {
	// SupportedDrynessLevels lists the levels the device offers, 1 to 4.
	SupportedDrynessLevels []DrynessLevel
	// SelectedDrynessLevel is the initial level, one of
	// SupportedDrynessLevels; nil is null.
	SelectedDrynessLevel *DrynessLevel
	// Listener, optional, is told about a controller's changes.
	Listener DryerListener
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// DryerServer implements [contract.ClusterServer] for LaundryDryerControls.
type DryerServer struct {
	*spec.Instance

	srv      *spec.Server
	levels   []DrynessLevel
	listener DryerListener
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*WasherServer)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*WasherServer)(nil)
	_ contract.AttributeChangeNotifier        = (*WasherServer)(nil)
	_ contract.ClusterDataVersion             = (*WasherServer)(nil)
	_ contract.ClusterServer                  = (*DryerServer)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*DryerServer)(nil)
	_ contract.AttributeChangeNotifier        = (*DryerServer)(nil)
	_ contract.ClusterDataVersion             = (*DryerServer)(nil)
)

// NewLaundryWasherControls builds a LaundryWasherControls server.
func NewLaundryWasherControls(cfg WasherConfig) (*WasherServer, error) {
	s := &WasherServer{speeds: slices.Clone(cfg.SpinSpeeds), rinses: slices.Clone(cfg.SupportedRinses), listener: cfg.Listener}
	srv, err := spec.NewServer(lwc.Definition, spec.Options{Features: cfg.Features},
		spec.ServerConfig{DataVersion: cfg.DataVersion, Sink: washerRules{s}})
	if err != nil {
		return nil, fmt.Errorf("laundrywashercontrols: %w", err)
	}
	s.Instance, s.srv = srv.Instance, srv
	values := map[uint32]any{}
	if s.HasFeature("SPIN") {
		values[lwc.AttrSpinSpeeds] = slices.Clone(s.speeds)
		values[lwc.AttrSpinSpeedCurrent] = nullable(cfg.SpinSpeedCurrent)
	}
	if s.HasFeature("RINSE") {
		values[lwc.AttrSupportedRinses] = uintList[NumberOfRinses](slices.Clone(s.rinses))
		values[lwc.AttrNumberOfRinses] = cfg.NumberOfRinses
	}
	if err := srv.SetAttributes(values); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLaundryValue, err)
	}
	if s.HasFeature("SPIN") {
		if err := s.checkSpin(cfg.SpinSpeedCurrent); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrLaundryValue, err)
		}
	}
	if s.HasFeature("RINSE") {
		if err := s.checkRinses(cfg.NumberOfRinses); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrLaundryValue, err)
		}
	}
	return s, nil
}

// checkSpin is chip's SpinSpeedIndexValidity: null, or an index of a spin
// speed — CONSTRAINT_ERROR otherwise (LaundryWasherControlsCluster.cpp:115-130).
func (s *WasherServer) checkSpin(spin *uint8) error {
	if spin != nil && int(*spin) >= len(s.speeds) {
		return spec.Errorf(im.StatusConstraintError, "laundrywashercontrols: SpinSpeedCurrent %d, %d spin speeds", *spin, len(s.speeds))
	}
	return nil
}

// checkRinses is chip's NumberOfRinsesValidity: one of SupportedRinses —
// INVALID_IN_STATE otherwise (LaundryWasherControlsCluster.cpp:132-145).
func (s *WasherServer) checkRinses(r NumberOfRinses) error {
	if !slices.Contains(s.rinses, r) {
		return spec.Errorf(im.StatusInvalidInState, "laundrywashercontrols: NumberOfRinses %d not supported", r)
	}
	return nil
}

// washerRules holds a controller's write to chip's rules after the
// definition's checks (spec.Sink).
type washerRules struct{ s *WasherServer }

func (w washerRules) MatterWriteAttribute(_ context.Context, attrID uint32, value any) error {
	switch attrID {
	case lwc.AttrSpinSpeedCurrent:
		var spin *uint8
		if n, ok := value.(uint8); ok {
			spin = &n
		}
		return w.s.checkSpin(spin)
	case lwc.AttrNumberOfRinses:
		n, _ := value.(uint8)
		return w.s.checkRinses(NumberOfRinses(n))
	}
	return nil
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *WasherServer) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *WasherServer) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.srv.OnMatterAttributesChanged(cb)
}

// MatterRead resolves an attribute.
func (s *WasherServer) MatterRead(attrID uint32) (any, bool) { return s.srv.MatterRead(attrID) }

// MatterWrite applies a controller's SpinSpeedCurrent or NumberOfRinses
// write: the definition's checks, then chip's, then the listener hears of
// a change (LaundryWasherControlsCluster.cpp:49-67, :83-113).
func (s *WasherServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	before, _ := s.srv.Value(attrID)
	if err := s.srv.MatterWrite(ctx, attrID, value); err != nil {
		return err
	}
	after, _ := s.srv.Value(attrID)
	if s.listener == nil || reflect.DeepEqual(before, after) {
		return nil
	}
	switch attrID {
	case lwc.AttrSpinSpeedCurrent:
		s.listener.SpinSpeedCurrentChanged(s.SpinSpeedCurrent())
	case lwc.AttrNumberOfRinses:
		s.listener.NumberOfRinsesChanged(s.NumberOfRinses())
	}
	return nil
}

// MatterInvoke answers every command: the cluster has none.
func (s *WasherServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return s.srv.MatterInvoke(ctx, cmdID, fields)
}

// SpinSpeedCurrent returns the current spin speed index; nil for null or
// without SPIN.
func (s *WasherServer) SpinSpeedCurrent() *uint8 {
	v, _ := s.srv.Value(lwc.AttrSpinSpeedCurrent)
	if n, ok := v.(uint8); ok {
		return &n
	}
	return nil
}

// NumberOfRinses returns the current rinse count.
func (s *WasherServer) NumberOfRinses() NumberOfRinses {
	v, _ := s.srv.Value(lwc.AttrNumberOfRinses)
	n, _ := v.(uint8)
	return NumberOfRinses(n)
}

// SetSpinSpeedCurrent records a spin speed the device took (SPIN), checked
// as a controller's write is.
func (s *WasherServer) SetSpinSpeedCurrent(spin *uint8) error {
	if !s.HasFeature("SPIN") {
		return spec.Errorf(im.StatusUnsupportedAttribute, "laundrywashercontrols: SpinSpeedCurrent needs SPIN")
	}
	if err := s.checkSpin(spin); err != nil {
		return err
	}
	return s.srv.Set(lwc.AttrSpinSpeedCurrent, nullable(spin))
}

// SetNumberOfRinses records a rinse count the device took (RINSE), checked
// as a controller's write is.
func (s *WasherServer) SetNumberOfRinses(r NumberOfRinses) error {
	if !s.HasFeature("RINSE") {
		return spec.Errorf(im.StatusUnsupportedAttribute, "laundrywashercontrols: NumberOfRinses needs RINSE")
	}
	if err := s.checkRinses(r); err != nil {
		return err
	}
	return s.srv.Set(lwc.AttrNumberOfRinses, r)
}

// NewLaundryDryerControls builds a LaundryDryerControls server.
func NewLaundryDryerControls(cfg DryerConfig) (*DryerServer, error) {
	s := &DryerServer{levels: slices.Clone(cfg.SupportedDrynessLevels), listener: cfg.Listener}
	srv, err := spec.NewServer(ldc.Definition, spec.Options{},
		spec.ServerConfig{DataVersion: cfg.DataVersion, Sink: dryerRules{s}})
	if err != nil {
		return nil, fmt.Errorf("laundrydryercontrols: %w", err)
	}
	s.Instance, s.srv = srv.Instance, srv
	var level any
	if cfg.SelectedDrynessLevel != nil {
		level = *cfg.SelectedDrynessLevel
	}
	if err := srv.SetAttributes(map[uint32]any{
		ldc.AttrSupportedDrynessLevels: uintList[DrynessLevel](slices.Clone(s.levels)),
		ldc.AttrSelectedDrynessLevel:   level,
	}); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLaundryValue, err)
	}
	if err := s.checkLevel(cfg.SelectedDrynessLevel); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLaundryValue, err)
	}
	return s, nil
}

// checkLevel is chip's SetSelectedDrynessLevel rule: null, or one of
// SupportedDrynessLevels — CONSTRAINT_ERROR otherwise
// (LaundryDryerControlsCluster.cpp:47-53).
func (s *DryerServer) checkLevel(level *DrynessLevel) error {
	if level != nil && !slices.Contains(s.levels, *level) {
		return spec.Errorf(im.StatusConstraintError, "laundrydryercontrols: SelectedDrynessLevel %d not supported", *level)
	}
	return nil
}

// dryerRules holds a controller's write to chip's rule (spec.Sink).
type dryerRules struct{ s *DryerServer }

func (d dryerRules) MatterWriteAttribute(_ context.Context, _ uint32, value any) error {
	var level *DrynessLevel
	if n, ok := value.(uint8); ok {
		l := DrynessLevel(n)
		level = &l
	}
	return d.s.checkLevel(level)
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *DryerServer) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *DryerServer) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.srv.OnMatterAttributesChanged(cb)
}

// MatterRead resolves an attribute.
func (s *DryerServer) MatterRead(attrID uint32) (any, bool) { return s.srv.MatterRead(attrID) }

// MatterWrite applies a controller's SelectedDrynessLevel write: the
// definition's checks, then chip's, then the listener hears of a change
// (LaundryDryerControlsCluster.cpp:115-128).
func (s *DryerServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	before, _ := s.srv.Value(attrID)
	if err := s.srv.MatterWrite(ctx, attrID, value); err != nil {
		return err
	}
	after, _ := s.srv.Value(attrID)
	if s.listener != nil && !reflect.DeepEqual(before, after) {
		s.listener.SelectedDrynessLevelChanged(s.SelectedDrynessLevel())
	}
	return nil
}

// MatterInvoke answers every command: the cluster has none.
func (s *DryerServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return s.srv.MatterInvoke(ctx, cmdID, fields)
}

// SelectedDrynessLevel returns the selected level; nil for null.
func (s *DryerServer) SelectedDrynessLevel() *DrynessLevel {
	v, _ := s.srv.Value(ldc.AttrSelectedDrynessLevel)
	if n, ok := v.(uint8); ok {
		l := DrynessLevel(n)
		return &l
	}
	return nil
}

// SetSelectedDrynessLevel records a level the device took, checked as a
// controller's write is.
func (s *DryerServer) SetSelectedDrynessLevel(level *DrynessLevel) error {
	if err := s.checkLevel(level); err != nil {
		return err
	}
	var v any
	if level != nil {
		v = *level
	}
	return s.srv.Set(ldc.AttrSelectedDrynessLevel, v)
}

// nullable turns an optional uint8 into a stored value, nil for null.
func nullable(p *uint8) any {
	if p == nil {
		return nil
	}
	return *p
}
