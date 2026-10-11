// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package appliance contains the servers of the appliance control
// clusters a device of the laundry, kitchen or oven families carries
// beside its OperationalState and ModeBase servers: MicrowaveOvenControl
// (0x005F), LaundryWasherControls (0x0053) and LaundryDryerControls
// (0x004A).
//
// Each is built on its generated definition
// (cluster/spec/microwaveovencontrol, cluster/spec/laundrywashercontrols,
// cluster/spec/laundrydryercontrols; ADR 0013): ids, enums, the
// feature-dependent attribute and command lists, FeatureMap,
// ClusterRevision, the privileges and the write checks come from it, and
// the generated server ([spec.Server]) stores the values, answers reads
// and writes and reports changes. matter.js adds no logic to the generated
// behaviors — MicrowaveOvenControlServer, LaundryWasherControlsServer and
// LaundryDryerControlsServer (packages/node/src/behaviors/<name>/
// <Name>Server.ts) are empty subclasses — so the rules these servers add
// are connectedhomeip's, at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20 (src/app/clusters/
// microwave-oven-control-server/, laundry-washer-controls-server/,
// laundry-dryer-controls-server/). Each rule cites its line.
package appliance

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	mwo "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// ClusterIDMicrowaveOvenControl is the MicrowaveOvenControl cluster id.
const ClusterIDMicrowaveOvenControl = mwo.ClusterID

// MicrowaveOvenControl features.
const (
	MicrowaveFeaturePowerAsNumber     = uint32(mwo.FeaturePowerAsNumber)     // PWRNUM
	MicrowaveFeaturePowerInWatts      = uint32(mwo.FeaturePowerInWatts)      // WATTS
	MicrowaveFeaturePowerNumberLimits = uint32(mwo.FeaturePowerNumberLimits) // PWRLMTS, with PWRNUM
)

// chip's defaults (MicrowaveOvenControlCluster.cpp:37-41): the CookTime a
// server starts with and a SetCookingParameters without CookTime asks for,
// and the power range of a PWRNUM server without PWRLMTS.
const (
	DefaultCookTime     uint32 = 30
	DefaultMinPower     uint8  = 10
	DefaultMaxPower     uint8  = 100
	DefaultPowerStep    uint8  = 10
	operationalStopped  uint8  = 0 // OperationalStateEnum Stopped
	operationalStateErr uint8  = 3 // OperationalStateEnum Error
)

// Oven is the host port of a MicrowaveOvenControl server: chip's
// IntegrationDelegate and AppDelegate together
// (microwave-oven-control-server/IntegrationDelegate.h, AppDelegate.h).
// The first four answer from the endpoint's OperationalState and
// MicrowaveOvenMode servers; the last two carry out a command.
type Oven interface {
	// OperationalState is the endpoint's current OperationalState.
	OperationalState() uint8
	// NormalMode is the MicrowaveOvenMode mode tagged Normal; false when
	// there is none (CodegenIntegration.cpp:44-47).
	NormalMode() (uint8, bool)
	// SupportedMode reports whether mode is a MicrowaveOvenMode mode.
	SupportedMode(mode uint8) bool
	// StartSupported reports whether the endpoint's OperationalState
	// accepts Start (CodegenIntegration.cpp:54-68).
	StartSupported() bool
	// SetCookingParameters applies checked cooking parameters: exactly one
	// of power (PWRNUM) and wattIndex (WATTS) is set. A nil error makes
	// cookTime the CookTime and the power or watt index the PowerSetting
	// or SelectedWattIndex; an error wrapping an im.StatusCodeError
	// answers that status, any other FAILURE.
	SetCookingParameters(ctx context.Context, mode uint8, cookTime uint32, start bool, power, wattIndex *uint8) error
	// ModifyCookTime applies AddMoreTime's new CookTime, with the same
	// error answers.
	ModifyCookTime(ctx context.Context, cookTime uint32) error
}

// MicrowaveConfig carries the construction parameters.
type MicrowaveConfig struct {
	// Features: MicrowaveFeaturePowerAsNumber (optionally with
	// MicrowaveFeaturePowerNumberLimits) or MicrowaveFeaturePowerInWatts.
	Features uint32
	// Oven is the host port; required.
	Oven Oven
	// AddMoreTime accepts the optional AddMoreTime command.
	AddMoreTime bool

	// MaxCookTime (fixed) bounds CookTime, 1 to 86400 s.
	MaxCookTime uint32
	// CookTime is the initial CookTime; zero starts at DefaultCookTime.
	CookTime uint32
	// PowerSetting (PWRNUM) is the initial power setting.
	PowerSetting uint8
	// MinPower, MaxPower and PowerStep (PWRLMTS, fixed) bound PowerSetting.
	MinPower, MaxPower, PowerStep uint8
	// SupportedWatts (WATTS, fixed) lists the power levels in W, 1 to 10.
	SupportedWatts []uint16
	// SelectedWattIndex (WATTS) is the initial index into SupportedWatts.
	SelectedWattIndex uint8
	// WattRating, when set, serves the optional WattRating (fixed).
	WattRating *uint16

	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// MicrowaveOvenControl configuration errors.
var (
	ErrNoOven         = errors.New("microwaveovencontrol: an Oven is required")
	ErrMicrowaveValue = errors.New("microwaveovencontrol: value outside the cluster's model")
)

// MicrowaveServer implements [contract.ClusterServer] for
// MicrowaveOvenControl.
type MicrowaveServer struct {
	*spec.Instance

	srv  *spec.Server
	oven Oven
	// op serialises a command's checks and its CookTime update.
	op sync.Mutex
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                 = (*MicrowaveServer)(nil)
	_ contract.ClusterDataVersion            = (*MicrowaveServer)(nil)
	_ contract.ClusterAttributeLister        = (*MicrowaveServer)(nil)
	_ contract.ClusterCommandLister          = (*MicrowaveServer)(nil)
	_ contract.ClusterCommandInvokePrivilege = (*MicrowaveServer)(nil)
	_ contract.AttributeChangeNotifier       = (*MicrowaveServer)(nil)
)

// NewMicrowaveOvenControl builds a MicrowaveOvenControl server.
func NewMicrowaveOvenControl(cfg MicrowaveConfig) (*MicrowaveServer, error) {
	if cfg.Oven == nil {
		return nil, ErrNoOven
	}
	opts := spec.Options{Features: cfg.Features}
	if cfg.AddMoreTime {
		opts.Commands = []uint32{mwo.CmdAddMoreTime}
	}
	if cfg.WattRating != nil {
		opts.Attributes = []uint32{mwo.AttrWattRating}
	}
	if cfg.Features&MicrowaveFeaturePowerInWatts != 0 {
		// SupportedWatts and SelectedWattIndex are "P, WATTS": provisional,
		// so optional to the conformance even with WATTS; chip serves both
		// with the feature (MicrowaveOvenControlCluster.cpp:153-154).
		opts.Attributes = append(opts.Attributes, mwo.AttrSupportedWatts, mwo.AttrSelectedWattIndex)
	}
	srv, err := spec.NewServer(mwo.Definition, opts, spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("microwaveovencontrol: %w", err)
	}
	s := &MicrowaveServer{Instance: srv.Instance, srv: srv, oven: cfg.Oven}
	cookTime := cfg.CookTime
	if cookTime == 0 {
		// mCookTimeSec(kDefaultCookTimeSec) (MicrowaveOvenControlCluster.cpp:58).
		cookTime = DefaultCookTime
	}
	if cookTime > cfg.MaxCookTime {
		return nil, fmt.Errorf("%w: CookTime %d above MaxCookTime %d", ErrMicrowaveValue, cookTime, cfg.MaxCookTime)
	}
	values := map[uint32]any{mwo.AttrMaxCookTime: cfg.MaxCookTime, mwo.AttrCookTime: cookTime}
	if s.HasFeature("PWRLMTS") {
		values[mwo.AttrMinPower], values[mwo.AttrMaxPower], values[mwo.AttrPowerStep] = cfg.MinPower, cfg.MaxPower, cfg.PowerStep
	}
	if s.HasFeature("WATTS") {
		// Startup refuses an empty list (MicrowaveOvenControlCluster.cpp:67-72).
		if len(cfg.SupportedWatts) == 0 || int(cfg.SelectedWattIndex) >= len(cfg.SupportedWatts) {
			return nil, fmt.Errorf("%w: %d supported watts, index %d", ErrMicrowaveValue, len(cfg.SupportedWatts), cfg.SelectedWattIndex)
		}
		values[mwo.AttrSupportedWatts] = uintList[uint16](append([]uint16(nil), cfg.SupportedWatts...))
		values[mwo.AttrSelectedWattIndex] = cfg.SelectedWattIndex
	}
	if cfg.WattRating != nil {
		values[mwo.AttrWattRating] = *cfg.WattRating
	}
	if err := srv.SetAttributes(values); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMicrowaveValue, err)
	}
	if s.HasFeature("PWRNUM") {
		if err := s.checkPower(cfg.PowerSetting); err != nil {
			return nil, fmt.Errorf("%w: PowerSetting: %w", ErrMicrowaveValue, err)
		}
		if err := srv.Set(mwo.AttrPowerSetting, cfg.PowerSetting); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrMicrowaveValue, err)
		}
	}
	srv.Handle(mwo.CmdSetCookingParameters, s.setCookingParameters)
	srv.Handle(mwo.CmdAddMoreTime, s.addMoreTime)
	return s, nil
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *MicrowaveServer) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *MicrowaveServer) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.srv.OnMatterAttributesChanged(cb)
}

// MatterRead resolves an attribute.
func (s *MicrowaveServer) MatterRead(attrID uint32) (any, bool) { return s.srv.MatterRead(attrID) }

// MatterWrite answers every write: every attribute is read-only.
func (s *MicrowaveServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches an accepted command.
func (s *MicrowaveServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return s.srv.MatterInvoke(ctx, cmdID, fields)
}

// CookTime returns the current CookTime.
func (s *MicrowaveServer) CookTime() uint32 { return s.uint32Value(mwo.AttrCookTime) }

// MaxCookTime returns the fixed MaxCookTime.
func (s *MicrowaveServer) MaxCookTime() uint32 { return s.uint32Value(mwo.AttrMaxCookTime) }

func (s *MicrowaveServer) uint32Value(id uint32) uint32 {
	v, _ := s.srv.Value(id)
	n, _ := v.(uint32)
	return n
}

func (s *MicrowaveServer) uint8Value(id uint32) uint8 {
	v, _ := s.srv.Value(id)
	n, _ := v.(uint8)
	return n
}

// SetCookTime records a CookTime the device changed: 1 to MaxCookTime,
// or CONSTRAINT_ERROR (SetCookTimeSec, MicrowaveOvenControlCluster.cpp:88-95).
func (s *MicrowaveServer) SetCookTime(seconds uint32) error {
	if seconds < 1 || seconds > s.MaxCookTime() {
		return spec.Errorf(im.StatusConstraintError, "microwaveovencontrol: CookTime %d outside 1..%d", seconds, s.MaxCookTime())
	}
	return s.srv.Set(mwo.AttrCookTime, seconds)
}

// SetPowerSetting records a PowerSetting the device changed (PWRNUM),
// checked as SetCookingParameters checks a requested one.
func (s *MicrowaveServer) SetPowerSetting(power uint8) error {
	if !s.HasFeature("PWRNUM") {
		return fmt.Errorf("%w: PowerSetting needs PWRNUM", ErrMicrowaveValue)
	}
	if err := s.checkPower(power); err != nil {
		return err
	}
	return s.srv.Set(mwo.AttrPowerSetting, power)
}

// SetSelectedWattIndex records a SelectedWattIndex the device changed
// (WATTS), an index into SupportedWatts.
func (s *MicrowaveServer) SetSelectedWattIndex(index uint8) error {
	if !s.HasFeature("WATTS") {
		return fmt.Errorf("%w: SelectedWattIndex needs WATTS", ErrMicrowaveValue)
	}
	if index > s.maxWattIndex() {
		return spec.Errorf(im.StatusConstraintError, "microwaveovencontrol: watt index %d above %d", index, s.maxWattIndex())
	}
	return s.srv.Set(mwo.AttrSelectedWattIndex, index)
}

// powerLimits are chip's: the defaults, or the PWRLMTS attributes
// (MicrowaveOvenControlCluster.cpp:235-249).
func (s *MicrowaveServer) powerLimits() (minPower, maxPower, step uint8) {
	if !s.HasFeature("PWRLMTS") {
		return DefaultMinPower, DefaultMaxPower, DefaultPowerStep
	}
	return s.uint8Value(mwo.AttrMinPower), s.uint8Value(mwo.AttrMaxPower), s.uint8Value(mwo.AttrPowerStep)
}

// checkPower is chip's power rule: within the limits and on a step from
// the minimum, CONSTRAINT_ERROR otherwise (:251-260).
func (s *MicrowaveServer) checkPower(p uint8) error {
	minPower, maxPower, step := s.powerLimits()
	if p < minPower || p > maxPower {
		return spec.Errorf(im.StatusConstraintError, "microwaveovencontrol: PowerSetting %d outside %d..%d", p, minPower, maxPower)
	}
	if step == 0 || (p-minPower)%step != 0 {
		return spec.Errorf(im.StatusConstraintError, "microwaveovencontrol: PowerSetting %d not on a step of %d from %d", p, step, minPower)
	}
	return nil
}

func (s *MicrowaveServer) maxWattIndex() uint8 {
	v, _ := s.srv.Value(mwo.AttrSupportedWatts)
	l, _ := v.(uintList[uint16])
	return uint8(len(l) - 1) //nolint:gosec // at most 10 entries, at least one (constructor)
}

// setCookingParameters is chip's HandleSetCookingParameters
// (MicrowaveOvenControlCluster.cpp:195-318).
func (s *MicrowaveServer) setCookingParameters(ctx context.Context, fields any) (any, error) {
	req, ok := fields.(mwo.SetCookingParametersRequest)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "microwaveovencontrol: SetCookingParameters fields %T", fields)
	}
	s.op.Lock()
	defer s.op.Unlock()
	// Only in Stopped (:206-207).
	if st := s.oven.OperationalState(); st != operationalStopped {
		return nil, spec.Errorf(im.StatusInvalidInState, "microwaveovencontrol: OperationalState %d is not Stopped", st)
	}
	// StartAfterSetting needs OperationalState's Start (:209-217).
	if req.StartAfterSetting != nil && !s.oven.StartSupported() {
		return nil, spec.Errorf(im.StatusInvalidCommand, "microwaveovencontrol: StartAfterSetting without OperationalState Start")
	}
	start := req.StartAfterSetting != nil && *req.StartAfterSetting
	// CookMode defaults to the Normal mode, which must exist (:220-226).
	normal, ok := s.oven.NormalMode()
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "microwaveovencontrol: no Normal mode")
	}
	mode := normal
	if req.CookMode != nil {
		mode = *req.CookMode
	}
	if !s.oven.SupportedMode(mode) {
		return nil, spec.Errorf(im.StatusConstraintError, "microwaveovencontrol: CookMode %d not supported", mode)
	}
	// CookTime defaults to 30 s and lies within 1..MaxCookTime (:228-230).
	cookTime := DefaultCookTime
	if req.CookTime != nil {
		cookTime = *req.CookTime
	}
	if cookTime < 1 || cookTime > s.MaxCookTime() {
		return nil, spec.Errorf(im.StatusConstraintError, "microwaveovencontrol: CookTime %d outside 1..%d", cookTime, s.MaxCookTime())
	}
	var power, watt *uint8
	values := map[uint32]any{mwo.AttrCookTime: cookTime}
	if s.HasFeature("PWRNUM") {
		// No WattSettingIndex (:239-242); PowerSetting defaults to the
		// maximum and is checked against the limits (:251-260).
		if req.WattSettingIndex != nil {
			return nil, spec.Errorf(im.StatusInvalidCommand, "microwaveovencontrol: WattSettingIndex with PWRNUM")
		}
		_, maxPower, _ := s.powerLimits()
		p := maxPower
		if req.PowerSetting != nil {
			p = *req.PowerSetting
		}
		if err := s.checkPower(p); err != nil {
			return nil, err
		}
		power = &p
		values[mwo.AttrPowerSetting] = p
	} else {
		// No PowerSetting (:278-280); WattSettingIndex defaults to the
		// last index and must not exceed it (:283-291).
		if req.PowerSetting != nil {
			return nil, spec.Errorf(im.StatusInvalidCommand, "microwaveovencontrol: PowerSetting with WATTS")
		}
		limit := s.maxWattIndex()
		w := limit
		if req.WattSettingIndex != nil {
			w = *req.WattSettingIndex
		}
		if w > limit {
			return nil, spec.Errorf(im.StatusConstraintError, "microwaveovencontrol: WattSettingIndex %d above %d", w, limit)
		}
		watt = &w
		values[mwo.AttrSelectedWattIndex] = w
	}
	if err := s.oven.SetCookingParameters(ctx, mode, cookTime, start, power, watt); err != nil {
		return nil, hostError("SetCookingParameters", err)
	}
	// On success CookTime becomes the requested one (:312-315).
	return nil, s.srv.SetAttributes(values)
}

// addMoreTime is chip's HandleAddMoreTime
// (MicrowaveOvenControlCluster.cpp:320-339).
func (s *MicrowaveServer) addMoreTime(ctx context.Context, fields any) (any, error) {
	req, ok := fields.(mwo.AddMoreTimeRequest)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "microwaveovencontrol: AddMoreTime fields %T", fields)
	}
	s.op.Lock()
	defer s.op.Unlock()
	// Not in Error (:323-324).
	if st := s.oven.OperationalState(); st == operationalStateErr {
		return nil, spec.Errorf(im.StatusInvalidInState, "microwaveovencontrol: OperationalState is Error")
	}
	// The sum may not pass MaxCookTime (:327-330).
	limit, current := s.MaxCookTime(), s.CookTime()
	if req.TimeToAdd > limit || current > limit-req.TimeToAdd {
		return nil, spec.Errorf(im.StatusConstraintError, "microwaveovencontrol: %d + %d above MaxCookTime %d", current, req.TimeToAdd, limit)
	}
	final := current + req.TimeToAdd
	if err := s.oven.ModifyCookTime(ctx, final); err != nil {
		return nil, hostError("AddMoreTime", err)
	}
	return nil, s.SetCookTime(final)
}

// hostError answers a host refusal: its own status when it carries one,
// FAILURE otherwise.
func hostError(cmd string, err error) error {
	var sce im.StatusCodeError
	if errors.As(err, &sce) {
		return spec.Errorf(sce.MatterStatusCode(), "%s: %v", cmd, err)
	}
	return spec.Errorf(im.StatusFailure, "%s: %v", cmd, err)
}

// uintList is a list of unsigned integers that encodes itself, each entry
// at its smallest width as matter.js's TlvUInt writes it.
type uintList[T spec.Unsigned] []T

// EncodeTLV implements [spec.Encodable].
func (l uintList[T]) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	spec.PutList(spec.PutUint[T])(enc, tag, []T(l))
}
