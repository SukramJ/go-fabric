// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import "fmt"

// The relaxation table: every place a server of this module judges a
// value the model's constraint would refuse, one section per server, each
// entry with the reference line that justifies it. matter.js relaxes a
// field by extending its server's schema (constraint "none",
// packages/node/src/behaviors/icd-management/IcdManagementServer.ts:58-75
// at b25ed960) or by a Supervision config of a command or a field
// (packages/node/src/behaviors/door-lock/DoorLockServer.ts:1494-1526 at
// b25ed960); connectedhomeip's behaviour stands in for
// an Ahead server under an owner decision. The generator writes each entry
// into the cluster's definition (spec.Field.Relaxed, spec.Attribute.Relaxed,
// spec.Command.Check) and drops the relaxed constraint from the generated
// decoder; the definition keeps the model's constraint verbatim beside it.
// An entry that names nothing of its cluster fails the generation.

// relaxKind is what an entry relaxes.
type relaxKind uint8

const (
	// relaxRequestField: a request field ("Cluster.Command.Field").
	relaxRequestField relaxKind = iota + 1
	// relaxStructField: a struct field, in every payload that carries the
	// struct ("Cluster.Struct.Field").
	relaxStructField
	// relaxAttribute: a controller's written value ("Cluster.Attribute").
	relaxAttribute
	// relaxRefusal: a request of the command the definition refuses
	// answers refusal in place of the schema's status ("Cluster.Command").
	relaxRefusal
	// relaxRequestListEntry: the entries of a request field's list
	// ("Cluster.Command.Field"): each entry's value — an enum's member
	// among them — is the server's to judge (validate = false on the
	// entry); the list's own constraint stays the model's.
	relaxRequestListEntry
)

// relaxation is one entry of the table.
type relaxation struct {
	kind    relaxKind
	key     string // matter.js names
	refusal uint8  // relaxRefusal: the IM status
	ref     string // the reference line, written into the definition
}

// statusInvalidCommand is IM INVALID_COMMAND (Core §8.10.1, im.StatusInvalidCommand).
const statusInvalidCommand = 0x85

// serverRelaxations is the table, one section per server.
var serverRelaxations = []struct {
	server  string
	entries []relaxation
}{
	{"cluster/core ICDManagement", []relaxation{
		// "Drop the length bound on the verificationKey so a malformed key
		// fails #assertMayModify's byte comparison rather than being
		// rejected with ConstraintError before the handler runs": an
		// Administrator's key is ignored, a Manage caller's mismatch is
		// FAILURE (review 2026-11 W3-clusec-06).
		{kind: relaxRequestField, key: "IcdManagement.RegisterClient.VerificationKey", ref: "matter.js packages/node/src/behaviors/icd-management/IcdManagementServer.ts:58-69 at b25ed960"},
		{kind: relaxRequestField, key: "IcdManagement.UnregisterClient.VerificationKey", ref: "matter.js packages/node/src/behaviors/icd-management/IcdManagementServer.ts:58-74 at b25ed960"},
	}},
	{"cluster/core OperationalCredentials", []relaxation{
		// "relax constraints on AttestationNonce and CsrNonce … so the
		// behavior can validate and return InvalidCommand".
		{kind: relaxRequestField, key: "OperationalCredentials.AttestationRequest.AttestationNonce", ref: "matter.js packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:53-66 at db021da0"},
		{kind: relaxRequestField, key: "OperationalCredentials.CsrRequest.CsrNonce", ref: "matter.js packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:53-66 at db021da0"},
		// "The cluster requires INVALID_COMMAND for a CertificateType that
		// CertificateChainTypeEnum does not define": the command's
		// onValidationError answers every validation error with it.
		{kind: relaxRefusal, key: "OperationalCredentials.CertificateChainRequest", refusal: statusInvalidCommand, ref: "matter.js packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:635-638 at b25ed960"},
	}},
	{"cluster/core GeneralCommissioning", []relaxation{
		// "The cluster answers an unknown regulatory location with
		// ValueOutsideRange in the response, which the handler sets": the
		// field's validation is off (validate = false); the TLV check of
		// its presence stays.
		{kind: relaxRequestField, key: "GeneralCommissioning.SetRegulatoryConfig.NewRegulatoryConfig", ref: "matter.js packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:288-289 at b25ed960"},
	}},
	{"cluster/core Identify", []relaxation{
		// The fields' validation is off (validate = false) until CHIP
		// refuses an effect its enum does not define; the server answers
		// SUCCESS and runs an undefined EffectVariant as Default
		// (IdentifyServer.ts:134-139, cluster/core/identify.go
		// triggerEffectOf).
		{kind: relaxRequestField, key: "Identify.TriggerEffect.EffectIdentifier", ref: identifyRelaxed},
		{kind: relaxRequestField, key: "Identify.TriggerEffect.EffectVariant", ref: identifyRelaxed},
	}},
	{"cluster/wire AdministratorCommissioning", []relaxation{
		// "relax constraints on OpenCommissioningWindow fields … so the
		// behavior can validate and return PakeParameterError or
		// INVALID_COMMAND" (BD-Matter-AdminCommissioning-OpenWindowParams).
		{kind: relaxRequestField, key: "AdministratorCommissioning.OpenCommissioningWindow.PakePasscodeVerifier", ref: adminCommRelaxed},
		{kind: relaxRequestField, key: "AdministratorCommissioning.OpenCommissioningWindow.Iterations", ref: adminCommRelaxed},
		{kind: relaxRequestField, key: "AdministratorCommissioning.OpenCommissioningWindow.Salt", ref: adminCommRelaxed},
		{kind: relaxRequestField, key: "AdministratorCommissioning.OpenCommissioningWindow.Discriminator", ref: adminCommRelaxed},
	}},
	{"cluster/core Groups", []relaxation{
		// "relax constraints on command request fields … so the behavior
		// can validate and return proper Status responses".
		{kind: relaxRequestField, key: "Groups.AddGroup.GroupId", ref: groupsRelaxed},
		{kind: relaxRequestField, key: "Groups.AddGroup.GroupName", ref: groupsRelaxed},
		{kind: relaxRequestField, key: "Groups.AddGroupIfIdentifying.GroupId", ref: groupsRelaxed},
		{kind: relaxRequestField, key: "Groups.AddGroupIfIdentifying.GroupName", ref: groupsRelaxed},
	}},
	{"cluster/core ScenesManagement", []relaxation{
		// The same relaxation for the scene commands.
		{kind: relaxRequestField, key: "ScenesManagement.AddScene.SceneId", ref: scenesRelaxed},
		{kind: relaxRequestField, key: "ScenesManagement.AddScene.TransitionTime", ref: scenesRelaxed},
		{kind: relaxRequestField, key: "ScenesManagement.AddScene.SceneName", ref: scenesRelaxed},
		{kind: relaxRequestField, key: "ScenesManagement.ViewScene.SceneId", ref: scenesRelaxed},
		{kind: relaxRequestField, key: "ScenesManagement.RemoveScene.SceneId", ref: scenesRelaxed},
		{kind: relaxRequestField, key: "ScenesManagement.StoreScene.SceneId", ref: scenesRelaxed},
		{kind: relaxRequestField, key: "ScenesManagement.CopyScene.SceneIdentifierFrom", ref: scenesRelaxed},
		{kind: relaxRequestField, key: "ScenesManagement.CopyScene.SceneIdentifierTo", ref: scenesRelaxed},
	}},
	{"cluster/lock DoorLockServer", []relaxation{
		// Fifteen commands answer every validation error of their request
		// with INVALID_COMMAND (onValidationError, "Invalid field value");
		// the cluster's other commands are held to the request schema and
		// the model as every command is (ProtocolService.ts:671-705 at b25ed960).
		{kind: relaxRefusal, key: "DoorLock.SetUser", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.GetUser", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.ClearUser", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.SetCredential", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.SetWeekDaySchedule", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.ClearWeekDaySchedule", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.SetYearDaySchedule", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.ClearYearDaySchedule", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.SetHolidaySchedule", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.ClearHolidaySchedule", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.GetWeekDaySchedule", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.GetYearDaySchedule", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.GetHolidaySchedule", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.ClearCredential", refusal: statusInvalidCommand, ref: doorLockRefusal},
		{kind: relaxRefusal, key: "DoorLock.GetCredentialStatus", refusal: statusInvalidCommand, ref: doorLockRefusal},
		// The get*Schedule commands answer an index outside the supported
		// range with a status in their response: the index fields' constraint
		// is off (constraint = false), so the handler receives the raw value.
		{kind: relaxRequestField, key: "DoorLock.GetWeekDaySchedule.WeekDayIndex", ref: doorLockIndex},
		{kind: relaxRequestField, key: "DoorLock.GetWeekDaySchedule.UserIndex", ref: doorLockIndex},
		{kind: relaxRequestField, key: "DoorLock.GetYearDaySchedule.YearDayIndex", ref: doorLockIndex},
		{kind: relaxRequestField, key: "DoorLock.GetYearDaySchedule.UserIndex", ref: doorLockIndex},
		{kind: relaxRequestField, key: "DoorLock.GetHolidaySchedule.HolidayIndex", ref: doorLockIndex},
	}},
	{"cluster/energy EvseServer", []relaxation{
		// matter.js answers more than ten targets with the model's "max 10"
		// (CONSTRAINT_ERROR); chip's handler counts them and answers
		// RESOURCE_EXHAUSTED, which TC-EEVSE-2.3 step 19 expects
		// (TC_EEVSE_2_3.py:411-439). The server's check is
		// cluster/energy/evse.go's evseMaxTargetsPerDay.
		{kind: relaxStructField, key: "EnergyEvse.ChargingTargetScheduleStruct.ChargingTargets", ref: "connectedhomeip src/app/clusters/energy-evse-server/EnergyEvseCluster.cpp:564-565 at 6170af84 (owner decision 2026-11-03, BD-chip-EVSE-TargetsServerHeld)"},
	}},
	{"cluster/camera PushTransport", []relaxation{
		// matter.js answers an empty VideoStreams or AudioStreams list with
		// the model's "1 to 16" (CONSTRAINT_ERROR); chip's server treats an
		// empty list as no stream field, which TC-PAVST-2.4 step 5,
		// TC-PAVST-2.6 step 8 and TC-PAVST-2.11 step 21a expect. More than
		// 16 stays CONSTRAINT_ERROR, the server's (pushtransport.go
		// maxPushStreams).
		{kind: relaxStructField, key: "PushAvStreamTransport.TransportOptionsStruct.VideoStreams", ref: pavstEmptyLists},
		{kind: relaxStructField, key: "PushAvStreamTransport.TransportOptionsStruct.AudioStreams", ref: pavstEmptyLists},
		// matter.js answers MotionSensitivity 11 with the model's "1 to
		// 10"; chip's server answers INVALID_COMMAND on a camera with
		// PERZONESENS, where the field is not expected whatever its value,
		// which TC-PAVST-2.3 step 25 expects; without PERZONESENS it judges
		// 1 to 10 itself (CONSTRAINT_ERROR, :556-569).
		{kind: relaxStructField, key: "PushAvStreamTransport.TransportTriggerOptionsStruct.MotionSensitivity", ref: "connectedhomeip src/app/clusters/push-av-stream-transport-server/PushAVStreamTransportLogic.cpp:571-577 at 6170af84 (owner decision 69, BD-chip-PushAvStreamTransport-RulesInServer)"},
	}},
	{"cluster/camera AvStream", []relaxation{
		// matter.js has no SetStreamPriorities handler; its generic gate
		// would answer a StreamUsage the enum does not define with
		// CONSTRAINT_ERROR. chip's handler answers INVALID_IN_STATE while a
		// stream is allocated, then INVALID_COMMAND for an unknown usage
		// (:2577-2578, :2583-2585), which its test expects
		// (TestCameraAVStreamManagementCluster.cpp:1460-1465). The server's
		// check is cluster/camera/avstream.go's setStreamPriorities.
		{kind: relaxRequestListEntry, key: "CameraAvStreamManagement.SetStreamPriorities.StreamPriorities", ref: "connectedhomeip src/app/clusters/camera-av-stream-management-server/CameraAVStreamManagementCluster.cpp:2577-2585 at 6170af84 (BD-chip-CameraAvStreamManagement-RulesInServer)"},
	}},
	{"cluster/thermo ThermostatServer", []relaxation{
		// A value written inside an atomic write goes to the pending state,
		// which matter.js does not validate against the model at the write;
		// the server's $AtomicChanging reactor judges it
		// (#validatePresetWriteRequest, ThermostatServer.ts:1367-1375:
		// RESOURCE_EXHAUSTED over NumberOfPresets), so the list's count is
		// the server's — as Schedules' is (BD-chip-Thermostat-Schedules).
		{kind: relaxAttribute, key: "Thermostat.Presets", ref: thermostatAtomic},
		{kind: relaxAttribute, key: "Thermostat.Schedules", ref: thermostatAtomic},
	}},
	{"cluster/appliance WasherServer", []relaxation{
		// matter.js answers a NumberOfRinses its enum does not define with
		// CONSTRAINT_ERROR; chip judges every value against
		// SupportedRinses and answers INVALID_IN_STATE, which
		// TC-WASHERCTRL-2.2 step 6 expects (Test_TC_WASHERCTRL_2_2.yaml:86-95).
		{kind: relaxAttribute, key: "LaundryWasherControls.NumberOfRinses", ref: "connectedhomeip src/app/clusters/laundry-washer-controls-server/LaundryWasherControlsCluster.cpp:132-145 at 6170af84 (owner decision 69)"},
	}},
}

const (
	adminCommRelaxed = "matter.js packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:28-44 at db021da0"
	groupsRelaxed    = "matter.js packages/node/src/behaviors/groups/GroupsServer.ts:20-50 at db021da0"
	scenesRelaxed    = "matter.js packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:128-159 at db021da0"
	thermostatAtomic = "matter.js packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:239-266 at db021da0"
	doorLockRefusal  = "matter.js packages/node/src/behaviors/door-lock/DoorLockServer.ts:1494-1516 at b25ed960"
	doorLockIndex    = "matter.js packages/node/src/behaviors/door-lock/DoorLockServer.ts:1518-1526 at b25ed960"
	identifyRelaxed  = "matter.js packages/node/src/behaviors/identify/IdentifyServer.ts:162-166 at b25ed960"
	pavstEmptyLists  = "connectedhomeip src/app/clusters/push-av-stream-transport-server/PushAVStreamTransportLogic.cpp:672-678 at 6170af84 (owner decision 69, BD-chip-PushAvStreamTransport-RulesInServer)"
)

// relaxationsOf returns the table's entries for cluster.
func relaxationsOf(cluster string) []relaxation {
	var out []relaxation
	for _, s := range serverRelaxations {
		for _, e := range s.entries {
			if e.key == cluster || len(e.key) > len(cluster) && e.key[:len(cluster)+1] == cluster+"." {
				out = append(out, e)
			}
		}
	}
	return out
}

// relax applies the table's entries for the cluster to its payloads,
// structs and attributes; an entry that names nothing of the cluster is
// an error.
func (g *generator) relax() error {
	name := g.c.Name
	refusals := map[string]relaxation{}
	for _, e := range relaxationsOf(name) {
		switch e.kind {
		case relaxRequestField:
			if !g.relaxField(e, true) {
				return fmt.Errorf("relaxation %s names no request field", e.key)
			}
		case relaxStructField:
			if !g.relaxField(e, false) {
				return fmt.Errorf("relaxation %s names no struct field", e.key)
			}
		case relaxAttribute:
			found := false
			for _, a := range g.attributes() {
				if name+"."+a.Name == e.key {
					g.attrRelaxed[a.ID], found = e.ref, true
				}
			}
			if !found {
				return fmt.Errorf("relaxation %s names no attribute", e.key)
			}
		case relaxRefusal:
			refusals[e.key] = e
		case relaxRequestListEntry:
			if !g.relaxListEntry(e) {
				return fmt.Errorf("relaxation %s names no request list field", e.key)
			}
		default:
			return fmt.Errorf("relaxation %s has no kind", e.key)
		}
	}
	for _, p := range g.payloads {
		if p.command == nil || p.response != nil {
			continue
		}
		if e, ok := refusals[name+"."+p.what]; ok {
			p.check = &requestCheck{refusal: e.refusal, ref: e.ref}
			delete(refusals, e.key)
		}
	}
	for key := range refusals {
		return fmt.Errorf("relaxation %s names no request command", key)
	}
	return nil
}

// relaxField marks the field e names: a request payload's field
// (request) or a struct's; false when the cluster has none.
func (g *generator) relaxField(e relaxation, request bool) bool {
	found := false
	mark := func(owner string, fields []*fieldInfo) {
		for _, f := range fields {
			if g.c.Name+"."+owner+"."+f.name == e.key {
				f.relaxed, found = e.ref, true
			}
		}
	}
	if request {
		for _, p := range g.payloads {
			if p.command != nil && p.response == nil {
				mark(p.what, p.fields)
			}
		}
		return found
	}
	for _, di := range g.dataList {
		mark(di.dt.Name, di.fields)
	}
	return found
}

// relaxListEntry marks the entry of the request list field e names;
// false when the cluster has no such list field.
func (g *generator) relaxListEntry(e relaxation) bool {
	found := false
	for _, p := range g.payloads {
		if p.command == nil || p.response != nil {
			continue
		}
		for _, f := range p.fields {
			if g.c.Name+"."+p.what+"."+f.name == e.key && f.t.kind == kindList && f.t.elem != nil {
				f.t.elem.relaxed, found = e.ref, true
			}
		}
	}
	return found
}

// constraintText is the model's constraint of f as matter.js states it.
func constraintText(f *fieldInfo) string {
	if f.v == nil || f.v.Constraint == nil {
		return ""
	}
	return f.v.Constraint.Text
}

// literal is the spec.RequestCheck literal of c; empty for none.
func (c *requestCheck) literal() string {
	switch {
	case c == nil:
		return ""
	case c.refusal != 0:
		return fmt.Sprintf("spec.RequestCheck{Refusal: 0x%02X, Ref: %q}", c.refusal, c.ref)
	default:
		return ""
	}
}
