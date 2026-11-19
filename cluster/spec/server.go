// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// Server errors; each error [NewServer], [Server.Set] or [Server.Emit]
// returns wraps one of these (or one of [New]'s).
var (
	// ErrFabricScoped: the selection serves a fabric-scoped attribute
	// (access "F"), whose value is one list per fabric; the generated
	// server holds one value per attribute. Scoping a read is not the
	// server's part: the dispatcher scopes every fabric-scoped attribute
	// of the model, whatever the server returns.
	ErrFabricScoped = errors.New("spec: fabric-scoped attributes are not supported by the generated server")
	// ErrNotServed: a value for an attribute the instance does not serve.
	ErrNotServed = errors.New("spec: attribute not served")
	// ErrInvalidValue: a value outside the attribute's type or constraint.
	ErrInvalidValue = errors.New("spec: value outside the attribute's type or constraint")
	// ErrNotEmitted: an event the instance's EventList does not hold.
	ErrNotEmitted = errors.New("spec: event not in EventList")
	// ErrMissingHandler: a command the conformance makes mandatory for
	// the feature selection, or an optional one [Options.Commands]
	// selects, has no handler in [ServerConfig.Handlers] (and
	// [ServerConfig.Unimplemented] is not set).
	ErrMissingHandler = errors.New("spec: accepted command has no handler")
	// ErrNotMounted: an event emitted while the server is not in the
	// topology — before it knows its endpoint or has an emitter, or after
	// the bridge withdrew its emitter ([Server.EmitEvent]).
	ErrNotMounted = errors.New("spec: the server is not mounted (no endpoint, or no event emitter)")
)

// Source is the optional read port for attributes the device owns: a
// value it answers takes precedence over the server's stored one.
type Source interface {
	MatterAttribute(attrID uint32) (any, bool)
}

// Sink is the optional write port: told about every controller write that
// passed the definition's checks, the [ServerConfig.WriteRule] and the
// host's veto and changes the value the server holds, before the server
// stores it; an error refuses the write and leaves the stored value as it
// was, and answers the write with the IM status it carries, FAILURE when
// it carries none. A write of the held value reaches no Sink and
// answers unchanged (matter.js observes no field whose value is equal,
// Datasource.ts:1406-1407 and :1479 at db021da0).
//
// value is the stored form described on [Server] — the Go type of the
// attribute's width (a uint32 for an epoch-s, a uint8 for an enum8), nil
// for null, a struct or list as decoded — not the uint64 / int64 /
// float64 [Instance.ValidateWrite] normalises to.
type Sink interface {
	MatterWriteAttribute(ctx context.Context, attrID uint32, value any) error
}

// CommandHandler carries out one accepted command. fields is the decoded
// request payload; the result is the response payload, or nil for a
// status-only command.
type CommandHandler func(ctx context.Context, fields any) (any, error)

// ServerConfig carries what a [Server] needs beyond the definition and
// the feature selection.
type ServerConfig struct {
	// DataVersion is an optional host-owned tracker; the server keeps its
	// own otherwise. The wire carries this version for a server on the
	// root endpoint or the Aggregator only; on every other endpoint the
	// endpoint keeps the cluster's version, which a change the server
	// reports advances, and this tracker is read by no one but the host.
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, and the server's own version
	// where it serves one (decision 165, F-db021da0-46); leave it nil.
	// Removable in v0.25.0.
	DataVersion *cluster.DataVersionTracker
	// Source optionally answers reads ahead of the stored values.
	Source Source
	// Sink is optionally told about controller writes.
	Sink Sink
	// WriteRule is the cluster author's rule on a controller write: called
	// by [Server.MatterWrite] after the definition's checks and before the
	// comparison with the held value — on every write, a write of the held
	// value included — with the value in its stored form. An error refuses
	// the write and answers it with the IM status it carries, FAILURE when
	// it carries none. [Server.SetBeforeWrite] and the typed server's
	// OnWrite never replace it: a host cannot remove a server's own rule.
	// It is the shape of a schema validator; a rule ported from a matter.js
	// `$Changing` reactor, which a held value never reaches, belongs behind
	// the comparison — in the veto ([ServerConfig.BeforeWrite]).
	WriteRule func(ctx context.Context, attrID uint32, value any) error
	// BeforeWrite optionally vetoes a controller write — the host's veto:
	// called by [Server.MatterWrite] after the definition's checks, the
	// [ServerConfig.WriteRule] and the comparison with the held value, and
	// before the Sink and the store, with the value in its stored form. A
	// write of the value the server holds calls no veto, advances no data
	// version and notifies nothing. An error refuses the write and nothing
	// is stored; the error answers the write with the IM status it
	// carries, FAILURE when it carries none. It is the hook the generated
	// typed server exposes as OnWrite. Also set after construction with
	// [Server.SetBeforeWrite], which replaces it.
	//
	// Deprecated: a server's own rule is [ServerConfig.WriteRule], which
	// no host veto replaces; the host's veto is [Server.SetBeforeWrite] (the
	// typed server's OnWrite), set after construction. Removable in
	// v0.25.0.
	BeforeWrite func(ctx context.Context, attrID uint32, value any) error
	// Initial holds the values the server starts with, checked as
	// [Server.Set] checks a value.
	Initial map[uint32]any
	// Handlers holds the command handlers, by request command id. Every
	// command the conformance makes mandatory for the feature selection,
	// and every optional one [Options.Commands] selects, must have one, or
	// construction fails with [ErrMissingHandler] naming each command that
	// has none: a selected command that nothing implements would otherwise
	// drop out of AcceptedCommandList with no word. With
	// [ServerConfig.Unimplemented] an accepted command without a handler is
	// allowed and left out of AcceptedCommandList (and its response out of
	// GeneratedCommandList) until [Server.Handle] registers one: the lists
	// name the commands the server implements.
	Handlers map[uint32]CommandHandler
	// Clock is the time source of the quieter attributes' report throttles
	// ([Attribute.QuietInterval]); nil is [clock.System].
	Clock clock.Clock
	// Unimplemented marks every accepted command Handlers leaves out as
	// the generated behavior's unimplemented one (matter.js
	// Behavior.unimplemented): construction does not fail on a mandatory
	// one, and the command stays out of AcceptedCommandList (and its
	// response out of GeneratedCommandList) until [Server.Handle]
	// registers it — matter.js's non-strict validation
	// (ValidatedElements.ts:106-108, :341-343, :361-368 at db021da0). The
	// device layer's generated default server is such a stub.
	Unimplemented bool
}

// Server is a complete [contract.ClusterServer] for a cluster whose
// server matter.js derives from the model alone — the case of
// packages/node/src/behaviors/fixed-label/FixedLabelServer.ts
// (`export class FixedLabelServer extends FixedLabelBehavior {}`): the
// element lists come from the definition and the feature selection
// (packages/node/src/behavior/cluster/ValidatedElements.ts, the embedded
// [Instance]), a value — written by a controller or set by the host — is
// checked against the model, and the state is the attribute values,
// starting at the model's defaults. It stores them, dispatches commands
// to registered handlers, emits events with the definition's priority and
// reports changes with a data-version bump and an attribute-change
// notification.
//
// Values are kept in the Go type of the attribute's width: uint8 to
// uint64 for an unsigned integer, enum or bitmap, int8 to int64 for a
// signed one, float32 or float64, bool, string, []byte; nil for null.
// Struct and list values are kept as given and must not be changed after
// they are handed over.
type Server struct {
	*Instance
	cluster.AttributeChanges

	embedded cluster.DataVersionTracker
	ext      *cluster.DataVersionTracker
	source   Source
	sink     Sink

	// rule is the cluster author's write rule ([ServerConfig.WriteRule]);
	// set once at construction, never replaced.
	rule func(ctx context.Context, attrID uint32, value any) error

	// mu guards beforeWrite, emitter, endpoint, mounted, handlers and
	// values.
	mu          sync.Mutex
	beforeWrite func(ctx context.Context, attrID uint32, value any) error
	values      map[uint32]any
	handlers    map[uint32]CommandHandler
	// own are the handlers construction was given ([ServerConfig.Handlers]),
	// which [Server.Handle] with a nil handler restores.
	own map[uint32]CommandHandler
	// stubs are the mandatory commands construction found unimplemented
	// ([ServerConfig.Unimplemented]) — the ones
	// [Server.UnimplementedMandatoryCommands] reports while they have no
	// handler. Every accepted command without a handler is unlisted
	// ([Server.unimplementedLocked]), a stub or not.
	stubs   []uint32
	emitter contract.EventEmitter
	// endpoint is the endpoint the server is mounted on, known once
	// mounted ([Server.SetEndpoint]).
	endpoint uint16
	mounted  bool

	// quiet holds the report throttle of every served quieter attribute,
	// by id; set at construction, read-only after.
	quiet map[uint32]*quietAttr

	// commit, guarded by mu, collects while a controller's write runs its
	// host veto and Sink the attributes a Set made meanwhile changed: they
	// are the write's commit ([writeCommit]).
	commit *writeCommit
	// holder is the commit whose write holds the server's commit window
	// and free closes when it leaves it; both guarded by windowGraph
	// (write_window.go).
	holder *writeCommit
	free   chan struct{}

	// storedForTests and leftForTests, nil in production, let this
	// package's tests stop a Set between its store and its notification
	// and a write between leaving its window and advancing the version.
	storedForTests, leftForTests func()
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*Server)(nil)
	_ contract.ClusterDataVersion             = (*Server)(nil)
	_ contract.ChangeReportingWriter          = (*Server)(nil)
	_ contract.ClusterAttributeLister         = (*Server)(nil)
	_ contract.ClusterCommandLister           = (*Server)(nil)
	_ contract.ClusterEventLister             = (*Server)(nil)
	_ contract.ClusterAttributeReadPrivilege  = (*Server)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*Server)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*Server)(nil)
	_ contract.TimedCommander                 = (*Server)(nil)
	_ contract.AttributeChangeNotifier        = (*Server)(nil)
	_ contract.EventReceiver                  = (*Server)(nil)
	_ contract.EndpointReceiver               = (*Server)(nil)
	_ contract.AttributeReportNotifier        = (*Server)(nil)
	_ contract.ClusterQuiescer                = (*Server)(nil)
)

// NewServer binds def to opts as [New] does and returns a server holding
// cfg.Initial; a served attribute cfg.Initial leaves out starts at the
// definition's default, and is absent when the definition states none.
// A selection that serves a fabric-scoped attribute is refused
// ([ErrFabricScoped]) — the server holds one value per attribute, not one
// per fabric — and an unserved one does not matter; the dispatcher scopes
// every fabric-scoped attribute of the model, whatever a server returns.
func NewServer(def *Cluster, opts Options, cfg ServerConfig) (*Server, error) {
	inst, err := New(def, opts)
	if err != nil {
		return nil, err
	}
	for _, id := range inst.attrs {
		if a := def.Attribute(id); a.Access.Fabric == "F" {
			return nil, fmt.Errorf("%w: %s attribute %s", ErrFabricScoped, def.Name, a.Name)
		}
	}
	s := &Server{
		Instance: inst,
		ext:      cfg.DataVersion,
		source:   cfg.Source,
		sink:     cfg.Sink,
		values:   map[uint32]any{},
		handlers: map[uint32]CommandHandler{},

		rule:        cfg.WriteRule,
		beforeWrite: cfg.BeforeWrite,
	}
	s.initQuiet(cfg.Clock)
	// A command the conformance requires is fatal at construction when it
	// has no implementation. Mirrors matter.js
	// packages/node/src/behavior/cluster/ValidatedElements.ts:331-333,357-370
	// at b25ed960 (requireCommand, #validateRequiredCommands: "Implementation
	// missing", fatal) — rather than advertising it in AcceptedCommandList
	// and answering UNSUPPORTED_COMMAND at invoke.
	for id, fn := range cfg.Handlers {
		if fn != nil {
			s.handlers[id] = fn
		}
	}
	s.own = maps.Clone(s.handlers)
	// Every mandatory command without a handler is named, not the first
	// alone: matter.js records a fatal error per command and reports them
	// together (ValidatedElements.ts:357-372 #validateRequiredCommands,
	// report :154-180 at db021da0). An optional command the selection
	// names without a handler is refused too, unless the server is the
	// generated behavior (Unimplemented): it would be left out of
	// AcceptedCommandList, so the selection would have no effect and
	// nothing would say so.
	var missing []error
	for _, id := range inst.accepted {
		required := slices.Contains(inst.required, id)
		switch {
		case s.handlers[id] != nil:
		case cfg.Unimplemented:
			if required {
				s.stubs = append(s.stubs, id)
			}
		case required:
			missing = append(missing, fmt.Errorf("%w: %s command %s (0x%02X)", ErrMissingHandler, def.Name, def.Command(id, Request).Name, id))
		default:
			missing = append(missing, fmt.Errorf("%w: %s optional command %s (0x%02X), selected by Options.Commands", ErrMissingHandler, def.Name, def.Command(id, Request).Name, id))
		}
	}
	if err := errors.Join(missing...); err != nil {
		return nil, err
	}
	for _, id := range inst.attrs {
		if v, ok := defaultValue(def.Attribute(id)); ok {
			s.values[id] = v
		}
	}
	for _, id := range slices.Sorted(maps.Keys(cfg.Initial)) {
		v, err := s.check(id, cfg.Initial[id])
		if err != nil {
			return nil, err
		}
		s.values[id] = v
	}
	// An "in" constraint names a sibling, so it is judged once every
	// initial value is in place, not in id order — matter.js validates the
	// whole state after the initializer has run. Mirrors matter.js
	// packages/node/src/behavior/internal/ServerBehaviorBacking.ts:60-63
	// (invokeInitializer: finalizeState → datasource.validate). So is a
	// bound that names a sibling ("holdTimeLimits.holdTimeMin to …",
	// DotQualifiedConstraintTest.ts at 862ad75f): an initial value is held
	// to it with every sibling in place ([Server.checkInState]).
	for _, id := range inst.attrs {
		if _, initial := cfg.Initial[id]; initial {
			if err := s.checkInState(id, s.values[id], s.peers); err != nil {
				return nil, err
			}
			continue
		}
		if err := s.checkIn(id, s.values[id], s.peers); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidValue, err)
		}
	}
	return s, nil
}

// checkIn holds v to attrID's "in" constraint, if it states one; a null or
// absent value is not judged.
func (s *Server) checkIn(attrID uint32, v any, peers Peers) error {
	a := s.def.Attribute(attrID)
	if a == nil || a.Constraint.In == "" || v == nil {
		return nil
	}
	if !s.inPeer(a.Constraint.In, v, peers) {
		return Errorf(im.StatusConstraintError, "%s: %s %v is not one of the values allowed by %q", s.def.Name, a.Name, v, a.Constraint.Text)
	}
	return nil
}

func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// MatterDataVersion implements [contract.ClusterDataVersion]: the
// server's own version ([ServerConfig.DataVersion]), the wire's on the
// root endpoint and the Aggregator only.
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterRead implements [contract.ClusterServer]: FeatureMap and
// ClusterRevision, then — for a served attribute — the Source's value,
// then the stored one. An attribute neither answers is absent.
func (s *Server) MatterRead(attrID uint32) (any, bool) {
	if v, ok := s.ReadGlobal(attrID); ok {
		return v, true
	}
	if !s.Serves(attrID) {
		return nil, false
	}
	if s.source != nil {
		if v, ok := s.source.MatterAttribute(attrID); ok {
			return v, true
		}
	}
	return s.Value(attrID)
}

// Value returns the stored value of attrID; false when none is stored.
func (s *Server) Value(attrID uint32) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[attrID]
	return v, ok
}

// SetBeforeWrite installs fn as the host's write veto
// ([ServerConfig.BeforeWrite]), replacing the one before; nil removes it.
// The server's own rule ([ServerConfig.WriteRule]) is not touched.
func (s *Server) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	s.mu.Lock()
	s.beforeWrite = fn
	s.mu.Unlock()
}

// peers resolves a sibling attribute a constraint names.
func (s *Server) peers(attrID uint32) (any, bool) { return s.MatterRead(attrID) }

// MatterWrite implements [contract.ClusterServer]: [Instance.ValidateWrite]
// answers the status of a write the definition refuses; the
// [ServerConfig.WriteRule], when set, may refuse it next; a value the
// server holds then ends the write, unchanged; another goes to the host's
// [ServerConfig.BeforeWrite] veto, when set, and to the Sink, each handed
// the value in its stored form, and is stored, and a change bumps the
// data version and is notified.
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	_, err := s.MatterWriteChanged(ctx, attrID, value)
	return err
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the write
// of [Server.MatterWrite], and whether its store changed the value — the
// comparison that bumps the data version, made under the lock that stores
// it (matter.js Datasource.ts:1479 at db021da0: an equal value, a list
// deep-equal to the one held included, is no change).
func (s *Server) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	changed, err := s.MatterWriteCommit(ctx, attrID, value)
	return len(changed) > 0, err
}

// MatterWriteCommit implements [contract.ChangeNamingWriter]: the write of
// [Server.MatterWrite], and the attributes it changed — attrID when the
// stored value moved, and every attribute a [Server.Set] changed while the
// write's veto and Sink ran (a sibling the Sink moves); none for a write
// of the value the server holds (matter.js Datasource.ts:1476-1484 at
// db021da0: a name is noted only when its value differs). The version
// advances once for the whole list ([writeCommit]).
func (s *Server) MatterWriteCommit(ctx context.Context, attrID uint32, value any) ([]uint32, error) {
	// The write runs inside the server's commit window: one write's at a
	// time, or the open one of the write whose veto or Sink makes this one
	// ([Server.enterWindow]).
	c, joined, err := s.enterWindow(ctx)
	if err != nil {
		return nil, err
	}
	if joined {
		changed, _, err := s.writeIn(ctx, attrID, value, nil)
		return changed, err
	}
	settled := false
	defer func() {
		if settled {
			return
		}
		// A panicking veto or Sink: what a Set changed meanwhile stays
		// changed, so it advances the version once and is told before the
		// panic goes on.
		if len(c.changed) > 0 {
			s.tracker().Bump()
		}
		for _, notify := range c.notifies {
			notify()
		}
	}()
	changed, wrote, err := func() ([]uint32, bool, error) {
		defer s.leaveWindow(c)
		return s.writeIn(context.WithValue(ctx, commitKey{}, c), attrID, value, c)
	}()
	settled = true
	if s.leftForTests != nil {
		s.leftForTests()
	}
	// What a Set inside the window changed is told now, after the window
	// is released and the version advanced, so a listener that writes the
	// server again does not wait for the window it was told from.
	if !wrote && len(c.changed) > 0 {
		// A refused write, or one of the held value: what a Set changed
		// meanwhile stays changed and advances the version once.
		s.tracker().Bump()
		changed = slices.Compact(slices.Sorted(slices.Values(c.changed)))
		if err != nil {
			changed = nil
		}
	}
	for _, notify := range c.notifies {
		notify()
	}
	return changed, err
}

// writeIn is a controller write of attrID inside the commit window ctx
// carries: the definition's checks, the author's rule and the comparison
// with the held value — asked here, after the window was entered, so the
// second of two writes of one value finds it held (matter.js notes no
// field whose value is equal, Datasource.ts:1476-1481 at db021da0) — then
// the host's veto and the Sink, and the store. wrote is false when the
// write stored nothing of its own (refused, or the held value). own is the
// commit of the window this write entered, nil for a write that joined an
// open one.
func (s *Server) writeIn(ctx context.Context, attrID uint32, value any, own *writeCommit) (changed []uint32, wrote bool, err error) {
	v, err := s.ValidateWrite(attrID, value, s.peers)
	if err != nil {
		return nil, false, err
	}
	// A struct's fields are held to their conformance as well (matter.js
	// validates the written state,
	// packages/node/src/behavior/state/validation/ValueValidator.ts:392-467).
	if err := s.CheckConformance(attrID, value, s.peers); err != nil {
		return nil, false, err
	}
	v = stored(s.def.Attribute(attrID).Type, v)
	// The author's rule judges every write, the held value included:
	// matter.js runs a property's schema validator on the assigned value
	// with no comparison to the previous one (StructManager.ts:273 assign,
	// :279-298 validate, :294-297 undo on failure, at db021da0).
	if s.rule != nil {
		if err := s.rule(ctx, attrID, v); err != nil {
			// As given: its IM status when it carries one, FAILURE
			// otherwise (the dispatcher reads no status from the text).
			return nil, false, err
		}
	}
	// The value the server holds reaches no veto and no Sink and stores
	// nothing: matter.js emits `$Changing` only for a returned change, and
	// a deep-equal value is none (Datasource.ts:1209-1212, :1406-1408 at
	// db021da0), and notes no field, advances no version and emits no
	// `$Changed` for it (:1476-1481, :1456-1457, :1486-1492). The store's
	// comparison below, under the lock that stores, stays the deciding
	// one: this check skips only when every answer the server gives equals
	// the value, so a write it lets through at worst stores an equal value
	// and answers unchanged.
	if s.holds(attrID, v) {
		return []uint32{}, false, nil
	}
	if err := s.runWriteHooks(ctx, attrID, v); err != nil {
		return nil, false, err
	}
	// The window's own write closes the collection before its store: the
	// store advances the version once for what it changed and what the
	// veto and the Sink changed, and its notification joins theirs, told
	// once the window is released. A joined write's store adds to the
	// open commit instead ([Server.storeWith]).
	if own == nil {
		// A joined write's store: while the commit it joined is open,
		// its notification joins that commit's ([Server.storeWith]).
		all, notify := s.storeWith(contract.OriginControllerWrite, map[uint32]any{attrID: v}, nil)
		if notify != nil {
			notify()
		}
		return all, true, nil
	}
	s.mu.Lock()
	s.commit = nil
	earlier := own.changed
	s.mu.Unlock()
	all, notify := s.storeWith(contract.OriginControllerWrite, map[uint32]any{attrID: v}, earlier)
	if notify != nil {
		own.notifies = append(own.notifies, notify)
	}
	return all, true, nil
}

// runWriteHooks runs the host's veto and the Sink of a controller's write
// of attrID, in that order; the first error refuses the write.
func (s *Server) runWriteHooks(ctx context.Context, attrID uint32, v any) error {
	s.mu.Lock()
	veto := s.beforeWrite
	s.mu.Unlock()
	if veto != nil {
		if err := veto(ctx, attrID, v); err != nil {
			return err
		}
	}
	if s.sink != nil {
		return s.sink.MatterWriteAttribute(ctx, attrID, v)
	}
	return nil
}

// holds reports whether the server holds v for attrID: its stored value
// is v (the comparison [Server.store] makes) and, when a Source answers
// the attribute, the Source's answer is v too.
func (s *Server) holds(attrID uint32, v any) bool {
	s.mu.Lock()
	old, ok := s.values[attrID]
	s.mu.Unlock()
	if !ok || !reflect.DeepEqual(old, v) {
		return false
	}
	if s.source != nil {
		if answer, ok := s.source.MatterAttribute(attrID); ok && !reflect.DeepEqual(answer, v) {
			return false
		}
	}
	return true
}

// Set records a change the device made to attrID. The value is checked
// against the attribute's type and constraint — not its access: the
// device may change what a controller may not write. A change bumps the
// data version and is notified; an equal value changes nothing. For an
// attribute the [Source] answers, the Source wins on read: Set stores and
// reports a value no read shows — report such an attribute with
// [Server.MarkChanged] instead.
func (s *Server) Set(attrID uint32, value any) error {
	return s.SetAttributes(map[uint32]any{attrID: value})
}

// SetAttributes is [Server.Set] for several attributes at once: every
// value is checked before any is stored, and the changes are reported
// with one data-version bump and one notification, in id order, as the
// host's ([contract.OriginHostSet]).
func (s *Server) SetAttributes(values map[uint32]any) error {
	return s.SetAttributesFrom(contract.OriginHostSet, values)
}

// SetAttributesFrom is [Server.SetAttributes] with the change's origin: a
// self-held server that keeps its state in a Server stores what a
// command made of it with [contract.OriginCommand], so its listeners tell
// it from the host's own Set.
//
// Every value is judged against the state the whole patch makes: a bound
// or an "in" constraint that names a sibling reads the sibling's value in
// values when the patch sets it, the stored one otherwise. A coherent new
// range set in one call is therefore accepted, and an incoherent one
// refused, whatever the order of the ids — as [NewServer] judges its
// initial values (review II, R2-P6-fsm-1303).
func (s *Server) SetAttributesFrom(origin contract.Origin, values map[uint32]any) error {
	checked := make(map[uint32]any, len(values))
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.check(id, values[id])
		if err != nil {
			return err
		}
		checked[id] = v
	}
	// The siblings a bound, an "in" constraint or a field's conformance
	// reads are the state with this change applied.
	pending := func(attrID uint32) (any, bool) {
		if v, ok := checked[attrID]; ok {
			return v, true
		}
		return s.peers(attrID)
	}
	for _, id := range slices.Sorted(maps.Keys(checked)) {
		if err := s.checkInState(id, checked[id], pending); err != nil {
			return err
		}
	}
	s.store(origin, checked)
	return nil
}

// check holds a device-side value to the served attribute's type and to
// the parts of its constraint that name no sibling — the checks of
// [Instance.CheckValue], list length and list entries and struct fields
// included — and returns it in its stored form. A bound that names a
// sibling admits here; [Server.checkInState] judges it once the state the
// value enters is known.
//
// The value is first patched with the defaults the feature selection
// makes storage hold ([Instance.PatchDefaults]), as matter.js patches a
// behavior's state over its defaults before it validates it.
func (s *Server) check(attrID uint32, value any) (any, error) {
	if s.Serves(attrID) {
		patched, err := s.PatchDefaults(attrID, value)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidValue, err)
		}
		value = patched
	}
	return s.CheckSet(attrID, value, unresolvedPeers)
}

// unresolvedPeers resolves no sibling: a bound computed from a sibling's
// name admits every value ([Peers]).
func unresolvedPeers(uint32) (any, bool) { return nil, false }

// checkInState holds a device-side value in its stored form to every check
// that reads the state it enters, with peers resolving that state: its
// type and constraint with the sibling bounds resolved
// ([Instance.CheckValue]) and an "in" constraint. A refusal wraps
// [ErrInvalidValue].
func (s *Server) checkInState(attrID uint32, v any, peers Peers) error {
	a := s.def.Attribute(attrID)
	if a == nil {
		return fmt.Errorf("%w: %s attribute 0x%04X is not defined", ErrInvalidValue, s.def.Name, attrID)
	}
	if _, err := s.checkValueIn(a.Name, a.Type, a.Quality.Nullable, a.Constraint, underlying(v), peers, nameScope{peers: peers, bits: attrID != featureMapAttributeID, conformance: true, enums: true}); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidValue, err)
	}
	if err := s.checkIn(attrID, v, peers); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidValue, err)
	}
	return nil
}

// CheckSet checks a value the device sets for attrID — not a controller's
// write: the attribute is served ([ErrNotServed] otherwise) and the value
// fits its type and constraint ([ErrInvalidValue]), whatever its access —
// and returns it in its stored form (nil for null, the Go type of the
// attribute's width). It is the check [Server.Set] applies, for a
// self-held server's [AttributeSetter].
func (i *Instance) CheckSet(attrID uint32, value any, peers Peers) (any, error) {
	if !i.Serves(attrID) {
		return nil, fmt.Errorf("%w: %s attribute 0x%04X", ErrNotServed, i.def.Name, attrID)
	}
	v, err := i.CheckValue(attrID, value, peers)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidValue, err)
	}
	return stored(i.def.Attribute(attrID).Type, v), nil
}

// defaultValue is the attribute's model default in its stored form: what
// matter.js starts a behavior's state with (packages/model/src/logic/
// DefaultValue.ts, SelectDefaultValue.ts). The generator renders a
// default as a float64, bool or string, [NullDefault] for null, or — for a
// struct whose fields state defaults — the generated struct value
// (script/clustergen/gen.go defaultLiteral, structDefault); one it does
// not render (a list) or one that does not fit the attribute's kind leaves
// the attribute without a value.
func defaultValue(a *Attribute) (any, bool) { return defaultOf(a.Type, a.Default) }

// defaultOf is the stated default d of an element of type t in its stored
// form ([defaultValue]); false for a default it does not render.
func defaultOf(t Type, d any) (any, bool) {
	switch d := d.(type) {
	case NullDefault:
		return nil, true
	case float64:
		switch t.Kind {
		case KindUint, KindEnum, KindBitmap:
			if d >= 0 {
				return stored(t, uint64(d)), true
			}
		case KindInt:
			return stored(t, int64(d)), true
		case KindFloat32, KindFloat64:
			return stored(t, d), true
		default:
		}
	case bool:
		if t.Kind == KindBool {
			return d, true
		}
	case string:
		if t.Kind == KindString {
			return d, true
		}
	case UnresolvedDefault:
	default:
		// A struct default the generator built from its fields' defaults
		// (script/clustergen structDefault): the generated struct value.
		if t.Kind == KindStruct && d != nil && reflect.TypeOf(d).Kind() == reflect.Struct {
			return d, true
		}
	}
	return nil, false
}

// store keeps values and reports the ones that changed, from origin; it
// returns the ids of those that changed, sorted, empty when none did
// ([Server.storeWith] without an earlier list).
func (s *Server) store(origin contract.Origin, values map[uint32]any) []uint32 {
	all, notify := s.storeWith(origin, values, nil)
	if s.storedForTests != nil {
		s.storedForTests()
	}
	if notify != nil {
		notify()
	}
	return all
}

// storeWith is store for a commit whose list already holds earlier, the
// attributes a Set inside a controller's write changed: the version
// advances once for the union, and the union is returned, sorted, each
// once. Inside an open commit ([writeCommit]) it advances nothing and adds
// its changes to that commit. A changes-omitted attribute ("C") is told to
// the change listeners and not to the report listeners: matter.js leaves
// the suppressed names out of the broadcast (ServerBehaviorBacking.ts:163-168
// at db021da0). Any change advances the data version once,
// now — a quieter attribute's too, as matter.js advances the version at
// the commit for every noted property (Datasource.ts:1446-1457 at
// db021da0). A change of a quieter attribute goes to the change listeners
// at once and to the report listeners when its throttle emits
// ([Server.quietReport]); every other change is reported at once.
//
// notify tells the change; nil when nothing changed, or when a commit is
// open: then the notification joins that commit's, in the same hold of
// s.mu that adds the change to it, and is told once the write left its
// window, after the version advanced. Decided later, a commit that closed
// between the store and the notification would let a notification out
// before the advance the commit makes for this change.
func (s *Server) storeWith(origin contract.Origin, values map[uint32]any, earlier []uint32) (all []uint32, notify func()) {
	type quietChange struct {
		qa              *quietAttr
		wasNull, isNull bool
	}
	var (
		changed, plain []uint32
		quiets         = map[uint32]quietChange{}
	)
	s.mu.Lock()
	for id, v := range values {
		old, ok := s.values[id]
		if ok && reflect.DeepEqual(old, v) {
			continue
		}
		s.values[id] = v
		changed = append(changed, id)
		if qa := s.quiet[id]; qa != nil {
			quiets[id] = quietChange{qa, !ok || old == nil, v == nil}
			qa.null, qa.origin, qa.computed = v == nil, origin, false
			continue
		}
		if a := s.def.Attribute(id); a != nil && a.Quality.ChangesOmitted {
			continue
		}
		plain = append(plain, id)
	}
	slices.Sort(changed)
	slices.Sort(plain)
	if len(changed) > 0 {
		notify = func() {
			if len(plain) == len(changed) {
				s.AttributeChanges.NotifyFrom(origin, changed...)
				return
			}
			s.AttributeChanges.ChangedFrom(origin, changed...)
			if len(plain) > 0 {
				s.AttributeChanges.ReportFrom(origin, plain...)
			}
			for _, id := range changed {
				if c, ok := quiets[id]; ok {
					c.qa.q.Changed(c.wasNull, c.isNull)
				}
			}
		}
	}
	open := s.commit
	if open != nil {
		open.changed = append(append(open.changed, changed...), earlier...)
		if notify != nil {
			open.notifies = append(open.notifies, notify)
			notify = nil
		}
	}
	s.mu.Unlock()
	all = slices.Compact(slices.Sorted(slices.Values(append(slices.Clone(changed), earlier...))))
	if all == nil {
		all = []uint32{}
	}
	if len(all) > 0 && open == nil {
		s.tracker().Bump()
	}
	return all, notify
}

// Handle implements [CommandRegistrar]: fn handles the request command
// cmdID, replacing the handler construction was given
// ([ServerConfig.Handlers]) or implementing a command without one; nil
// restores the handler construction was given (none for a command that
// had none). A command the instance does not accept is refused with
// [ErrNotAccepted]. A mandatory command's handler belongs in
// [ServerConfig.Handlers]: construction refuses a server without it
// ([ErrMissingHandler]). A handler for a command that had none belongs
// before the server is mounted: it changes AcceptedCommandList and
// GeneratedCommandList, and a change made later advances no data version
// and reaches no subscriber.
func (s *Server) Handle(cmdID uint32, fn CommandHandler) error {
	if !s.Instance.Accepts(cmdID) {
		return fmt.Errorf("%w: %s command 0x%02X", ErrNotAccepted, s.def.Name, cmdID)
	}
	s.mu.Lock()
	if fn == nil {
		fn = s.own[cmdID]
	}
	s.handlers[cmdID] = fn
	s.mu.Unlock()
	return nil
}

// Handles reports whether an accepted command has a handler.
func (s *Server) Handles(cmdID uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Instance.Accepts(cmdID) && s.handlers[cmdID] != nil
}

// unimplementedLocked reports whether the accepted command cmdID has no
// handler — a mandatory stub ([ServerConfig.Unimplemented]) or an
// optional command nobody implemented: either stays out of the command
// lists until [Server.Handle] registers it, as matter.js lists the
// commands a behavior implements
// (packages/node/src/behavior/cluster/ValidatedElements.ts:341-343,
// behavior/internal/ServerBehaviorBacking.ts:122-136 at db021da0).
func (s *Server) unimplementedLocked(cmdID uint32) bool { return s.handlers[cmdID] == nil }

// UnimplementedMandatoryCommands returns the request commands the
// conformance requires for the feature selection that are still the
// generated behavior's unimplemented stub ([ServerConfig.Unimplemented]):
// matter.js's "Throws unimplemented exception" — a warning, and the
// command left out of AcceptedCommandList, or under strict validation a
// fatal ClusterImplementationError
// (packages/node/src/behavior/cluster/ValidatedElements.ts:357-372 at
// b25ed960, #validateRequiredCommands). The device-type validator reports
// each as a violation of the endpoint (endpoint.ViolationUnimplemented).
func (s *Server) UnimplementedMandatoryCommands() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []uint32
	for _, id := range s.stubs {
		if slices.Contains(s.required, id) && s.handlers[id] == nil {
			out = append(out, id)
		}
	}
	return out
}

// Accepts reports whether the server accepts the request command: the
// instance accepts it and it has a handler.
func (s *Server) Accepts(cmdID uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Instance.Accepts(cmdID) && !s.unimplementedLocked(cmdID)
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister]: the
// instance's accepted commands that have a handler
// (ServerBehaviorBacking.ts:119-135 at db021da0 lists the implemented
// commands only).
func (s *Server) MatterAcceptedCommands() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.DeleteFunc(s.Instance.MatterAcceptedCommands(), s.unimplementedLocked)
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister]: the
// responses of the listed accepted commands (ServerBehaviorBacking.ts:137-148).
func (s *Server) MatterGeneratedCommands() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []uint32{}
	for _, id := range s.Instance.MatterAcceptedCommands() {
		if s.unimplementedLocked(id) {
			continue
		}
		if r := s.def.responseOf(s.def.Command(id, Request)); r != nil && !slices.Contains(out, r.ID) {
			out = append(out, r.ID)
		}
	}
	slices.Sort(out) // ascending, as the instance lists it
	return out
}

// MatterInvoke implements [contract.ClusterServer]: an accepted command
// goes to its handler; one not accepted, or accepted without a handler,
// is UNSUPPORTED_COMMAND. The request's types and constraints are the
// decoder's to refuse, before the server runs (decision 64). Before the
// handler the request is held to the definition again
// ([Instance.admitRequest]): a misfit is refused with the command's
// stated refusal ([RequestCheck.Refusal]), else a value an enum does not
// define with CONSTRAINT_ERROR, else it is logged and the handler runs. A
// response
// that breaks its fields' types, constraints or conformance — a field
// mandatory by the request's ("TestRequest.Enable") left out — is FAILURE
// ([Instance.checkResponse]).
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if err := s.admitRequest(ctx, cmdID, fields, s.peers); err != nil {
		return nil, err
	}
	return s.dispatch(ctx, cmdID, fields)
}

// dispatch runs the handler of an admitted request ([Server.MatterInvoke]).
func (s *Server) dispatch(ctx context.Context, cmdID uint32, fields any) (any, error) {
	s.mu.Lock()
	fn := s.handlers[cmdID]
	s.mu.Unlock()
	if !s.Instance.Accepts(cmdID) || fn == nil {
		return nil, im.UnsupportedCommandf("%s: command 0x%02X is not supported", s.def.Name, cmdID)
	}
	resp, err := fn(ctx, fields)
	if err != nil {
		return resp, err
	}
	if err := s.checkResponse(cmdID, fields, resp, s.peers); err != nil {
		return nil, err
	}
	return resp, nil
}

// SetMatterEventEmitter implements [contract.EventReceiver]; nil withdraws
// the emitter: [Server.EmitEvent] answers ErrNotMounted after it, and
// [Server.Emit] drops the event.
func (s *Server) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.mu.Lock()
	s.emitter = emitter
	s.mu.Unlock()
}

// SetEndpoint implements [contract.EndpointReceiver]: the endpoint the
// server is mounted on, which [Server.EmitEvent] emits on. The bridge
// calls it when it mounts the server.
func (s *Server) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint, s.mounted = endpoint, true
	s.mu.Unlock()
}

// EmitEvent emits eventID with data on the endpoint the server is mounted
// on, with the priority the definition gives it. An event outside
// EventList is an error ([ErrNotEmitted]), and so is one while the server
// is not in the topology ([ErrNotMounted]): before it knows its endpoint
// ([Server.SetEndpoint]) — it is never emitted on endpoint 0 in its place
// — and while it has no emitter: before mounting, or after the bridge
// withdrew it from a server that left the topology
// (SetMatterEventEmitter(nil)).
func (s *Server) EmitEvent(eventID uint32, data any) error {
	if !s.Emits(eventID) {
		return fmt.Errorf("%w: %s event 0x%02X", ErrNotEmitted, s.def.Name, eventID)
	}
	s.mu.Lock()
	emitter, endpoint, mounted := s.emitter, s.endpoint, s.mounted
	s.mu.Unlock()
	if !mounted || emitter == nil {
		return fmt.Errorf("%w: %s event 0x%02X", ErrNotMounted, s.def.Name, eventID)
	}
	emitter.MatterEmitEvent(endpoint, s.def.ID, eventID, data, s.EventPriority(eventID))
	return nil
}

// Emit emits eventID on endpoint with the priority the definition gives
// it. An event outside EventList is an error ([ErrNotEmitted]); without
// an emitter the event is dropped.
//
// Deprecated: use [Server.EmitEvent], which emits on the endpoint the
// server is mounted on instead of one its caller names. Removable in
// v0.25.0.
func (s *Server) Emit(endpoint uint16, eventID uint32, data any) error {
	if !s.Emits(eventID) {
		return fmt.Errorf("%w: %s event 0x%02X", ErrNotEmitted, s.def.Name, eventID)
	}
	s.mu.Lock()
	emitter := s.emitter
	s.mu.Unlock()
	if emitter != nil {
		emitter.MatterEmitEvent(endpoint, s.def.ID, eventID, data, s.EventPriority(eventID))
	}
	return nil
}

// underlying turns a value of a named scalar type (a generated enum, a
// bitmap) into its underlying type, the form the checks read; a value of
// any other type is returned as given.
func underlying(v any) any {
	if v == nil {
		return nil
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return r.Uint()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return r.Int()
	case reflect.Float32, reflect.Float64:
		return r.Float()
	case reflect.Bool:
		return r.Bool()
	case reflect.String:
		return r.String()
	default:
		return v
	}
}

// stored turns a checked value (uint64, int64, float64) into the Go type
// of the attribute's width.
func stored(t Type, v any) any {
	// The width check ran before (checkUnsigned / checkSigned), but the
	// bound is stated here again so the narrowing is correct on its own: a
	// value outside the width is returned unchanged, never truncated. Each
	// bound stands directly around its conversion.
	switch x := v.(type) {
	case uint64:
		switch {
		case t.Bits > 0 && t.Bits <= 8:
			if x <= math.MaxUint8 {
				return uint8(x)
			}
		case t.Bits > 8 && t.Bits <= 16:
			if x <= math.MaxUint16 {
				return uint16(x)
			}
		case t.Bits > 16 && t.Bits <= 32:
			if x <= math.MaxUint32 {
				return uint32(x)
			}
		}
	case int64:
		switch {
		case t.Bits > 0 && t.Bits <= 8:
			if x >= math.MinInt8 && x <= math.MaxInt8 {
				return int8(x)
			}
		case t.Bits > 8 && t.Bits <= 16:
			if x >= math.MinInt16 && x <= math.MaxInt16 {
				return int16(x)
			}
		case t.Bits > 16 && t.Bits <= 32:
			if x >= math.MinInt32 && x <= math.MaxInt32 {
				return int32(x)
			}
		}
	case float64:
		if t.Kind == KindFloat32 {
			return float32(x)
		}
	}
	return v
}
