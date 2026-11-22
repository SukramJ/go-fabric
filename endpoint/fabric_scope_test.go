// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// scopeServer serves one list attribute and declares the scope it is
// given, if any.
type scopeServer struct {
	cluster, attr uint32
	declared      *contract.FabricScope
}

func (s scopeServer) MatterClusterID() uint32 { return s.cluster }
func (s scopeServer) MatterRead(attr uint32) (any, bool) {
	if attr == s.attr {
		return []uint8{}, true
	}
	return nil, false
}
func (scopeServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (scopeServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (scopeServer) MatterReportable() []uint32                             { return nil }

// declaringServer is a scopeServer that is a contract.FabricScopeDeclarer.
type declaringServer struct{ scopeServer }

func (s declaringServer) MatterFabricScope(attr uint32) (contract.FabricScope, bool) {
	if attr != s.attr || s.declared == nil {
		return contract.FabricScope{}, false
	}
	return *s.declared, true
}

// TestReadOneAttachesTheScopeOnARequest: a request's read of a
// fabric-scoped attribute carries its scope — from the generated table for
// a cluster of the snapshot, whatever the server declares, and from the
// server's declaration for a cluster outside it — with the request's
// fabric and filter; a read with no request behind it, and an attribute
// that is not fabric-scoped, carry none.
func TestReadOneAttachesTheScopeOnARequest(t *testing.T) {
	t.Parallel()
	request := im.WithFabricFilter(context.Background(), true, 3)
	path := func(srv contract.ClusterServer, attr uint32) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{Endpoint: 1, Cluster: srv.MatterClusterID(), Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	ep := &Endpoint{ID: 1}
	vendor := contract.FabricScope{Sensitive: true, Fields: []uint32{4}}
	for _, c := range []struct {
		name string
		srv  contract.ClusterServer
		ctx  context.Context
		want *im.FabricScope
	}{
		{"standard cluster, host server", scopeServer{cluster: 0x0065, attr: 0x0000}, request, &im.FabricScope{Fabric: 3, Filtered: true, Fields: []uint32{2}}},
		{"standard cluster, a declaration that drops it", declaringServer{scopeServer{0x0065, 0x0000, nil}}, request, &im.FabricScope{Fabric: 3, Filtered: true, Fields: []uint32{2}}},
		{"standard cluster, a declaration that adds one", declaringServer{scopeServer{0x0006, 0x0000, &vendor}}, request, nil},
		{"standard cluster, a manufacturer-specific attribute, declared", declaringServer{scopeServer{0x0006, 0xFFF10000, &vendor}}, request, &im.FabricScope{Fabric: 3, Filtered: true, Sensitive: true, Fields: []uint32{4}}},
		{"standard cluster, a manufacturer-specific attribute, undeclared", scopeServer{cluster: 0x0006, attr: 0xFFF10000}, request, nil},
		{"vendor cluster, declared", declaringServer{scopeServer{0xFFF1FC01, 0x0001, &vendor}}, request, &im.FabricScope{Fabric: 3, Filtered: true, Sensitive: true, Fields: []uint32{4}}},
		{"vendor cluster, undeclared", scopeServer{cluster: 0xFFF1FC01, attr: 0x0001}, request, nil},
		{"no request behind the read", scopeServer{cluster: 0x0065, attr: 0x0000}, context.Background(), nil},
	} {
		var a uint32
		switch s := c.srv.(type) {
		case scopeServer:
			a = s.attr
		case declaringServer:
			a = s.attr
		}
		res := readOne(c.ctx, ep, c.srv, path(c.srv, a))
		if res.Status != im.StatusSuccess {
			t.Fatalf("%s: status %v", c.name, res.Status)
		}
		got := res.FabricScope
		if (got == nil) != (c.want == nil) || got != nil && (got.Fabric != c.want.Fabric || got.Filtered != c.want.Filtered || got.Sensitive != c.want.Sensitive || !slices.Equal(got.Fields, c.want.Fields)) {
			t.Errorf("%s: scope %+v, want %+v", c.name, got, c.want)
		}
	}
	// The scope is the result's own: editing it leaves the declaration as
	// it was.
	srv := declaringServer{scopeServer{0xFFF1FC01, 0x0001, &vendor}}
	readOne(request, ep, srv, path(srv, 0x0001)).FabricScope.Fields[0] = 9
	if vendor.Fields[0] != 4 {
		t.Error("a result's scope shares the declaration's Fields")
	}
}

// TestSpecInstanceDeclaresItsDefinitionsScope: spec.Instance declares the
// scope its definition states — an "S" list Sensitive, an "F" list with
// the entry's fabric-sensitive fields — and nothing for any other
// attribute. For a generated definition it is the generated table's
// (script/fabricscopecheck TestGeneratedDefinitionsDeclareTheTablesScope).
func TestSpecInstanceDeclaresItsDefinitionsScope(t *testing.T) {
	t.Parallel()
	entry := &spec.Struct{Name: "E", Fields: []spec.Field{
		{ID: 0, Name: "A", Type: spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}},
		{ID: 1, Name: "B", Type: spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}, FabricSensitive: true},
		{ID: 254, Name: "FabricIndex", Type: spec.Type{Name: "FabricIndex", Kind: spec.KindUint, Bits: 8}},
	}}
	list := spec.Type{Name: "list", Kind: spec.KindList, Entry: &spec.Field{Name: "entry", Type: spec.Type{Name: "E", Kind: spec.KindStruct, Struct: entry}}}
	def := &spec.Cluster{ID: 0xFFF1FC02, Name: "Vendor", Revision: 1, Attributes: []spec.Attribute{
		{ID: 0x0000, Name: "Scoped", Type: list, Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory}, Access: spec.Access{RW: "R", Read: spec.PrivilegeView, Fabric: "F"}},
		{ID: 0x0001, Name: "Sensitive", Type: list, Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory}, Access: spec.Access{RW: "R", Read: spec.PrivilegeView, Fabric: "S"}},
		{ID: 0x0002, Name: "Plain", Type: list, Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory}, Access: spec.Access{RW: "R", Read: spec.PrivilegeView}},
	}}
	inst, err := spec.New(def, spec.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := inst.MatterFabricScope(0x0000); !ok || s.Sensitive || !slices.Equal(s.Fields, []uint32{1}) {
		t.Errorf("Scoped: %+v %v", s, ok)
	}
	if s, ok := inst.MatterFabricScope(0x0001); !ok || !s.Sensitive || !slices.Equal(s.Fields, []uint32{1}) {
		t.Errorf("Sensitive: %+v %v", s, ok)
	}
	for _, attr := range []uint32{0x0002, 0x0009} {
		if s, ok := inst.MatterFabricScope(attr); ok {
			t.Errorf("0x%04X: declared %+v", attr, s)
		}
	}
}
