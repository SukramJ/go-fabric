// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"fmt"
	"go/token"
	"log/slog"
	"strings"
	"unicode"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// InitialRequirement is an attribute a device's kit server lists and has
// no value for: neither the model nor the kit gives it a default, so the
// host states it in the cluster's declaration ([Type.RequiredInitial]).
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

// String names the attribute and the declaration that states it.
func (r InitialRequirement) String() string {
	return fmt.Sprintf("%s (0x%04X) attribute %s (0x%04X) has no value; state it with %s", r.ClusterName, r.Cluster, r.AttributeName, r.Attribute, r.Declare)
}

// RequiredInitial lists the attributes a device of the type serves with no
// value unless the host's declarations state one: listed in the cluster's
// AttributeList, a read of each answers UNSUPPORTED_ATTRIBUTE. A
// data-driven host checks its configuration against it before it builds a
// device. It measures the
// type's default servers, built without declarations; a type whose
// defaults New refuses for another reason first (a feature selection, a
// port, a server the host supplies) reports what the servers it could
// build list. Nil: the type's defaults answer every attribute they list.
func (t *Type) RequiredInitial() []InitialRequirement {
	if t == nil {
		return nil
	}
	s := newSettings()
	s.key = "device.RequiredInitial:" + t.Name
	s.logger = slog.New(slog.DiscardHandler)
	s.clock = &deviceClock{}
	d, err := buildDevice(t.clone(), Options{StableKey: endpoint.StringKey(s.key)}, s)
	if d == nil || err != nil {
		return nil
	}
	return d.missingInitial()
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
			if attr >= cluster.AttrGlobalGeneratedCommandList || answersRead(srv, attr) {
				continue
			}
			out = append(out, initialRequirement(def, id, attr))
		}
	}
	return out
}

// answersRead reports whether srv answers a read of attr, as the
// dispatcher reads it: the fabric-scoped read first, MatterRead otherwise.
func answersRead(srv contract.ClusterServer, attr uint32) bool {
	if fsr, ok := srv.(contract.FabricScopedReader); ok {
		if _, ok := fsr.MatterReadFiltered(context.Background(), attr); ok {
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
