// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package spectest holds the parity assertions every generated cluster
// definition, and every server built on one, is checked with: the
// definition against the matter.js snapshot it was generated from
// ([CheckDefinition]), a server's lists, globals, privileges and read-only
// writes against the definition's conformance ([CheckServer]), and the TLV
// round trip of generated payloads ([RoundTrip] & co).
//
// It is test support, on the same API-stability terms as bridge/bridgetest.
package spectest

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/tlv"
)

// Ptr returns a pointer to v: the value of an optional generated field.
func Ptr[T any](v T) *T { return &v }

var privileges = map[string]spec.Privilege{
	"V": spec.PrivilegeView, "P": spec.PrivilegeProxyView, "O": spec.PrivilegeOperate,
	"M": spec.PrivilegeManage, "A": spec.PrivilegeAdminister,
}

var qualities = []struct {
	key string
	get func(spec.Quality) bool
}{
	{"nullable", func(q spec.Quality) bool { return q.Nullable }},
	{"nonvolatile", func(q spec.Quality) bool { return q.NonVolatile }},
	{"fixed", func(q spec.Quality) bool { return q.Fixed }},
	{"scene", func(q spec.Quality) bool { return q.Scene }},
	{"reportable", func(q spec.Quality) bool { return q.Reportable }},
	{"changesOmitted", func(q spec.Quality) bool { return q.ChangesOmitted }},
	{"singleton", func(q spec.Quality) bool { return q.Singleton }},
	{"quieter", func(q spec.Quality) bool { return q.Quieter }},
	{"largeMessage", func(q spec.Quality) bool { return q.LargeMessage }},
	{"diagnostics", func(q spec.Quality) bool { return q.Diagnostics }},
	{"atomic", func(q spec.Quality) bool { return q.Atomic }},
}

// CheckDefinition holds def against the snapshot entry of its cluster:
// identity, revision and base, every feature's bit, title and conformance,
// and every attribute's, command's and event's id, name, conformance,
// access, quality, priority and response — and no element either side
// lacks.
func CheckDefinition(tb testing.TB, def *spec.Cluster) {
	tb.Helper()
	js := paritytest.ClusterSnapshot(tb, def.ID)
	if def.Name != js.Name || def.Revision != js.Revision || def.Base != js.Base {
		tb.Errorf("%s: identity %s rev %d base %q, snapshot %s rev %d base %q",
			def.Name, def.Name, def.Revision, def.Base, js.Name, js.Revision, js.Base)
	}
	checkFeatures(tb, def, js)
	checkAttributes(tb, def, js)
	checkCommands(tb, def, js)
	checkEvents(tb, def, js)
}

func checkFeatures(tb testing.TB, def *spec.Cluster, js *paritytest.Cluster) {
	tb.Helper()
	if len(def.Features) != len(js.Features) {
		tb.Errorf("%s: %d features, snapshot %d", def.Name, len(def.Features), len(js.Features))
	}
	for _, f := range js.Features {
		got := def.Feature(f.Name)
		switch {
		case got == nil:
			tb.Errorf("%s: feature %s missing", def.Name, f.Name)
		case got.Bit != uint(f.Bit):
			tb.Errorf("%s: feature %s bit %d, snapshot %d", def.Name, f.Name, got.Bit, f.Bit)
		case f.Effective != nil && (got.Title != f.Effective.Title || got.Conformance.Text != text(f.Effective.Conformance)):
			tb.Errorf("%s: feature %s %q %q, snapshot %q %q", def.Name, f.Name, got.Title, got.Conformance.Text,
				f.Effective.Title, text(f.Effective.Conformance))
		}
	}
}

func checkAttributes(tb testing.TB, def *spec.Cluster, js *paritytest.Cluster) {
	tb.Helper()
	n := 0
	for _, a := range js.Attributes {
		if a.ID >= 0xFFF0 {
			continue
		}
		n++
		got := def.Attribute(a.ID)
		if got == nil || got.Name != a.Name {
			tb.Errorf("%s: attribute 0x%04X %s missing", def.Name, a.ID, a.Name)
			continue
		}
		checkEffective(tb, def.Name+"."+a.Name, a.Effective, got.Conformance, got.Access)
		if a.Effective == nil {
			continue
		}
		if got.Type.Name != a.Effective.Type {
			tb.Errorf("%s.%s: type %q, snapshot %q", def.Name, a.Name, got.Type.Name, a.Effective.Type)
		}
		for _, q := range qualities {
			if q.get(got.Quality) != a.Effective.Quality[q.key] {
				tb.Errorf("%s.%s: quality %s %t, snapshot %t", def.Name, a.Name, q.key, q.get(got.Quality), a.Effective.Quality[q.key])
			}
		}
	}
	if len(def.Attributes) != n {
		tb.Errorf("%s: %d attributes, snapshot %d", def.Name, len(def.Attributes), n)
	}
}

func checkCommands(tb testing.TB, def *spec.Cluster, js *paritytest.Cluster) {
	tb.Helper()
	if len(def.Commands) != len(js.Commands) {
		tb.Errorf("%s: %d commands, snapshot %d", def.Name, len(def.Commands), len(js.Commands))
	}
	for _, c := range js.Commands {
		dir := spec.Request
		if c.Effective != nil && c.Effective.Direction == "response" {
			dir = spec.Response
		}
		got := def.Command(c.ID, dir)
		if got == nil || got.Name != c.Name {
			tb.Errorf("%s: command 0x%02X %s missing", def.Name, c.ID, c.Name)
			continue
		}
		checkEffective(tb, def.Name+"."+c.Name, c.Effective, got.Conformance, got.Access)
		if c.Effective != nil && got.Response != c.Effective.Response {
			tb.Errorf("%s.%s: response %q, snapshot %q", def.Name, c.Name, got.Response, c.Effective.Response)
		}
		if got.Decode == nil {
			tb.Errorf("%s.%s: no payload decoder", def.Name, c.Name)
		}
	}
}

var priorities = map[string]spec.Priority{"debug": spec.PriorityDebug, "info": spec.PriorityInfo, "critical": spec.PriorityCritical}

func checkEvents(tb testing.TB, def *spec.Cluster, js *paritytest.Cluster) {
	tb.Helper()
	if len(def.Events) != len(js.Events) {
		tb.Errorf("%s: %d events, snapshot %d", def.Name, len(def.Events), len(js.Events))
	}
	for _, e := range js.Events {
		got := def.Event(e.ID)
		if got == nil || got.Name != e.Name {
			tb.Errorf("%s: event 0x%02X %s missing", def.Name, e.ID, e.Name)
			continue
		}
		checkEffective(tb, def.Name+"."+e.Name, e.Effective, got.Conformance, got.Access)
		if e.Effective != nil && got.Priority != priorities[e.Effective.Priority] {
			tb.Errorf("%s.%s: priority %d, snapshot %s", def.Name, e.Name, got.Priority, e.Effective.Priority)
		}
	}
}

func text(t *paritytest.Text) string {
	if t == nil {
		return ""
	}
	return t.Text
}

func checkEffective(tb testing.TB, what string, eff *paritytest.Effective, c spec.Conformance, a spec.Access) {
	tb.Helper()
	if eff == nil {
		tb.Errorf("%s: the snapshot has no resolved layer", what)
		return
	}
	if c.Text != text(eff.Conformance) {
		tb.Errorf("%s: conformance %q, snapshot %q", what, c.Text, text(eff.Conformance))
	}
	var want spec.Access
	if eff.Access != nil {
		want = spec.Access{
			RW: eff.Access.RW, Read: privileges[eff.Access.ReadPriv], Write: privileges[eff.Access.WritePriv],
			Fabric: eff.Access.Fabric, Timed: eff.Access.Timed,
		}
	}
	if a != want {
		tb.Errorf("%s: access %+v, snapshot %+v", what, a, want)
	}
}

// Encode encodes v under an anonymous tag.
func Encode(tb testing.TB, v spec.Encodable) []byte {
	tb.Helper()
	enc := tlv.NewEncoder()
	v.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		tb.Fatalf("encode %T: %v", v, err)
	}
	return b
}

// Decode reads one element from b.
func Decode(tb testing.TB, b []byte) spec.Node {
	tb.Helper()
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		tb.Fatalf("decode % X: %v", b, err)
	}
	return n
}

// RoundTrip encodes v, decodes the bytes with dec and wants v back.
func RoundTrip[T spec.Encodable](tb testing.TB, v T, dec spec.Decoder[T]) {
	tb.Helper()
	b := Encode(tb, v)
	got, err := dec(Decode(tb, b))
	if err != nil {
		tb.Errorf("%T: decode % X: %v", v, b, err)
		return
	}
	if !reflect.DeepEqual(got, v) {
		tb.Errorf("%T: round trip\n got %+v\nwant %+v", v, got, v)
	}
}

// roundTripVia encodes v and decodes it with dec, wanting v back.
func roundTripVia(tb testing.TB, what string, dec func(spec.Node) (any, error), v spec.Encodable) {
	tb.Helper()
	if dec == nil {
		tb.Fatalf("%s: no decoder", what)
	}
	b := Encode(tb, v)
	got, err := dec(Decode(tb, b))
	if err != nil {
		tb.Errorf("%s: decode % X: %v", what, b, err)
		return
	}
	if !reflect.DeepEqual(got, v) {
		tb.Errorf("%s: round trip\n got %+v\nwant %+v", what, got, v)
	}
}

// RoundTripCommand encodes a command payload and decodes it through the
// definition's decoder for the command — the path the bridge takes for a
// request. A response payload must also name the response command.
func RoundTripCommand(tb testing.TB, def *spec.Cluster, cmdID uint32, dir spec.Direction, v spec.Encodable) {
	tb.Helper()
	cmd := def.Command(cmdID, dir)
	if cmd == nil {
		tb.Fatalf("%s: no command 0x%02X in direction %d", def.Name, cmdID, dir)
	}
	roundTripVia(tb, def.Name+"."+cmd.Name, cmd.Decode, v)
	if dir != spec.Response {
		return
	}
	r, ok := v.(spec.ResponsePayload)
	if !ok {
		tb.Errorf("%T is no response payload", v)
		return
	}
	if clusterID, id := r.ResponseCommand(); clusterID != def.ID || id != cmdID {
		tb.Errorf("%T names response 0x%04X/0x%02X, want 0x%04X/0x%02X", v, clusterID, id, def.ID, cmdID)
	}
}

// RoundTripEvent round-trips an event payload through the definition's
// decoder for the event.
func RoundTripEvent(tb testing.TB, def *spec.Cluster, eventID uint32, v spec.Encodable) {
	tb.Helper()
	e := def.Event(eventID)
	if e == nil {
		tb.Fatalf("%s: no event 0x%02X", def.Name, eventID)
	}
	roundTripVia(tb, def.Name+"."+e.Name, e.Decode, v)
}

// RoundTripAttribute round-trips an attribute value through the
// definition's decoder for the attribute.
func RoundTripAttribute(tb testing.TB, def *spec.Cluster, attrID uint32, v spec.Encodable) {
	tb.Helper()
	a := def.Attribute(attrID)
	if a == nil {
		tb.Fatalf("%s: no attribute 0x%04X", def.Name, attrID)
	}
	roundTripVia(tb, def.Name+"."+a.Name, a.Decode, v)
}

// CheckServer holds a server built on def with the given FeatureMap against
// def's conformance, as matter.js's ValidatedElements would: FeatureMap and
// ClusterRevision read as the selection and the definition say; every
// listed attribute, command and event is defined and allowed, every
// mandatory one listed, GeneratedCommandList the responses of the accepted
// requests; the privileges are matter.js's; a write to a served read-only
// attribute is UNSUPPORTED_WRITE.
func CheckServer(tb testing.TB, srv contract.ClusterServer, def *spec.Cluster, features uint32) {
	tb.Helper()
	if srv.MatterClusterID() != def.ID {
		tb.Errorf("cluster id 0x%04X, want 0x%04X", srv.MatterClusterID(), def.ID)
	}
	if v, ok := srv.MatterRead(spec.AttrFeatureMap); !ok || v != features {
		tb.Errorf("%s: FeatureMap = %v (%v), want 0x%X", def.Name, v, ok, features)
	}
	if v, ok := srv.MatterRead(spec.AttrClusterRevision); !ok || v != def.Revision {
		tb.Errorf("%s: ClusterRevision = %v (%v), want %d", def.Name, v, ok, def.Revision)
	}
	ctx := spec.Context(def, features)

	if l, ok := srv.(contract.ClusterAttributeLister); ok {
		attrs := l.MatterAttributes()
		for ai := range def.Attributes {
			a := &def.Attributes[ai]
			checkPresence(tb, def.Name, "attribute", a.Name, a.Conformance.Applicability(ctx), slices.Contains(attrs, a.ID))
		}
		for _, id := range attrs {
			if def.Attribute(id) == nil {
				tb.Errorf("%s: lists undefined attribute 0x%04X", def.Name, id)
			}
		}
		checkWrites(tb, srv, def, attrs)
	}

	if l, ok := srv.(contract.ClusterCommandLister); ok {
		accepted, generated := l.MatterAcceptedCommands(), l.MatterGeneratedCommands()
		var want []uint32
		for ci := range def.Commands {
			c := &def.Commands[ci]
			if c.Direction != spec.Request {
				continue
			}
			has := slices.Contains(accepted, c.ID)
			checkPresence(tb, def.Name, "command", c.Name, c.Conformance.Applicability(ctx), has)
			if r := def.CommandByName(c.Response); has && r != nil && !slices.Contains(want, r.ID) {
				want = append(want, r.ID)
			}
		}
		slices.Sort(want)
		got := slices.Sorted(slices.Values(generated))
		if !slices.Equal(got, want) {
			tb.Errorf("%s: GeneratedCommandList %v, want %v", def.Name, got, want)
		}
		if p, ok := srv.(contract.ClusterCommandInvokePrivilege); ok {
			for _, id := range accepted {
				if c := def.Command(id, spec.Request); c != nil && p.MinInvokePrivilege(id) != uint8(c.Access.WritePrivilege()) {
					tb.Errorf("%s.%s: invoke privilege %d, want %d", def.Name, c.Name, p.MinInvokePrivilege(id), c.Access.WritePrivilege())
				}
			}
		}
	}

	if l, ok := srv.(contract.ClusterEventLister); ok {
		events := l.MatterEvents()
		for ei := range def.Events {
			e := &def.Events[ei]
			checkPresence(tb, def.Name, "event", e.Name, e.Conformance.Applicability(ctx), slices.Contains(events, e.ID))
		}
	}
}

func checkPresence(tb testing.TB, cluster, kind, name string, a spec.Applicability, has bool) {
	tb.Helper()
	switch {
	case a == spec.ApplicabilityMandatory && !has:
		tb.Errorf("%s: mandatory %s %s missing", cluster, kind, name)
	case a == spec.ApplicabilityNone && has:
		tb.Errorf("%s: %s %s listed but disallowed", cluster, kind, name)
	}
}

func checkWrites(tb testing.TB, srv contract.ClusterServer, def *spec.Cluster, attrs []uint32) {
	tb.Helper()
	wp, hasPriv := srv.(contract.ClusterAttributeWritePrivilege)
	for _, id := range attrs {
		a := def.Attribute(id)
		if a == nil {
			continue
		}
		if a.Writable() {
			if hasPriv && wp.MinWritePrivilege(id) != uint8(a.Access.WritePrivilege()) {
				tb.Errorf("%s.%s: write privilege %d, want %d", def.Name, a.Name, wp.MinWritePrivilege(id), a.Access.WritePrivilege())
			}
			continue
		}
		err := srv.MatterWrite(context.Background(), id, nil)
		var sce im.StatusCodeError
		if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusUnsupportedWrite {
			tb.Errorf("%s.%s: write to a read-only attribute answered %v, want UNSUPPORTED_WRITE", def.Name, a.Name, err)
		}
	}
}
