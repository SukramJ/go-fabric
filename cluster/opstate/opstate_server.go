// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package opstate contains the Matter OperationalState cluster server
// (0x0060) and its derived RvcOperationalState (0x0061): the state
// machine of a laundry washer, laundry dryer or dishwasher, and of a
// robotic vacuum cleaner.
//
// One [Server] serves both. [NewServer] builds OperationalState,
// [NewRvcServer] RvcOperationalState; the derivation only widens the
// state and error enums, swaps Start / Stop for GoHome and tightens which
// state Pause and Resume accept — exactly the parts matter.js's
// OperationalStateUtils parameterises (assertPause / assertRvcPause,
// assertResume / assertRvcResume, assertRvcGoHome).
//
// The server holds the cluster's state, as matter.js's
// OperationalStateServer does (packages/node/src/behaviors/
// operational-state/OperationalStateServer.ts); the host moves it through
// the setters, which enforce what that server's reactors enforce:
//
//   - OperationalStateList must contain Error (#assertOperationalStateList);
//   - OperationalState must be a listed state (#assertOperationalState);
//     leaving Error clears OperationalError to NoError;
//   - a non-NoError OperationalError sets OperationalState to Error and
//     emits the OperationalError event (#handleOperationalError);
//   - CurrentPhase is null exactly when PhaseList is null or empty, and
//     otherwise an index into it (#syncCurrentPhaseWithPhaseList,
//     #assertCurrentPhase).
//
// A setter that breaks one of these returns an error and changes nothing,
// where matter.js throws an ImplementationError.
//
// Commands reach the host through [CommandHandler] after the server has
// applied matter.js's state checks (OperationalStateUtils) and the
// specification's "already in that state" rule; the host performs the
// command on the device, moves OperationalState itself through
// [Server.SetOperationalState], and answers with the ErrorState the
// response carries. matter.js leaves the commands to the device the same
// way: OperationalStateServer implements none of them.
//
// CountdownTime has quality Q. Its changes are reported as matter.js
// reports a quieter attribute (see [cluster.Quieter]); every other
// attribute change is reported at once, through
// [contract.AttributeChangeNotifier].
//
// The clusters' identity is their generated definitions
// (cluster/spec/operationalstate, cluster/spec/rvcoperationalstate, ADR
// 0013): ids, revisions, the state and error enums each derivation
// defines, the attribute, command and event lists, the event priorities
// and the write statuses. The reactors, the command checks and the
// cluster/wire payloads the bridge encodes stay here.
package opstate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	rvcdef "github.com/SukramJ/go-fabric/cluster/spec/rvcoperationalstate"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// Cluster ids.
const (
	ClusterIDOperationalState    = opdef.ClusterID
	ClusterIDRvcOperationalState = rvcdef.ClusterID
)

// Device types built on these clusters (laundry-washer.element.ts,
// robotic-vacuum-cleaner.element.ts, dishwasher.element.ts,
// laundry-dryer.element.ts). Each mandates its OperationalState cluster
// with the OperationCompletion event.
const (
	DeviceTypeLaundryWasher        uint16 = 0x0073
	DeviceTypeRoboticVacuumCleaner uint16 = 0x0074
	DeviceTypeDishwasher           uint16 = 0x0075
	DeviceTypeLaundryDryer         uint16 = 0x007C
)

// Attribute ids (operational-state.element.ts:23-44).
const (
	AttrPhaseList            = opdef.AttrPhaseList            // M, list[string max 64] max 32, X
	AttrCurrentPhase         = opdef.AttrCurrentPhase         // M, uint8, X
	AttrCountdownTime        = opdef.AttrCountdownTime        // O, elapsed-s max 259200, X Q
	AttrOperationalStateList = opdef.AttrOperationalStateList // M, list[OperationalStateStruct]
	AttrOperationalState     = opdef.AttrOperationalState     // M, OperationalStateEnum
	AttrOperationalError     = opdef.AttrOperationalError     // M, ErrorStateStruct
)

// Command ids.
const (
	CmdPause                      = opdef.CmdPause
	CmdStop                       = opdef.CmdStop
	CmdStart                      = opdef.CmdStart
	CmdResume                     = opdef.CmdResume
	CmdOperationalCommandResponse = opdef.CmdOperationalCommandResponse
	CmdGoHome                     = rvcdef.CmdGoHome
)

// Event ids (operational-state.element.ts:45-55).
const (
	EventOperationalError    = opdef.EventOperationalError    // M, critical
	EventOperationCompletion = opdef.EventOperationCompletion // O, info
)

// Limits from the element's constraints.
const (
	// PhaseListMaxEntries / PhaseMaxBytes: PhaseList "max 32[max 64]".
	PhaseListMaxEntries = 32
	PhaseMaxBytes       = 64
	// LabelMaxBytes: OperationalStateLabel, ErrorStateLabel and
	// ErrorStateDetails "max 64".
	LabelMaxBytes = 64
	// CountdownTimeMax: CountdownTime "max 259200".
	CountdownTimeMax uint32 = 259200
)

// State is an OperationalStateEnum value. RvcOperationalState adds the
// 0x40 range; 0x80-0xBF are manufacturer-specific states, which carry a
// label.
type State uint8

// OperationalStateEnum (operational-state.element.ts:82-88,
// rvc-operational-state.element.ts:31-44).
const (
	StateStopped          = State(opdef.OperationalStateStopped)
	StateRunning          = State(opdef.OperationalStateRunning)
	StatePaused           = State(opdef.OperationalStatePaused)
	StateError            = State(opdef.OperationalStateError)
	StateSeekingCharger   = State(rvcdef.OperationalStateSeekingCharger)   // RVC
	StateCharging         = State(rvcdef.OperationalStateCharging)         // RVC
	StateDocked           = State(rvcdef.OperationalStateDocked)           // RVC
	StateEmptyingDustBin  = State(rvcdef.OperationalStateEmptyingDustBin)  // RVC, O
	StateCleaningMop      = State(rvcdef.OperationalStateCleaningMop)      // RVC, O
	StateFillingWaterTank = State(rvcdef.OperationalStateFillingWaterTank) // RVC, O
	StateUpdatingMaps     = State(rvcdef.OperationalStateUpdatingMaps)     // RVC, O
)

// ErrorID is an ErrorStateEnum value. RvcOperationalState adds the 0x40
// range; 0x80-0xBF are manufacturer-specific errors, which carry a label.
type ErrorID uint8

// ErrorStateEnum (operational-state.element.ts:99-105,
// rvc-operational-state.element.ts:46-66).
const (
	ErrorNoError                   = ErrorID(opdef.ErrorStateNoError)
	ErrorUnableToStartOrResume     = ErrorID(opdef.ErrorStateUnableToStartOrResume)
	ErrorUnableToCompleteOperation = ErrorID(opdef.ErrorStateUnableToCompleteOperation)
	ErrorCommandInvalidInState     = ErrorID(opdef.ErrorStateCommandInvalidInState)
	ErrorFailedToFindChargingDock  = ErrorID(rvcdef.ErrorStateFailedToFindChargingDock) // RVC
	ErrorStuck                     = ErrorID(rvcdef.ErrorStateStuck)                    // RVC
	ErrorDustBinMissing            = ErrorID(rvcdef.ErrorStateDustBinMissing)           // RVC
	ErrorDustBinFull               = ErrorID(rvcdef.ErrorStateDustBinFull)              // RVC
	ErrorWaterTankEmpty            = ErrorID(rvcdef.ErrorStateWaterTankEmpty)           // RVC
	ErrorWaterTankMissing          = ErrorID(rvcdef.ErrorStateWaterTankMissing)         // RVC
	ErrorWaterTankLidOpen          = ErrorID(rvcdef.ErrorStateWaterTankLidOpen)         // RVC
	ErrorMopCleaningPadMissing     = ErrorID(rvcdef.ErrorStateMopCleaningPadMissing)    // RVC
	ErrorLowBattery                = ErrorID(rvcdef.ErrorStateLowBattery)               // RVC
	ErrorCannotReachTargetArea     = ErrorID(rvcdef.ErrorStateCannotReachTargetArea)    // RVC
	ErrorDirtyWaterTankFull        = ErrorID(rvcdef.ErrorStateDirtyWaterTankFull)       // RVC
	ErrorDirtyWaterTankMissing     = ErrorID(rvcdef.ErrorStateDirtyWaterTankMissing)    // RVC
	ErrorWheelsJammed              = ErrorID(rvcdef.ErrorStateWheelsJammed)             // RVC
	ErrorBrushJammed               = ErrorID(rvcdef.ErrorStateBrushJammed)              // RVC
	ErrorNavigationSensorObscured  = ErrorID(rvcdef.ErrorStateNavigationSensorObscured) // RVC
)

// StateEntry is one OperationalStateList entry. Label is sent for a
// manufacturer-specific id (0x80-0xBF) only.
type StateEntry struct {
	ID    State
	Label string
}

// ErrorState is the ErrorStateStruct: the OperationalError attribute and
// the state a command response reports. Label is sent for a
// manufacturer-specific id only; Details is optional and sent when set.
type ErrorState struct {
	ID      ErrorID
	Label   string
	Details string
}

func (e ErrorState) wire() clusterwire.ErrorStateStruct {
	return clusterwire.ErrorStateStruct{ErrorStateID: uint8(e.ID), ErrorStateLabel: e.Label, ErrorStateDetails: e.Details}
}

// OperationCompletion is the OperationCompletion event the host raises
// when an operation ends: Code is the ErrorStateEnum result (NoError for
// success); the two times are optional, nil leaves them out.
type OperationCompletion struct {
	Code                 ErrorID
	TotalOperationalTime *clusterwire.ElapsedS
	PausedTime           *clusterwire.ElapsedS
}

// Command names the cluster's request commands, as a set.
type Command uint8

// Commands. GoHome exists on RvcOperationalState only; Start and Stop on
// OperationalState only (conformance "X" in the derivation).
const (
	CommandPause Command = 1 << iota
	CommandStop
	CommandStart
	CommandResume
	CommandGoHome
)

// CommandHandler is the host port: it performs an accepted command on the
// device. The server has already refused what matter.js refuses
// (CommandInvalidInState) and answered a command that finds the device
// in its target state already; what reaches the handler is a command the
// device has to act on. On success the handler moves OperationalState
// through [Server.SetOperationalState] — Paused, Stopped, Running, the
// state before the pause, or SeekingCharger — and returns NoError; a
// device that cannot comply returns the ErrorState the response reports
// (CommandInvalidInState, UnableToStartOrResume, …). A Go error answers
// the invoke with FAILURE.
type CommandHandler interface {
	HandleOperationalCommand(ctx context.Context, cmd Command) (ErrorState, error)
}

// Config carries the construction parameters for [NewServer] and
// [NewRvcServer].
type Config struct {
	// Handler takes the commands in Commands; required when Commands is
	// not empty.
	Handler CommandHandler
	// Commands are the commands the device accepts. Pause and Resume come
	// together ("Resume, O" / "Pause, O"), Start needs Stop
	// ("Start, O").
	Commands Command
	// States is OperationalStateList. It must contain Error, and the
	// states the commands lead to: Paused for Pause, Running for Resume
	// and Start, Stopped for Stop, SeekingCharger for GoHome.
	States []StateEntry
	// State is the initial OperationalState, a listed state.
	State State
	// Phases is the initial PhaseList; nil is null. CurrentPhase is the
	// initial index into it, nil (null) when Phases is null or empty.
	Phases       []string
	CurrentPhase *uint8
	// CountdownTime serves the optional CountdownTime attribute, null
	// until [Server.SetCountdownTime].
	CountdownTime bool
	// OperationCompletion declares the optional OperationCompletion
	// event. The four appliance device types make it mandatory, and a
	// server built with one of them as DeviceType declares it anyway.
	OperationCompletion bool
	// DeviceType is the endpoint's device type, when the host wants it
	// checked against the cluster; zero skips the check.
	DeviceType uint16
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Configuration and setter errors. A setter error leaves the state as it
// was, as a matter.js ImplementationError rolls the transaction back.
var (
	ErrNoHandler           = errors.New("opstate: commands need a CommandHandler")
	ErrCommandNotAllowed   = errors.New("opstate: command not defined for this cluster")
	ErrPauseResume         = errors.New("opstate: Pause and Resume are supported together")
	ErrStartNeedsStop      = errors.New("opstate: Start needs Stop")
	ErrNoErrorState        = errors.New("opstate: operational state list must at least contain an error entry")
	ErrStateMissing        = errors.New("opstate: a supported command's target state is not in the operational state list")
	ErrDuplicateState      = errors.New("opstate: operational state listed twice")
	ErrUnknownState        = errors.New("opstate: not an operational state of this cluster")
	ErrStateNotListed      = errors.New("opstate: operational state is not in the operational state list")
	ErrUnknownError        = errors.New("opstate: not an error state of this cluster")
	ErrTooLong             = errors.New("opstate: string exceeds 64 bytes")
	ErrPhaseList           = errors.New("opstate: phase list exceeds 32 entries")
	ErrCurrentPhase        = errors.New("opstate: current phase does not fit the phase list")
	ErrCountdownTime       = errors.New("opstate: countdown time is not served or exceeds 259200")
	ErrDeviceType          = errors.New("opstate: device type does not use this cluster")
	ErrEventNotDeclared    = errors.New("opstate: OperationCompletion is not declared")
	ErrInvalidHandlerReply = errors.New("opstate: the command handler answered an undefined error state")
)

// variant is what distinguishes OperationalState from its RVC derivation.
type variant struct {
	def         *spec.Cluster
	commands    Command
	states      *spec.Enum // the derivation's OperationalStateEnum
	errors      *spec.Enum // the derivation's ErrorStateEnum
	deviceTypes []uint16
	// pause / resume / goHome return CommandInvalidInState for a state
	// the command is not valid in, NoError otherwise — matter.js
	// OperationalStateUtils.
	pause, resume, goHome func(State) ErrorID
}

var (
	baseVariant = variant{
		def:         opdef.Definition,
		commands:    CommandPause | CommandStop | CommandStart | CommandResume,
		states:      opdef.OperationalStateEnumDef,
		errors:      opdef.ErrorStateEnumDef,
		deviceTypes: []uint16{DeviceTypeLaundryWasher, DeviceTypeDishwasher, DeviceTypeLaundryDryer},
		pause:       assertPause,
		resume:      assertResume,
	}
	rvcVariant = variant{
		def:         rvcdef.Definition,
		commands:    CommandPause | CommandResume | CommandGoHome,
		states:      rvcdef.OperationalStateEnumDef,
		errors:      rvcdef.ErrorStateEnumDef,
		deviceTypes: []uint16{DeviceTypeRoboticVacuumCleaner},
		pause:       assertRvcPause,
		resume:      assertRvcResume,
		goHome:      assertRvcGoHome,
	}
)

// assertPause mirrors OperationalStateUtils.assertPause: Pause is invalid
// in Stopped and Error.
func assertPause(st State) ErrorID {
	if st == StateStopped || st == StateError {
		return ErrorCommandInvalidInState
	}
	return ErrorNoError
}

// assertRvcPause mirrors OperationalStateUtils.assertRvcPause: also
// invalid in Charging and Docked.
func assertRvcPause(st State) ErrorID {
	if r := assertPause(st); r != ErrorNoError {
		return r
	}
	if st == StateCharging || st == StateDocked {
		return ErrorCommandInvalidInState
	}
	return ErrorNoError
}

// assertResume mirrors OperationalStateUtils.assertResume: Resume is
// invalid in Stopped and Error.
func assertResume(st State) ErrorID {
	if st == StateStopped || st == StateError {
		return ErrorCommandInvalidInState
	}
	return ErrorNoError
}

// assertRvcResume mirrors OperationalStateUtils.assertRvcResume: also
// invalid in SeekingCharger.
func assertRvcResume(st State) ErrorID {
	if r := assertResume(st); r != ErrorNoError {
		return r
	}
	if st == StateSeekingCharger {
		return ErrorCommandInvalidInState
	}
	return ErrorNoError
}

// assertRvcGoHome mirrors OperationalStateUtils.assertRvcGoHome: GoHome is
// invalid in Docked and Charging.
func assertRvcGoHome(st State) ErrorID {
	if st == StateDocked || st == StateCharging {
		return ErrorCommandInvalidInState
	}
	return ErrorNoError
}

// validState reports whether s is a state of the derivation: a value of
// its OperationalStateEnum, or a manufacturer-specific one (0x80-0xBF).
// No enum value carries a feature conformance, so an empty context
// decides them.
func (v variant) validState(s State) bool {
	return spec.EnumSupported(v.states, uint64(s), spec.Context(v.def, 0)) || clusterwire.HasOperationalStateLabel(uint8(s))
}

// validError is validState for the ErrorStateEnum.
func (v variant) validError(e ErrorID) bool {
	return spec.EnumSupported(v.errors, uint64(e), spec.Context(v.def, 0)) || clusterwire.HasOperationalStateLabel(uint8(e))
}

// Server implements [contract.ClusterServer] for OperationalState or
// RvcOperationalState.
type Server struct {
	cluster.AttributeChanges

	v        variant
	inst     *spec.Instance
	embedded cluster.DataVersionTracker
	ext      *cluster.DataVersionTracker
	handler  CommandHandler
	commands Command
	states   []StateEntry
	countOn  bool
	quiet    *cluster.Quieter

	mu           sync.Mutex
	state        State
	opError      ErrorState
	phases       []string
	currentPhase *uint8
	countdown    *uint32
	emitter      contract.EventEmitter
	endpoint     uint16
}

// Compile-time assertions.
var (
	_ contract.ClusterServer           = (*Server)(nil)
	_ contract.ClusterDataVersion      = (*Server)(nil)
	_ contract.ClusterAttributeLister  = (*Server)(nil)
	_ contract.ClusterCommandLister    = (*Server)(nil)
	_ contract.ClusterEventLister      = (*Server)(nil)
	_ contract.EventReceiver           = (*Server)(nil)
	_ contract.AttributeChangeNotifier = (*Server)(nil)
)

// NewServer builds an OperationalState (0x0060) server.
func NewServer(cfg Config) (*Server, error) { return newServer(baseVariant, cfg) }

// NewRvcServer builds an RvcOperationalState (0x0061) server.
func NewRvcServer(cfg Config) (*Server, error) { return newServer(rvcVariant, cfg) }

func newServer(v variant, cfg Config) (*Server, error) {
	if err := checkCommands(v, cfg); err != nil {
		return nil, err
	}
	if err := checkStates(v, cfg); err != nil {
		return nil, err
	}
	if err := checkPhases(cfg.Phases, cfg.CurrentPhase); err != nil {
		return nil, err
	}
	required := false
	if cfg.DeviceType != 0 {
		if !slices.Contains(v.deviceTypes, cfg.DeviceType) {
			return nil, fmt.Errorf("%w: 0x%04X", ErrDeviceType, cfg.DeviceType)
		}
		// <device>.element.ts: Requirement OperationCompletion, "M".
		required = true
	}
	opts := spec.Options{}
	if cfg.OperationCompletion || required {
		opts.Events = []uint32{EventOperationCompletion}
	}
	if cfg.CountdownTime {
		opts.Attributes = []uint32{AttrCountdownTime}
	}
	for _, c := range commandIDs {
		if cfg.Commands&c.cmd != 0 {
			opts.Commands = append(opts.Commands, c.id)
		}
	}
	// checkCommands has refused every command the derivation disallows
	// (Start and Stop are "X" on RvcOperationalState); nothing else is
	// left for New to refuse.
	inst, _ := spec.New(v.def, opts)
	s := &Server{
		v: v, inst: inst, ext: cfg.DataVersion, handler: cfg.Handler, commands: cfg.Commands,
		states: slices.Clone(cfg.States), countOn: cfg.CountdownTime,
		state: cfg.State, phases: slices.Clone(cfg.Phases), currentPhase: syncedPhase(cfg.Phases, cfg.CurrentPhase),
	}
	s.quiet = &cluster.Quieter{Report: func() { s.changed(AttrCountdownTime) }}
	return s, nil
}

// checkCommands enforces the command conformance of the element.
func checkCommands(v variant, cfg Config) error {
	if cfg.Commands&^v.commands != 0 {
		return fmt.Errorf("%w: 0x%02X", ErrCommandNotAllowed, uint8(cfg.Commands&^v.commands))
	}
	if cfg.Commands != 0 && cfg.Handler == nil {
		return ErrNoHandler
	}
	// Pause "Resume, O", Resume "Pause, O"
	// (operational-state.element.ts:57-72).
	if (cfg.Commands&CommandPause != 0) != (cfg.Commands&CommandResume != 0) {
		return ErrPauseResume
	}
	// Stop "Start, O" (operational-state.element.ts:61-64).
	if cfg.Commands&CommandStart != 0 && cfg.Commands&CommandStop == 0 {
		return ErrStartNeedsStop
	}
	return nil
}

// checkStates validates OperationalStateList and the initial state.
func checkStates(v variant, cfg Config) error {
	seen := map[State]bool{}
	for _, e := range cfg.States {
		if !v.validState(e.ID) {
			return fmt.Errorf("%w: 0x%02X", ErrUnknownState, uint8(e.ID))
		}
		if seen[e.ID] {
			return fmt.Errorf("%w: 0x%02X", ErrDuplicateState, uint8(e.ID))
		}
		if len(e.Label) > LabelMaxBytes || !utf8.ValidString(e.Label) {
			return fmt.Errorf("%w: label of state 0x%02X", ErrTooLong, uint8(e.ID))
		}
		seen[e.ID] = true
	}
	// OperationalStateServer #assertOperationalStateList.
	if !seen[StateError] {
		return ErrNoErrorState
	}
	// "All devices shall, at a minimum, expose the set of states matching
	// the commands that are also supported by the cluster instance, in
	// addition to Error" (operational-state.resource.ts:98-99).
	for _, need := range []struct {
		cmd Command
		st  State
	}{
		{CommandPause, StatePaused},
		{CommandResume, StateRunning},
		{CommandStart, StateRunning},
		{CommandStop, StateStopped},
		{CommandGoHome, StateSeekingCharger},
	} {
		if cfg.Commands&need.cmd != 0 && !seen[need.st] {
			return fmt.Errorf("%w: 0x%02X", ErrStateMissing, uint8(need.st))
		}
	}
	// "This shall be populated with a valid OperationalStateID from the
	// set of values in the OperationalStateList Attribute"
	// (operational-state.resource.ts:106-107).
	if !seen[cfg.State] {
		return fmt.Errorf("%w: 0x%02X", ErrStateNotListed, uint8(cfg.State))
	}
	return nil
}

// checkPhases validates a PhaseList and a CurrentPhase together: the
// list's constraint "max 32[max 64]", and #assertCurrentPhase — for a
// non-empty list CurrentPhase is an index into it. For a null or empty
// list any CurrentPhase is accepted and becomes null
// (#syncCurrentPhaseWithPhaseList).
func checkPhases(phases []string, current *uint8) error {
	if len(phases) > PhaseListMaxEntries {
		return fmt.Errorf("%w: %d", ErrPhaseList, len(phases))
	}
	for i, p := range phases {
		if len(p) > PhaseMaxBytes || !utf8.ValidString(p) {
			return fmt.Errorf("%w: phase %d", ErrTooLong, i)
		}
	}
	if len(phases) == 0 {
		return nil
	}
	if current == nil || int(*current) >= len(phases) {
		return fmt.Errorf("%w: %v for %d phases", ErrCurrentPhase, current, len(phases))
	}
	return nil
}

// syncedPhase is CurrentPhase after #syncCurrentPhaseWithPhaseList.
func syncedPhase(phases []string, current *uint8) *uint8 {
	if len(phases) == 0 || current == nil {
		return nil
	}
	c := *current
	return &c
}

// Revision returns OperationalState's revision from its generated
// definition.
func Revision() uint16 { return opdef.Revision }

// RvcRevision returns RvcOperationalState's revision from its generated
// definition.
func RvcRevision() uint16 { return rvcdef.Revision }

func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// changed advances the DataVersion and reports attrs.
func (s *Server) changed(attrs ...uint32) {
	if len(attrs) == 0 {
		return
	}
	s.tracker().Bump()
	s.Notify(attrs...)
}

// MatterClusterID returns 0x0060 or 0x0061.
func (s *Server) MatterClusterID() uint32 { return s.inst.MatterClusterID() }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterAttributes implements [contract.ClusterAttributeLister].
func (s *Server) MatterAttributes() []uint32 { return s.inst.MatterAttributes() }

// MatterReportable lists the attributes whose change is reported at once.
// CountdownTime is not among them: its quality is Q, and it is reported
// only when its quiet throttle lets a change through, as matter.js
// ServerBehaviorBacking keeps a quieter attribute out of bulk change
// reporting. OperationalStateList is fixed at construction.
func (*Server) MatterReportable() []uint32 {
	return []uint32{AttrPhaseList, AttrCurrentPhase, AttrOperationalState, AttrOperationalError}
}

// commandIDs pairs each Command with its id.
var commandIDs = []struct {
	cmd Command
	id  uint32
}{{CommandPause, CmdPause}, {CommandStop, CmdStop}, {CommandStart, CmdStart}, {CommandResume, CmdResume}, {CommandGoHome, CmdGoHome}}

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (s *Server) MatterAcceptedCommands() []uint32 { return s.inst.MatterAcceptedCommands() }

// MatterGeneratedCommands implements [contract.ClusterCommandLister]:
// OperationalCommandResponse, the response of every accepted command.
func (s *Server) MatterGeneratedCommands() []uint32 { return s.inst.MatterGeneratedCommands() }

// MatterEvents implements [contract.ClusterEventLister].
func (s *Server) MatterEvents() []uint32 { return s.inst.MatterEvents() }

// MatterRead resolves an attribute.
func (s *Server) MatterRead(attrID uint32) (any, bool) {
	if v, ok := s.inst.ReadGlobal(attrID); ok {
		return v, true
	}
	if attrID == AttrOperationalStateList {
		out := make([]clusterwire.OperationalStateStruct, 0, len(s.states))
		for _, e := range s.states {
			out = append(out, clusterwire.OperationalStateStruct{OperationalStateID: uint8(e.ID), OperationalStateLabel: e.Label})
		}
		return out, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch attrID {
	case AttrPhaseList:
		if s.phases == nil {
			return nil, true
		}
		return slices.Clone(s.phases), true
	case AttrCurrentPhase:
		if s.currentPhase == nil {
			return nil, true
		}
		return *s.currentPhase, true
	case AttrCountdownTime:
		if !s.countOn {
			return nil, false
		}
		if s.countdown == nil {
			return nil, true
		}
		return *s.countdown, true
	case AttrOperationalState:
		return uint8(s.state), true
	case AttrOperationalError:
		return s.opError.wire(), true
	}
	return nil, false
}

// MatterWrite refuses every write: each attribute is "R V"
// (UNSUPPORTED_WRITE), and one not served is UNSUPPORTED_ATTRIBUTE.
func (s *Server) MatterWrite(_ context.Context, attrID uint32, value any) error {
	_, err := s.inst.ValidateWrite(attrID, value, nil)
	return err
}

// MatterInvoke answers Pause, Stop, Start, Resume and GoHome with an
// OperationalCommandResponse. Each command is first checked against the
// current state as matter.js OperationalStateUtils checks it; a command
// that finds the device in its target state already answers NoError and
// "take[s] no further action" (operational-state.resource.ts: Pause :176,
// Stop :213, Start :241, Resume :266; rvc-operational-state.resource.ts
// GoHome); every other one is the handler's.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, _ any) (any, error) {
	cmd, ok := commandFor(cmdID)
	if !ok || s.commands&cmd == 0 {
		return nil, im.UnsupportedCommandf("opstate: command 0x%02X is not supported", cmdID)
	}
	s.mu.Lock()
	st := s.state
	s.mu.Unlock()
	var verdict ErrorID
	target := st
	switch cmd {
	case CommandPause:
		verdict, target = s.v.pause(st), StatePaused
	case CommandResume:
		verdict, target = s.v.resume(st), StateRunning
	case CommandStop:
		target = StateStopped
	case CommandStart:
		target = StateRunning
	case CommandGoHome:
		verdict, target = s.v.goHome(st), StateSeekingCharger
	}
	if verdict != ErrorNoError || st == target {
		return response(ErrorState{ID: verdict}), nil
	}
	reply, err := s.handler.HandleOperationalCommand(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("opstate: command 0x%02X: %w", cmdID, err)
	}
	if err := s.checkErrorState(reply); err != nil {
		return nil, statusError{im.StatusFailure, fmt.Sprintf("%v: %v", ErrInvalidHandlerReply, err)}
	}
	return response(reply), nil
}

func response(e ErrorState) clusterwire.OperationalCommandResponse {
	return clusterwire.OperationalCommandResponse{CommandResponseState: e.wire()}
}

func commandFor(id uint32) (Command, bool) {
	switch id {
	case CmdPause:
		return CommandPause, true
	case CmdStop:
		return CommandStop, true
	case CmdStart:
		return CommandStart, true
	case CmdResume:
		return CommandResume, true
	case CmdGoHome:
		return CommandGoHome, true
	}
	return 0, false
}

// checkErrorState validates an ErrorStateStruct against the cluster's
// enum and the "max 64" string constraints.
func (s *Server) checkErrorState(e ErrorState) error {
	if !s.v.validError(e.ID) {
		return fmt.Errorf("%w: 0x%02X", ErrUnknownError, uint8(e.ID))
	}
	if len(e.Label) > LabelMaxBytes || len(e.Details) > LabelMaxBytes || !utf8.ValidString(e.Label) || !utf8.ValidString(e.Details) {
		return ErrTooLong
	}
	return nil
}

// SetOperationalState moves OperationalState. It mirrors
// OperationalStateServer #assertOperationalState: the state must be
// listed, and leaving Error clears a pending OperationalError to NoError.
func (s *Server) SetOperationalState(st State) error {
	if !slices.ContainsFunc(s.states, func(e StateEntry) bool { return e.ID == st }) {
		return fmt.Errorf("%w: 0x%02X", ErrStateNotListed, uint8(st))
	}
	s.mu.Lock()
	if s.state == st {
		s.mu.Unlock()
		return nil
	}
	changed := []uint32{AttrOperationalState}
	if s.state == StateError && s.opError.ID != ErrorNoError && st != StateError {
		s.opError = ErrorState{}
		changed = append(changed, AttrOperationalError)
	}
	s.state = st
	s.mu.Unlock()
	s.changed(changed...)
	return nil
}

// SetOperationalError sets OperationalError. A value other than NoError
// also sets OperationalState to Error and emits the OperationalError
// event, mirroring OperationalStateServer #handleOperationalError; the
// event goes out only when the value changed, as matter.js reacts to
// operationalError$Changed.
func (s *Server) SetOperationalError(e ErrorState) error {
	if err := s.checkErrorState(e); err != nil {
		return err
	}
	s.mu.Lock()
	if s.opError == e {
		s.mu.Unlock()
		return nil
	}
	s.opError = e
	changed := []uint32{AttrOperationalError}
	if e.ID != ErrorNoError && s.state != StateError {
		s.state = StateError
		changed = append(changed, AttrOperationalState)
	}
	emitter, endpoint := s.emitter, s.endpoint
	s.mu.Unlock()
	s.changed(changed...)
	if e.ID != ErrorNoError && emitter != nil {
		emitter.MatterEmitEvent(endpoint, s.inst.MatterClusterID(), EventOperationalError,
			clusterwire.OperationalErrorEvent{ErrorState: e.wire()}, s.inst.EventPriority(EventOperationalError))
	}
	return nil
}

// SetPhaseList replaces PhaseList (nil is null) and CurrentPhase together,
// so the pair is never inconsistent in between. For a null or empty list
// CurrentPhase becomes null whatever current says
// (#syncCurrentPhaseWithPhaseList); for a non-empty one it must index the
// list (#assertCurrentPhase).
func (s *Server) SetPhaseList(phases []string, current *uint8) error {
	if err := checkPhases(phases, current); err != nil {
		return err
	}
	phases = slices.Clone(phases)
	cur := syncedPhase(phases, current)
	s.mu.Lock()
	var changed []uint32
	if (s.phases == nil) != (phases == nil) || !slices.Equal(s.phases, phases) {
		changed = append(changed, AttrPhaseList)
	}
	if !equalPhase(s.currentPhase, cur) {
		changed = append(changed, AttrCurrentPhase)
	}
	s.phases, s.currentPhase = phases, cur
	s.mu.Unlock()
	s.changed(changed...)
	return nil
}

// SetCurrentPhase moves CurrentPhase within the current PhaseList
// (#assertCurrentPhase): null when the list is null or empty, an index
// into it otherwise.
func (s *Server) SetCurrentPhase(current *uint8) error {
	s.mu.Lock()
	n := len(s.phases)
	ok := (n == 0 && current == nil) || (n > 0 && current != nil && int(*current) < n)
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("%w: %v for %d phases", ErrCurrentPhase, current, n)
	}
	if equalPhase(s.currentPhase, current) {
		s.mu.Unlock()
		return nil
	}
	s.currentPhase = syncedPhase(s.phases, current)
	s.mu.Unlock()
	s.changed(AttrCurrentPhase)
	return nil
}

func equalPhase(a, b *uint8) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// SetCountdownTime sets CountdownTime (nil is null). The value is read
// back at once, but a change is reported as matter.js reports its "Q"
// quality: at once from or to null, otherwise at most once a second, with
// the latest value (see [cluster.Quieter]). The server applies matter.js's
// throttle, not the specification's fuller list of when a countdown change
// is reportable — matter.js's RvcOperationalStateServer notes the same
// gap.
func (s *Server) SetCountdownTime(seconds *uint32) error {
	if !s.countOn || (seconds != nil && *seconds > CountdownTimeMax) {
		return ErrCountdownTime
	}
	s.mu.Lock()
	old := s.countdown
	if (old == nil) == (seconds == nil) && (old == nil || *old == *seconds) {
		s.mu.Unlock()
		return nil
	}
	if seconds == nil {
		s.countdown = nil
	} else {
		v := *seconds
		s.countdown = &v
	}
	s.mu.Unlock()
	s.quiet.Changed(old == nil, seconds == nil)
	return nil
}

// EmitOperationCompletion raises the OperationCompletion event (info). Its
// Code must be one of the cluster's error states (NoError for success).
func (s *Server) EmitOperationCompletion(ev OperationCompletion) error {
	if !s.inst.Emits(EventOperationCompletion) {
		return ErrEventNotDeclared
	}
	if !s.v.validError(ev.Code) {
		return fmt.Errorf("%w: 0x%02X", ErrUnknownError, uint8(ev.Code))
	}
	s.mu.Lock()
	emitter, endpoint := s.emitter, s.endpoint
	s.mu.Unlock()
	if emitter != nil {
		emitter.MatterEmitEvent(endpoint, s.inst.MatterClusterID(), EventOperationCompletion, clusterwire.OperationCompletionEvent{
			CompletionErrorCode: uint8(ev.Code), TotalOperationalTime: ev.TotalOperationalTime, PausedTime: ev.PausedTime,
		}, s.inst.EventPriority(EventOperationCompletion))
	}
	return nil
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *Server) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.mu.Lock()
	s.emitter = emitter
	s.mu.Unlock()
}

// SetEndpoint stamps the endpoint the events are addressed to; the
// bridge calls it at reassembly.
func (s *Server) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
}

// statusError carries an exact IM status to the dispatcher.
type statusError struct {
	status im.StatusCode
	msg    string
}

func (e statusError) Error() string                   { return e.msg }
func (e statusError) MatterStatusCode() im.StatusCode { return e.status }

var _ im.StatusCodeError = statusError{}
