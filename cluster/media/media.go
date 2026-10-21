// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package media holds the servers of the media and content clusters
// (ADR 0022): MediaPlayback, KeypadInput, ContentLauncher,
// ApplicationLauncher, ApplicationBasic, AccountLogin, AudioOutput,
// Channel, TargetNavigator, MediaInput, LowPower, WakeOnLan,
// ContentControl and ContentAppObserver. Messages, whose Messages list is
// fabric-scoped, is cluster/core's [core.Messages].
//
// matter.js has no logic for thirteen of them: each server is the
// generated behaviour, `export class MediaPlaybackServer extends
// MediaPlaybackBehavior {}` (packages/node/src/behaviors/media-playback/
// MediaPlaybackServer.ts:14 at 0ceb202d) and its siblings; LowPower's
// `sleep()` emits the `enterLowPowerMode` observable
// (packages/node/src/behaviors/low-power/LowPowerServer.ts:16-18). A
// matter.js application implements the commands in a subclass and changes
// the behaviour's state from them. Here each server is the generated
// definition (cluster/spec/<name>, ADR 0013) mounted by the standard
// server (ADR 0017, [spec.NewServer]) and a typed host port: the host
// implements the commands; the server decodes the request, calls the port
// and encodes its answer.
//
// The commands of a cluster with state get the state the attributes
// report, as a pointer the port may change — matter.js's command
// implementation writing `this.state`. A command that succeeds commits
// the change (checked against the definition, a data-version bump and a
// change report for what moved); one that fails leaves the state as it
// was, as a matter.js transaction rolls back when its command throws. A
// change the device makes on its own goes through the server's SetState.
//
// A command answers the port's error as its status: a [StatusError] with
// its IM status, any other error FAILURE. A command the cluster's
// conformance makes optional is accepted only when the port implements it
// — matter.js leaves an unimplemented command out of AcceptedCommandList
// and answers it UNSUPPORTED_COMMAND (packages/node/src/node/integration/
// ProtocolService.ts:541-545); one the conformance requires and the port
// does not implement is refused at construction ([ErrNotImplemented]), as
// matter.js's ValidatedElements reports "Implementation missing"
// (packages/node/src/behavior/cluster/ValidatedElements.ts:357-371).
//
// A list- or struct-typed attribute the definition states no default for
// has no value until the host gives one: a server whose selection serves
// such an attribute refuses construction without it ([ErrMissingState]).
package media

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// Construction errors; each error a constructor returns wraps one of these
// or one of [spec.NewServer]'s.
var (
	// ErrNilPort: the configuration carries no host port.
	ErrNilPort = errors.New("media: a host port is required")
	// ErrMissingState: a served list- or struct-typed attribute without a
	// definition default has no initial value.
	ErrMissingState = errors.New("media: a served attribute has no initial state")
	// ErrNotImplemented: the feature selection requires a command the port
	// does not implement.
	ErrNotImplemented = errors.New("media: the port does not implement a required command")
)

// StatusError is a port's answer to a command with an IM status other than
// FAILURE: a command handler returns it, and the invoke answers Status. A
// cluster-specific status a response command carries (its StatusEnum) is
// the response's field, not this error.
type StatusError struct {
	// Status is the IM status the invoke answers.
	Status im.StatusCode
	// Err optionally says why; it is not sent.
	Err error
}

// Error implements error.
func (e StatusError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("media: command answered %s: %v", e.Status, e.Err)
	}
	return fmt.Sprintf("media: command answered %s", e.Status)
}

// Unwrap returns the cause.
func (e StatusError) Unwrap() error { return e.Err }

// MatterStatusCode implements [im.StatusCodeError].
func (e StatusError) MatterStatusCode() im.StatusCode { return e.Status }

// commandStatus maps a port's error to the invoke's: a [StatusError] or any
// other [im.StatusCodeError] keeps its status, every other error is
// FAILURE.
func commandStatus(err error) error {
	if err == nil {
		return nil
	}
	var sce im.StatusCodeError
	if errors.As(err, &sce) {
		return err
	}
	return StatusError{Status: im.StatusFailure, Err: err}
}

// request converts a command's decoded fields to the generated request
// payload: the generated decoder hands the value, a caller may hand a
// pointer.
func request[Req any](fields any) (Req, error) {
	var zero Req
	switch r := fields.(type) {
	case *Req:
		if r != nil {
			return *r, nil
		}
	case Req:
		return r, nil
	default:
		// A command without fields: its generated empty payload, or none
		// at all, stands for an empty Req.
		if emptyStruct(reflect.TypeOf(zero)) && (fields == nil || emptyStruct(reflect.TypeOf(fields))) {
			return zero, nil
		}
	}
	return zero, StatusError{Status: im.StatusInvalidCommand, Err: fmt.Errorf("fields %T, want %T", fields, zero)}
}

// emptyStruct reports whether t is a struct without fields.
func emptyStruct(t reflect.Type) bool {
	return t != nil && t.Kind() == reflect.Struct && t.NumField() == 0
}

// handlers collects a server's command handlers before construction:
// [spec.NewServer] refuses a mandatory command without one.
type handlers map[uint32]spec.CommandHandler

// newServer is [spec.NewServer]; a mandatory command left without a
// handler — the port does not implement it — is [ErrNotImplemented]
// (ValidatedElements.ts:357-371 at 0ceb202d).
func newServer(def *spec.Cluster, opts spec.Options, cfg spec.ServerConfig) (*spec.Server, error) {
	srv, err := spec.NewServer(def, opts, cfg)
	if errors.Is(err, spec.ErrMissingHandler) {
		return nil, fmt.Errorf("%w: %w", ErrNotImplemented, err)
	}
	return srv, err
}

// handle registers fn for cmdID in h: the request is decoded into Req,
// fn's error becomes the invoke's status (commandStatus) and its result
// the response payload.
func handle[Req any](h handlers, cmdID uint32, fn func(ctx context.Context, req Req) (any, error)) {
	h[cmdID] = func(ctx context.Context, fields any) (any, error) {
		req, err := request[Req](fields)
		if err != nil {
			return nil, err
		}
		resp, err := fn(ctx, req)
		if err != nil {
			return nil, commandStatus(err)
		}
		return resp, nil
	}
}

// handleState registers fn for cmdID in h as a command of the state s:
// fn changes a copy of the state, committed when it succeeds.
func handleState[Req, S any](h handlers, s *stateful[S], cmdID uint32, fn func(ctx context.Context, st *S, req Req) (any, error)) {
	handle(h, cmdID, func(ctx context.Context, req Req) (any, error) {
		return s.transact(func(st *S) (any, error) { return fn(ctx, st, req) })
	})
}

// statusOnly adapts a status-only command of the state S to handleState.
func statusOnly[Req, S any](fn func(ctx context.Context, st *S, req Req) error) func(ctx context.Context, st *S, req Req) (any, error) {
	return func(ctx context.Context, st *S, req Req) (any, error) { return nil, fn(ctx, st, req) }
}

// uint16sOr is l, or absent when l is nil.
func uint16sOr(l []uint16) any {
	if l == nil {
		return absent
	}
	return l
}

// command is one request command of a cluster: its id and whether the
// port implements it.
type command struct {
	id          uint32
	implemented bool
}

// optionalCommands lists the commands among cmds the port implements, for
// spec.Options.Commands: a conformance-optional command is accepted only
// then.
func optionalCommands(cmds []command) []uint32 {
	var out []uint32
	for _, c := range cmds {
		if c.implemented {
			out = append(out, c.id)
		}
	}
	return out
}

// checkImplemented refuses a selection that accepts a command the port
// does not implement — a mandatory one, or one the features require.
func checkImplemented(srv *spec.Server, cmds []command) error {
	def := srv.Definition()
	for _, c := range cmds {
		if !c.implemented && srv.Accepts(c.id) {
			cmd := def.Command(c.id, spec.Request)
			return fmt.Errorf("%w: %s %s", ErrNotImplemented, def.Name, cmd.Name)
		}
	}
	return nil
}

// checkState refuses a server whose selection serves a list- or
// struct-typed attribute that has no value: the definition states no
// default for it and the host gave none.
func checkState(srv *spec.Server) error {
	def := srv.Definition()
	for i := range def.Attributes {
		a := &def.Attributes[i]
		if !srv.Serves(a.ID) {
			continue
		}
		if a.Type.Kind != spec.KindList && a.Type.Kind != spec.KindStruct {
			continue
		}
		if _, ok := srv.Value(a.ID); !ok {
			return fmt.Errorf("%w: %s %s", ErrMissingState, def.Name, a.Name)
		}
	}
	return nil
}

// served keeps the values of the attributes srv serves. An attribute the
// state leaves unset (absent) that srv serves is refused
// ([ErrMissingState]): it would read as nothing.
func served(srv *spec.Server, values map[uint32]any) (map[uint32]any, error) {
	out := make(map[uint32]any, len(values))
	for id, v := range values {
		if !srv.Serves(id) {
			continue
		}
		if v == absent {
			def := srv.Definition()
			return nil, fmt.Errorf("%w: %s %s", ErrMissingState, def.Name, def.Attribute(id).Name)
		}
		out[id] = v
	}
	return out, nil
}

// absentValue marks an attribute the state leaves unset.
type absentValue struct{}

// absent is the value of an attribute the state leaves unset: a nil list
// or struct pointer.
var absent any = absentValue{}

// listOr is l as a generated list, or absent when l is nil.
func listOr[T spec.Encodable](l []T) any {
	if l == nil {
		return absent
	}
	return spec.List[T](l)
}

// structOr is *p, or absent when p is nil.
func structOr[T any](p *T) any {
	if p == nil {
		return absent
	}
	return *p
}

// nullOr is *p, or nil (null) when p is nil.
func nullOr[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// stateful is the command transaction of a server with state S: one
// command or SetState at a time, the port changing a copy that is committed
// only when the command succeeds.
type stateful[S any] struct {
	srv *spec.Server
	// values maps a state to the attribute values it reports.
	values func(S) map[uint32]any
	// clone copies a state so a port changing it touches no committed
	// value.
	clone func(S) S

	// mu guards clone, srv, state and values.
	mu    sync.Mutex
	state S
}

// init commits the initial state; the server's checks apply.
func (s *stateful[S]) init(srv *spec.Server, st S, values func(S) map[uint32]any, clone func(S) S) error {
	s.srv, s.values, s.clone = srv, values, clone
	vals, err := served(srv, values(st))
	if err != nil {
		return err
	}
	if err := srv.SetAttributes(vals); err != nil {
		return err
	}
	s.state = clone(st)
	return nil
}

// State returns a copy of the committed state.
func (s *stateful[S]) State() S {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clone(s.state)
}

// SetState commits st, a change the device made: every value is checked
// against the definition (an error changes nothing), and what moved is
// reported with one data-version bump.
func (s *stateful[S]) SetState(st S) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commitLocked(st)
}

func (s *stateful[S]) commitLocked(st S) error {
	vals, err := served(s.srv, s.values(st))
	if err != nil {
		return err
	}
	if err := s.srv.SetAttributes(vals); err != nil {
		return err
	}
	s.state = s.clone(st)
	return nil
}

// transact runs fn on a copy of the state and commits the copy when fn
// succeeds; a state the definition refuses fails the command with
// FAILURE.
func (s *stateful[S]) transact(fn func(st *S) (any, error)) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.clone(s.state)
	resp, err := fn(&next)
	if err != nil {
		return nil, err
	}
	if err := s.commitLocked(next); err != nil {
		return nil, StatusError{Status: im.StatusFailure, Err: err}
	}
	return resp, nil
}

// dataVersion is the optional host-owned data version every config takes.
type dataVersion = *cluster.DataVersionTracker

// events stamps the endpoint a server's events are addressed to; the
// bridge calls SetEndpoint when it mounts the server.
type events struct {
	// epMu guards ep.
	epMu sync.Mutex
	ep   uint16
}

// SetEndpoint stamps the endpoint the server's events are addressed to.
func (e *events) SetEndpoint(endpoint uint16) {
	e.epMu.Lock()
	e.ep = endpoint
	e.epMu.Unlock()
}

func (e *events) endpoint() uint16 {
	e.epMu.Lock()
	defer e.epMu.Unlock()
	return e.ep
}
