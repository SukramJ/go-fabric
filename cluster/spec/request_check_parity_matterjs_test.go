// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	lwc "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// decodeRequest is req as the definition's request check answers it:
// encoded, then decoded through spec.DecodeRequest.
func decodeRequest(t *testing.T, clusterID, cmdID uint32, req spec.Encodable) (any, error) {
	t.Helper()
	enc := tlv.NewEncoder()
	req.EncodeTLV(enc, tlv.AnonymousTag())
	raw, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(raw)
	open, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	fields, ok, err := spec.DecodeRequest(clusterID, cmdID, dec, open)
	if !ok {
		t.Fatalf("cluster 0x%04X command 0x%02X has no request decoder", clusterID, cmdID)
	}
	return fields, err
}

// TestRequestCheckParityMatterJS holds the request check a definition
// makes before the server runs to matter.js's: the request schema refuses
// a field outside its constraint with CONSTRAINT_ERROR
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:474-487
// at db021da0), and a server that departs from it says so in the
// relaxation table (script/clustergen/relaxed.go) — DoorLock skips the
// schema's validation for every command (ProtocolService.ts:343-348) and
// answers INVALID_COMMAND for ten of them (DoorLockServer.ts:1493-1511).
func TestRequestCheckParityMatterJS(t *testing.T) {
	t.Parallel()
	status := spec.StatusOf

	// A server held to the model: OnOff's OnTime "max 65534".
	if _, err := decodeRequest(t, onoffdef.ClusterID, onoffdef.CmdOnWithTimedOff, onoffdef.OnWithTimedOffRequest{OnTime: 65535}); status(err) != im.StatusConstraintError {
		t.Errorf("OnWithTimedOff OnTime 65535: %v, want CONSTRAINT_ERROR", err)
	}
	// DoorLock, a refusal command: a UserName over "max 10" is
	// INVALID_COMMAND.
	long := spec.ValueOf("elevenchars")
	if _, err := decodeRequest(t, lockdef.ClusterID, lockdef.CmdSetUser, lockdef.SetUserRequest{OperationType: lockdef.DataOperationTypeAdd, UserIndex: 1, UserName: long}); status(err) != im.StatusInvalidCommand {
		t.Errorf("SetUser with an 11-character UserName: %v, want INVALID_COMMAND", err)
	}
	// DoorLock, an unchecked command: a SigningKey of 31 bytes reaches the
	// server.
	got, err := decodeRequest(t, lockdef.ClusterID, lockdef.CmdSetAliroReaderConfig, lockdef.SetAliroReaderConfigRequest{
		SigningKey: make([]byte, 31), VerificationKey: make([]byte, 65), GroupIdentifier: make([]byte, 16),
	})
	if err != nil || len(got.(lockdef.SetAliroReaderConfigRequest).SigningKey) != 31 {
		t.Errorf("SetAliroReaderConfig with a 31-byte SigningKey: %v %v, want it decoded", got, err)
	}
	// DoorLock, an unchecked command: an index of 0 reaches the server,
	// which answers it in its response (DoorLockServer.ts:887-893); the
	// type's width still holds.
	if _, err := decodeRequest(t, lockdef.ClusterID, lockdef.CmdGetHolidaySchedule, lockdef.GetHolidayScheduleRequest{HolidayIndex: 0}); err != nil {
		t.Errorf("GetHolidaySchedule index 0: %v, want it decoded", err)
	}
	cmd := lockdef.Definition.Command(lockdef.CmdGetHolidaySchedule, spec.Request)
	if !cmd.Check.Unchecked || cmd.Check.Ref == "" {
		t.Errorf("GetHolidaySchedule check %+v, want unchecked with its reference", cmd.Check)
	}
}

// TestRelaxedAttributeIsHeldToItsWidth: a written value of an attribute
// the relaxation table relaxes is held to its type's width only — the
// server judges the rest (LaundryWasherControls NumberOfRinses, owner
// decision 69).
func TestRelaxedAttributeIsHeldToItsWidth(t *testing.T) {
	t.Parallel()
	inst, err := spec.New(lwc.Definition, spec.Options{Features: uint32(lwc.FeatureRinse)})
	if err != nil {
		t.Fatal(err)
	}
	if a := lwc.Definition.Attribute(lwc.AttrNumberOfRinses); a.Relaxed == "" {
		t.Fatal("NumberOfRinses carries no relaxation")
	}
	if _, err := inst.ValidateWrite(lwc.AttrNumberOfRinses, uint64(4), nil); err != nil {
		t.Errorf("NumberOfRinses 4: %v, want it to reach the server", err)
	}
	if _, err := inst.ValidateWrite(lwc.AttrNumberOfRinses, uint64(256), nil); spec.StatusOf(err) != im.StatusConstraintError {
		t.Errorf("NumberOfRinses 256: %v, want CONSTRAINT_ERROR", err)
	}
}
