// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

// FabricScope is what a request reads of a fabric-scoped list attribute.
// Every entry of such a list carries the FabricIndex (field 254) of the
// fabric that owns it; the endpoint dispatcher scopes the encoded value by
// it before the value leaves the node:
//
//   - a fabric-filtered request reads its own fabric's entries alone;
//   - a request of a Sensitive attribute reads its own fabric's entries
//     alone, filtered or not;
//   - an unfiltered request of any other scoped attribute reads every
//     entry, and an entry another fabric owns — every entry, for a session
//     without a fabric — without the fields Fields names;
//   - an entry without a FabricIndex fails the attribute's path.
type FabricScope struct {
	// Sensitive marks an attribute whose own access is fabric-sensitive
	// ("S" in the cluster's schema).
	Sensitive bool
	// Fields are the context tags of the entry struct's fields that are
	// fabric-sensitive ("S"): withheld from an entry another fabric owns.
	Fields []uint32
}

// FabricScopeDeclarer is the optional capability a [ClusterServer]
// implements to state the fabric scope of its attributes for a cluster
// the module's schema snapshot does not hold — a vendor cluster. The
// dispatcher asks it for an attribute on a request; ok false declares the
// attribute not fabric-scoped.
//
// For a cluster of the snapshot the snapshot's scope table is the whole
// answer and the declarer is not asked: a server cannot narrow or drop the
// scope the model states. The scope applies whatever the server returns,
// from [ClusterServer.MatterRead] or [FabricScopedReader.MatterReadFiltered];
// a server returns every fabric's entries whole and leaves the scoping to
// the dispatcher.
type FabricScopeDeclarer interface {
	MatterFabricScope(attrID uint32) (scope FabricScope, ok bool)
}
