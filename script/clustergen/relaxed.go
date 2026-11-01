// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

// relaxedRequestFields are command request fields whose generated decoder
// drops the schema's constraint so that the server, not the interaction
// layer, judges the value — as matter.js's server extends its cluster
// schema with `constraint: "none"` for the field. The definition keeps the
// schema's constraint verbatim; only the request decoder is relaxed. Keyed
// `Cluster.Command.Field` (matter.js names); the value is the matter.js
// line at the pin that relaxes it, written into the generated file.
var relaxedRequestFields = map[string]string{
	// IcdManagementServer.ts:59-74 at 862ad75f: "Drop the length bound on
	// the verificationKey so a malformed key fails #assertMayModify's byte
	// comparison rather than being rejected with ConstraintError before
	// the handler runs" — an Administrator's key is ignored, a Manage
	// caller's mismatch is FAILURE (:756-763; review 2026-11 W3-clusec-06).
	"IcdManagement.RegisterClient.VerificationKey":   "packages/node/src/behaviors/icd-management/IcdManagementServer.ts:66-70",
	"IcdManagement.UnregisterClient.VerificationKey": "packages/node/src/behaviors/icd-management/IcdManagementServer.ts:71-74",
}
