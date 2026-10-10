// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tc "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TemperatureControl (0x0056) is built on its generated definition
// (cluster/spec/temperaturecontrol; ADR 0013): the ids, the
// feature-dependent attribute list, FeatureMap, ClusterRevision, the
// feature selection check (TN or TL, exactly one; STEP only with TN), the
// privileges and the write answers come from it. matter.js's
// TemperatureControlServer (packages/node/src/behaviors/temperature-control/
// TemperatureControlServer.ts) adds nothing to the generated behavior, and
// SetTemperature is the host's. The rules this server adds are those of
// Matter Application Cluster Specification 1.6.1 §8.8 TemperatureControl
// as connectedhomeip's server implements them — the authority for a
// cluster matter.js has no server logic for
// (src/app/clusters/temperature-control-server/TemperatureControlCluster.cpp
// at the harness pin 6170af8461b10b1766044122ac83332c6d00ab20):
//
//   - only the field of the server's own feature is read; the other one is
//     ignored (HandleSetTemperature :174-217 reads TargetTemperature under
//     TN, TargetTemperatureLevel under TL);
//   - the own field missing is INVALID_COMMAND (:176, :196);
//   - a TargetTemperature outside MinTemperature..MaxTemperature, or with
//     STEP not on a step from MinTemperature, is CONSTRAINT_ERROR
//     (SetTemperatureSetpoint :119-125);
//   - a TargetTemperatureLevel not below the length of
//     SupportedTemperatureLevels is CONSTRAINT_ERROR (:135);
//   - a change the device cannot accept now is INVALID_IN_STATE (:191,
//     :215) — the host says so with [ErrTemperatureRefused]; any other
//     host error is FAILURE;
//   - on success TemperatureSetpoint / SelectedTemperatureLevel become the
//     requested value (:127, :138);
//   - MaxTemperature need not lie on a Step from MinTemperature: the
//     constructor checks Step only against 1..Max-Min (:48-51).

// ClusterIDTemperatureControl is the TemperatureControl cluster id, from the
// generated definition.
const ClusterIDTemperatureControl = tc.ClusterID

// TemperatureControlFeature is a TemperatureControl FeatureMap bit.
type TemperatureControlFeature = tc.Feature

// TemperatureControl features.
const (
	TemperatureControlFeatureNumber = tc.FeatureTemperatureNumber // TN
	TemperatureControlFeatureLevel  = tc.FeatureTemperatureLevel  // TL
	TemperatureControlFeatureStep   = tc.FeatureTemperatureStep   // STEP, with TN
)

// TemperatureSetter is the host port for SetTemperature. Exactly one of
// target (TN, in 0.01 °C) and level (TL, an index into
// SupportedTemperatureLevels) is set, already checked against the
// cluster's rules. A nil error makes the value the new
// TemperatureSetpoint or SelectedTemperatureLevel. An error wrapping
// [ErrTemperatureRefused] — the device cannot accept the change now —
// answers INVALID_IN_STATE; any other error answers FAILURE. Either way
// nothing changes.
type TemperatureSetter interface {
	SetTemperature(ctx context.Context, target *int16, level *uint8) error
}

// TemperatureControlConfig carries the construction parameters.
type TemperatureControlConfig struct {
	// Features: TemperatureControlFeatureNumber or
	// TemperatureControlFeatureLevel, exactly one; Step only with Number.
	Features TemperatureControlFeature
	// Setter carries out SetTemperature; required.
	Setter TemperatureSetter

	// MinTemperature and MaxTemperature (TN, fixed) bound the setpoint;
	// MinTemperature is at most MaxTemperature - 1.
	MinTemperature, MaxTemperature int16
	// Step (STEP, fixed) is 1 to MaxTemperature - MinTemperature.
	Step int16
	// TemperatureSetpoint (TN) is the initial setpoint, within Min..Max.
	TemperatureSetpoint int16

	// SupportedTemperatureLevels (TL) names the levels: at most 32, each
	// at most 16 characters.
	SupportedTemperatureLevels []string
	// SelectedTemperatureLevel (TL) is the initial level, an index into
	// SupportedTemperatureLevels and at most 31.
	SelectedTemperatureLevel uint8

	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// TemperatureControl configuration errors.
var (
	ErrNoTemperatureSetter     = errors.New("temperaturecontrol: a TemperatureSetter is required")
	ErrTemperatureControlValue = errors.New("temperaturecontrol: value outside the cluster's model")
)

// ErrTemperatureRefused is what a [TemperatureSetter] returns (or wraps)
// when the device cannot accept a temperature change at the time — "due
// to the design of a device it cannot accept a change in its temperature
// setting after it has begun operation". The server answers
// INVALID_IN_STATE, as connectedhomeip's HandleSetTemperature does
// (TemperatureControlCluster.cpp:191, :215).
var ErrTemperatureRefused = errors.New("temperaturecontrol: the device cannot accept the change now")

// TemperatureControlServer implements [contract.ClusterServer] for
// TemperatureControl. The lists, globals and privileges are the embedded
// [spec.Instance]'s.
type TemperatureControlServer struct {
	*spec.Instance
	cluster.AttributeChanges

	embedded cluster.DataVersionTracker
	ext      *cluster.DataVersionTracker
	cfg      TemperatureControlConfig

	mu       sync.Mutex
	setpoint int16
	level    uint8
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*TemperatureControlServer)(nil)
	_ contract.ClusterDataVersion             = (*TemperatureControlServer)(nil)
	_ contract.ClusterAttributeLister         = (*TemperatureControlServer)(nil)
	_ contract.ClusterCommandLister           = (*TemperatureControlServer)(nil)
	_ contract.ClusterEventLister             = (*TemperatureControlServer)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*TemperatureControlServer)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*TemperatureControlServer)(nil)
	_ contract.AttributeChangeNotifier        = (*TemperatureControlServer)(nil)
)

// NewTemperatureControl builds a TemperatureControl server.
func NewTemperatureControl(cfg TemperatureControlConfig) (*TemperatureControlServer, error) {
	if cfg.Setter == nil {
		return nil, ErrNoTemperatureSetter
	}
	inst, err := spec.New(tc.Definition, spec.Options{Features: uint32(cfg.Features)})
	if err != nil {
		return nil, fmt.Errorf("temperaturecontrol: %w", err)
	}
	s := &TemperatureControlServer{Instance: inst, ext: cfg.DataVersion, cfg: cfg}
	s.cfg.SupportedTemperatureLevels = slices.Clone(cfg.SupportedTemperatureLevels)
	if err := s.checkConfig(); err != nil {
		return nil, err
	}
	s.setpoint, s.level = cfg.TemperatureSetpoint, cfg.SelectedTemperatureLevel
	return s, nil
}

// checkConfig holds the fixed attributes and the initial state to the
// definition's constraints.
func (s *TemperatureControlServer) checkConfig() error {
	c := s.cfg
	if s.Serves(tc.AttrMinTemperature) {
		// MinTemperature "max maxTemperature - 1".
		if int32(c.MinTemperature) > int32(c.MaxTemperature)-1 {
			return fmt.Errorf("%w: MinTemperature %d, MaxTemperature %d", ErrTemperatureControlValue, c.MinTemperature, c.MaxTemperature)
		}
		if s.Serves(tc.AttrStep) {
			// Step "1 to maxTemperature - minTemperature"; MaxTemperature
			// need not be on a step, as chip's constructor does not ask it
			// to be (TemperatureControlCluster.cpp:48-51).
			if c.Step < 1 || int32(c.Step) > int32(c.MaxTemperature)-int32(c.MinTemperature) {
				return fmt.Errorf("%w: Step %d", ErrTemperatureControlValue, c.Step)
			}
		}
		if err := s.checkTarget(c.TemperatureSetpoint); err != nil {
			return fmt.Errorf("%w: TemperatureSetpoint %d", ErrTemperatureControlValue, c.TemperatureSetpoint)
		}
	}
	if s.Serves(tc.AttrSupportedTemperatureLevels) {
		list := s.Definition().Attribute(tc.AttrSupportedTemperatureLevels).Constraint
		if int64(len(c.SupportedTemperatureLevels)) > list.Max.Int {
			return fmt.Errorf("%w: %d temperature levels, at most %d", ErrTemperatureControlValue, len(c.SupportedTemperatureLevels), list.Max.Int)
		}
		for _, l := range c.SupportedTemperatureLevels {
			if int64(spec.StringLength(l)) > list.Entry.Max.Int {
				return fmt.Errorf("%w: temperature level %q longer than %d", ErrTemperatureControlValue, l, list.Entry.Max.Int)
			}
		}
		if err := s.checkLevel(c.SelectedTemperatureLevel); err != nil {
			return fmt.Errorf("%w: SelectedTemperatureLevel %d", ErrTemperatureControlValue, c.SelectedTemperatureLevel)
		}
	}
	return nil
}

// checkTarget is the TN rule: within Min..Max, and with STEP on a step
// from MinTemperature.
func (s *TemperatureControlServer) checkTarget(t int16) error {
	c := s.cfg
	if t < c.MinTemperature || t > c.MaxTemperature {
		return constraintError(fmt.Sprintf("temperaturecontrol: TargetTemperature %d outside %d..%d", t, c.MinTemperature, c.MaxTemperature))
	}
	if s.Serves(tc.AttrStep) && (int32(t)-int32(c.MinTemperature))%int32(c.Step) != 0 {
		return constraintError(fmt.Sprintf("temperaturecontrol: TargetTemperature %d not on a step of %d from %d", t, c.Step, c.MinTemperature))
	}
	return nil
}

// checkLevel is the TL rule: below the length of SupportedTemperatureLevels
// (and so within SelectedTemperatureLevel's "max 31").
func (s *TemperatureControlServer) checkLevel(l uint8) error {
	limit := s.Definition().Attribute(tc.AttrSelectedTemperatureLevel).Constraint.Max.Int
	if int(l) >= len(s.cfg.SupportedTemperatureLevels) || int64(l) > limit {
		return constraintError(fmt.Sprintf("temperaturecontrol: TargetTemperatureLevel %d, %d levels supported", l, len(s.cfg.SupportedTemperatureLevels)))
	}
	return nil
}

func (s *TemperatureControlServer) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *TemperatureControlServer) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterRead resolves an attribute.
func (s *TemperatureControlServer) MatterRead(attrID uint32) (any, bool) {
	if v, ok := s.ReadGlobal(attrID); ok {
		return v, true
	}
	if !s.Serves(attrID) {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch attrID {
	case tc.AttrTemperatureSetpoint:
		return s.setpoint, true
	case tc.AttrMinTemperature:
		return s.cfg.MinTemperature, true
	case tc.AttrMaxTemperature:
		return s.cfg.MaxTemperature, true
	case tc.AttrStep:
		return s.cfg.Step, true
	case tc.AttrSelectedTemperatureLevel:
		return s.level, true
	}
	// SupportedTemperatureLevels, the last attribute Serves admits.
	return slices.Clone(s.cfg.SupportedTemperatureLevels), true
}

// MatterWrite refuses every write: every attribute is read-only, and the
// definition answers each with its status.
func (s *TemperatureControlServer) MatterWrite(_ context.Context, attrID uint32, value any) error {
	_, err := s.ValidateWrite(attrID, value, nil)
	return err
}

// MatterInvoke carries out SetTemperature: the request is checked against
// the rules above, the host applies it, and the setpoint or level becomes
// the requested one. The command answers with a status only.
func (s *TemperatureControlServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if !s.Accepts(cmdID) {
		return nil, im.UnsupportedCommandf("temperaturecontrol: command 0x%02X is not supported", cmdID)
	}
	req, ok := fields.(tc.SetTemperatureRequest)
	if !ok {
		return nil, invalidCommand(fmt.Sprintf("temperaturecontrol: SetTemperature fields %T", fields))
	}
	number := s.HasFeature("TN")
	// Only the own feature's field is read; the other is ignored, as chip's
	// HandleSetTemperature does (TemperatureControlCluster.cpp:174-217).
	if number {
		req.TargetTemperatureLevel = nil
	} else {
		req.TargetTemperature = nil
	}
	switch {
	// A missing field of the own feature: INVALID_COMMAND, as chip
	// answers it (TemperatureControlCluster.cpp:176, :196).
	case number && req.TargetTemperature == nil:
		return nil, invalidCommand("temperaturecontrol: TargetTemperature missing")
	case !number && req.TargetTemperatureLevel == nil:
		return nil, invalidCommand("temperaturecontrol: TargetTemperatureLevel missing")
	}
	var err error
	if number {
		err = s.checkTarget(*req.TargetTemperature)
	} else {
		err = s.checkLevel(*req.TargetTemperatureLevel)
	}
	if err != nil {
		return nil, err
	}
	if err := s.cfg.Setter.SetTemperature(ctx, req.TargetTemperature, req.TargetTemperatureLevel); err != nil {
		if errors.Is(err, ErrTemperatureRefused) {
			// INVALID_IN_STATE, as chip answers a change the device
			// cannot accept (TemperatureControlCluster.cpp:191, :215).
			return nil, tcStatusError{im.StatusInvalidInState, fmt.Sprintf("temperaturecontrol: SetTemperature: %v", err)}
		}
		return nil, fmt.Errorf("temperaturecontrol: SetTemperature: %w", err)
	}
	// The requested value becomes the attribute's, as chip's
	// SetTemperatureSetpoint / SetSelectedTemperatureLevel set it
	// (TemperatureControlCluster.cpp:127, :138).
	if number {
		s.apply(tc.AttrTemperatureSetpoint, func() bool { return swap(&s.setpoint, *req.TargetTemperature) })
	} else {
		s.apply(tc.AttrSelectedTemperatureLevel, func() bool { return swap(&s.level, *req.TargetTemperatureLevel) })
	}
	return nil, nil
}

// SetTemperatureSetpoint records a setpoint the device took on its own
// (TN); it is refused outside the cluster's rules.
func (s *TemperatureControlServer) SetTemperatureSetpoint(t int16) error {
	if !s.Serves(tc.AttrTemperatureSetpoint) {
		return fmt.Errorf("%w: TemperatureSetpoint needs the TN feature", ErrTemperatureControlValue)
	}
	if err := s.checkTarget(t); err != nil {
		return fmt.Errorf("%w: %w", ErrTemperatureControlValue, err)
	}
	s.apply(tc.AttrTemperatureSetpoint, func() bool { return swap(&s.setpoint, t) })
	return nil
}

// SetSelectedTemperatureLevel records a level the device took on its own
// (TL); it is refused outside SupportedTemperatureLevels.
func (s *TemperatureControlServer) SetSelectedTemperatureLevel(l uint8) error {
	if !s.Serves(tc.AttrSelectedTemperatureLevel) {
		return fmt.Errorf("%w: SelectedTemperatureLevel needs the TL feature", ErrTemperatureControlValue)
	}
	if err := s.checkLevel(l); err != nil {
		return fmt.Errorf("%w: %w", ErrTemperatureControlValue, err)
	}
	s.apply(tc.AttrSelectedTemperatureLevel, func() bool { return swap(&s.level, l) })
	return nil
}

// apply runs change under the lock and reports attr when it changed.
func (s *TemperatureControlServer) apply(attr uint32, change func() bool) {
	s.mu.Lock()
	changed := change()
	s.mu.Unlock()
	if !changed {
		return
	}
	s.tracker().Bump()
	s.Notify(attr)
}

// swap stores v in *p and reports whether it differed.
func swap[T comparable](p *T, v T) bool {
	if *p == v {
		return false
	}
	*p = v
	return true
}

// tcStatusError carries an exact IM status to the dispatcher.
type tcStatusError struct {
	status im.StatusCode
	msg    string
}

func (e tcStatusError) Error() string                   { return e.msg }
func (e tcStatusError) MatterStatusCode() im.StatusCode { return e.status }

func constraintError(msg string) error { return tcStatusError{im.StatusConstraintError, msg} }
func invalidCommand(msg string) error  { return tcStatusError{im.StatusInvalidCommand, msg} }
