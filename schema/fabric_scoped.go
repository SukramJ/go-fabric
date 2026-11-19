// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema

import "slices"

// IsFabricScopedInvoke reports whether the (cluster, command) pair carries
// matter.js's fabric-scoped access quality. Mirrors matter.js
// Access.Fabric.Scoped on a command (AccessControl.ts:524 `fabricScoped`).
// A fabric-scoped command invoked on a session with no accessing fabric — a
// PASE session before AddNOC — answers UNSUPPORTED_ACCESS (matter.js
// packages/protocol/src/action/server/CommandInvokeResponse.ts:287-290 at
// db021da0, `limits.fabricScoped && !this.session.fabric`).
// fabricScopedInvokePaths (invoke_access_gen.go) is generated from each
// request command's effective access in parity/schema.json, every cluster
// of the snapshot.
func IsFabricScopedInvoke(clusterID, commandID uint32) bool {
	cmds, ok := fabricScopedInvokePaths[clusterID]
	if !ok {
		return false
	}
	_, ok = cmds[commandID]
	return ok
}

// IsFabricScopedWrite reports whether the (cluster, attribute) pair is a
// writable attribute whose effective access is fabric-scoped ("F") or
// fabric-sensitive ("S") — matter.js's limits.fabricScoped
// (packages/protocol/src/action/server/AccessControl.ts:524 at b25ed960:
// `access.fabric === Access.Fabric.Scoped || access.fabric ===
// Access.Fabric.Sensitive`) on an attribute with limits.writable. A write to
// one on a session with no accessing fabric answers UNSUPPORTED_ACCESS
// (packages/protocol/src/action/server/AttributeWriteResponse.ts:248-251 at
// db021da0, `limits.fabricScoped && !this.session.fabric`).
// fabricScopedWritePaths (attribute_access_gen.go) is generated from each
// attribute's effective access in parity/schema.json.
func IsFabricScopedWrite(clusterID, attributeID uint32) bool {
	_, ok := fabricScopedWritePaths[clusterID][attributeID]
	return ok
}

// IsFabricScopedRead reports whether the (cluster, attribute) pair is a
// readable attribute whose effective access is fabric-scoped ("F") or
// fabric-sensitive ("S") — matter.js's limits.fabricScoped
// (packages/protocol/src/action/server/AccessControl.ts:524 at b25ed960) on
// an attribute with limits.readable: what a request reads of it depends on
// the accessing fabric (packages/node/src/behavior/state/managed/values/
// ListManager.ts:609-616 at db021da0, createProxy). It is the presence of a
// row in [AttributeFabricScope]'s table.
func IsFabricScopedRead(clusterID, attributeID uint32) bool {
	_, ok := attributeFabricScopes[clusterID][attributeID]
	return ok
}

// FabricScope is what a request reads of a fabric-scoped list attribute,
// as the model states it: every entry carries the FabricIndex (field 254)
// of the fabric that owns it.
type FabricScope struct {
	// Sensitive is an attribute whose own access is fabric-sensitive
	// ("S"): a request reads its own fabric's entries alone, filtered or
	// not. Without it (access "F") the filter applies to a fabric-filtered
	// request only.
	Sensitive bool
	// Fields are the ids of the entry struct's fields whose access is
	// fabric-sensitive ("S"): an entry another fabric owns is read without
	// them.
	Fields []uint32
}

// AttributeFabricScope returns the scope of the (cluster, attribute) pair;
// ok is false when the model does not mark the attribute fabric-scoped or
// fabric-sensitive (an unknown cluster included). The table
// (fabric_scope_gen.go) is generated from each attribute's effective access
// and its list entry's struct in parity/schema.json. The returned Fields is
// the caller's own copy.
func AttributeFabricScope(clusterID, attributeID uint32) (FabricScope, bool) {
	s, ok := attributeFabricScopes[clusterID][attributeID]
	if !ok {
		return FabricScope{}, false
	}
	s.Fields = slices.Clone(s.Fields)
	return s, true
}

// EventScope is what the model states of an event's read: Sensitive for a
// fabric-sensitive event ("S"), whose record only the fabric it names
// reads, and the event's read privilege (1 View, 3 Operate, 4 Manage,
// 5 Administer).
type EventScope struct {
	Sensitive     bool
	ReadPrivilege uint8
}

// EventReadScope returns the scope of the (cluster, event) pair; ok is
// false for an event the snapshot does not hold. The table
// (fabric_scope_gen.go) is generated from each event's effective access in
// parity/schema.json.
func EventReadScope(clusterID, eventID uint32) (EventScope, bool) {
	s, ok := eventScopes[clusterID][eventID]
	return s, ok
}

// ClusterEventScope is the scope of clusterID's events taken together:
// Sensitive when any of them is fabric-sensitive, and the lowest read
// privilege among them — what a subject needs to read any event of the
// cluster. A cluster whose events the snapshot does not hold is View and
// not sensitive. It answers for an event the snapshot does not hold, and
// for a check that names a cluster but no event.
func ClusterEventScope(clusterID uint32) EventScope {
	events := eventScopes[clusterID]
	if len(events) == 0 {
		return EventScope{ReadPrivilege: 1}
	}
	out := EventScope{ReadPrivilege: 5}
	for _, s := range events {
		out.Sensitive = out.Sensitive || s.Sensitive
		out.ReadPrivilege = min(out.ReadPrivilege, s.ReadPrivilege)
	}
	return out
}
