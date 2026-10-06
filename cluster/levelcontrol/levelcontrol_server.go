// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package levelcontrol contains the Matter LevelControl cluster server
// (0x0008) — the cluster Matter offers for a characteristic that moves
// along a continuous scale, as against the labelled choices of
// ModeSelect.
//
// The server owns no level. Every attribute it answers is read from a
// [LevelSource] the host implements; a server that moved an internal
// field and reported Success would tell a controller the device changed
// without anything having reached it.
//
// # Two ways to move the level
//
// A device that ramps by itself takes the eight conformance-M commands as
// they arrive: by default the server forwards each to the port method of
// the same name, and the host applies TransitionTime and Rate natively
// (notes/parity/by_design.md BD-Matter-LevelControl-NativeRamp).
//
// A device that cannot ramp turns on the module's transition engine with
// [Config.Transitions]. The server then runs matter.js LevelControlServer
// itself — MoveToLevel, Move, Step and Stop, their On/Off coupling, the
// ExecuteIfOff gate, CoupleColorTempToLevel — on the transition engine of
// [transition] (matter.js behavior/Transitions.ts), and the host receives
// the levels it steps through as [LevelSource.MoveToLevel] requests with
// TransitionTime 0. CurrentLevel and RemainingTime are then reported by
// the server's own rules (see [Server.OnMatterAttributesChanged]).
//
// # What is served, and what is deliberately not
//
// Three attributes carry conformance "M": CurrentLevel (0x0), Options
// (0xf) and OnLevel (0x11)
// (matter.js packages/model/src/standard/elements/level-control.element.ts:29-30,
// :65, :49-50), and MinLevel (0x2) / MaxLevel (0x3), whose "Rev >= v7, O"
// makes them mandatory at the revision served (:34-41; matter.js sets both
// on every LevelControl, LevelControlServer.ts:114-120). With
// [Config.Lighting] the server advertises the LT feature and adds the two
// attributes LT makes mandatory, RemainingTime (0x1) and
// StartUpCurrentLevel (0x4000) (element :33, :68-71); MinLevel is then 1.
// Every other attribute is feature-gated or optional and is absent here,
// because advertising an attribute whose feature bit is clear is the shape
// a controller can end a commissioning over:
//
//   - CurrentFrequency (0x4), MinFrequency (0x5) and MaxFrequency (0x6)
//     carry "FQ" (:42-47);
//   - OnOffTransitionTime (0x10), OnTransitionTime (0x12),
//     OffTransitionTime (0x13) and DefaultMoveRate (0x14) carry "O"
//     (:48, :53-64). The engine reads them as matter.js does when a state
//     leaves them unset: a MoveToLevel without TransitionTime and a Move
//     without Rate apply at once.
//
// MoveToClosestFrequency (0x8) is likewise absent: conformance "FQ"
// (element :118-124). The eight commands this server does handle are all
// conformance "M" (element :72, :80, :88, :97, :101, :105, :109, :113),
// so they are accepted whatever the FeatureMap says.
//
// # The On/Off coupling
//
// Four of the eight commands are the "with On/Off" variants, which differ
// from their plain forms only in that they also drive the OnOff cluster
// on the same endpoint. Forwarded, they reach the host through their own
// port methods rather than through a flag on a shared call: a flag inside
// a request struct is a field a host implementation can leave unread and
// still compile, which turns "turn the speaker on and set it to 40" into
// a silent "set it to 40 while it stays muted". A separate method cannot
// be left unimplemented — the interface does not accept the host until
// each of the eight exists.
//
// Forwarded, the server does not decide whether a plain (non-On/Off)
// command executes while the device is off. That gate reads the OnOff
// attribute of a different cluster on the same endpoint
// (matter.js LevelControlServer.ts:729-736 #optionsAllowExecution), which
// the host owns. The arithmetic that *is* this cluster's — folding
// OptionsMask and OptionsOverride onto the Options attribute — is
// [EffectiveOptions], so a host applies the gate without re-deriving the
// bitmap rule. With the engine the host names its On/Off state in
// [Transitions.OnOff] and the server applies the gate and the coupling
// itself.
package levelcontrol

import (
	"context"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/transition"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/schema"
)

// ClusterID is the LevelControl cluster id
// (matter.js level-control.element.ts:19).
const ClusterID uint32 = 0x0008

// Attribute ids of the served attributes; the package doc names the gated
// and optional ones this server leaves out.
const (
	// AttrCurrentLevel is the level the device is at, access "R V",
	// quality "X N S Q" — the X is why an unobserved level reads as TLV
	// null rather than as 0, which is a real level
	// (element :29-30).
	AttrCurrentLevel uint32 = 0x0000
	// AttrOptions is the OptionsBitmap that sets the default behaviour of
	// the commands that consult it, access "RW VO" (element :65).
	AttrOptions uint32 = 0x000F
	// AttrMinLevel and AttrMaxLevel carry conformance "Rev >= v7, O"
	// (element :34-41): mandatory at the revision this server advertises.
	// matter.js sets both on every LevelControl (LevelControlServer.ts:
	// 114-120 initialize: "Spec 1.5.1 made minLevel/maxLevel mandatory at
	// rev 7"), to 0 (1 with LT) and 254. Found missing by the CHIP
	// conformance checker (TC-IDM-10.2).
	AttrMinLevel uint32 = 0x0002
	AttrMaxLevel uint32 = 0x0003
	// AttrOnLevel is the level CurrentLevel is set to when an On/Off
	// cluster on the same endpoint turns on, access "RW VO", quality "X"
	// — null means "no effect" (element :49-50, resource :144-153).
	AttrOnLevel uint32 = 0x0011
	// AttrRemainingTime is the time left in the current transition, in
	// tenths of a second, access "R V", quality "Q", conformance "LT"
	// (element :33). Served with [Config.Lighting].
	AttrRemainingTime uint32 = 0x0001
	// AttrStartUpCurrentLevel is the level the device takes at power-up,
	// access "RW VM", quality "X N", conformance "LT" (element :68-71).
	// Served with [Config.Lighting].
	AttrStartUpCurrentLevel uint32 = 0x4000
)

// Command ids, taken from the wire package so the repository carries one
// set. All eight are conformance "M" with response "status", so none of
// them produces a generated command
// (element :72, :80, :88, :97, :101, :105, :109, :113).
const (
	CmdMoveToLevel          = wire.LevelCtrlCmdMoveToLevel
	CmdMove                 = wire.LevelCtrlCmdMove
	CmdStep                 = wire.LevelCtrlCmdStep
	CmdStop                 = wire.LevelCtrlCmdStop
	CmdMoveToLevelWithOnOff = wire.LevelCtrlCmdMoveToLevelWithOnOff
	CmdMoveWithOnOff        = wire.LevelCtrlCmdMoveWithOnOff
	CmdStepWithOnOff        = wire.LevelCtrlCmdStepWithOnOff
	CmdStopWithOnOff        = wire.LevelCtrlCmdStopWithOnOff
)

// FeatureMap bits, named so a later projection has the verified bit
// positions rather than a guess. The element gives each one as a
// constraint, which is the bit index (element :24-26).
const (
	// FeatureOnOff is the OO bit — this cluster is coupled to an OnOff
	// cluster on the same endpoint. Conformance "O", default 1 (:24).
	FeatureOnOff uint32 = 1 << 0
	// FeatureLighting is the LT bit — the lighting profile, which adds
	// RemainingTime and StartUpCurrentLevel. Conformance "O" (:25).
	FeatureLighting uint32 = 1 << 1
	// FeatureFrequency is the FQ bit — a frequency axis alongside the
	// level one. Conformance "P" (:26).
	FeatureFrequency uint32 = 1 << 2
)

// featureMap is what this server advertises by default: OO alone.
//
// OO is the element's own default (default 1, :24) and it gates no
// attribute, so advertising it promises nothing this server does not
// serve — it says the four "with On/Off" commands act on a real OnOff
// cluster, which is exactly what the port's four WithOnOff methods do.
// LT is added with [Config.Lighting], together with the attributes it
// gates; FQ stays clear because this server serves none of its
// attributes.
const featureMap uint32 = FeatureOnOff

// OptionsBitmap bits (element :127-130). ExecuteIfOff carries
// conformance "LT | OO" and CoupleColorTempToLevel carries "LT".
const (
	// OptionExecuteIfOff, when set, lets a plain (non-On/Off) command run
	// even while the device is off (resource :190-199).
	OptionExecuteIfOff uint8 = 1 << 0
	// OptionCoupleColorTempToLevel, when set, moves the ColorControl
	// colour temperature along with the level. LT-gated: accepted with
	// [Config.Lighting] only.
	OptionCoupleColorTempToLevel uint8 = 1 << 1
)

// optionsSupportedMask is the set of Options bits that are conformant
// under [featureMap]: ExecuteIfOff exists under "LT | OO" and OO is
// advertised; CoupleColorTempToLevel needs LT (lightingOptionsMask). A
// write carrying any other bit is refused rather than stored, because a
// stored bit would be read back as a capability the cluster does not
// have.
const optionsSupportedMask = OptionExecuteIfOff

// lightingOptionsMask is optionsSupportedMask under OO | LT.
const lightingOptionsMask = OptionExecuteIfOff | OptionCoupleColorTempToLevel

// MoveMode values, taken from the wire package (MoveModeEnum, element
// :131-135).
const (
	MoveModeUp   = wire.LevelMoveModeUp
	MoveModeDown = wire.LevelMoveModeDown
)

// StepMode values, taken from the wire package (StepModeEnum, element
// :136-140).
const (
	StepModeUp   = wire.LevelStepModeUp
	StepModeDown = wire.LevelStepModeDown
)

// Level bounds applied to Level and OnLevel.
//
// LevelMax is the MoveToLevel Level constraint "max 254" (element :74)
// and the MaxLevel default (:38-41). LevelMin is MinLevel without the
// Lighting feature and LightingLevelMin with it: matter.js resolves an
// unset MinLevel to `this.features.lighting ? 1 : 0`
// (LevelControlServer.ts:89-90, :115-116).
const (
	LevelMin uint8 = 0
	LevelMax uint8 = 254
	// LightingLevelMin is MinLevel under the Lighting feature — the only
	// value LT allows (matter.js LevelControlServer.ts:205-220
	// initializeLighting warns on any other).
	LightingLevelMin uint8 = 1
)

// Command payload types. These are aliases, not copies: the bridge's
// command-fields reader already decodes MoveToLevel /
// MoveToLevelWithOnOff into [wire.MoveToLevelRequest]
// (bridge/fields_reader.go decodeMoveToLevelRequest), and cluster/wire
// carries the decoders for all four shapes. A second set of structs here
// would have to be kept in step with those by hand.
type (
	// MoveToLevelRequest carries MoveToLevel / MoveToLevelWithOnOff
	// (element :72-78).
	MoveToLevelRequest = wire.MoveToLevelRequest
	// MoveRequest carries Move / MoveWithOnOff (element :80-86).
	MoveRequest = wire.MoveRequest
	// StepRequest carries Step / StepWithOnOff (element :88-95).
	StepRequest = wire.StepRequest
	// StopRequest carries Stop / StopWithOnOff (element :97-100).
	StopRequest = wire.StopRequest
)

// LevelSource is the narrow host port this server reads and drives. The
// host implements it over whatever actually owns the level; nothing in
// this package caches or invents a value.
//
// CurrentLevel and OnLevel report (value, known). known=false becomes a
// TLV null, which is what quality X asks for on both (element :29-30,
// :49-50) — the spec's own way of saying the value is not known, as
// opposed to reporting level 0 for a device that has not answered yet.
// Options carries no X quality (element :65), so it has no unknown
// reading: the host names the bitmap a controller should see.
//
// The eight command methods own the southbound urgency of the writes
// they perform: the cluster contract carries no priority, so a host whose
// command queue ranks by urgency names the value it wants inside its own
// implementation, where the device vocabulary is in scope.
//
// The four WithOnOff methods exist separately from their plain forms on
// purpose — see the package doc. Implementing one by delegating to the
// other is a decision the host writes down; forgetting the coupling is
// not something it can do by accident.
//
// Under [Config.Transitions] the server calls MoveToLevel alone, with
// TransitionTime 0 and ExecuteIfOff forced, once per level it applies;
// the other seven command methods are not called.
type LevelSource interface {
	// CurrentLevel reports the level the device is at.
	CurrentLevel() (level uint8, known bool)
	// Options reports the stored OptionsBitmap.
	Options() (options uint8)
	// OnLevel reports the configured on-level.
	OnLevel() (level uint8, known bool)

	// SetOptions persists a bitmap the server has already checked
	// against the advertised FeatureMap.
	SetOptions(ctx context.Context, options uint8) error
	// SetOnLevel persists a new on-level; level is nil for a null write,
	// which is the spec's "OnLevel has no effect".
	SetOnLevel(ctx context.Context, level *uint8) error

	// MoveToLevel drives the device to req.Level without touching OnOff.
	MoveToLevel(ctx context.Context, req MoveToLevelRequest) error
	// MoveToLevelWithOnOff drives the device to req.Level and drives the
	// endpoint's OnOff state with it.
	MoveToLevelWithOnOff(ctx context.Context, req MoveToLevelRequest) error
	// Move starts a continuous move without touching OnOff.
	Move(ctx context.Context, req MoveRequest) error
	// MoveWithOnOff starts a continuous move and drives the endpoint's
	// OnOff state with it.
	MoveWithOnOff(ctx context.Context, req MoveRequest) error
	// Step applies one step without touching OnOff.
	Step(ctx context.Context, req StepRequest) error
	// StepWithOnOff applies one step and drives the endpoint's OnOff
	// state with it.
	StepWithOnOff(ctx context.Context, req StepRequest) error
	// Stop halts a running move or step without touching OnOff.
	Stop(ctx context.Context, req StopRequest) error
	// StopWithOnOff halts a running move or step and drives the
	// endpoint's OnOff state with it.
	StopWithOnOff(ctx context.Context, req StopRequest) error
}

// EffectiveOptions folds a command's OptionsMask and OptionsOverride
// onto the stored Options attribute and returns the temporary bitmap
// that is in effect for that command (resource :182-186).
//
// Per bit: a set mask bit takes the bit from override, a clear mask bit
// keeps the attribute's bit. Mirrors matter.js
// LevelControlServer.ts:714-727 #calculateEffectiveOptions
//
//	executeIfOff: optionsMask.executeIfOff ? optionsOverride.executeIfOff : options.executeIfOff
//
// The result is masked to [optionsSupportedMask] for the same reason a
// write is: a bit whose feature is not advertised has no meaning here.
//
// It is exported because the ExecuteIfOff gate itself belongs to the
// host — it reads the OnOff cluster's state, which this server cannot
// see — and the host should not have to restate the bitmap rule to
// apply it.
func EffectiveOptions(attribute, mask, override uint8) uint8 {
	return ((attribute &^ mask) | (override & mask)) & optionsSupportedMask
}

// Config carries the construction parameters for [Server].
type Config struct {
	// Source is the host port. A nil Source makes every attribute read
	// null and every command fail — the server never pretends to have
	// moved a device it cannot reach.
	Source LevelSource
	// DataVersion is an optional tracker owned by the host. When non-nil
	// the server bumps and reports the caller's counter, so a level the
	// device changed on its own reaches a subscriber the moment the host
	// bumps it, and the version survives server reconstruction; when nil
	// an embedded tracker is used.
	DataVersion *cluster.DataVersionTracker
	// Lighting advertises the LT feature: MinLevel 1, RemainingTime,
	// StartUpCurrentLevel and the CoupleColorTempToLevel option
	// (level-control.element.ts:25, :33, :68-71, :127-130). A light needs
	// it — every lighting device type marks LT mandatory. The server keeps
	// StartUpCurrentLevel itself and reports it, but never applies it: a
	// bridged endpoint has no power-up of its own, the reason matter.js
	// skips start-up behaviour on an endpoint an Aggregator owns
	// (LevelControlServer.ts initializeLighting).
	Lighting bool
	// Transitions, when non-nil, runs the module's transition engine
	// instead of forwarding the commands (see the package doc). Nil keeps
	// the hand-off path, where the host ramps natively and RemainingTime
	// reads 0.
	Transitions *Transitions
}

// Server implements [contract.ClusterServer] for LevelControl (0x0008).
type Server struct {
	src      LevelSource
	lighting bool

	embedded cluster.DataVersionTracker // used when Config.DataVersion is nil
	ext      *cluster.DataVersionTracker

	// The transition engine and what it couples to; engine is nil on the
	// hand-off path.
	engine *transition.Engine
	onOff  OnOff
	ct     ColorTemperatureCoupling
	// cmd serialises the commands the engine runs.
	cmd sync.Mutex
	// changes and quiet report CurrentLevel and RemainingTime under the
	// engine.
	changes cluster.AttributeChanges
	quiet   *cluster.Quieter

	mu sync.Mutex
	// startUp is StartUpCurrentLevel; nil is null.
	startUp *uint8
	// level / levelKnown is the CurrentLevel last seen, so a change is
	// reported once, by the quieter rules.
	level      uint8
	levelKnown bool
	// coupling is what the running transition couples to.
	coupling coupling
	// listeners counts the change listeners; the host's notifier is
	// subscribed while there is one.
	listeners int
	unsubSrc  func()
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*Server)(nil)
	_ contract.ClusterDataVersion             = (*Server)(nil)
	_ contract.ClusterAttributeLister         = (*Server)(nil)
	_ contract.ClusterCommandLister           = (*Server)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*Server)(nil)
	_ contract.ChangeNotifier                 = (*Server)(nil)
	_ contract.AttributeChangeNotifier        = (*Server)(nil)
	_ contract.SelfReportedAttributeLister    = (*Server)(nil)
	_ contract.ClusterQuiescer                = (*Server)(nil)
)

// NewServer constructs a LevelControl server over the host port in cfg.
func NewServer(cfg Config) *Server {
	s := &Server{src: cfg.Source, ext: cfg.DataVersion, lighting: cfg.Lighting}
	if cfg.Transitions != nil {
		s.initTransitions(*cfg.Transitions)
	}
	return s
}

// minLevel is MinLevel: 1 under the Lighting feature, 0 otherwise
// (matter.js LevelControlServer.ts:89-91).
func (s *Server) minLevel() uint8 {
	if s.lighting {
		return LightingLevelMin
	}
	return LevelMin
}

// optionsMask is the set of Options bits the advertised FeatureMap makes
// conformant.
func (s *Server) optionsMask() uint8 {
	if s.lighting {
		return lightingOptionsMask
	}
	return optionsSupportedMask
}

// features is the advertised FeatureMap.
func (s *Server) features() uint32 {
	if s.lighting {
		return featureMap | FeatureLighting
	}
	return featureMap
}

// tracker returns the active DataVersion counter — the host's when it
// supplied one, the embedded one otherwise.
func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// OnMatterValueChanged implements [contract.ChangeNotifier] by
// forwarding the host port's own notifier, bumping the DataVersion
// before each fire so the report the bridge ships carries the
// post-change version. CurrentLevel carries quality "N ... Q"
// (element :29-30) and is the attribute this server reports; without
// this hop a level the device changed by itself — a speaker turned up at
// the device — would reach a controller only on its next read. Mirrors
// the forwarding in cluster/modeselect/modeselect_server.go. Returns a
// no-op unsubscribe when the port cannot notify.
//
// Under the engine the server reports CurrentLevel itself (see
// [Server.OnMatterAttributesChanged]) and this hop stays silent.
func (s *Server) OnMatterValueChanged(cb func()) (unsubscribe func()) {
	n, ok := s.src.(contract.ChangeNotifier)
	if !ok || n == nil || s.engine != nil {
		return func() {}
	}
	return n.OnMatterValueChanged(func() {
		s.tracker().Bump()
		if cb != nil {
			cb()
		}
	})
}

// Revision returns the cluster revision from the generated matter.js
// schema snapshot (default 7 at level-control.element.ts:20). Reading it
// rather than restating it is the point: a regeneration moves this
// value, and a hand-written copy would not follow.
func Revision() uint16 { return schema.ClusterRevisions[ClusterID] }

// MatterClusterID returns 0x0008.
func (*Server) MatterClusterID() uint32 { return ClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterRead resolves the three conformance-M attributes plus the two
// universal globals every cluster server answers itself.
//
// With no host port all three read as TLV null. Only CurrentLevel and
// OnLevel carry quality X, so null is not the reading the spec asks for
// on Options — but the alternative is a bitmap the host never stated,
// and a value invented here is indistinguishable on the wire from one
// the host confirmed.
func (s *Server) MatterRead(attrID uint32) (value any, ok bool) {
	switch attrID {
	case AttrCurrentLevel, AttrOptions, AttrOnLevel:
		if s.src == nil {
			return nil, true
		}
		return s.readFromSource(attrID)
	case AttrMinLevel:
		return s.minLevel(), true
	case AttrMaxLevel:
		return LevelMax, true
	case AttrRemainingTime:
		if !s.lighting {
			return nil, false
		}
		return s.remainingTime(), true
	case AttrStartUpCurrentLevel:
		if !s.lighting {
			return nil, false
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.startUp == nil {
			return nil, true
		}
		return *s.startUp, true
	case cluster.AttrGlobalFeatureMap:
		return s.features(), true
	case cluster.AttrGlobalClusterRevision:
		return Revision(), true
	default:
		return nil, false
	}
}

// readFromSource projects one host-reported value onto the wire types
// the encoder expects: uint8 for the two level attributes and for the
// map8 bitmap. Only reached with a non-nil port.
func (s *Server) readFromSource(attrID uint32) (any, bool) {
	switch attrID {
	case AttrCurrentLevel:
		return nullableLevel(s.src.CurrentLevel())
	case AttrOnLevel:
		return nullableLevel(s.src.OnLevel())
	case AttrOptions:
		// Masked on the way out for the same reason a write is checked:
		// a bit whose feature is not advertised must not be readable as
		// though the cluster honoured it.
		return s.src.Options() & s.optionsMask(), true
	default:
		return nil, false
	}
}

// nullableLevel maps a host reading onto (value, true) or the TLV null
// the quality-X attributes carry when nothing has been observed.
func nullableLevel(level uint8, known bool) (any, bool) {
	if !known {
		return nil, true
	}
	return level, true
}

// MatterWrite applies the two writable attributes.
//
// Options and OnLevel carry access "RW VO" (element :65, :49);
// CurrentLevel is "R V" (:29) and is rejected as unwritable rather than
// silently dropped.
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	switch attrID {
	case AttrOptions:
		return s.writeOptions(ctx, value)
	case AttrOnLevel:
		return s.writeOnLevel(ctx, value)
	case AttrStartUpCurrentLevel:
		if s.lighting {
			return s.writeStartUpCurrentLevel(value)
		}
		return unsupportedAttributeErr{
			fmt.Sprintf("levelcontrol: attribute 0x%04X is not implemented by this server", attrID),
		}
	case AttrCurrentLevel:
		return unsupportedWriteErr{fmt.Sprintf("levelcontrol: attribute 0x%04X is read-only", attrID)}
	default:
		return unsupportedAttributeErr{
			fmt.Sprintf("levelcontrol: attribute 0x%04X is not implemented by this server", attrID),
		}
	}
}

// writeOptions validates and forwards an Options write. The bitmap
// carries constraint "desc" (element :65); what "desc" resolves to here
// is [optionsSupportedMask] — the bits whose conformance the advertised
// FeatureMap satisfies.
func (s *Server) writeOptions(ctx context.Context, value any) error {
	if value == nil {
		// Options carries no X quality (element :65), so null is not a
		// value it can hold.
		return constraintErr{"levelcontrol: Options is not nullable"}
	}
	options, ok := asUint8(value)
	if !ok {
		return constraintErr{fmt.Sprintf("levelcontrol: Options expected a map8, got %T", value)}
	}
	if options&^s.optionsMask() != 0 {
		return constraintErr{fmt.Sprintf(
			"levelcontrol: Options 0x%02X sets a bit that is not conformant under FeatureMap 0x%02X", options, s.features(),
		)}
	}
	if s.src == nil {
		return errNoSource("Options write")
	}
	if err := s.src.SetOptions(ctx, options); err != nil {
		return fmt.Errorf("levelcontrol: Options write: %w", err)
	}
	s.tracker().Bump()
	return nil
}

// writeOnLevel validates and forwards an OnLevel write. A null clears
// the on-level, which the spec reads as "it has no effect"
// (resource :146-149); any other value must satisfy constraint
// "minLevel to maxLevel" (element :49-50), resolved here to MinLevel
// ([LevelMin], or [LightingLevelMin] under LT) .. [LevelMax].
func (s *Server) writeOnLevel(ctx context.Context, value any) error {
	var level *uint8
	if value != nil {
		v, ok := asUint8(value)
		if !ok {
			return constraintErr{fmt.Sprintf("levelcontrol: OnLevel expected a number, got %T", value)}
		}
		if v < s.minLevel() || v > LevelMax {
			return constraintErr{fmt.Sprintf(
				"levelcontrol: OnLevel %d violates constraint minLevel to maxLevel (%d to %d)", v, s.minLevel(), LevelMax,
			)}
		}
		level = &v
	}
	if s.src == nil {
		return errNoSource("OnLevel write")
	}
	if err := s.src.SetOnLevel(ctx, level); err != nil {
		return fmt.Errorf("levelcontrol: OnLevel write: %w", err)
	}
	s.tracker().Bump()
	return nil
}

// writeStartUpCurrentLevel stores StartUpCurrentLevel: null, or a level
// the uint8 range allows up to [LevelMax] (0 is "the minimum level", 0xFF
// is the null of the wire).
func (s *Server) writeStartUpCurrentLevel(value any) error {
	var next *uint8
	if value != nil {
		v, ok := asUint8(value)
		if !ok || v > LevelMax {
			return constraintErr{fmt.Sprintf("levelcontrol: StartUpCurrentLevel %v is outside 0 to %d", value, LevelMax)}
		}
		next = &v
	}
	s.mu.Lock()
	s.startUp = next
	s.mu.Unlock()
	s.tracker().Bump()
	return nil
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// StartUpCurrentLevel is "RW VM" (element :68), Options and OnLevel
// "RW VO" (:65, :49).
func (*Server) MinWritePrivilege(attrID uint32) uint8 {
	if attrID == AttrStartUpCurrentLevel {
		return privilegeManage
	}
	return privilegeOperate
}

// Access-control privileges (Matter §9.10.5.2, AccessControlEntryPrivilegeEnum).
const (
	privilegeOperate uint8 = 3
	privilegeManage  uint8 = 4
)

// MatterInvoke dispatches the eight conformance-M commands. Forwarded,
// each one reaches the host port; none reports Success on its own, and a
// host refusal is returned as an error so the dispatcher maps it to a
// failure status rather than to Success. Under the engine the server runs
// the command (transitions.go) and the host receives the levels it steps
// through.
//
// The DataVersion is bumped only after the command succeeded: a refused
// command changed nothing a subscriber caches.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	switch cmdID {
	case CmdMoveToLevel, CmdMoveToLevelWithOnOff:
		return s.invokeMoveToLevel(ctx, cmdID, fields)
	case CmdMove, CmdMoveWithOnOff:
		return s.invokeMove(ctx, cmdID, fields)
	case CmdStep, CmdStepWithOnOff:
		return s.invokeStep(ctx, cmdID, fields)
	case CmdStop, CmdStopWithOnOff:
		return s.invokeStop(ctx, cmdID, fields)
	default:
		return nil, im.UnsupportedCommandf("levelcontrol: unknown command 0x%02X", cmdID)
	}
}

// invokeMoveToLevel handles MoveToLevel (0x0) and MoveToLevelWithOnOff
// (0x4), which share a payload (element :101, type "MoveToLevel").
func (s *Server) invokeMoveToLevel(ctx context.Context, cmdID uint32, fields any) (any, error) {
	name := commandName(cmdID)
	req, err := moveToLevelRequestFrom(name, fields)
	if err != nil {
		return nil, err
	}
	if req.Level > LevelMax {
		// constraint "max 254" (element :74).
		return nil, constraintErr{fmt.Sprintf("levelcontrol: %s Level %d violates constraint max %d",
			name, req.Level, LevelMax)}
	}
	if s.src == nil {
		return nil, errNoSource(name)
	}
	if s.engine != nil {
		return s.finish(name, s.runMoveToLevel(ctx, req, cmdID == CmdMoveToLevelWithOnOff))
	}
	call := s.src.MoveToLevel
	if cmdID == CmdMoveToLevelWithOnOff {
		call = s.src.MoveToLevelWithOnOff
	}
	return s.finish(name, call(ctx, req))
}

// invokeMove handles Move (0x1) and MoveWithOnOff (0x5), which share a
// payload (element :105, type "Move").
func (s *Server) invokeMove(ctx context.Context, cmdID uint32, fields any) (any, error) {
	name := commandName(cmdID)
	req, err := moveRequestFrom(name, fields)
	if err != nil {
		return nil, err
	}
	if err := checkMoveMode(name, req.MoveMode); err != nil {
		return nil, err
	}
	if req.Rate != nil && *req.Rate == 0 {
		// matter.js LevelControlServer.ts:305-308 #assertRateValue:
		// `Illegal move rate of 0`, Status.InvalidCommand.
		return nil, invalidCommandErr{fmt.Sprintf("levelcontrol: %s carries an illegal move rate of 0", name)}
	}
	if s.src == nil {
		return nil, errNoSource(name)
	}
	if s.engine != nil {
		return s.finish(name, s.runMove(ctx, req, cmdID == CmdMoveWithOnOff))
	}
	call := s.src.Move
	if cmdID == CmdMoveWithOnOff {
		call = s.src.MoveWithOnOff
	}
	return s.finish(name, call(ctx, req))
}

// invokeStep handles Step (0x2) and StepWithOnOff (0x6), which share a
// payload (element :109, type "Step").
func (s *Server) invokeStep(ctx context.Context, cmdID uint32, fields any) (any, error) {
	name := commandName(cmdID)
	req, err := stepRequestFrom(name, fields)
	if err != nil {
		return nil, err
	}
	if err := checkStepMode(name, req.StepMode); err != nil {
		return nil, err
	}
	if s.src == nil {
		return nil, errNoSource(name)
	}
	if s.engine != nil {
		return s.finish(name, s.runStep(ctx, req, cmdID == CmdStepWithOnOff))
	}
	call := s.src.Step
	if cmdID == CmdStepWithOnOff {
		call = s.src.StepWithOnOff
	}
	return s.finish(name, call(ctx, req))
}

// invokeStop handles Stop (0x3) and StopWithOnOff (0x7), which share a
// payload (element :113, type "Stop").
func (s *Server) invokeStop(ctx context.Context, cmdID uint32, fields any) (any, error) {
	name := commandName(cmdID)
	req, err := stopRequestFrom(name, fields)
	if err != nil {
		return nil, err
	}
	if s.src == nil {
		return nil, errNoSource(name)
	}
	if s.engine != nil {
		return s.finish(name, s.runStop(req, cmdID == CmdStopWithOnOff))
	}
	call := s.src.Stop
	if cmdID == CmdStopWithOnOff {
		call = s.src.StopWithOnOff
	}
	return s.finish(name, call(ctx, req))
}

// finish turns a port result into the (response, error) pair the
// dispatcher expects. A refusal keeps the DataVersion where it was.
func (s *Server) finish(name string, err error) (any, error) {
	if err != nil {
		return nil, fmt.Errorf("levelcontrol: %s: %w", name, err)
	}
	s.tracker().Bump()
	return nil, nil
}

// commandName renders a command id for error messages. Only the eight
// ids this server dispatches reach it.
func commandName(cmdID uint32) string {
	switch cmdID {
	case CmdMoveToLevel:
		return "MoveToLevel"
	case CmdMove:
		return "Move"
	case CmdStep:
		return "Step"
	case CmdStop:
		return "Stop"
	case CmdMoveToLevelWithOnOff:
		return "MoveToLevelWithOnOff"
	case CmdMoveWithOnOff:
		return "MoveWithOnOff"
	case CmdStepWithOnOff:
		return "StepWithOnOff"
	case CmdStopWithOnOff:
		return "StopWithOnOff"
	default:
		return fmt.Sprintf("command 0x%02X", cmdID)
	}
}

// checkMoveMode refuses a MoveMode outside MoveModeEnum. The field
// carries constraint "desc" (element :82) and the enum defines exactly
// Up (0x0) and Down (0x1) (element :131-135), so a third value names no
// direction — forwarding it would let a host pick one.
func checkMoveMode(name string, mode uint8) error {
	if mode != MoveModeUp && mode != MoveModeDown {
		return constraintErr{fmt.Sprintf("levelcontrol: %s MoveMode %d is not in MoveModeEnum", name, mode)}
	}
	return nil
}

// checkStepMode mirrors [checkMoveMode] for StepModeEnum
// (element :90, :136-140).
func checkStepMode(name string, mode uint8) error {
	if mode != StepModeUp && mode != StepModeDown {
		return constraintErr{fmt.Sprintf("levelcontrol: %s StepMode %d is not in StepModeEnum", name, mode)}
	}
	return nil
}

// moveToLevelRequestFrom normalises the payload the bridge hands over.
//
// Three shapes arrive in practice. The bridge decodes MoveToLevel and
// MoveToLevelWithOnOff into the typed request already
// (bridge/fields_reader.go commandFieldsReader, case 0x0008), every
// other LevelControl command falls through to the generic tag map its
// fields reader salvages, and a host holding the raw command payload can
// pass the bytes — which go through [wire.DecodeMoveToLevel] rather than
// through a second parser written here.
func moveToLevelRequestFrom(name string, fields any) (MoveToLevelRequest, error) {
	switch v := fields.(type) {
	case nil:
		return MoveToLevelRequest{}, missingFields(name)
	case MoveToLevelRequest:
		return v, nil
	case *MoveToLevelRequest:
		if v == nil {
			return MoveToLevelRequest{}, missingFields(name)
		}
		return *v, nil
	case []byte:
		req, err := wire.DecodeMoveToLevel(v)
		if err != nil {
			return req, invalidCommandErr{fmt.Sprintf("levelcontrol: %s: %v", name, err)}
		}
		return req, nil
	case map[uint8]any:
		return moveToLevelFromTagMap(name, v)
	default:
		return MoveToLevelRequest{}, unexpectedFields(name, "MoveToLevelRequest", fields)
	}
}

// moveToLevelFromTagMap reads the four MoveToLevel fields out of the
// generic tag map (element :73-77).
func moveToLevelFromTagMap(name string, m map[uint8]any) (MoveToLevelRequest, error) {
	var req MoveToLevelRequest
	level, err := requiredUint8(name, m, 0, "Level")
	if err != nil {
		return req, err
	}
	req.Level = level
	if req.TransitionTime, err = nullableUint16(name, m, 1, "TransitionTime"); err != nil {
		return req, err
	}
	if req.OptionsMask, err = optionalBitmap(name, m, 2, "OptionsMask"); err != nil {
		return req, err
	}
	req.OptionsOverride, err = optionalBitmap(name, m, 3, "OptionsOverride")
	return req, err
}

// moveRequestFrom mirrors [moveToLevelRequestFrom] for Move /
// MoveWithOnOff, reusing [wire.DecodeMove] for the raw-payload shape.
func moveRequestFrom(name string, fields any) (MoveRequest, error) {
	switch v := fields.(type) {
	case nil:
		return MoveRequest{}, missingFields(name)
	case MoveRequest:
		return v, nil
	case *MoveRequest:
		if v == nil {
			return MoveRequest{}, missingFields(name)
		}
		return *v, nil
	case []byte:
		req, err := wire.DecodeMove(v)
		if err != nil {
			return req, invalidCommandErr{fmt.Sprintf("levelcontrol: %s: %v", name, err)}
		}
		return req, nil
	case map[uint8]any:
		return moveFromTagMap(name, v)
	default:
		return MoveRequest{}, unexpectedFields(name, "MoveRequest", fields)
	}
}

// moveFromTagMap reads the four Move fields out of the generic tag map
// (element :81-85).
func moveFromTagMap(name string, m map[uint8]any) (MoveRequest, error) {
	var req MoveRequest
	mode, err := requiredUint8(name, m, 0, "MoveMode")
	if err != nil {
		return req, err
	}
	req.MoveMode = mode
	if req.Rate, err = nullableUint8(name, m, 1, "Rate"); err != nil {
		return req, err
	}
	if req.OptionsMask, err = optionalBitmap(name, m, 2, "OptionsMask"); err != nil {
		return req, err
	}
	req.OptionsOverride, err = optionalBitmap(name, m, 3, "OptionsOverride")
	return req, err
}

// stepRequestFrom mirrors [moveToLevelRequestFrom] for Step /
// StepWithOnOff, reusing [wire.DecodeStep] for the raw-payload shape.
func stepRequestFrom(name string, fields any) (StepRequest, error) {
	switch v := fields.(type) {
	case nil:
		return StepRequest{}, missingFields(name)
	case StepRequest:
		return v, nil
	case *StepRequest:
		if v == nil {
			return StepRequest{}, missingFields(name)
		}
		return *v, nil
	case []byte:
		req, err := wire.DecodeStep(v)
		if err != nil {
			return req, invalidCommandErr{fmt.Sprintf("levelcontrol: %s: %v", name, err)}
		}
		return req, nil
	case map[uint8]any:
		return stepFromTagMap(name, v)
	default:
		return StepRequest{}, unexpectedFields(name, "StepRequest", fields)
	}
}

// stepFromTagMap reads the five Step fields out of the generic tag map
// (element :89-94).
func stepFromTagMap(name string, m map[uint8]any) (StepRequest, error) {
	var req StepRequest
	mode, err := requiredUint8(name, m, 0, "StepMode")
	if err != nil {
		return req, err
	}
	req.StepMode = mode
	if req.StepSize, err = requiredUint8(name, m, 1, "StepSize"); err != nil {
		return req, err
	}
	if req.TransitionTime, err = nullableUint16(name, m, 2, "TransitionTime"); err != nil {
		return req, err
	}
	if req.OptionsMask, err = optionalBitmap(name, m, 3, "OptionsMask"); err != nil {
		return req, err
	}
	req.OptionsOverride, err = optionalBitmap(name, m, 4, "OptionsOverride")
	return req, err
}

// stopRequestFrom mirrors [moveToLevelRequestFrom] for Stop /
// StopWithOnOff, reusing [wire.DecodeStop] for the raw-payload shape.
func stopRequestFrom(name string, fields any) (StopRequest, error) {
	switch v := fields.(type) {
	case nil:
		return StopRequest{}, missingFields(name)
	case StopRequest:
		return v, nil
	case *StopRequest:
		if v == nil {
			return StopRequest{}, missingFields(name)
		}
		return *v, nil
	case []byte:
		req, err := wire.DecodeStop(v)
		if err != nil {
			return req, invalidCommandErr{fmt.Sprintf("levelcontrol: %s: %v", name, err)}
		}
		return req, nil
	case map[uint8]any:
		return stopFromTagMap(name, v)
	default:
		return StopRequest{}, unexpectedFields(name, "StopRequest", fields)
	}
}

// stopFromTagMap reads the two Stop fields out of the generic tag map
// (element :98-99).
func stopFromTagMap(name string, m map[uint8]any) (StopRequest, error) {
	var req StopRequest
	var err error
	if req.OptionsMask, err = optionalBitmap(name, m, 0, "OptionsMask"); err != nil {
		return req, err
	}
	req.OptionsOverride, err = optionalBitmap(name, m, 1, "OptionsOverride")
	return req, err
}

// requiredUint8 reads a mandatory, non-nullable uint8 field. An absent
// or null tag is a malformed command rather than a zero value, because
// zero is a legal Level, MoveMode and StepMode.
func requiredUint8(name string, m map[uint8]any, tag uint8, field string) (uint8, error) {
	raw, present := m[tag]
	if !present || raw == nil {
		return 0, invalidCommandErr{fmt.Sprintf("levelcontrol: %s is missing the mandatory %s field", name, field)}
	}
	v, ok := asUint8(raw)
	if !ok {
		return 0, constraintErr{fmt.Sprintf("levelcontrol: %s %s is %T, want a uint8", name, field, raw)}
	}
	return v, nil
}

// nullableUint8 reads a mandatory but quality-X uint8 field. An absent
// tag is read as null, mirroring bridge/fields_reader.go
// decodeMoveToLevelRequest, which leaves an absent nullable field nil.
func nullableUint8(name string, m map[uint8]any, tag uint8, field string) (*uint8, error) {
	raw, present := m[tag]
	if !present || raw == nil {
		return nil, nil
	}
	v, ok := asUint8(raw)
	if !ok {
		return nil, constraintErr{fmt.Sprintf("levelcontrol: %s %s is %T, want a uint8", name, field, raw)}
	}
	return &v, nil
}

// nullableUint16 mirrors [nullableUint8] for the uint16 TransitionTime
// fields.
func nullableUint16(name string, m map[uint8]any, tag uint8, field string) (*uint16, error) {
	raw, present := m[tag]
	if !present || raw == nil {
		return nil, nil
	}
	v, ok := asUint16(raw)
	if !ok {
		return nil, constraintErr{fmt.Sprintf("levelcontrol: %s %s is %T, want a uint16", name, field, raw)}
	}
	return &v, nil
}

// optionalBitmap reads an OptionsMask / OptionsOverride field. Both are
// conformance M, but an absent bitmap means "override nothing", which is
// the same as the zero value — so absence is accepted rather than
// refused.
func optionalBitmap(name string, m map[uint8]any, tag uint8, field string) (uint8, error) {
	raw, present := m[tag]
	if !present || raw == nil {
		return 0, nil
	}
	v, ok := asUint8(raw)
	if !ok {
		return 0, constraintErr{fmt.Sprintf("levelcontrol: %s %s is %T, want a map8", name, field, raw)}
	}
	return v, nil
}

// asUint8 narrows a decoded wire value to the uint8 a level, enum or
// map8 field carries. The bridge surfaces unsigned TLV integers as
// uint64 and signed ones as int64 (bridge/attribute_value_reader.go
// primitiveAttributeValue), so a strict assertion on uint8 would reject
// every value that actually arrives. Out-of-range and negative values
// are rejected rather than wrapped — [cluster.AsUint8] narrows silently,
// which would turn level 300 into level 44.
func asUint8(v any) (uint8, bool) {
	n, ok := asInt64(v)
	if !ok || n < 0 || n > 0xFF {
		return 0, false
	}
	return uint8(n), true //nolint:gosec // range-checked against 0 and uint8 max above
}

// asUint16 mirrors [asUint8] for the uint16 TransitionTime fields.
func asUint16(v any) (uint16, bool) {
	n, ok := asInt64(v)
	if !ok || n < 0 || n > 0xFFFF {
		return 0, false
	}
	return uint16(n), true //nolint:gosec // range-checked against 0 and uint16 max above
}

// asInt64 funnels every numeric shape the decoders produce to int64 so
// the two narrowing helpers share one range check.
func asInt64(v any) (int64, bool) {
	const maxInt64 = uint64(1)<<63 - 1
	switch x := v.(type) {
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		if x > maxInt64 {
			return 0, false
		}
		return int64(x), true //nolint:gosec // range-checked against int64 max on the line above
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	default:
		return 0, false
	}
}

// missingFields reports an invocation that carried no payload at all.
// Every one of the eight commands has at least one conformance-M field,
// so nil is malformed rather than "take the defaults".
func missingFields(name string) error {
	return invalidCommandErr{fmt.Sprintf("levelcontrol: %s carried no fields", name)}
}

// unexpectedFields reports a payload shape this server cannot read.
func unexpectedFields(name, want string, got any) error {
	return invalidCommandErr{fmt.Sprintf(
		"levelcontrol: %s expected %s, []byte or map[uint8]any, got %T", name, want, got,
	)}
}

// errNoSource reports that the server has no host port to reach.
func errNoSource(what string) error {
	return fmt.Errorf("levelcontrol: %s has no host port", what)
}

// MatterReportable lists the attributes that move while the device runs
// and that the host's change notification reports. Options and OnLevel
// are absent: both change only through a write, which already bumps the
// DataVersion the dispatcher reports. Under the engine the list is
// empty: the server reports CurrentLevel and RemainingTime itself
// ([Server.MatterSelfReportedAttributes]).
func (s *Server) MatterReportable() []uint32 {
	if s.engine != nil {
		return []uint32{}
	}
	return []uint32{AttrCurrentLevel}
}

// MatterAttributes implements [contract.ClusterAttributeLister], in id
// order and without the universal globals — the dispatcher merges those.
func (s *Server) MatterAttributes() []uint32 {
	if s.lighting {
		return []uint32{
			AttrCurrentLevel, AttrRemainingTime, AttrMinLevel, AttrMaxLevel,
			AttrOptions, AttrOnLevel, AttrStartUpCurrentLevel,
		}
	}
	return []uint32{AttrCurrentLevel, AttrMinLevel, AttrMaxLevel, AttrOptions, AttrOnLevel}
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister], in
// command-id order.
func (*Server) MatterAcceptedCommands() []uint32 {
	return []uint32{
		CmdMoveToLevel, CmdMove, CmdStep, CmdStop,
		CmdMoveToLevelWithOnOff, CmdMoveWithOnOff, CmdStepWithOnOff, CmdStopWithOnOff,
	}
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
// All eight commands declare response "status", so the server emits no
// command payloads.
func (*Server) MatterGeneratedCommands() []uint32 { return nil }

// constraintErr maps onto the Matter ConstraintError status.
type constraintErr struct{ msg string }

func (e constraintErr) Error() string                 { return e.msg }
func (constraintErr) MatterStatusCode() im.StatusCode { return im.StatusConstraintError }

// unsupportedWriteErr maps onto UnsupportedWrite for an attribute that
// exists but is read-only.
type unsupportedWriteErr struct{ msg string }

func (e unsupportedWriteErr) Error() string                 { return e.msg }
func (unsupportedWriteErr) MatterStatusCode() im.StatusCode { return im.StatusUnsupportedWrite }

// unsupportedAttributeErr maps onto UnsupportedAttribute for an id this
// server does not carry.
type unsupportedAttributeErr struct{ msg string }

func (e unsupportedAttributeErr) Error() string { return e.msg }
func (unsupportedAttributeErr) MatterStatusCode() im.StatusCode {
	return im.StatusUnsupportedAttribute
}

// invalidCommandErr maps onto InvalidCommand for a malformed or
// unacceptable command payload.
type invalidCommandErr struct{ msg string }

func (e invalidCommandErr) Error() string                 { return e.msg }
func (invalidCommandErr) MatterStatusCode() im.StatusCode { return im.StatusInvalidCommand }

// Compile-time assertions for the typed status carriers.
var (
	_ im.StatusCodeError = constraintErr{}
	_ im.StatusCodeError = unsupportedWriteErr{}
	_ im.StatusCodeError = unsupportedAttributeErr{}
	_ im.StatusCodeError = invalidCommandErr{}
)
