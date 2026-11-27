// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"fmt"
	"go/token"
	"log/slog"
	"strings"
	"unicode"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
)

// InitialRequirement is an attribute a device's kit server lists and has
// no value for, one matter.js requires a value of when the server
// initialises: the host states it in the cluster's declaration, or [New]
// refuses the device ([Type.RequiredInitial]).
type InitialRequirement struct {
	// Cluster and ClusterName name the cluster.
	Cluster     uint32
	ClusterName string
	// Attribute and AttributeName name the attribute.
	Attribute     uint32
	AttributeName string
	// Declare is the declaration that states it, as Go source:
	// "<cluster>def.Declare{Initial: <cluster>def.Patch{<Attribute>: …}}".
	Declare string
}

// initialError is New's refusal of a device whose kit servers leave an
// attribute without the value matter.js requires at initialisation.
func initialError(t *Type, refused []InitialRequirement) error {
	parts := make([]string, len(refused))
	for i, m := range refused {
		parts[i] = m.String()
	}
	return fmt.Errorf("%w: %s: %s", ErrConfiguration, t.Name, strings.Join(parts, "; "))
}

// String names the attribute and the declaration that states it.
func (r InitialRequirement) String() string {
	return fmt.Sprintf("%s (0x%04X) attribute %s (0x%04X) has no value; state it with %s", r.ClusterName, r.Cluster, r.AttributeName, r.Attribute, r.Declare)
}

// RequiredInitial lists the attributes [New] refuses a device of the type
// for unless the host's declarations state them ([InitialRequirement]): a
// data-driven host checks its configuration against it before it builds a
// device. It measures the type's default servers, built without
// declarations. A type whose defaults New refuses for another reason
// first — a feature selection, a port, a server the host supplies — is
// not measured: the error is that refusal, and New with the host's
// declarations names what is still missing. No rows and no error: the
// type's defaults answer every attribute they list (a nil type requires
// nothing).
func (t *Type) RequiredInitial() ([]InitialRequirement, error) {
	if t == nil {
		return nil, nil
	}
	s := newSettings()
	s.key = "device.RequiredInitial:" + t.Name
	s.logger = slog.New(slog.DiscardHandler)
	s.clock = &deviceClock{}
	s.skipInitialCheck = true
	d, err := buildDevice(t.clone(), Options{StableKey: endpoint.StringKey(s.key)}, s)
	if err != nil {
		return nil, fmt.Errorf("device: %s: the defaults do not build without declarations, nothing measured: %w", t.Name, err)
	}
	if d == nil {
		return nil, fmt.Errorf("device: %s: the defaults built no device, nothing measured", t.Name)
	}
	return d.refusedInitial(), nil
}

// refusedInitial is the part of missingInitial matter.js refuses at a
// server's initialisation: the attributes a host must state.
func (d *Device) refusedInitial() []InitialRequirement {
	var out []InitialRequirement
	for _, r := range d.missingInitial() {
		def := spec.Lookup(r.Cluster)
		if def == nil {
			continue
		}
		if a := def.Attribute(r.Attribute); a != nil && refusedAtInit(a.Type, a.Quality, a.Constraint, a.Default) {
			out = append(out, r)
		}
	}
	return out
}

// refusedAtInit reports whether matter.js refuses a server whose state
// holds no value of its own for an attribute of type t, quality q,
// constraint c and stated default def (decision 17, "refuses, as matter.js
// does"). matter.js validates the whole cluster state once a server
// behaviour initialised (packages/node/src/behavior/internal/
// ServerBehaviorBacking.ts:46-63 at db021da0). Before that a served member
// without a value gets the model default, else the datatype fallback —
// null when nullable, 0 for an integer or float, false, {} for a bitmap or
// struct, [] for a list; none for an enum, a string or an octet string
// (packages/model/src/logic/SelectDefaultValue.ts:144-176); a struct is
// built from the defaults its children state (DefaultValue.ts:153-179).
// conformance-compiler.ts:717-721 then refuses a conformant, non-nullable
// member that is still undefined — an enum, string or octet string, or a
// struct one of whose mandatory children stays undefined ([structRefused])
// — and the same
// validation refuses a list whose minimum length is above 0 and an integer
// whose constraint excludes 0 (inferred from the dispatch at
// constraint.ts:112-113). A stated default that the runtime does not
// resolve (spec.UnresolvedDefault) counts as none.
func refusedAtInit(t spec.Type, q spec.Quality, c spec.Constraint, def any) bool {
	if q.Nullable {
		return false
	}
	if def != nil {
		if _, unresolved := def.(spec.UnresolvedDefault); !unresolved {
			return false
		}
	}
	switch t.Kind {
	case spec.KindEnum, spec.KindString, spec.KindBytes:
		return true
	case spec.KindStruct:
		return structRefused(t.Struct, 0)
	case spec.KindList:
		return excludesZero(c)
	case spec.KindUint, spec.KindInt, spec.KindFloat32, spec.KindFloat64:
		return excludesZero(c)
	case spec.KindAny, spec.KindBool, spec.KindBitmap:
		// false, {} — and a value of no known shape — always validate.
		return false
	}
	return false
}

// structRefused reports whether matter.js leaves a mandatory field of a
// struct built from its children's defaults undefined (DefaultValue.ts
// buildObject, :153-179 at db91886c): a mandatory, non-nullable child
// without a default of its own is undefined unless it is a list whose
// constraint admits the empty list — matter.js gives such a child []
// (DefaultValue.ts:130-143) — or a struct that is itself built from its
// children's defaults. A struct of no known shape is not judged; depth
// bounds a definition that nests without end.
func structRefused(st *spec.Struct, depth int) bool {
	if st == nil || depth > 16 {
		return false
	}
	for i := range st.Fields {
		f := &st.Fields[i]
		if !f.Mandatory() || f.Quality.Nullable || f.Default != nil {
			continue
		}
		switch f.Type.Kind {
		case spec.KindList:
			if excludesZero(f.Constraint) {
				return true
			}
		case spec.KindStruct:
			if structRefused(f.Type.Struct, depth+1) {
				return true
			}
		default:
			return true
		}
	}
	return false
}

// excludesZero reports whether constraint c leaves out 0 — a value, or a
// list's or string's length: a minimum above 0, an exact value other than
// 0, alternatives none of which admits it. A bound that names another
// element or an expression admits it (it is not known here).
func excludesZero(c spec.Constraint) bool {
	if len(c.Parts) > 0 {
		for _, p := range c.Parts {
			if !excludesZero(p) {
				return false
			}
		}
		return true
	}
	literal := func(b *spec.Bound) (int64, bool) {
		if b == nil || b.Ref != "" || b.Expr != "" {
			return 0, false
		}
		return b.Int, true
	}
	if v, ok := literal(c.Value); ok {
		return v != 0
	}
	if v, ok := literal(c.Min); ok && v > 0 {
		return true
	}
	if v, ok := literal(c.Max); ok && v < 0 {
		return true
	}
	return false
}

// missingInitial lists the attributes the device's kit servers list and
// answer no read of. The global attributes the dispatcher answers itself
// are not read, and a server the host supplied (HostServer) is the host's
// to keep ([contract.ClusterServer]).
func (d *Device) missingInitial() []InitialRequirement {
	var out []InitialRequirement
	for _, srv := range d.servers {
		id := srv.MatterClusterID()
		if d.hosted[id] {
			continue
		}
		lister, ok := srv.(contract.ClusterAttributeLister)
		if !ok {
			continue
		}
		def := spec.Lookup(id)
		for _, attr := range lister.MatterAttributes() {
			if attr >= cluster.AttrGlobalGeneratedCommandList || answersRead(srv, def, attr) {
				continue
			}
			out = append(out, initialRequirement(def, id, attr))
		}
	}
	return out
}

// answersRead reports whether srv answers a read of attr outside a
// request: MatterRead, and a fabric-scoped attribute of a
// [contract.FabricScopedReader] counts as answered.
func answersRead(srv contract.ClusterServer, def *spec.Cluster, attr uint32) bool {
	if _, scoped := srv.(contract.FabricScopedReader); scoped && def != nil {
		if a := def.Attribute(attr); a != nil && a.Access.Fabric != "" {
			// A fabric-scoped attribute answers a request only (the
			// dispatcher never lets MatterRead stand in for it); New has
			// none to judge it by.
			return true
		}
	}
	_, ok := srv.MatterRead(attr)
	return ok
}

// initialRequirement names attr of cluster id and the declaration that
// states it.
func initialRequirement(def *spec.Cluster, id, attr uint32) InitialRequirement {
	r := InitialRequirement{Cluster: id, Attribute: attr, ClusterName: fmt.Sprintf("cluster 0x%04X", id), AttributeName: fmt.Sprintf("0x%04X", attr)}
	if def == nil {
		r.Declare = "the cluster's declaration"
		return r
	}
	r.ClusterName = def.Name
	if a := def.Attribute(attr); a != nil {
		r.AttributeName = a.Name
	}
	pkg := declarePackage(def.Name)
	r.Declare = fmt.Sprintf("%s.Declare{Initial: %s.Patch{%s: …}}", pkg, pkg, r.AttributeName)
	return r
}

// declarePackage is the alias a host imports a cluster's generated
// package under: its package name (the cluster's name in lower case, with
// "cluster" appended where that is a Go keyword, as script/clustergen
// names it) and "def".
func declarePackage(clusterName string) string {
	var b strings.Builder
	for _, r := range clusterName {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	name := b.String()
	if token.IsKeyword(name) {
		name += "cluster"
	}
	return name + "def"
}
