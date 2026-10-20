// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"fmt"
	"go/format"
	"strings"
)

// The typed server ("the hull", concept v6 A.3 step 1): server_gen.go
// beside each definition declares State, Patch and Server, a typed form of
// spec.Server that embeds it and adds no behavior. The runtime half is
// cluster/spec/typed.go.
//
// Server.Set takes a Patch: the fields set, the others left. Mirrors
// matter.js packages/node/src/endpoint/Endpoint.ts:256-262 (`set`, a
// patch of the behaviors' state applied in one transaction, an error
// leaving no change): spec.Server.SetAttributes checks every value before
// it stores any.
//
// Server.OnChanged reports a change with the state and the changed
// attribute ids. Mirrors matter.js
// packages/node/src/behavior/state/managed/Datasource.ts:202 (Events, the
// per-attribute `$Changed` observables emitted after a change commits),
// delivered through spec.Server's contract.AttributeChangeNotifier.
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
	var fields []hullField
	var omitted []hullOmitted
	for _, a := range g.attributes() {
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
	w.p("")
	w.p("\t\"github.com/SukramJ/go-fabric/cluster/spec\"")
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

	w.p("// Server is the typed %s server: the generated server it embeds,", c.Name)
	w.p("// with a typed state, typed change reports, a write veto and typed")
	w.p("// command handlers. It adds no behavior: every method of the embedded")
	w.p("// server is its own, Set taking a Patch (the embedded one's is")
	w.p("// s.Server.Set).")
	w.p("type Server struct{ *spec.Server }")
	w.p("")
	w.p("// Wrap returns s as the typed server; nil for nil.")
	w.p("func Wrap(s *spec.Server) *Server {")
	w.p("\tif s == nil {")
	w.p("\t\treturn nil")
	w.p("\t}")
	w.p("\treturn &Server{s}")
	w.p("}")
	w.p("")
	w.p("// Set applies p: every value is checked as spec.Server.SetAttributes")
	w.p("// checks it, and all of them are stored or none.")
	w.p("func (s *Server) Set(p Patch) error { return s.SetAttributes(spec.PatchValues(p)) }")
	w.p("")
	w.p("// Get returns the state, read at one instant.")
	w.p("func (s *Server) Get() State { return spec.StateOf[State](s.Server) }")
	w.p("")
	w.p("// OnChanged calls fn with the state and the ids of the attributes that")
	w.p("// changed, after each change; it returns the unsubscribe function.")
	w.p("func (s *Server) OnChanged(fn func(state State, changed []uint32)) (unsubscribe func()) {")
	w.p("\treturn spec.OnChangedOf(s.Server, fn)")
	w.p("}")
	w.p("")
	w.p("// OnWrite installs fn as the veto of a controller's write")
	w.p("// (spec.ServerConfig.BeforeWrite): next is the state with the written")
	w.p("// value in place, attrID the attribute written. An error refuses the")
	w.p("// write — with the IM status it carries, FAILURE otherwise — and nothing")
	w.p("// is stored. nil removes the veto.")
	w.p("func (s *Server) OnWrite(fn func(ctx context.Context, next State, attrID uint32) error) {")
	w.p("\ts.SetBeforeWrite(spec.BeforeWriteOf(s.Server, fn))")
	w.p("}")
	for _, hc := range cmds {
		w.p("")
		w.p("// %s registers fn for the %s command; nil removes the handler.", hc.method, strings.TrimPrefix(hc.cmdID, "Cmd"))
		w.p("func (s *Server) %s(fn func(ctx context.Context, req %s) (%s, error)) {", hc.method, hc.request, hc.response)
		w.p("\ts.Handle(%s, spec.HandlerOf(fn))", hc.cmdID)
		w.p("}")
	}
	w.p("")
	w.p("func init() { spec.RegisterTyped(ClusterID, Wrap) }")

	out, err := format.Source(w.Bytes())
	if err != nil {
		return nil, fmt.Errorf("gofmt server: %w\n%s", err, w.String())
	}
	return out, nil
}

// serverTest is the generated test's TestTypedServer: every method of the
// typed server once, on a server of an empty definition with the
// cluster's id, so every cluster's hull runs whatever its feature and
// handler requirements.
func (g *generator) serverTest(w *writer) {
	cmds := g.hullCommands()
	w.p("")
	w.p("// TestTypedServer drives every method of the typed server once, and")
	w.p("// finds it registered for the cluster.")
	w.p("func TestTypedServer(t *testing.T) {")
	w.p("\tt.Parallel()")
	w.p("\tif Wrap(nil) != nil {")
	w.p("\t\tt.Error(\"Wrap(nil) is not nil\")")
	w.p("\t}")
	w.p("\tsrv, err := spec.NewServer(&spec.Cluster{ID: ClusterID, Name: Name, Revision: Revision}, spec.Options{}, spec.ServerConfig{})")
	w.p("\tif err != nil {")
	w.p("\t\tt.Fatal(err)")
	w.p("\t}")
	w.p("\tif _, ok := spec.TypedServer(srv).(*Server); !ok {")
	w.p("\t\tt.Errorf(\"spec.TypedServer: %%T\", spec.TypedServer(srv))")
	w.p("\t}")
	w.p("\ts := Wrap(srv)")
	w.p("\tif err := s.Set(Patch{}); err != nil {")
	w.p("\t\tt.Error(err)")
	w.p("\t}")
	w.p("\t_ = s.Get()")
	w.p("\ts.OnChanged(func(State, []uint32) {})()")
	w.p("\ts.OnWrite(func(context.Context, State, uint32) error { return nil })")
	for _, hc := range cmds {
		w.p("\ts.%s(func(context.Context, %s) (%s, error) { return %s{}, nil })", hc.method, hc.request, hc.response, hc.response)
	}
	w.p("}")
}
