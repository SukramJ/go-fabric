// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"fmt"
	"go/format"
	"strings"
)

// The typed server ("the hull", concept v6 A.3 step 1): server_gen.go
// beside each definition declares State, Patch and Server, a typed view of
// any contract.ClusterServer of the cluster (brief BT and its addendum) that
// adds no behavior, with Emit<Event> per event. The runtime half is
// cluster/spec/typed.go and cluster/spec/wrapper.go.
//
// Server.Set takes a Patch: the fields set, the others left. Mirrors
// matter.js packages/node/src/endpoint/Endpoint.ts:256-262 (`set`, a
// patch of the behaviors' state applied in one transaction, an error
// leaving no change): spec.Server.SetAttributes checks every value before
// it stores any.
//
// Server.OnChanged reports a change with the state and the change (the
// changed attribute ids and the origin). Mirrors matter.js
// packages/node/src/behavior/state/managed/Datasource.ts:202 (Events, the
// per-attribute `$Changed` observables emitted after a change commits) and
// :1473-1476 (each emitted with the new value, the old one and the session
// that made the change), delivered through the server's
// contract.AttributeChangeNotifier or contract.ChangeNotifier.
// Server.OnChangedAsync queues the same call on the device's goroutine.
//
// Server.OnWrite installs a veto on a controller's write, called after the
// definition's checks and before the store, an error refusing the write.
// Mirrors matter.js packages/node/src/behavior/state/managed/Datasource.ts:1172-1195
// (preCommit, which emits `$Changing` with the new and old value before a
// change commits) and packages/general/src/transaction/Participant.ts:16-23
// (a preCommit failure rolls the transaction back). matter.js fires
// `$Changing` for every change in the transaction; the hull vetoes the
// controller's writes, spec.Server.MatterWrite — a host's own Set is the
// host's decision.
//
// Server.Handle<Command> registers a typed handler for a request command.
// Mirrors matter.js packages/node/src/devices/on-off-light.ts:48-100
// (OnOffServer and the other requirement behaviors a device type names,
// which an application extends to override a command method with the
// command's typed request and response).

// hullField is one attribute field of State and Patch.
type hullField struct {
	a        *element
	goName   string
	goType   string // the State type, a nullable one a pointer
	base     string // the attribute's Go type
	nullable bool
}

// hullOmitted is an attribute the hull's State leaves out.
type hullOmitted struct {
	a   *element
	why string
}

// hullAttributes splits the definition's attributes into the State's
// fields and the ones it cannot express.
func (g *generator) hullAttributes() ([]hullField, []hullOmitted) {
	attrs := g.attributes()
	fields := make([]hullField, 0, len(attrs))
	var omitted []hullOmitted
	for _, a := range attrs {
		v := &a.Effective.value
		t := g.attrType[a.ID]
		switch {
		case v.Access != nil && v.Access.Fabric == "F":
			omitted = append(omitted, hullOmitted{a, "fabric-scoped: spec.NewServer refuses to serve it"})
			continue
		case untyped(t):
			omitted = append(omitted, hullOmitted{a, "its type is one the generator maps to any"})
			continue
		}
		f := hullField{a: a, goName: exportName(a.Name), base: t.goType, goType: t.goType, nullable: v.Quality["nullable"]}
		if f.nullable {
			f.goType = "*" + t.goType
		}
		fields = append(fields, f)
	}
	return fields, omitted
}

// untyped reports whether t is, or is a list of, a value the generator
// maps to any.
func untyped(t typeInfo) bool {
	switch t.kind {
	case kindAny:
		return true
	case kindList:
		return untyped(t.elem.t)
	default:
		return false
	}
}

// hullCommand is one request command's typed handler.
type hullCommand struct {
	method, cmdID, request, response string
}

// hullCommands are the request commands with their typed request and
// response types; a command answered by status alone responds struct{}.
// The response is named as spec.Cluster.responseOf names it
// (cluster/spec/instance.go).
func (g *generator) hullCommands() []hullCommand {
	var out []hullCommand
	for i, cmd := range g.c.Commands {
		e := cmd.Effective
		if e.Direction != "request" {
			continue
		}
		// The method name is unique as the Cmd constant source() claims is.
		hc := hullCommand{method: "Handle" + exportName(cmd.Name), cmdID: "Cmd" + exportName(cmd.Name), request: g.payloads[i].goName, response: "struct{}"}
		if e.Response != "" && e.Response != "status" {
			for _, p := range g.payloads {
				if p.response != nil && p.response.Name == e.Response {
					hc.response = p.goName
					break
				}
			}
		}
		out = append(out, hc)
	}
	return out
}

// server renders server_gen.go.
//
//nolint:funlen // a template: the sections of the generated file in order
func (g *generator) server() ([]byte, error) {
	fields, omitted := g.hullAttributes()
	cmds := g.hullCommands()
	events := g.hullEvents()
	nullable := false
	for _, f := range fields {
		nullable = nullable || f.nullable
		if f.goName == "Null" {
			return nil, fmt.Errorf("attribute %s collides with Patch.Null", f.a.Name)
		}
	}
	c := g.c
	w := &writer{}
	w.WriteString(header)
	w.p("package %s", g.pkg)
	w.p("")
	w.p("import (")
	w.p("\t\"context\"")
	w.p("\t\"fmt\"")
	w.p("")
	w.p("\t\"github.com/SukramJ/go-fabric/cluster/spec\"")
	w.p("\t\"github.com/SukramJ/go-fabric/contract\"")
	w.p(")")
	w.p("")

	w.p("// State is the typed state of the %s cluster: one field per attribute", c.Name)
	w.p("// of [Definition], a nullable one a pointer (nil for null), a list a")
	w.p("// slice. [Server.Get] fills it from the stored values; an attribute the")
	w.p("// server does not serve stays at its zero value.")
	if len(omitted) > 0 {
		w.p("//")
		w.p("// Not in State, and set only untyped (spec.Server.Set):")
		for _, o := range omitted {
			w.p("//   - %s (0x%04X): %s.", o.a.Name, o.a.ID, o.why)
		}
	}
	w.p("type State struct {")
	for _, f := range fields {
		w.p("\t%s %s `matter:\"0x%04X\"` // attribute 0x%04X: %s", f.goName, f.goType, f.a.ID, f.a.ID, describe(&f.a.Effective.value))
	}
	w.p("}")
	w.p("")

	w.p("// Patch is a change to the %s state: each non-nil field is set, the", c.Name)
	w.p("// others are left as they are.")
	if nullable {
		w.p("// Null lists the nullable attributes the patch sets to null.")
	}
	w.p("type Patch struct {")
	for _, f := range fields {
		w.p("\t%s *%s `matter:\"0x%04X\"` // attribute 0x%04X", f.goName, f.base, f.a.ID, f.a.ID)
	}
	if nullable {
		w.p("\tNull []uint32 // attribute ids set to null")
	}
	w.p("}")
	w.p("")

	w.p("// Server is the typed %s server over the cluster's server — the", c.Name)
	w.p("// generated spec.Server or a self-held one (Underlying) — with a typed")
	w.p("// state, typed change reports, a write veto, typed command handlers and")
	w.p("// typed events. It adds no behavior: each method reaches the server's")
	w.p("// own. It is not a contract.ClusterServer: mount the underlying server,")
	w.p("// not this one.")
	w.p("type Server struct{ spec.Wrapper }")
	w.p("")
	w.p("// Wrap returns srv as the typed server; nil for nil. A typed server is")
	w.p("// no contract.ClusterServer, so it is never wrapped twice.")
	w.p("func Wrap(srv contract.ClusterServer) *Server {")
	w.p("\tif srv == nil {")
	w.p("\t\treturn nil")
	w.p("\t}")
	w.p("\treturn &Server{spec.NewWrapper(srv)}")
	w.p("}")
	w.p("")
	w.p("// Set applies p through the server's SetAttributes: every value is")
	w.p("// checked, and all of them are stored or none; spec.ErrNotSettable for")
	w.p("// a server without one.")
	w.p("func (s *Server) Set(p Patch) error { return spec.SetPatch(s.Underlying(), p) }")
	w.p("")
	w.p("// Get returns the state, each attribute as the server reads it.")
	w.p("func (s *Server) Get() State { return spec.StateOf[State](s.Underlying()) }")
	w.p("")
	w.p("// OnChanged calls fn with the state and the change — the attributes")
	w.p("// that changed and its origin (contract.Origin) — after each change the")
	w.p("// server reports, inline: on the goroutine that made the change, before")
	w.p("// the write, command or Set that made it returns. It returns the")
	w.p("// unsubscribe function.")
	w.p("func (s *Server) OnChanged(fn func(state State, change contract.Change)) (unsubscribe func()) {")
	w.p("\treturn spec.OnChangedOf(s.Underlying(), fn)")
	w.p("}")
	w.p("")
	w.p("// OnChangedAsync is OnChanged with fn queued: the state and the change")
	w.p("// are taken when the server reports it, and fn runs later on the")
	w.p("// device's goroutine, one call after the other. spec.ErrNoQueue for a")
	w.p("// server wrapped outside a device (Wrap).")
	w.p("func (s *Server) OnChangedAsync(fn func(state State, change contract.Change)) (unsubscribe func(), err error) {")
	w.p("\treturn spec.OnChangedAsyncOf(s.Wrapper, fn)")
	w.p("}")
	w.p("")
	w.p("// OnWrite installs fn as the veto of a controller's write")
	w.p("// (spec.ServerConfig.BeforeWrite): next is the state with the written")
	w.p("// value in place, attrID the attribute written. An error refuses the")
	w.p("// write — with the IM status it carries, FAILURE otherwise — and nothing")
	w.p("// is stored. nil removes the veto; spec.ErrNoWriteVeto for a server")
	w.p("// that takes none.")
	w.p("func (s *Server) OnWrite(fn func(ctx context.Context, next State, attrID uint32) error) error {")
	w.p("\treturn spec.OnWriteOf(s.Underlying(), fn)")
	w.p("}")
	for _, hc := range cmds {
		w.p("")
		w.p("// %s registers fn for the %s command; nil removes the handler;", hc.method, strings.TrimPrefix(hc.cmdID, "Cmd"))
		w.p("// spec.ErrNoHandlers for a server that takes none.")
		w.p("func (s *Server) %s(fn func(ctx context.Context, req %s) (%s, error)) error {", hc.method, hc.request, hc.response)
		w.p("\treturn spec.HandleOn(s.Underlying(), %s, fn)", hc.cmdID)
		w.p("}")
	}
	for _, he := range events {
		w.p("")
		w.p("// %s emits the %s event on endpoint through the server's Emit;", he.method, strings.TrimPrefix(he.eventID, "Event"))
		w.p("// spec.ErrNoEvents for a server without one.")
		w.p("func (s *Server) %s(endpoint uint16, data %s) error {", he.method, he.payload)
		w.p("\treturn spec.EmitOn(s.Underlying(), endpoint, %s, data)", he.eventID)
		w.p("}")
	}
	w.p("")
	g.declaration(w, cmds)

	out, err := format.Source(w.Bytes())
	if err != nil {
		return nil, fmt.Errorf("gofmt server: %w\n%s", err, w.String())
	}
	return out, nil
}

// serverTest is the generated test's TestTypedServer: every method of the
// typed server once, over a server of an empty definition with the
// cluster's id, so every cluster's hull runs whatever its feature and
// handler requirements.
func (g *generator) serverTest(w *writer) {
	cmds := g.hullCommands()
	w.p("")
	w.p("// TestTypedServer drives every method of the typed server once.")
	w.p("func TestTypedServer(t *testing.T) {")
	w.p("\tt.Parallel()")
	w.p("\tif Wrap(nil) != nil {")
	w.p("\t\tt.Error(\"Wrap(nil) is not nil\")")
	w.p("\t}")
	w.p("\tsrv, err := spec.NewServer(&spec.Cluster{ID: ClusterID, Name: Name, Revision: Revision}, spec.Options{}, spec.ServerConfig{})")
	w.p("\tif err != nil {")
	w.p("\t\tt.Fatal(err)")
	w.p("\t}")
	w.p("\ts := Wrap(srv)")
	w.p("\tif s.Underlying() != srv {")
	w.p("\t\tt.Error(\"the typed server does not hold its server\")")
	w.p("\t}")
	w.p("\tif err := s.Set(Patch{}); err != nil {")
	w.p("\t\tt.Error(err)")
	w.p("\t}")
	w.p("\t_ = s.Get()")
	w.p("\ts.OnChanged(func(State, contract.Change) {})()")
	w.p("\tif _, err := s.OnChangedAsync(func(State, contract.Change) {}); !errors.Is(err, spec.ErrNoQueue) {")
	w.p("\t\tt.Errorf(\"OnChangedAsync without a queue: %%v, want spec.ErrNoQueue\", err)")
	w.p("\t}")
	w.p("\tif err := s.OnWrite(func(context.Context, State, uint32) error { return nil }); err != nil {")
	w.p("\t\tt.Error(err)")
	w.p("\t}")
	for _, hc := range cmds {
		// Not in the empty definition's AcceptedCommandList: refused
		// (spec.ErrNotAccepted).
		w.p("\tif err := s.%s(func(context.Context, %s) (%s, error) { return %s{}, nil }); err == nil {", hc.method, hc.request, hc.response, hc.response)
		w.p("\t\tt.Error(\"%s: a command the server does not accept was registered\")", hc.method)
		w.p("\t}")
	}
	for _, he := range g.hullEvents() {
		w.p("\t_ = s.%s(0, %s{}) // not in the empty definition's EventList: refused", he.method, he.payload)
	}
	w.p("\t// The declaration: its cluster, its kit form, its veto installed.")
	w.p("\td := Declare{OnWrite: func(context.Context, State, uint32) error { return nil }}")
	w.p("\tdecl := d.ClusterDeclaration()")
	w.p("\tif d.ClusterID() != ClusterID || decl.Cluster != ClusterID || decl.Attach == nil {")
	w.p("\t\tt.Errorf(\"Declare: cluster %%v / %%v, attach %%v\", d.ClusterID(), decl.Cluster, decl.Attach != nil)")
	w.p("\t}")
	w.p("\tif err := decl.Attach(srv); err != nil {")
	w.p("\t\tt.Error(err)")
	w.p("\t}")
	for _, port := range ports[g.c.Name].ports {
		w.p("\tvar _ %s = d.%s", port.name, port.name)
	}
	w.p("}")
}

// hullEvent is one event's typed emitter.
type hullEvent struct {
	method, eventID, payload string
}

// hullEvents are the definition's events with their payload types (the
// <Event>Event structs collect declares).
func (g *generator) hullEvents() []hullEvent {
	var out []hullEvent
	for _, p := range g.payloads {
		if p.event != nil {
			out = append(out, hullEvent{method: "Emit" + exportName(p.event.Name), eventID: "Event" + exportName(p.event.Name), payload: p.goName})
		}
	}
	return out
}
