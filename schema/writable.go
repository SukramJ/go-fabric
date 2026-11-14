// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema

// The read-only-attribute write gate. matter.js gives every attribute its
// limits from the model's effective access and quality
// (packages/protocol/src/action/server/AccessControl.ts:523 at db021da0):
//
//	writable: access.writable && !fixed,
//
// where access.writable is an rw other than "R"
// (packages/model/src/aspects/Access.ts:44-46 at db021da0: "RW" and the
// optional-write "R[W]" are writable, "R" and no rw are not). A remote write
// to an attribute that is not writable is answered before any behaviour
// runs, before the write privilege and before the DataVersion
// (packages/protocol/src/action/server/AttributeWriteResponse.ts at
// db021da0):
//   - a concrete path → UNSUPPORTED_WRITE (:231-234, `if (!limits.writable)
//     … return this.#asStatus(path, Status.UnsupportedWrite)`), after the
//     View pass (:215) and the existence statuses (:221-229), ahead of the
//     write privilege (:236-241) and the DataVersion (:254-257);
//   - a wildcard path → the attribute is skipped without a status
//     (:331-333, `if (!attribute.limits.writable) return;`).
//
// connectedhomeip answers the same status at the same place
// (src/app/WriteHandler.cpp:790-791 at 6170af84, between the View pass :777
// and the write-privilege pass :794).
//
// The dispatcher has no behaviour layer that carries limits.writable, so
// readOnlyAttributes (attribute_access_gen.go) gives the verdict
// schema-side: generated from the effective access and quality of every
// attribute of every cluster in parity/schema.json, an attribute a derived
// cluster inherits included. It replaces a hand-written table of the
// clusters this module served (review II, W3-clugen-05): outside that
// table a read-only attribute's write reached the access-control gate, the
// DataVersion check and the cluster server.

// globalAttributeFirst and globalAttributeLast bound the global attribute
// range: GeneratedCommandList (0xFFF8), AcceptedCommandList, EventList,
// AttributeList, FeatureMap and ClusterRevision (0xFFFD). matter.js defines
// them once for every cluster, each "R V"
// (packages/model/src/standard/elements/generated-command-list.element.ts:14,
// accepted-command-list.element.ts:14, attribute-list.element.ts:13,
// feature-map.element.ts:13, cluster-revision.element.ts:13 at db021da0) but
// the deprecated EventList, which states no access
// (event-list.element.ts:12) and is therefore not writable either
// (Access.ts:44-46, `!!this.rw`).
const (
	globalAttributeFirst uint32 = 0xFFF8
	globalAttributeLast  uint32 = 0xFFFD
)

// AttributeWritable reports the matter.js writability verdict for a cluster
// attribute. It returns (writable=false, known=true) for an attribute
// matter.js does not let a remote party write — an entry of
// readOnlyAttributes, or an id of the global attribute range on any cluster
// — and (writable=true, known=false) for every other (clusterID, attrID)
// pair: a writable attribute, an attribute the snapshot's cluster does not
// define, a cluster the snapshot does not have. Because the table records
// only read-only attributes, the writable return is authoritative only when
// known is true (then it is always false); callers gate on known &&
// !writable.
//
// Mirrors matter.js limits.writable
// (packages/protocol/src/action/server/AccessControl.ts:523 at db021da0) as
// AttributeWriteResponse.ts:231 (concrete write) and :331 (wildcard write)
// consume it.
func AttributeWritable(clusterID, attrID uint32) (writable, known bool) {
	if attrID >= globalAttributeFirst && attrID <= globalAttributeLast {
		return false, true
	}
	if _, readOnly := readOnlyAttributes[clusterID][attrID]; readOnly {
		return false, true
	}
	return true, false
}

// AttributeWritePrivilege returns the privilege a subject needs to write the
// attribute: matter.js's write privilege from the access string (generated
// into attributeWritePrivileges), Operate when it names none higher. Mirrors
// AccessControl(attribute).limits.writeLevel (matter.js
// packages/protocol/src/action/server/AttributeWriteResponse.ts authorizes
// the write with it).
func AttributeWritePrivilege(clusterID, attrID uint32) uint8 {
	if p, ok := attributeWritePrivileges[clusterID][attrID]; ok {
		return p
	}
	return PrivilegeOperate
}

// AttributeReadPrivilege returns the privilege a subject needs to read the
// attribute: matter.js's read privilege from the access string (generated
// into attributeReadPrivileges), View when it names none higher. Mirrors
// AccessControl(attribute).limits.readLevel, which matter.js authorizes
// every read against (packages/protocol/src/action/server/AttributeReadResponse.ts:272,
// :410 at db021da0).
func AttributeReadPrivilege(clusterID, attrID uint32) uint8 {
	if p, ok := attributeReadPrivileges[clusterID][attrID]; ok {
		return p
	}
	return PrivilegeView
}

// AttributeChangesOmitted reports whether matter.js gives the attribute the
// "C" (changesOmitted) quality: a change to it is never reported to a
// subscriber, though it reads normally. Mirrors matter.js
// ProtocolService.addCluster, which leaves such attributes out of the
// changed set, and ServerBehaviorBacking, which suppresses their change
// broadcasts. Generated from parity/schema.json (changesOmittedAttributes).
func AttributeChangesOmitted(clusterID, attrID uint32) bool {
	_, ok := changesOmittedAttributes[clusterID][attrID]
	return ok
}

// ClusterHasChangesOmittedAttribute reports whether any attribute of the
// cluster carries the "C" quality — the set matter.js's client structure
// enumerates on the ClusterModel when it decides whether a read may skip a
// cluster's DataVersion filter (packages/node/src/node/client/ClientStructure.ts
// hasChangesOmittedAttribute, :1674-1692 at 0ceb202d). The model knows every
// snapshot cluster, so the answer does not depend on a generated definition.
func ClusterHasChangesOmittedAttribute(clusterID uint32) bool {
	return len(changesOmittedAttributes[clusterID]) > 0
}
