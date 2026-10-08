// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package valve contains the Matter ValveConfigurationAndControl cluster
// server (0x0081) for a valve that opens and closes without a level axis.
//
// The server owns no valve state. Every attribute it answers is read
// from a [StateSource] the host implements, and both commands are
// forwarded to that same port; a server that flipped an internal field
// and reported Success would tell a controller the valve moved without
// anything having reached the device.
//
// The Level (LVL) feature is deliberately not implemented. CurrentLevel
// (0x6) and TargetLevel (0x7) carry conformance "LVL"
// (matter.js packages/model/src/standard/elements/
// valve-configuration-and-control.element.ts:39-40), so they exist only
// while that feature bit is advertised — and this server advertises an
// empty FeatureMap. A host that needs a positionable valve extends the
// port and the FeatureMap together; advertising LVL without the two
// attributes would be the broken half of that pair.
//
// The cluster's identity is the generated definition
// (cluster/spec/valveconfigurationandcontrol, ADR 0013): ids, revision,
// the attribute and command lists the empty feature selection yields, the
// write checks and the decoding of the Open request. What stays here is
// the host port and the OpenDuration presence rule.
package valve

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ClusterID is the ValveConfigurationAndControl cluster id.
const ClusterID = valvedef.ClusterID

// Attribute ids. Only the conformance-M attributes are listed: the five
// this server serves. AutoCloseTime (0x2) is "TS"-gated
// (valve-configuration-and-control.element.ts:35), CurrentLevel (0x6),
// TargetLevel (0x7), DefaultOpenLevel (0x8) and LevelStep (0xa) are
// LVL-gated (:39-40, :41-44, :46-49), and ValveFault (0x9) is optional
// (:45) — none of them belong to this projection.
const (
	// AttrOpenDuration is the duration the valve stays open for the
	// current opening, in seconds; null means "until closed by the user
	// or some other automation" (element :27-30, resource :37-43).
	AttrOpenDuration = valvedef.AttrOpenDuration
	// AttrDefaultOpenDuration is the duration applied when an Open
	// command carries no OpenDuration field (element :31-34,
	// resource :46-52). It is the cluster's only writable attribute
	// here: access "RW VO" (:32).
	AttrDefaultOpenDuration = valvedef.AttrDefaultOpenDuration
	// AttrRemainingDuration is the time left until the valve closes;
	// null while OpenDuration is null or the valve is closed
	// (element :36, resource :77-105).
	AttrRemainingDuration = valvedef.AttrRemainingDuration
	// AttrCurrentState is the valve's current [State]; null means the
	// state is not known (element :37, resource :109-113).
	AttrCurrentState = valvedef.AttrCurrentState
	// AttrTargetState is the state the valve is moving towards; null
	// means no target is set because the change is done or failed
	// (element :38, resource :116-120).
	AttrTargetState = valvedef.AttrTargetState
)

// Command ids. Both carry conformance "M" and response "status"
// (element :60, :64), so neither produces a generated command.
const (
	// CmdOpen sets the valve to its open position (element :60).
	CmdOpen = valvedef.CmdOpen
	// CmdClose sets the valve to its closed position (element :64).
	CmdClose = valvedef.CmdClose
)

// Open command field tags of the tag map an in-process caller hands over
// (element :61-62).
const (
	openFieldOpenDuration uint8 = 0
	openFieldTargetLevel  uint8 = 1
)

// FeatureMap bits, named so a later projection has the verified bit
// positions rather than a guess. Neither is advertised by this server.
//
// TS carries conformance "desc" and constraint "0"; LVL carries "O" and
// constraint "1" (element :24-25).
const (
	// FeatureTimeSync is the TS bit — the valve expresses durations and
	// AutoCloseTime against synchronized UTC time. Not advertised: it
	// requires the Time Synchronization cluster (resource :21-26).
	FeatureTimeSync = uint32(valvedef.FeatureTimeSync)
	// FeatureLevel is the LVL bit — the valve can be driven to a
	// percentage of its range (resource :29-32).
	FeatureLevel = uint32(valvedef.FeatureLevel)
)

// featureMapNone is the FeatureMap this server advertises: no optional
// feature. TS is conformance "desc" and LVL is optional, so zero is a
// conformant value for a valve that only opens and closes.
const featureMapNone uint32 = 0

// instance is the definition bound to [featureMapNone] with no optional
// element declared: the five mandatory attributes, Open and Close. The
// selection is conformant, so New has nothing to refuse; the parity test
// holds the server against it (spectest.CheckServer).
var instance, _ = spec.New(valvedef.Definition, spec.Options{Features: featureMapNone})

// State is the ValveStateEnum (enum8) a controller reads from
// CurrentState and TargetState.
type State = valvedef.ValveStateEnum

// ValveStateEnum values (element :76-81).
const (
	// StateClosed — valve is in closed position (resource :250).
	StateClosed = valvedef.ValveStateClosed
	// StateOpen — valve is in open position (resource :251).
	StateOpen = valvedef.ValveStateOpen
	// StateTransitioning — valve is moving between positions
	// (resource :252-255).
	StateTransitioning = valvedef.ValveStateTransitioning
)

// OpenRequest is the cluster-native payload of the Open command
// (element :59-63).
//
// Presence and null are distinct and both are carried: an absent
// OpenDuration field means "use DefaultOpenDuration" while a present
// null means "stay open until something closes me" (resource :46-52 vs
// :213-218). Collapsing the two into one nil pointer would silently turn
// an indefinite opening into the default one.
type OpenRequest struct {
	// HasOpenDuration reports whether the command carried the
	// OpenDuration field at all.
	HasOpenDuration bool
	// OpenDuration is the field's value in seconds, or nil when the
	// field was present and null.
	OpenDuration *uint32
}

// StateSource is the host port this server reads and drives. The host
// implements it over whatever actually owns the valve; nothing in this
// package caches or invents a value.
//
// Every reader returns (value, known). known=false becomes a TLV null,
// which is what quality X asks for on all five attributes
// (element :27-38) — the spec's own way of saying the value is not
// known, as opposed to guessing Closed for a device that has not
// reported.
//
// The command methods own the southbound urgency of the writes they
// perform: the cluster contract carries no priority, so a host whose
// command queue ranks by urgency names the value it wants inside its
// own implementation, where the device vocabulary is in scope.
type StateSource interface {
	// CurrentState reports the valve's observed state.
	CurrentState() (state State, known bool)
	// TargetState reports the state the valve is moving towards.
	TargetState() (state State, known bool)
	// OpenDuration reports the duration of the current opening, in seconds.
	OpenDuration() (seconds uint32, known bool)
	// RemainingDuration reports the time left before the valve closes.
	RemainingDuration() (seconds uint32, known bool)
	// DefaultOpenDuration reports the configured default open duration.
	DefaultOpenDuration() (seconds uint32, known bool)
	// SetDefaultOpenDuration persists a new default; seconds is nil for
	// a null write, which clears the default.
	SetDefaultOpenDuration(ctx context.Context, seconds *uint32) error
	// Open drives the valve to its open position.
	Open(ctx context.Context, req OpenRequest) error
	// Close drives the valve to its closed position.
	Close(ctx context.Context) error
}

// Config carries the construction parameters for [Server].
type Config struct {
	// Source is the host port. A nil Source makes every attribute read
	// null and every command fail — the server never pretends to have
	// moved a valve it cannot reach.
	Source StateSource
	// DataVersion is an optional tracker owned by the host. When
	// non-nil the server bumps and reports the caller's counter, so the
	// version survives server reconstruction; when nil an embedded
	// tracker is used.
	DataVersion *cluster.DataVersionTracker
}

// Server implements [contract.ClusterServer] for
// ValveConfigurationAndControl (0x0081).
type Server struct {
	embedded cluster.DataVersionTracker // used when Config.DataVersion is nil
	ext      *cluster.DataVersionTracker
	src      StateSource
}

// Compile-time assertions.
var (
	_ contract.ClusterServer          = (*Server)(nil)
	_ contract.ClusterDataVersion     = (*Server)(nil)
	_ contract.ClusterAttributeLister = (*Server)(nil)
	_ contract.ClusterCommandLister   = (*Server)(nil)
)

// NewServer constructs a [Server] over the host port in cfg.
func NewServer(cfg Config) *Server {
	return &Server{src: cfg.Source, ext: cfg.DataVersion}
}

// tracker returns the active DataVersion counter — the host's when it
// supplied one, the embedded one otherwise.
func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// Revision returns the cluster revision of the generated definition. Reading
// it rather than restating it is the point: a regeneration moves this value
// and a hand-written copy would not follow.
func Revision() uint16 { return valvedef.Revision }

// MatterClusterID returns 0x0081.
func (*Server) MatterClusterID() uint32 { return ClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterRead resolves the five conformance-M attributes plus the two
// universal globals every cluster server answers itself.
func (s *Server) MatterRead(attrID uint32) (value any, ok bool) {
	switch attrID {
	case AttrOpenDuration, AttrDefaultOpenDuration, AttrRemainingDuration,
		AttrCurrentState, AttrTargetState:
		if s.src == nil {
			return nil, true
		}
		return s.readFromSource(attrID)
	default:
		return instance.ReadGlobal(attrID)
	}
}

// readFromSource projects one host-reported value onto the wire types
// the encoder expects: elapsed-s as uint32 (4-byte TLV uint) and
// ValveStateEnum as uint8. Returning the named [State] type instead
// would fall through the encoder's type switch.
func (s *Server) readFromSource(attrID uint32) (any, bool) {
	switch attrID {
	case AttrOpenDuration:
		return nullableSeconds(s.src.OpenDuration())
	case AttrDefaultOpenDuration:
		return nullableSeconds(s.src.DefaultOpenDuration())
	case AttrRemainingDuration:
		return nullableSeconds(s.src.RemainingDuration())
	case AttrCurrentState:
		return nullableState(s.src.CurrentState())
	case AttrTargetState:
		return nullableState(s.src.TargetState())
	default:
		return nil, false
	}
}

// nullableSeconds maps a host reading onto (value, true) or the TLV
// null the quality-X attributes carry when nothing has been observed.
func nullableSeconds(seconds uint32, known bool) (any, bool) {
	if !known {
		return nil, true
	}
	return seconds, true
}

// nullableState mirrors [nullableSeconds] for the two enum attributes.
func nullableState(state State, known bool) (any, bool) {
	if !known {
		return nil, true
	}
	return uint8(state), true
}

// MatterWrite applies the one writable attribute this cluster has.
//
// DefaultOpenDuration carries access "RW VO"; the other four are "R V" and
// are rejected as UNSUPPORTED_WRITE rather than silently dropped, an id the
// cluster does not serve as UNSUPPORTED_ATTRIBUTE. The definition checks the
// value: a null clears the default (resource :50-52), any other value must
// satisfy constraint "min 1", so zero is a CONSTRAINT_ERROR rather than a
// duration of no seconds.
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	v, err := instance.ValidateWrite(attrID, value, nil)
	if err != nil {
		return err
	}
	var seconds *uint32
	if n, ok := v.(uint64); ok {
		sec := uint32(n) //nolint:gosec // a nullable uint32 per ValidateWrite
		seconds = &sec
	}
	if s.src == nil {
		return errNoSource("DefaultOpenDuration write")
	}
	if err := s.src.SetDefaultOpenDuration(ctx, seconds); err != nil {
		return fmt.Errorf("valve: DefaultOpenDuration write: %w", err)
	}
	s.tracker().Bump()
	return nil
}

// MatterInvoke dispatches the two conformance-M commands. Both reach
// the host port; neither reports Success on its own.
//
// A host error surfaces as the generic Failure status. The cluster's own
// StatusCodeEnum carries FailureDueToFault (0x2, element :83-85), but
// nothing in the port says a refusal came from a valve fault, and
// claiming one would put a diagnosis on the wire that was never measured.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	switch cmdID {
	case CmdOpen:
		req, err := openRequestFrom(fields)
		if err != nil {
			return nil, err
		}
		if s.src == nil {
			return nil, errNoSource("Open")
		}
		if err := s.src.Open(ctx, req); err != nil {
			return nil, fmt.Errorf("valve: Open: %w", err)
		}
		s.tracker().Bump()
		return nil, nil
	case CmdClose:
		if s.src == nil {
			return nil, errNoSource("Close")
		}
		if err := s.src.Close(ctx); err != nil {
			return nil, fmt.Errorf("valve: Close: %w", err)
		}
		s.tracker().Bump()
		return nil, nil
	default:
		return nil, im.UnsupportedCommandf("valve: unknown command 0x%02X", cmdID)
	}
}

// openRequestFrom normalises the payload the bridge hands over. The
// bridge decodes the request through the generated definition into a
// [valvedef.OpenRequest], with matter.js's field checks (OpenDuration "min
// 1", TargetLevel "1 to 100"); an in-process caller may hand over the
// tag-keyed map, a host that decodes the command itself [OpenRequest]
// directly, and a parameterless Open arrives as nil.
func openRequestFrom(fields any) (OpenRequest, error) {
	switch v := fields.(type) {
	case nil:
		return OpenRequest{}, nil
	case valvedef.OpenRequest:
		return openRequestFromGenerated(v)
	case *valvedef.OpenRequest:
		if v == nil {
			return OpenRequest{}, nil
		}
		return openRequestFromGenerated(*v)
	case OpenRequest:
		return v, nil
	case *OpenRequest:
		if v == nil {
			return OpenRequest{}, nil
		}
		return *v, nil
	case map[uint8]any:
		return openRequestFromTagMap(v)
	default:
		return OpenRequest{}, fmt.Errorf(
			"valve: Open expected valve.OpenRequest or map[uint8]any, got %T", fields,
		)
	}
}

// openRequestFromGenerated carries the decoded request over to the host's
// [OpenRequest], keeping an absent OpenDuration apart from a null one.
func openRequestFromGenerated(r valvedef.OpenRequest) (OpenRequest, error) {
	if r.TargetLevel != nil {
		return OpenRequest{}, errTargetLevel
	}
	if r.OpenDuration == nil {
		return OpenRequest{}, nil
	}
	req := OpenRequest{HasOpenDuration: true}
	if !r.OpenDuration.Null {
		seconds := r.OpenDuration.Value
		req.OpenDuration = &seconds
	}
	return req, nil
}

// errTargetLevel refuses an Open that carries TargetLevel: it is
// conformance "[LVL]" (element :62) and this server advertises no LVL.
// Ignoring the field would open the valve to a level the controller never
// got told was impossible.
var errTargetLevel = constraintError{
	"valve: Open carries TargetLevel, which needs the Level feature this server does not advertise",
}

// openRequestFromTagMap reads the Open command's two fields out of the
// generic tag map. Unsigned TLV integers surface as uint64 there
// (bridge/fields_reader.go decodeGenericTagMap), and an explicit null
// keeps its tag with a nil value — which is how presence stays
// distinguishable from absence.
func openRequestFromTagMap(m map[uint8]any) (OpenRequest, error) {
	var req OpenRequest
	if _, present := m[openFieldTargetLevel]; present {
		return req, errTargetLevel
	}
	raw, present := m[openFieldOpenDuration]
	if !present {
		return req, nil
	}
	req.HasOpenDuration = true
	if raw == nil {
		return req, nil
	}
	seconds, ok := asUint32(raw)
	if !ok {
		return req, constraintError{fmt.Sprintf("valve: Open OpenDuration expected a number, got %T", raw)}
	}
	if seconds < 1 {
		// constraint "min 1" (element :61).
		return req, constraintError{"valve: Open OpenDuration violates constraint min 1"}
	}
	req.OpenDuration = &seconds
	return req, nil
}

// asUint32 narrows a decoded wire value to the uint32 an elapsed-s field
// carries. The bridge surfaces unsigned TLV integers as uint64 and
// signed ones as int64 (bridge/attribute_value_reader.go
// primitiveAttributeValue), so a strict type assertion on uint32 would
// reject every value that actually arrives. Out-of-range and negative
// values are rejected rather than wrapped.
func asUint32(v any) (uint32, bool) {
	const maxUint32 = int64(^uint32(0))
	var n int64
	switch x := v.(type) {
	case uint8:
		n = int64(x)
	case uint16:
		n = int64(x)
	case uint32:
		n = int64(x)
	case uint64:
		if x > uint64(maxUint32) {
			return 0, false
		}
		n = int64(x) //nolint:gosec // range-checked against uint32 max on the line above
	case int:
		n = int64(x)
	case int8:
		n = int64(x)
	case int16:
		n = int64(x)
	case int32:
		n = int64(x)
	case int64:
		n = x
	default:
		return 0, false
	}
	if n < 0 || n > maxUint32 {
		return 0, false
	}
	return uint32(n), true //nolint:gosec // range-checked against 0 and uint32 max above
}

// MatterReportable lists the attributes that move while the valve runs.
// DefaultOpenDuration is absent: it changes only through a write, which
// the dispatcher already reflects in the endpoint's DataVersion.
func (*Server) MatterReportable() []uint32 {
	return []uint32{
		AttrOpenDuration,
		AttrRemainingDuration,
		AttrCurrentState,
		AttrTargetState,
	}
}

// MatterAttributes implements [contract.ClusterAttributeLister], in id
// order and without the universal globals — the dispatcher merges those.
func (*Server) MatterAttributes() []uint32 { return instance.MatterAttributes() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (*Server) MatterAcceptedCommands() []uint32 { return instance.MatterAcceptedCommands() }

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
// Open and Close both declare response "status", so the server emits no
// command payloads.
func (*Server) MatterGeneratedCommands() []uint32 { return instance.MatterGeneratedCommands() }

// errNoSource reports that the server has no host port to reach.
func errNoSource(what string) error {
	return fmt.Errorf("valve: %s has no host port", what)
}

// constraintError maps onto the Matter ConstraintError status.
type constraintError struct{ msg string }

func (e constraintError) Error() string                 { return e.msg }
func (constraintError) MatterStatusCode() im.StatusCode { return im.StatusConstraintError }

// Compile-time assertion for the typed status carrier.
var _ im.StatusCodeError = constraintError{}
