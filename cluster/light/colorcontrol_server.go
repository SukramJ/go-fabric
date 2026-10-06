// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package light provides standalone Matter cluster-server implementations
// for light device types (OnOffLight, DimmableLight, ColorTemperatureLight,
// ExtendedColorLight). The servers in this package are thin, stateless
// wrappers whose state is owned by the caller; they implement
// [contract.ClusterServer] and the optional lister interfaces so the
// endpoint assembler can attach them without
// knowing the device-specific model types.
//
// ColorTemperatureLight (0x010C) requires ColorControl (0x0300) in CT-only
// mode. The [ColorControlServer] here serves the CT feature as matter.js
// ColorControlServer does: MoveToColorTemperature, MoveColorTemperature,
// StepColorTemperature and StopMoveStep, the ExecuteIfOff gate, the
// coupling to the LevelControl level, and — with
// [ColorControlServerConfig.ManageTransitions] — gradual transitions on
// the module's transition engine (cluster/transition, matter.js
// behavior/Transitions.ts) with a live RemainingTime. Hue, saturation, xy
// and the colour loop are not served (FeatureMap CT only).
//
// The server holds the colour temperature itself and pushes every value it
// applies — at once, or step by step — to an optional
// [ColorTemperatureWriter], the host's device sink. The reference daemon
// (examples/reference-bridge) mounts it in front of chip-tool; a host whose
// lamp ramps natively and reports its own colour temperature mounts its own
// [contract.ClusterServer] instead.
package light

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/transition"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ColorControlServerConfig holds the static configuration injected at
// construction time. All fields are in mired units.
type ColorControlServerConfig struct {
	// MinMireds is the physical lower bound of the CT range expressed in
	// mireds. Corresponds to ColorTempPhysicalMinMireds (0x400B). Higher
	// Kelvin → lower mired value, so MinMireds < MaxMireds.
	MinMireds uint16
	// MaxMireds is the physical upper bound of the CT range in mireds.
	// Corresponds to ColorTempPhysicalMaxMireds (0x400C).
	MaxMireds uint16
	// InitialMireds is the starting value for CurrentColorTemperature (0x0007).
	InitialMireds uint16
	// ManageTransitions runs the colour temperature commands as gradual
	// transitions, stepping the value every TransitionStepInterval and
	// reporting RemainingTime (matter.js managedTransitionTimeHandling).
	// Without it every command applies its target at once, as matter.js
	// does by default.
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
// LED range (153–500 mireds, corresponding to ~6535 K–2000 K). Device
// profiles that expose narrower Kelvin ranges should compute their own
// config from their Min/MaxKelvin fields before calling
// [NewColorControlServer].
func DefaultColorControlServerConfig() ColorControlServerConfig {
	return ColorControlServerConfig{
		MinMireds:     153, // ≈ 6535 K
		MaxMireds:     500, // ≈ 2000 K
		InitialMireds: 370, // ≈ 2700 K, warm white default
	}
}

// ColorTemperatureWriter is the optional sink a [ColorControlServer]
// drives on a successful MoveToColorTemperature. Implementations translate
// the cropped mired value into the device's native unit — HM exposes
// COLOR_TEMPERATURE in Kelvin (mireds = 1_000_000 / Kelvin) — and push it
// to the CCU. A write error aborts the command and leaves the in-process
// CurrentColorTemperatureMireds attribute unchanged, so the reported state
// never claims a value the device did not accept.
//
// The implementation owns the southbound urgency of the write it
// performs: the cluster contract carries no priority, so a host whose
// command queue ranks by urgency names the value it wants inside
// SetColorTemperatureMireds.
type ColorTemperatureWriter interface {
	SetColorTemperatureMireds(ctx context.Context, mireds uint16) error
}

// ColorControlServer is a CT-only ColorControl cluster server (cluster
// 0x0300) for the ColorTemperatureLight (0x010C) device type. It holds the
// current CT value in-process and, when a [ColorTemperatureWriter] is
// wired via [ColorControlServer.SetWriter], pushes every value it applies
// down to the device. Without a writer the server updates in-process
// state only and answers Success — the chip-tool conformance / test path
// that needs no live device.
type ColorControlServer struct {
	cfg    ColorControlServerConfig
	engine *transition.Engine
	onOff  OnOffState
	// cmd serialises the commands.
	cmd sync.Mutex
	// changes and quiet report ColorTemperatureMireds and RemainingTime.
	changes cluster.AttributeChanges
	quiet   *cluster.Quieter

	// mu guards everything below: commands, the transition's steps and
	// subscription reports reach the server from different goroutines.
	mu      sync.Mutex
	current uint16
	writer  ColorTemperatureWriter
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

// colorOptionsDefinedBits are the defined bits of the ColorControl
// OptionsBitmap (color-control.element.ts: ExecuteIfOff).
const colorOptionsDefinedBits uint8 = 0x01

// NewColorControlServer constructs a ColorControlServer with the given
// configuration. cfg.InitialMireds is clamped to [cfg.MinMireds,
// cfg.MaxMireds].
func NewColorControlServer(cfg ColorControlServerConfig) *ColorControlServer {
	init := min(max(cfg.InitialMireds, cfg.MinMireds), cfg.MaxMireds)
	s := &ColorControlServer{cfg: cfg, current: init, onOff: cfg.OnOff}
	s.quiet = &cluster.Quieter{Report: func() { s.changes.Notify(wire.ColorCtrlAttrColorTemperatureMireds) }}
	s.engine = transition.New(transition.Config{
		Manage:       cfg.ManageTransitions,
		StepInterval: cfg.TransitionStepInterval,
		Properties: map[string]transition.Property{
			propColorTemperature: {Min: float64(s.minimumMireds()), Max: float64(s.maximumMireds())},
		},
		Read: func(string) (float64, bool) {
			s.mu.Lock()
			defer s.mu.Unlock()
			return float64(s.current), true
		},
		Apply:                s.applyMireds,
		RemainingTimeChanged: func() { s.changes.Notify(wire.ColorCtrlAttrRemainingTime) },
		Settled:              func(string) { s.quiet.EmitNow() },
	})
	return s
}

// SetWriter wires the optional write-through sink. Pass nil to detach.
func (s *ColorControlServer) SetWriter(w ColorTemperatureWriter) {
	s.mu.Lock()
	s.writer = w
	s.mu.Unlock()
}

// MatterClusterID returns the ColorControl cluster ID (0x0300).
func (s *ColorControlServer) MatterClusterID() uint32 { return wire.ColorControlClusterID }

// MatterRead returns attribute values for the CT-only subset. Returns
// (nil, false) for unrecognised attribute IDs so the IM dispatcher can
// fall back to the global attribute path.
//
// CurrentHue / CurrentSaturation (HS conformance) and CurrentX /
// CurrentY (XY conformance) are NOT served — CT-only mode sets neither
// the HS nor the XY feature bit, so advertising those attributes would
// violate the conformance rules in Matter §3.2.6.
func (s *ColorControlServer) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case wire.ColorCtrlAttrColorTemperatureMireds:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.current, true
	case wire.ColorCtrlAttrColorMode, wire.ColorCtrlAttrEnhancedColorMode:
		// ColorMode 2 = ColorTemperatureMireds is the active mode.
		return colorModeCT, true
	case wire.ColorCtrlAttrOptions:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.options, true
	case wire.ColorCtrlAttrColorCapabilities:
		// CT feature bit only (bit 4).
		return colorCapCT, true
	case wire.ColorCtrlAttrColorTempPhysicalMin:
		return s.cfg.MinMireds, true
	case wire.ColorCtrlAttrColorTempPhysicalMax:
		return s.cfg.MaxMireds, true
	case wire.ColorCtrlAttrRemainingTime:
		// The ColorTemperatureLight device type requires RemainingTime
		// (color-temperature-light.element.ts). The time left in the
		// running transition, 0 outside one and whenever transitions are
		// not managed (matter.js ColorControlServer State remainingTime →
		// Transitions.remainingTime). Found missing by the CHIP
		// conformance checker (TC-IDM-10.2 device-type element override).
		return uint16(min(s.engine.RemainingTime(), math.MaxUint16)), true //nolint:gosec // bounded by MaxUint16
	case wire.ColorCtrlAttrCoupleColorTempToLevelMinMireds:
		// CT-mandatory (color-control.element.ts:183-184). The server
		// keeps no separate coupling floor, so the value is the one
		// matter.js falls back to: the physical minimum
		// (ColorControlServer.ts:1582
		// `coupleColorTempToLevelMinMireds ?? minimumColorTemperatureMireds`).
		return s.cfg.MinMireds, true
	case wire.ColorCtrlAttrStartUpColorTemperatureMireds:
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.startUpMireds == nil {
			return nil, true
		}
		return *s.startUpMireds, true
	case wire.ColorCtrlAttrNumberOfPrimaries:
		// NumberOfPrimaries is mandatory (spec §3.2.6.6) with Quality X
		// (nullable). CT-only mode has no primary colours; return null
		// per the Quality X sentinel contract (nil value, present=true).
		return nil, true
	case cluster.AttrGlobalFeatureMap:
		// CT feature = bit 4 (constraint "4" in matter.js color-control.element.ts).
		return colorFeatureCT, true
	case cluster.AttrGlobalClusterRevision:
		return ColorControlClusterRevision, true
	default:
		return nil, false
	}
}

// MatterWrite accepts StartUpColorTemperatureMireds, the one writable
// attribute of the CT surface (access "RW VM", constraint "1 to 65279",
// nullable — color-control.element.ts:187-188); every other ColorControl
// attribute is read-only and commands are the intended mutation path.
//
// The stored value is reported, never applied: a bridged endpoint has no
// start-up of its own, the same reason matter.js gives for skipping
// StartUpOnOff on an Aggregator-owned endpoint (OnOffServer.ts:33-36).
func (s *ColorControlServer) MatterWrite(_ context.Context, attrID uint32, value any) error {
	if attrID == wire.ColorCtrlAttrOptions {
		// Options is "RW VO" (color-control.element.ts), a bitmap8 whose
		// only defined bit is ExecuteIfOff; matter.js stores what the
		// controller writes and validates the bitmap against its bits
		// (ColorControlServer, TC-CC-6.5 step 0a). A set undefined bit is
		// CONSTRAINT_ERROR.
		v, ok := cluster.AsUintMax(value, 0xFF)
		if !ok || uint8(v)&^colorOptionsDefinedBits != 0 { //nolint:gosec // bounded by AsUintMax
			return colorControlConstraintErr(fmt.Sprintf("colorcontrol: Options %v sets an undefined bit", value))
		}
		s.mu.Lock()
		s.options = uint8(v) //nolint:gosec // bounded by AsUintMax
		s.mu.Unlock()
		return nil
	}
	if attrID != wire.ColorCtrlAttrStartUpColorTemperatureMireds {
		return fmt.Errorf("colorcontrol: attribute 0x%04X is not writable", attrID)
	}
	var next *uint16
	if value != nil {
		v, ok := cluster.AsUintMax(value, 0xFFFF)
		if !ok || v < 1 || v > startUpMiredsMax {
			return colorControlConstraintErr(fmt.Sprintf(
				"colorcontrol: StartUpColorTemperatureMireds %v outside 1 to %d", value, startUpMiredsMax,
			))
		}
		m := uint16(v)
		next = &m
	}
	s.mu.Lock()
	s.startUpMireds = next
	s.mu.Unlock()
	return nil
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// StartUpColorTemperatureMireds is "RW VM" (Manage).
func (s *ColorControlServer) MinWritePrivilege(attrID uint32) uint8 {
	if attrID == wire.ColorCtrlAttrStartUpColorTemperatureMireds {
		return privilegeManage
	}
	return privilegeOperate
}

// startUpMiredsMax is the upper bound of StartUpColorTemperatureMireds'
// constraint "1 to 65279".
const startUpMiredsMax = 65279

// Access-control privileges (Matter §9.10.5.2).
const (
	privilegeOperate uint8 = 3
	privilegeManage  uint8 = 4
)

// colorControlConstraintErr answers a write with CONSTRAINT_ERROR.
type colorControlConstraintErr string

func (e colorControlConstraintErr) Error() string                 { return string(e) }
func (colorControlConstraintErr) MatterStatusCode() im.StatusCode { return im.StatusConstraintError }

// MatterReportable implements [contract.ClusterServer]. It is empty: the
// server reports ColorTemperatureMireds and RemainingTime itself
// ([ColorControlServer.OnMatterAttributesChanged]), by the quieter rules
// of their "Q" quality, so a change notification of the endpoint's source
// must not report them as well.
func (s *ColorControlServer) MatterReportable() []uint32 { return []uint32{} }

// MatterAttributes lists the CT-only attribute set served by MatterRead.
// CurrentHue / CurrentSaturation (HS conformance) and CurrentX / CurrentY
// (XY conformance) are excluded — they are only legal when the HS or XY
// feature bit is set in FeatureMap. CT-only FeatureMap has neither.
func (s *ColorControlServer) MatterAttributes() []uint32 {
	return []uint32{
		wire.ColorCtrlAttrRemainingTime,
		wire.ColorCtrlAttrColorTemperatureMireds,
		wire.ColorCtrlAttrColorMode,
		wire.ColorCtrlAttrOptions,
		wire.ColorCtrlAttrNumberOfPrimaries,
		wire.ColorCtrlAttrEnhancedColorMode,
		wire.ColorCtrlAttrColorCapabilities,
		wire.ColorCtrlAttrColorTempPhysicalMin,
		wire.ColorCtrlAttrColorTempPhysicalMax,
		wire.ColorCtrlAttrCoupleColorTempToLevelMinMireds,
		wire.ColorCtrlAttrStartUpColorTemperatureMireds,
	}
}

// MatterAcceptedCommands lists the command IDs the server handles via
// MatterInvoke. Required by MatterClusterCommandLister so
// AcceptedCommandList (0xFFF9) is populated correctly during
// commissioning.
func (s *ColorControlServer) MatterAcceptedCommands() []uint32 {
	return []uint32{
		wire.ColorCtrlCmdMoveToColorTemperature,
		wire.ColorCtrlCmdStopMoveStep,
		wire.ColorCtrlCmdMoveColorTemperature,
		wire.ColorCtrlCmdStepColorTemperature,
	}
}

// MatterGeneratedCommands returns nil; ColorControl commands carry no
// response payload.
func (s *ColorControlServer) MatterGeneratedCommands() []uint32 { return nil }

// colorModeCT is the ColorMode / EnhancedColorMode enum value for
// ColorTemperatureMireds mode (Matter §3.2.7.7 / §3.2.7.18).
const colorModeCT uint8 = 2

// colorCapCT is the ColorCapabilities bitmap value advertising CT-only
// capability (bit 4 = CT feature, Matter §3.2.7.19).
const colorCapCT uint16 = 1 << 4

// colorFeatureCT is the FeatureMap bitmap for CT-only mode.
// Matter §3.2.4 Feature bit CT has constraint "4"
// (packages/model/src/standard/elements/color-control.element.ts).
const colorFeatureCT uint32 = 1 << 4

// ColorControlClusterRevision is the cluster revision for ColorControl
// pinned to matter.js HEAD (@matter/model 0.16.11).
const ColorControlClusterRevision uint16 = 9
