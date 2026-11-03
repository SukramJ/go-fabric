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

// serverJudgedStructFields are struct fields whose generated decoder drops
// the schema's constraint because a deliberate divergence from matter.js
// lets the server answer first — chip's handler judges the value, a CSA
// case expects chip's status, and the owner decided to follow it. Keyed
// `Cluster.Struct.Field` (matter.js names). The definition keeps the
// schema's constraint verbatim; only the struct decoder is relaxed, for
// every payload that carries the struct.
var serverJudgedStructFields = map[string]serverJudged{
	// matter.js validates the request against the model's `max 10` before
	// the invoker (CommandInvokeResponse.ts:484-487) and answers
	// CONSTRAINT_ERROR; chip's handler counts the targets and answers
	// RESOURCE_EXHAUSTED, which TC-EEVSE-2.3 step 19 expects
	// (TC_EEVSE_2_3.py:411-439). The server's check is
	// cluster/energy/evse.go's evseMaxTargetsPerDay.
	"EnergyEvse.ChargingTargetScheduleStruct.ChargingTargets": {
		at:  "src/app/clusters/energy-evse-server/EnergyEvseCluster.cpp:564-565",
		why: "owner decision 2026-11-03, BD-chip-EVSE-TargetsServerHeld",
	},
}

// serverJudged names the chip line whose handler judges a struct field and
// the decision that records the divergence from matter.js.
type serverJudged struct {
	at  string // chip path:line at the harness commit
	why string // the owner decision and the by_design row
}
