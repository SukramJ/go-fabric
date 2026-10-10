// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package closure

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	cd "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ClosureDimension (0x0105) is built on its generated definition
// (cluster/spec/closuredimension; ADR 0013): the ids, the structs and
// enums with their codecs, the feature selection check, the
// feature-dependent attribute and command lists, the privileges (SetTarget
// and Step are timed) and the write answers come from it. matter.js's
// ClosureDimensionServer (packages/node/src/behaviors/closure-dimension/
// ClosureDimensionServer.ts) is empty, so the rules are those of
// connectedhomeip's server — the authority for a cluster matter.js has no
// server logic for (src/app/clusters/closure-dimension-server/
// ClosureDimensionCluster.cpp at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20; line numbers below are that
// file's):
//
//   - SetTarget with no field is INVALID_COMMAND (:461); a field of a
//     feature the server lacks is ignored (:476, :510, :524);
//   - a Position above 100.00 % is CONSTRAINT_ERROR (:478); with LM it is
//     clamped into LimitRange (:482-494), then rounded to the nearest
//     multiple of Resolution (:496-505);
//   - a Latch the LatchControlModes do not allow remotely is
//     INVALID_IN_STATE (:514-518); an unknown Speed is CONSTRAINT_ERROR
//     (:526);
//   - a null CurrentState, or with PS a missing or null current Position,
//     is INVALID_IN_STATE (:531-537); a Position while latched, unless the
//     same command unlatches, is INVALID_IN_STATE (:541-552);
//   - Step: an unknown Direction or zero NumberOfSteps is CONSTRAINT_ERROR
//     (:566-567); a null CurrentState or Position, or a latched closure,
//     is INVALID_IN_STATE (:589-605); the new target is the current
//     Position moved by NumberOfSteps × StepValue, clamped to 0 / 100.00 %
//     or to LimitRange with LM (:607-653);
//   - the host's handler runs before TargetState changes, and its error is
//     FAILURE (:555, :656), as is a target TargetState refuses (:557,
//     :659);
//   - TargetState: a Position above 100.00 % or off the Resolution grid,
//     or a field of a feature the server lacks, is refused (:326-378);
//   - CurrentState (quality Q) is reported when it changes to or from
//     null, when Position changes to or from null, when Position reaches
//     the target's, when Latch or Speed changes, and otherwise at most once
//     every 5 s (:210-324);
//   - UnitRange: Min ≤ Max; with Millimeter both in 0..32767, with Degree
//     both in -360..360 and Max - Min ≤ 360 (:380-427); LimitRange: Min ≤
//     Max ≤ 100.00 %, both multiples of Resolution (:429-455).
//
// chip's constructor checks the feature selection (:53-78); the
// definition's check is matter.js's and is stricter in one place: with
// PS it requires exactly one of Translation, Rotation and Modulation
// ("[PS].b"), where chip requires at most one.

// ClusterIDClosureDimension is the ClosureDimension cluster id.
const ClusterIDClosureDimension = cd.ClusterID

// DeviceTypeClosurePanel is the ClosurePanel device type
// (closure-panel.element.ts, 0x0231), which mandates ClosureDimension.
const DeviceTypeClosurePanel uint16 = 0x0231

// The ClosureDimension datatypes, from the generated definition.
type (
	// DimensionFeature is a ClosureDimension FeatureMap bit.
	DimensionFeature = cd.Feature
	// DimensionState is the DimensionStateStruct of CurrentState and
	// TargetState.
	DimensionState = cd.DimensionStateStruct
	// UnitRange is the UnitRangeStruct.
	UnitRange = cd.UnitRangeStruct
	// LimitRange is the RangePercent100thsStruct of LimitRange.
	LimitRange = cd.RangePercent100thsStruct
	// ThreeLevelAuto is the ThreeLevelAutoEnum of a Speed.
	ThreeLevelAuto = cd.ThreeLevelAutoEnum
	// StepDirection is the StepDirectionEnum.
	StepDirection = cd.StepDirectionEnum
	// LatchControlModes is the LatchControlModesBitmap.
	LatchControlModes = cd.LatchControlModesBitmap
)

// ClosureDimension features.
const (
	DimensionFeaturePositioning    = cd.FeaturePositioning    // PS
	DimensionFeatureMotionLatching = cd.FeatureMotionLatching // LT
	DimensionFeatureUnit           = cd.FeatureUnit           // UT, with PS
	DimensionFeatureLimitation     = cd.FeatureLimitation     // LM, with PS
	DimensionFeatureSpeed          = cd.FeatureSpeed          // SP, with PS
	DimensionFeatureTranslation    = cd.FeatureTranslation    // TR, with PS
	DimensionFeatureRotation       = cd.FeatureRotation       // RO, with PS
	DimensionFeatureModulation     = cd.FeatureModulation     // MD, with PS
)

// Percent100thsMax is 100.00 % (chip kPercents100thsMaxValue, :41).
const Percent100thsMax uint16 = 10000

// positionQuietInterval is how often a Position change between two
// non-null values is reported at most (chip
// kPositionQuietReportingInterval, :42).
const positionQuietInterval = 5 * time.Second

// DimensionHandler carries out the commands on the device. It is called
// after the server's checks and before TargetState changes; an error
// answers FAILURE and leaves TargetState as it was.
type DimensionHandler interface {
	// SetTarget receives the request's fields as chip's delegate does:
	// Position after the LimitRange clamp and the Resolution rounding,
	// Latch and Speed as sent.
	SetTarget(ctx context.Context, position *uint16, latch *bool, speed *ThreeLevelAuto) error
	// Step receives the request's fields as sent.
	Step(ctx context.Context, direction StepDirection, numberOfSteps uint16, speed *ThreeLevelAuto) error
}

// DimensionConfig carries the construction parameters.
type DimensionConfig struct {
	// Features is the FeatureMap: PS, LT or both, the rest with PS.
	Features DimensionFeature
	// Handler carries out SetTarget and Step; required.
	Handler DimensionHandler

	// Resolution and StepValue (PS, fixed) are in 0.01 %, 1 to 10000.
	Resolution, StepValue uint16
	// Unit (UT, fixed) and UnitRange (UT, nullable: nil is null).
	Unit      cd.ClosureUnitEnum
	UnitRange *UnitRange
	// LimitRange (LM).
	LimitRange LimitRange
	// TranslationDirection (TR), RotationAxis and Overflow (RO),
	// ModulationType (MD), all fixed.
	TranslationDirection cd.TranslationDirectionEnum
	RotationAxis         cd.RotationAxisEnum
	Overflow             cd.OverflowEnum
	ModulationType       cd.ModulationTypeEnum
	// LatchControlModes (LT, fixed) says which latch changes a controller
	// may request.
	LatchControlModes LatchControlModes

	// CurrentState and TargetState are the initial states; nil is null.
	CurrentState, TargetState *DimensionState

	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
	// Now defaults to time.Now; tests set it.
	Now func() time.Time
}

// ClosureDimension errors.
var (
	ErrNoDimensionHandler = errors.New("closuredimension: a DimensionHandler is required")
	// ErrDimensionValue: a value chip's setters refuse (CHIP_ERROR_
	// INVALID_ARGUMENT or CHIP_ERROR_UNSUPPORTED_CHIP_FEATURE).
	ErrDimensionValue = errors.New("closuredimension: value outside the cluster's rules")
)

// DimensionServer implements [contract.ClusterServer] for
// ClosureDimension. The lists, globals and privileges are the embedded
// [spec.Instance]'s.
type DimensionServer struct {
	*spec.Instance
	cluster.AttributeChanges

	embedded cluster.DataVersionTracker
	ext      *cluster.DataVersionTracker
	cfg      DimensionConfig

	mu          sync.Mutex
	current     *DimensionState
	target      *DimensionState
	unitRange   *UnitRange
	limit       LimitRange
	lastPosDirt time.Time // the last reported Position change
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*DimensionServer)(nil)
	_ contract.ClusterDataVersion             = (*DimensionServer)(nil)
	_ contract.ClusterAttributeLister         = (*DimensionServer)(nil)
	_ contract.ClusterCommandLister           = (*DimensionServer)(nil)
	_ contract.ClusterEventLister             = (*DimensionServer)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*DimensionServer)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*DimensionServer)(nil)
	_ contract.AttributeChangeNotifier        = (*DimensionServer)(nil)
)

// NewDimension builds a ClosureDimension server.
func NewDimension(cfg DimensionConfig) (*DimensionServer, error) {
	if cfg.Handler == nil {
		return nil, ErrNoDimensionHandler
	}
	inst, err := spec.New(cd.Definition, spec.Options{Features: uint32(cfg.Features)})
	if err != nil {
		return nil, fmt.Errorf("closuredimension: %w", err)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	s := &DimensionServer{Instance: inst, ext: cfg.DataVersion, cfg: cfg}
	if err := s.checkFixed(); err != nil {
		return nil, err
	}
	if s.has(cd.FeatureUnit) {
		if err := s.checkUnitRange(cfg.UnitRange); err != nil {
			return nil, err
		}
		s.unitRange = clone(cfg.UnitRange)
	}
	if s.has(cd.FeatureLimitation) {
		if err := s.checkLimitRange(cfg.LimitRange); err != nil {
			return nil, err
		}
		s.limit = cfg.LimitRange
	}
	if cfg.CurrentState != nil {
		if err := s.checkState(*cfg.CurrentState, false); err != nil {
			return nil, err
		}
		s.current = clone(cfg.CurrentState)
	}
	if cfg.TargetState != nil {
		if err := s.checkState(*cfg.TargetState, true); err != nil {
			return nil, err
		}
		s.target = clone(cfg.TargetState)
	}
	return s, nil
}

func (s *DimensionServer) has(f DimensionFeature) bool { return s.FeatureMap()&uint32(f) != 0 }

func clone[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func dimInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrDimensionValue}, args...)...)
}

// checkFixed holds the fixed attributes to the definition: Resolution and
// StepValue "0.01% to 10000" (1 to 10000 in 0.01 %), the enums defined.
func (s *DimensionServer) checkFixed() error {
	c := s.cfg
	if s.has(cd.FeaturePositioning) {
		for name, v := range map[string]uint16{"Resolution": c.Resolution, "StepValue": c.StepValue} {
			if v < 1 || v > Percent100thsMax {
				return dimInvalid("%s %d outside 1..%d", name, v, Percent100thsMax)
			}
		}
	}
	enums := []struct {
		feature DimensionFeature
		def     *spec.Enum
		v       uint8
	}{
		{cd.FeatureUnit, cd.ClosureUnitEnumDef, uint8(c.Unit)},
		{cd.FeatureTranslation, cd.TranslationDirectionEnumDef, uint8(c.TranslationDirection)},
		{cd.FeatureRotation, cd.RotationAxisEnumDef, uint8(c.RotationAxis)},
		{cd.FeatureRotation, cd.OverflowEnumDef, uint8(c.Overflow)},
		{cd.FeatureModulation, cd.ModulationTypeEnumDef, uint8(c.ModulationType)},
	}
	for _, e := range enums {
		if s.has(e.feature) && !s.EnumSupported(e.def, uint64(e.v)) {
			return dimInvalid("%s %d", e.def.Name, e.v)
		}
	}
	if s.has(cd.FeatureMotionLatching) && c.LatchControlModes&^(cd.LatchControlModesRemoteLatching|cd.LatchControlModesRemoteUnlatching) != 0 {
		return dimInvalid("LatchControlModes 0x%02X", uint8(c.LatchControlModes))
	}
	return nil
}

// checkUnitRange is chip's SetUnitRange (:380-427).
func (s *DimensionServer) checkUnitRange(r *UnitRange) error {
	if r == nil {
		return nil
	}
	if r.Min > r.Max {
		return dimInvalid("UnitRange %d > %d", r.Min, r.Max)
	}
	switch s.cfg.Unit {
	case cd.ClosureUnitMillimeter:
		if r.Min < 0 || r.Max < 0 { // the upper bound 32767 is the int16's own
			return dimInvalid("UnitRange %d..%d is not 0..32767 mm", r.Min, r.Max)
		}
	case cd.ClosureUnitDegree:
		if r.Min < -360 || r.Max > 360 || int32(r.Max)-int32(r.Min) > 360 {
			return dimInvalid("UnitRange %d..%d is not a span within -360..360°", r.Min, r.Max)
		}
	}
	return nil
}

// checkLimitRange is chip's SetLimitRange (:429-455).
func (s *DimensionServer) checkLimitRange(r LimitRange) error {
	switch {
	case r.Min > r.Max || r.Max > Percent100thsMax:
		return dimInvalid("LimitRange %d..%d", r.Min, r.Max)
	case s.cfg.Resolution > 0 && (r.Min%s.cfg.Resolution != 0 || r.Max%s.cfg.Resolution != 0):
		return dimInvalid("LimitRange %d..%d is not on Resolution %d", r.Min, r.Max, s.cfg.Resolution)
	}
	return nil
}

// checkState is chip's validation in SetCurrentState (:231-310) and
// SetTargetState (:332-373): each field needs its feature, a Position is
// at most 100.00 % (and for a target on the Resolution grid), a Speed is
// a known value.
func (s *DimensionServer) checkState(st DimensionState, target bool) error {
	if st.Position != nil {
		if !s.has(cd.FeaturePositioning) {
			return dimInvalid("Position without Positioning")
		}
		if p := st.Position; !p.Null {
			if p.Value > Percent100thsMax {
				return dimInvalid("Position %d", p.Value)
			}
			if target && p.Value%s.cfg.Resolution != 0 {
				return dimInvalid("target Position %d is not on Resolution %d", p.Value, s.cfg.Resolution)
			}
		}
	}
	if st.Latch != nil && !s.has(cd.FeatureMotionLatching) {
		return dimInvalid("Latch without MotionLatching")
	}
	if st.Speed != nil {
		if !s.has(cd.FeatureSpeed) {
			return dimInvalid("Speed without Speed")
		}
		if !s.EnumSupported(cd.ThreeLevelAutoEnumDef, uint64(*st.Speed)) {
			return dimInvalid("Speed %d", *st.Speed)
		}
	}
	return nil
}

func (s *DimensionServer) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

func (s *DimensionServer) changed(attrIDs ...uint32) {
	s.tracker().Bump()
	s.Notify(attrIDs...)
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *DimensionServer) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterReportable lists the attributes whose change is reported at once:
// every served one but CurrentState, whose quality is Q and whose reports
// follow chip's rules (see [DimensionServer.SetCurrentState]).
func (s *DimensionServer) MatterReportable() []uint32 {
	out := make([]uint32, 0, 8)
	for _, id := range s.MatterAttributes() {
		if id != cd.AttrCurrentState {
			out = append(out, id)
		}
	}
	return out
}

// MatterRead resolves an attribute.
func (s *DimensionServer) MatterRead(attrID uint32) (any, bool) {
	if v, ok := s.ReadGlobal(attrID); ok {
		return v, true
	}
	if !s.Serves(attrID) {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Enums and bitmaps read as their uint8, the form the bridge's value
	// writer encodes.
	c := s.cfg
	switch attrID {
	case cd.AttrCurrentState:
		return stateValue(s.current), true
	case cd.AttrTargetState:
		return stateValue(s.target), true
	case cd.AttrResolution:
		return c.Resolution, true
	case cd.AttrStepValue:
		return c.StepValue, true
	case cd.AttrUnit:
		return uint8(c.Unit), true
	case cd.AttrUnitRange:
		if s.unitRange == nil {
			return nil, true
		}
		return *s.unitRange, true
	case cd.AttrLimitRange:
		return s.limit, true
	case cd.AttrTranslationDirection:
		return uint8(c.TranslationDirection), true
	case cd.AttrRotationAxis:
		return uint8(c.RotationAxis), true
	case cd.AttrOverflow:
		return uint8(c.Overflow), true
	case cd.AttrModulationType:
		return uint8(c.ModulationType), true
	}
	// LatchControlModes, the last attribute Serves admits.
	return uint8(c.LatchControlModes), true
}

// stateValue is a nullable DimensionState in its read form: nil for null.
func stateValue(p *DimensionState) any {
	if p == nil {
		return nil
	}
	return *p
}

// MatterWrite refuses every write: every attribute is read-only, and the
// definition answers each with its status.
func (s *DimensionServer) MatterWrite(_ context.Context, attrID uint32, value any) error {
	_, err := s.ValidateWrite(attrID, value, nil)
	return err
}

// MatterInvoke carries out SetTarget and Step; both answer with a status
// only.
func (s *DimensionServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if !s.Accepts(cmdID) {
		return nil, im.UnsupportedCommandf("closuredimension: command 0x%02X is not supported", cmdID)
	}
	if cmdID == cd.CmdSetTarget {
		req, ok := dimRequest[cd.SetTargetRequest](fields)
		if !ok {
			return nil, spec.Errorf(im.StatusInvalidCommand, "closuredimension: SetTarget fields %T", fields)
		}
		return nil, s.setTarget(ctx, req)
	}
	// Step, the other command Accepts admits.
	req, ok := dimRequest[cd.StepRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "closuredimension: Step fields %T", fields)
	}
	return nil, s.step(ctx, req)
}

// dimRequest takes a generated request by value or by pointer.
func dimRequest[T any](fields any) (T, bool) {
	switch f := fields.(type) {
	case *T:
		if f != nil {
			return *f, true
		}
	case T:
		return f, true
	}
	var zero T
	return zero, false
}

// setTarget is chip's HandleSetTargetCommand (:457-560).
func (s *DimensionServer) setTarget(ctx context.Context, req cd.SetTargetRequest) error {
	if req.Position == nil && req.Latch == nil && req.Speed == nil {
		return spec.Errorf(im.StatusInvalidCommand, "closuredimension: SetTarget without a field")
	}
	s.mu.Lock()
	target := DimensionState{}
	if s.target != nil {
		target = *s.target
	}
	current := clone(s.current)
	s.mu.Unlock()

	position := clone(req.Position)
	if position != nil && s.has(cd.FeaturePositioning) {
		if *position > Percent100thsMax {
			return spec.Errorf(im.StatusConstraintError, "closuredimension: Position %d", *position)
		}
		if s.has(cd.FeatureLimitation) {
			s.mu.Lock()
			limit := s.limit
			s.mu.Unlock()
			*position = min(max(*position, limit.Min), limit.Max)
		}
		if res := s.cfg.Resolution; *position%res != 0 {
			*position = (*position + res/2) / res * res
		}
		target.Position = &spec.Nullable[uint16]{Value: *position}
	}
	if req.Latch != nil && s.has(cd.FeatureMotionLatching) {
		modes := s.cfg.LatchControlModes
		if (*req.Latch && modes&cd.LatchControlModesRemoteLatching == 0) || (!*req.Latch && modes&cd.LatchControlModesRemoteUnlatching == 0) {
			return spec.Errorf(im.StatusInvalidInState, "closuredimension: Latch %v is not remotely controllable", *req.Latch)
		}
		target.Latch = &spec.Nullable[bool]{Value: *req.Latch}
	}
	if req.Speed != nil && s.has(cd.FeatureSpeed) {
		if !s.EnumSupported(cd.ThreeLevelAutoEnumDef, uint64(*req.Speed)) {
			return spec.Errorf(im.StatusConstraintError, "closuredimension: Speed %d", *req.Speed)
		}
		target.Speed = clone(req.Speed)
	}
	if current == nil {
		return spec.Errorf(im.StatusInvalidInState, "closuredimension: CurrentState is null")
	}
	if s.has(cd.FeaturePositioning) && (current.Position == nil || current.Position.Null) {
		return spec.Errorf(im.StatusInvalidInState, "closuredimension: the current Position is unknown")
	}
	// A position change while latched needs the same command to unlatch
	// (:541-552); chip asks for the request's Position, whatever the
	// feature selection.
	if s.has(cd.FeatureMotionLatching) && req.Position != nil && latched(current) && (req.Latch == nil || *req.Latch) {
		return spec.Errorf(im.StatusInvalidInState, "closuredimension: latched; a position change must unlatch")
	}
	if err := s.cfg.Handler.SetTarget(ctx, position, req.Latch, req.Speed); err != nil {
		return spec.Errorf(im.StatusFailure, "closuredimension: SetTarget: %v", err)
	}
	if err := s.SetTargetState(&target); err != nil {
		return spec.Errorf(im.StatusFailure, "closuredimension: SetTarget: %v", err)
	}
	return nil
}

// latched reports whether st's Latch is a non-null true.
func latched(st *DimensionState) bool {
	return st.Latch != nil && !st.Latch.Null && st.Latch.Value
}

// step is chip's HandleStepCommand (:562-662).
func (s *DimensionServer) step(ctx context.Context, req cd.StepRequest) error {
	if !s.EnumSupported(cd.StepDirectionEnumDef, uint64(req.Direction)) {
		return spec.Errorf(im.StatusConstraintError, "closuredimension: Direction %d", req.Direction)
	}
	if req.NumberOfSteps == 0 {
		return spec.Errorf(im.StatusConstraintError, "closuredimension: NumberOfSteps 0")
	}
	s.mu.Lock()
	target := DimensionState{}
	if s.target != nil {
		target = *s.target
	}
	current := clone(s.current)
	limit := s.limit
	s.mu.Unlock()

	if req.Speed != nil && s.has(cd.FeatureSpeed) {
		if !s.EnumSupported(cd.ThreeLevelAutoEnumDef, uint64(*req.Speed)) {
			return spec.Errorf(im.StatusConstraintError, "closuredimension: Speed %d", *req.Speed)
		}
		target.Speed = clone(req.Speed)
	}
	if current == nil || current.Position == nil || current.Position.Null {
		return spec.Errorf(im.StatusInvalidInState, "closuredimension: the current Position is unknown")
	}
	if s.has(cd.FeatureMotionLatching) && latched(current) {
		return spec.Errorf(im.StatusInvalidInState, "closuredimension: latched; Step cannot move")
	}
	delta := uint32(req.NumberOfSteps) * uint32(s.cfg.StepValue)
	pos := uint32(current.Position.Value)
	lm := s.has(cd.FeatureLimitation)
	if req.Direction == cd.StepDirectionDecrease {
		pos -= min(pos, delta)
		if lm {
			pos = max(pos, uint32(limit.Min))
		}
	} else {
		ceiling := uint32(Percent100thsMax)
		if lm {
			ceiling = uint32(limit.Max)
		}
		pos = min(pos+delta, ceiling)
	}
	if err := s.cfg.Handler.Step(ctx, req.Direction, req.NumberOfSteps, req.Speed); err != nil {
		return spec.Errorf(im.StatusFailure, "closuredimension: Step: %v", err)
	}
	target.Position = &spec.Nullable[uint16]{Value: uint16(pos)} //nolint:gosec // at most the ceiling, 10000
	if err := s.SetTargetState(&target); err != nil {
		return spec.Errorf(im.StatusFailure, "closuredimension: Step: %v", err)
	}
	return nil
}

// CurrentState returns the current state; nil is null.
func (s *DimensionServer) CurrentState() *DimensionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.current)
}

// TargetState returns the target state; nil is null.
func (s *DimensionServer) TargetState() *DimensionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.target)
}

// SetCurrentState records the device's state; nil is null. It is refused
// as chip's SetCurrentState refuses a value (:231-310), and reported per
// its rules (:210-222, :247-316): at once when CurrentState or its
// Position changes to or from null, when the Position reaches the
// target's, when Latch or Speed changes; any other Position change at
// most once every 5 s. A change held back is stored and read, but neither
// bumps the data version nor is reported.
func (s *DimensionServer) SetCurrentState(st *DimensionState) error {
	if st != nil {
		if err := s.checkState(*st, false); err != nil {
			return err
		}
	}
	s.mu.Lock()
	old := s.current
	if equalState(old, st) {
		s.mu.Unlock()
		return nil
	}
	dirty := (old == nil) != (st == nil)
	if st != nil && old != nil {
		dirty = dirty || !equalNullable(old.Latch, st.Latch) || !equalPtr(old.Speed, st.Speed)
	}
	if st != nil && st.Position != nil {
		var was *spec.Nullable[uint16]
		if old != nil {
			was = old.Position
		}
		if !equalNullable(was, st.Position) {
			now := s.cfg.Now()
			reached := s.target != nil && s.target.Position != nil && !s.target.Position.Null && equalNullable(s.target.Position, st.Position)
			nullness := was == nil || was.Null != st.Position.Null
			if reached || nullness || s.lastPosDirt.IsZero() || now.Sub(s.lastPosDirt) >= positionQuietInterval {
				s.lastPosDirt = now
				dirty = true
			}
		}
	}
	s.current = clone(st)
	s.mu.Unlock()
	if dirty {
		s.changed(cd.AttrCurrentState)
	}
	return nil
}

// SetTargetState records a target the device took on its own; nil is
// null. It is refused as chip's SetTargetState refuses a value
// (:332-373).
func (s *DimensionServer) SetTargetState(st *DimensionState) error {
	if st != nil {
		if err := s.checkState(*st, true); err != nil {
			return err
		}
	}
	s.mu.Lock()
	if equalState(s.target, st) {
		s.mu.Unlock()
		return nil
	}
	s.target = clone(st)
	s.mu.Unlock()
	s.changed(cd.AttrTargetState)
	return nil
}

// SetUnitRange changes UnitRange (UT); nil is null.
func (s *DimensionServer) SetUnitRange(r *UnitRange) error {
	if !s.has(cd.FeatureUnit) {
		return dimInvalid("UnitRange without Unit")
	}
	if err := s.checkUnitRange(r); err != nil {
		return err
	}
	s.mu.Lock()
	same := equalPtr(s.unitRange, r)
	s.unitRange = clone(r)
	s.mu.Unlock()
	if !same {
		s.changed(cd.AttrUnitRange)
	}
	return nil
}

// SetLimitRange changes LimitRange (LM).
func (s *DimensionServer) SetLimitRange(r LimitRange) error {
	if !s.has(cd.FeatureLimitation) {
		return dimInvalid("LimitRange without Limitation")
	}
	if err := s.checkLimitRange(r); err != nil {
		return err
	}
	s.mu.Lock()
	same := s.limit == r
	s.limit = r
	s.mu.Unlock()
	if !same {
		s.changed(cd.AttrLimitRange)
	}
	return nil
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// equalNullable compares two optional nullable fields: absent equals
// absent, null equals null.
func equalNullable[T comparable](a, b *spec.Nullable[T]) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Null || b.Null {
		return a.Null == b.Null
	}
	return a.Value == b.Value
}

func equalState(a, b *DimensionState) bool {
	if a == nil || b == nil {
		return a == b
	}
	return equalNullable(a.Position, b.Position) && equalNullable(a.Latch, b.Latch) && equalPtr(a.Speed, b.Speed)
}
