// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package light provides standalone Matter cluster-server implementations
// for light device types (OnOffLight, DimmableLight, ColorTemperatureLight,
// ExtendedColorLight). The servers in this package implement
// [contract.ClusterServer] and the optional lister interfaces so the
// endpoint assembler can attach them without knowing the device-specific
// model types.
//
// The [ColorControlServer] is a port of matter.js ColorControlServer
// (packages/node/src/behaviors/color-control/ColorControlServer.ts) built on
// the generated ColorControl definition (cluster/spec/colorcontrol, ADR
// 0013). The host declares the features it serves at construction
// ([ColorControlServerConfig.Features]); the attribute and command lists
// follow from them by the element file's conformance, as matter.js's
// ValidatedElements derives them. It serves every feature matter.js
// implements:
//
//   - CT: MoveToColorTemperature, MoveColorTemperature,
//     StepColorTemperature, the coupling to the LevelControl level and
//     StartUpColorTemperatureMireds — what ColorTemperatureLight (0x010C)
//     requires. A configuration without Features is CT only, as this
//     server was before it served the other features.
//   - XY: CurrentX / CurrentY, MoveToColor, MoveColor, StepColor — what
//     ExtendedColorLight (0x010D) requires besides CT (matter.js
//     devices/extended-color-light.ts: ColorControlServer.with("Xy",
//     "ColorTemperature")).
//   - HS: CurrentHue / CurrentSaturation and the seven hue and saturation
//     commands, with matter.js's direction and wrap rules.
//   - EHUE: EnhancedCurrentHue and the four Enhanced* commands.
//   - CL: ColorLoopSet and the colour loop attributes.
//
// StopMoveStep, the Options / ExecuteIfOff gate, the colour mode switching
// with its colour-space conversions (ported in colorconversion.go) and —
// with [ColorControlServerConfig.ManageTransitions] — gradual transitions on
// the module's transition engine (cluster/transition, matter.js
// behavior/Transitions.ts) with a live RemainingTime apply to every
// feature. endpoint.ValidateDeviceTypes accepts a ColorControlServer with
// XY and CT for ExtendedColorLight and one with CT for
// ColorTemperatureLight.
//
// The server holds the colour state itself and pushes every value it
// applies — at once, or step by step — to the host's optional sinks: the
// colour temperature to a [ColorTemperatureWriter], the whole colour, in
// whichever mode, to a [ColorWriter]. The reference daemon
// (examples/reference-bridge) mounts it in front of chip-tool; a host whose
// lamp ramps natively and reports its own colour mounts its own
// [contract.ClusterServer] instead.
package light

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/cluster/transition"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ColorFeature is a ColorControl FeatureMap bit (color-control.element.ts).
type ColorFeature = ccdef.Feature

// FeatureMap bits. HS is mandatory with EHUE, EHUE with CL (their
// conformance "EHUE, O" and "CL, O").
const (
	ColorFeatureHueSaturation    = ccdef.FeatureHueSaturation    // HS
	ColorFeatureEnhancedHue      = ccdef.FeatureEnhancedHue      // EHUE
	ColorFeatureColorLoop        = ccdef.FeatureColorLoop        // CL
	ColorFeatureXY               = ccdef.FeatureXy               // XY
	ColorFeatureColorTemperature = ccdef.FeatureColorTemperature // CT
)

// ColorMode is the EnhancedColorModeEnum: the colour mode the light is in.
// ColorMode (0x0008) reads the same value, with the enhanced hue mode read
// as hue and saturation.
type ColorMode uint8

// EnhancedColorModeEnum values (color-control.element.ts).
const (
	ColorModeHueSaturation         ColorMode = 0
	ColorModeXY                    ColorMode = 1
	ColorModeColorTemperature      ColorMode = 2
	ColorModeEnhancedHueSaturation ColorMode = 3
)

// ColorControlServerConfig holds the static configuration injected at
// construction time. The colour temperature fields are in mired units and
// apply with the CT feature.
type ColorControlServerConfig struct {
	// Features is the FeatureMap. Zero is CT only, which is what every
	// configuration written before the other features meant.
	Features ColorFeature
	// MinMireds is the physical lower bound of the CT range expressed in
	// mireds. Corresponds to ColorTempPhysicalMinMireds (0x400B). Higher
	// Kelvin → lower mired value, so MinMireds < MaxMireds.
	MinMireds uint16
	// MaxMireds is the physical upper bound of the CT range in mireds.
	// Corresponds to ColorTempPhysicalMaxMireds (0x400C).
	MaxMireds uint16
	// InitialMireds is the starting value for ColorTemperatureMireds (0x0007).
	InitialMireds uint16
	// InitialColorMode is the colour mode at start; nil is the XY mode
	// with the XY feature, else the CT mode with CT, else hue and
	// saturation. matter.js leaves ColorMode without a default for the
	// application to set (ColorControlOverrides.ts); its default
	// EnhancedColorMode is XY.
	InitialColorMode *ColorMode
	// InitialX and InitialY are the starting CurrentX / CurrentY; both
	// zero is matter.js's default, 24939 / 24701 (ColorControlServer.ts
	// State, "the default values specified prior to Matter 1.4.2").
	InitialX, InitialY uint16
	// InitialHue, InitialSaturation and InitialEnhancedHue are the starting
	// CurrentHue, CurrentSaturation and EnhancedCurrentHue (matter.js
	// default 0).
	InitialHue, InitialSaturation uint8
	InitialEnhancedHue            uint16
	// ManageTransitions runs the commands as gradual transitions, stepping
	// the values every TransitionStepInterval and reporting RemainingTime
	// (matter.js managedTransitionTimeHandling). Without it every command
	// applies its target at once, as matter.js does by default.
	ManageTransitions bool
	// TransitionStepInterval is the time between two steps; zero is
	// [transition.DefaultStepInterval] (matter.js transitionStepInterval).
	TransitionStepInterval time.Duration
	// OnOff is the On/Off state of the endpoint, read for the ExecuteIfOff
	// gate. Nil means the endpoint has none and every command executes
	// (matter.js #optionsAllowExecution: `!this.agent.has(OnOffServer)`).
	OnOff OnOffState
}

// OnOffState is the On/Off state of the endpoint a [ColorControlServer]
// gates its commands on.
type OnOffState interface {
	OnOff() bool
}

// DefaultColorControlServerConfig returns a sensible default: warm-cool
// LED range (153–500 mireds, corresponding to ~6535 K–2000 K), CT only.
// Device profiles that expose narrower Kelvin ranges should compute their
// own config from their Min/MaxKelvin fields before calling
// [NewColorControlServer].
func DefaultColorControlServerConfig() ColorControlServerConfig {
	return ColorControlServerConfig{
		MinMireds:     153, // ≈ 6535 K
		MaxMireds:     500, // ≈ 2000 K
		InitialMireds: 370, // ≈ 2700 K, warm white default
	}
}

// ColorTemperatureWriter is the optional sink a [ColorControlServer]
// drives with every colour temperature it applies: the target of a command
// that applies at once, each step of a transition, or the value a colour
// mode switch converts to. Implementations translate the cropped mired
// value into the device's native unit — HM exposes COLOR_TEMPERATURE in
// Kelvin (mireds = 1_000_000 / Kelvin) — and push it to the device. A
// write error fails the command, or ends the transition, and leaves the
// in-process ColorTemperatureMireds attribute unchanged, so the reported
// state never claims a value the device did not accept.
//
// The implementation owns the southbound urgency of the write it
// performs: the cluster contract carries no priority, so a host whose
// command queue ranks by urgency names the value it wants inside
// SetColorTemperatureMireds.
type ColorTemperatureWriter interface {
	SetColorTemperatureMireds(ctx context.Context, mireds uint16) error
}

// Color is the colour state of a [ColorControlServer], in the attributes'
// Matter units. Mode says which fields describe the colour the light
// shows: X / Y in the XY mode, Hue / Saturation in the hue and saturation
// mode, EnhancedHue / Saturation in the enhanced one, the colour
// temperature in the CT mode. The other fields keep the last value of
// their mode. [HSVToXY], [XYToHSV], [MiredsToXY] and the rest convert
// between them as the server does when it switches mode.
type Color struct {
	Mode ColorMode
	// X and Y are CurrentX / CurrentY: CIE 1931 x and y times 65536
	// (0..65279).
	X, Y uint16
	// Hue is CurrentHue (0..254 for 0..360°), EnhancedHue
	// EnhancedCurrentHue (0..65535 for 0..360°).
	Hue         uint8
	EnhancedHue uint16
	// Saturation is CurrentSaturation (0..254 for 0..100 %).
	Saturation uint8
	// ColorTemperatureMireds is ColorTemperatureMireds.
	ColorTemperatureMireds uint16
}

// ColorWriter is the optional sink a [ColorControlServer] drives with the
// whole colour whenever it changes: a command that applies at once, each
// step of a transition, a colour mode switch and the colour loop. It is
// how a host whose device takes x/y or hue and saturation receives them; a
// [ColorTemperatureWriter], when one is wired as well, still receives
// every colour temperature. Errors behave as for the
// ColorTemperatureWriter: the change is refused and the attributes keep
// their value.
type ColorWriter interface {
	SetColor(ctx context.Context, c Color) error
}

// colorState is the server's colour state (matter.js ColorControlServer
// State, the attributes the commands change).
type colorState struct {
	mode         ColorMode // ColorMode: 0..2
	enhancedMode ColorMode // EnhancedColorMode: 0..3
	hue          uint8
	saturation   uint8
	enhancedHue  uint16
	x, y         uint16
	mireds       uint16

	loopActive    uint8
	loopDirection uint8
	loopTime      uint16
	loopStart     uint16
	loopStored    uint16
}

func (st colorState) color() Color {
	return Color{
		Mode: st.enhancedMode, X: st.x, Y: st.y, Hue: st.hue, EnhancedHue: st.enhancedHue,
		Saturation: st.saturation, ColorTemperatureMireds: st.mireds,
	}
}

// ColorControlServer is the ColorControl cluster server (cluster 0x0300),
// a port of matter.js ColorControlServer for the features it is configured
// with. It holds the colour state in-process and, when sinks are wired
// ([ColorControlServer.SetWriter], [ColorControlServer.SetColorWriter]),
// pushes every value it applies down to the device. Without a sink the
// server updates in-process state only and answers Success — the
// chip-tool conformance / test path that needs no live device.
type ColorControlServer struct {
	cfg    ColorControlServerConfig
	inst   *spec.Instance
	engine *transition.Engine
	onOff  OnOffState
	// cmd serialises the commands.
	cmd sync.Mutex
	// commit serialises the changes of the colour state, which reach it
	// from commands and from transition steps.
	commit sync.Mutex
	// changes and quiet report the attributes the server changes; quiet
	// holds a quieter per "Q" attribute.
	changes cluster.AttributeChanges
	quiet   map[uint32]*cluster.Quieter

	// mu guards everything below: commands, the transition's steps and
	// subscription reports reach the server from different goroutines.
	mu          sync.Mutex
	st          colorState
	writer      ColorTemperatureWriter
	colorWriter ColorWriter
	// startUpMireds is StartUpColorTemperatureMireds (0x4010); nil is
	// null, "keep the previous colour temperature at start-up" (the
	// attribute's quality X, matter.js ColorControlServer.ts:382
	// `startUpColorTemperatureMireds ?? null`).
	startUpMireds *uint16
	// options is the Options bitmap (0x000F, "RW VO"): bit 0
	// ExecuteIfOff is the only defined bit.
	options uint8
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*ColorControlServer)(nil)
	_ contract.ClusterAttributeLister         = (*ColorControlServer)(nil)
	_ contract.ClusterCommandLister           = (*ColorControlServer)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*ColorControlServer)(nil)
	_ contract.AttributeChangeNotifier        = (*ColorControlServer)(nil)
	_ contract.SelfReportedAttributeLister    = (*ColorControlServer)(nil)
	_ contract.ClusterQuiescer                = (*ColorControlServer)(nil)
)

// ErrColorFeatures reports a feature selection the ColorControl
// conformance does not allow: EHUE without HS, CL without EHUE, or an
// initial colour mode of a feature the server does not have.
var ErrColorFeatures = errors.New("colorcontrol: feature selection")

// The defaults matter.js's ColorControlServer State carries for the
// attributes whose specification default went away in Matter 1.4.2.
const (
	defaultCurrentX         = 24939
	defaultCurrentY         = 24701
	defaultColorLoopTime    = 25
	defaultColorLoopStart   = 8960
	colorLoopIncrement      = 1
	colorLoopActiveInactive = 0
	colorLoopActiveActive   = 1
)

// NewColorControlServer constructs a ColorControlServer with the given
// configuration. cfg.InitialMireds is clamped to [cfg.MinMireds,
// cfg.MaxMireds]. It panics on a feature selection [NewColorControl]
// refuses; a configuration without Features never does.
func NewColorControlServer(cfg ColorControlServerConfig) *ColorControlServer {
	s, err := NewColorControl(cfg)
	if err != nil {
		panic(err)
	}
	return s
}

// NewColorControl constructs a ColorControlServer, refusing a feature
// selection the cluster's conformance does not allow with
// [ErrColorFeatures].
func NewColorControl(cfg ColorControlServerConfig) (*ColorControlServer, error) {
	if cfg.Features == 0 {
		cfg.Features = ColorFeatureColorTemperature
	}
	inst, err := spec.New(ccdef.Definition, spec.Options{
		Features: uint32(cfg.Features),
		// RemainingTime is "O"; ColorTemperatureLight and
		// ExtendedColorLight make it mandatory, and the server keeps it
		// live (matter.js extended-color-light.ts alters remainingTime to
		// not optional).
		Attributes: []uint32{ccdef.AttrRemainingTime},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrColorFeatures, err)
	}
	s := &ColorControlServer{cfg: cfg, inst: inst, onOff: cfg.OnOff}
	s.st = colorState{
		mireds:        min(max(cfg.InitialMireds, cfg.MinMireds), cfg.MaxMireds),
		x:             cfg.InitialX,
		y:             cfg.InitialY,
		hue:           min(cfg.InitialHue, maxHueValue),
		saturation:    min(cfg.InitialSaturation, maxSaturationValue),
		enhancedHue:   cfg.InitialEnhancedHue,
		loopActive:    colorLoopActiveInactive,
		loopDirection: colorLoopIncrement,
		loopTime:      defaultColorLoopTime,
		loopStart:     defaultColorLoopStart,
	}
	if cfg.InitialX == 0 && cfg.InitialY == 0 {
		s.st.x, s.st.y = defaultCurrentX, defaultCurrentY
	}
	s.st.x, s.st.y = min(s.st.x, maxCIEXYValue), min(s.st.y, maxCIEXYValue)
	mode, err := s.initialMode()
	if err != nil {
		return nil, err
	}
	s.st.enhancedMode = mode
	s.st.mode = mode
	if mode == ColorModeEnhancedHueSaturation {
		s.st.mode = ColorModeHueSaturation
	}

	s.quiet = map[uint32]*cluster.Quieter{}
	for _, id := range quietAttributes {
		s.quiet[id] = &cluster.Quieter{Report: func() { s.changes.Notify(id) }}
	}
	s.engine = transition.New(transition.Config{
		Manage:       cfg.ManageTransitions,
		StepInterval: cfg.TransitionStepInterval,
		Properties: map[string]transition.Property{
			propHue:              {Min: minHueValue, Max: maxHueValue, Cyclic: true},
			propEnhancedHue:      {Min: minHueValue, Max: maxEnhancedHueValue, Cyclic: true},
			propSaturation:       {Min: minSaturationValue, Max: maxSaturationValue},
			propColorTemperature: {Min: float64(s.minimumMireds()), Max: float64(s.maximumMireds())},
			propX:                {Min: minCIEXYValue, Max: maxCIEXYValue},
			propY:                {Min: minCIEXYValue, Max: maxCIEXYValue},
		},
		Read: func(name string) (float64, bool) {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.st.property(name)
		},
		Apply:                s.applyChanges,
		RemainingTimeChanged: func() { s.changes.Notify(wire.ColorCtrlAttrRemainingTime) },
		Settled: func(name string) {
			if q := s.quiet[propAttribute[name]]; q != nil {
				q.EmitNow()
			}
		},
	})
	return s, nil
}

// initialMode resolves cfg.InitialColorMode against the features.
func (s *ColorControlServer) initialMode() (ColorMode, error) {
	if m := s.cfg.InitialColorMode; m != nil {
		if !s.supportsColorMode(*m) {
			return 0, fmt.Errorf("%w: initial colour mode %d needs a feature the server does not have", ErrColorFeatures, *m)
		}
		return *m, nil
	}
	switch {
	case s.has(ColorFeatureXY):
		return ColorModeXY, nil
	case s.has(ColorFeatureColorTemperature):
		return ColorModeColorTemperature, nil
	default:
		return ColorModeHueSaturation, nil
	}
}

// has reports whether the server has feature f.
func (s *ColorControlServer) has(f ColorFeature) bool { return s.cfg.Features&f != 0 }

// supportsColorMode is matter.js #supportsColorMode.
func (s *ColorControlServer) supportsColorMode(m ColorMode) bool {
	switch m {
	case ColorModeEnhancedHueSaturation:
		return s.has(ColorFeatureEnhancedHue)
	case ColorModeHueSaturation:
		return s.has(ColorFeatureHueSaturation)
	case ColorModeXY:
		return s.has(ColorFeatureXY)
	case ColorModeColorTemperature:
		return s.has(ColorFeatureColorTemperature)
	}
	return false
}

// SetWriter wires the optional colour temperature sink. Pass nil to detach.
func (s *ColorControlServer) SetWriter(w ColorTemperatureWriter) {
	s.mu.Lock()
	s.writer = w
	s.mu.Unlock()
}

// SetColorWriter wires the optional colour sink. Pass nil to detach.
func (s *ColorControlServer) SetColorWriter(w ColorWriter) {
	s.mu.Lock()
	s.colorWriter = w
	s.mu.Unlock()
}

// Color returns the current colour state.
func (s *ColorControlServer) Color() Color {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.color()
}

// MatterClusterID returns the ColorControl cluster ID (0x0300).
func (s *ColorControlServer) MatterClusterID() uint32 { return wire.ColorControlClusterID }

// MatterRead returns the value of a served attribute. Returns (nil, false)
// for an attribute the feature selection does not serve, so the IM
// dispatcher can fall back to the global attribute path.
func (s *ColorControlServer) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case cluster.AttrGlobalFeatureMap:
		return uint32(s.cfg.Features), true
	case cluster.AttrGlobalClusterRevision:
		return ColorControlClusterRevision, true
	}
	if !s.inst.Serves(attrID) {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.st
	switch attrID {
	case ccdef.AttrCurrentHue:
		return st.hue, true
	case ccdef.AttrCurrentSaturation:
		return st.saturation, true
	case ccdef.AttrRemainingTime:
		// The time left in the running transition, 0 outside one and
		// whenever transitions are not managed (matter.js
		// ColorControlServer State remainingTime →
		// Transitions.remainingTime).
		return uint16(min(s.engine.RemainingTime(), math.MaxUint16)), true //nolint:gosec // bounded by MaxUint16
	case ccdef.AttrCurrentX:
		return st.x, true
	case ccdef.AttrCurrentY:
		return st.y, true
	case ccdef.AttrColorTemperatureMireds:
		return st.mireds, true
	case ccdef.AttrColorMode:
		return uint8(st.mode), true
	case ccdef.AttrOptions:
		return s.options, true
	case ccdef.AttrNumberOfPrimaries:
		// NumberOfPrimaries is mandatory with Quality X (nullable); the
		// server declares no primaries, so it reads null.
		return nil, true
	case ccdef.AttrEnhancedCurrentHue:
		return st.enhancedHue, true
	case ccdef.AttrEnhancedColorMode:
		return uint8(st.enhancedMode), true
	case ccdef.AttrColorLoopActive:
		return st.loopActive, true
	case ccdef.AttrColorLoopDirection:
		return st.loopDirection, true
	case ccdef.AttrColorLoopTime:
		return st.loopTime, true
	case ccdef.AttrColorLoopStartEnhancedHue:
		return st.loopStart, true
	case ccdef.AttrColorLoopStoredEnhancedHue:
		return st.loopStored, true
	case ccdef.AttrColorCapabilities:
		// matter.js initialize: `this.state.colorCapabilities = this.features`.
		return uint16(s.cfg.Features), true //nolint:gosec // five feature bits, checked by spec.New
	case ccdef.AttrColorTempPhysicalMinMireds:
		return s.cfg.MinMireds, true
	case ccdef.AttrColorTempPhysicalMaxMireds:
		return s.cfg.MaxMireds, true
	case ccdef.AttrCoupleColorTempToLevelMinMireds:
		// The server keeps no separate coupling floor, so the value is the
		// one matter.js falls back to: the physical minimum
		// (ColorControlServer.ts syncColorTemperatureWithLevelLogic
		// `coupleColorTempToLevelMinMireds ?? minimumColorTemperatureMireds`).
		return s.cfg.MinMireds, true
	case ccdef.AttrStartUpColorTemperatureMireds:
		if s.startUpMireds == nil {
			return nil, true
		}
		return *s.startUpMireds, true
	}
	return nil, false
}

// MatterWrite accepts the writable attributes: Options ("RW VO") and, with
// CT, StartUpColorTemperatureMireds ("RW VM", constraint "1 to 65279",
// nullable — color-control.element.ts). The generated definition answers
// every other write and every refused value with matter.js's status
// (UNSUPPORTED_ATTRIBUTE, UNSUPPORTED_WRITE, CONSTRAINT_ERROR — an Options
// value with an undefined bit included, TC-CC-6.5 step 0a).
//
// StartUpColorTemperatureMireds is reported, never applied: a bridged
// endpoint has no start-up of its own, the same reason matter.js gives for
// skipping it on an Aggregator-owned endpoint (ColorControlServer.ts
// initializeColorTemperature: `!this.endpoint.ownerOfType(AggregatorEndpoint)`).
func (s *ColorControlServer) MatterWrite(_ context.Context, attrID uint32, value any) error {
	v, err := s.inst.ValidateWrite(attrID, value, nil)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch attrID {
	case ccdef.AttrOptions:
		n, _ := v.(uint64)
		s.options = uint8(n) //nolint:gosec // a bitmap8 ValidateWrite bounded
	case ccdef.AttrStartUpColorTemperatureMireds:
		s.startUpMireds = nil
		if n, ok := v.(uint64); ok {
			m := uint16(n) //nolint:gosec // within "1 to 65279" per ValidateWrite
			s.startUpMireds = &m
		}
	}
	return nil
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// Options is "RW VO" (Operate), StartUpColorTemperatureMireds "RW VM"
// (Manage).
func (s *ColorControlServer) MinWritePrivilege(attrID uint32) uint8 {
	return s.inst.MinWritePrivilege(attrID)
}

// colorControlConstraintErr answers a command with CONSTRAINT_ERROR.
type colorControlConstraintErr string

func (e colorControlConstraintErr) Error() string                 { return string(e) }
func (colorControlConstraintErr) MatterStatusCode() im.StatusCode { return im.StatusConstraintError }

// colorControlInvalidCommandErr answers a command with INVALID_COMMAND.
type colorControlInvalidCommandErr string

func (e colorControlInvalidCommandErr) Error() string                 { return string(e) }
func (colorControlInvalidCommandErr) MatterStatusCode() im.StatusCode { return im.StatusInvalidCommand }

// MatterReportable implements [contract.ClusterServer]. It is empty: the
// server reports every attribute it changes itself
// ([ColorControlServer.OnMatterAttributesChanged]), the "Q" ones by their
// quieter rules, so a change notification of the endpoint's source must
// not report them as well.
func (s *ColorControlServer) MatterReportable() []uint32 { return []uint32{} }

// MatterAttributes lists the attributes the feature selection serves, by
// the element file's conformance (the generated definition), with
// RemainingTime.
func (s *ColorControlServer) MatterAttributes() []uint32 { return s.inst.MatterAttributes() }

// MatterAcceptedCommands lists the commands the feature selection serves
// (AcceptedCommandList, 0xFFF9).
func (s *ColorControlServer) MatterAcceptedCommands() []uint32 {
	return s.inst.MatterAcceptedCommands()
}

// MatterGeneratedCommands returns nil; ColorControl commands carry no
// response payload.
func (s *ColorControlServer) MatterGeneratedCommands() []uint32 { return nil }

// ColorControlClusterRevision is the cluster revision for ColorControl
// pinned to matter.js HEAD (the generated definition's).
const ColorControlClusterRevision = ccdef.Revision
