// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"testing"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/spec"
	icddef "thingfabric.dev/go-fabric/cluster/spec/icdmanagement"
	messagesdef "thingfabric.dev/go-fabric/cluster/spec/messages"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/inflight"
	"thingfabric.dev/go-fabric/tlv"
)

// The tests of this file port matter.js
// packages/node/test/behavior/state/managed/values/ListManagerTest.ts:263
// ("masks another fabric's sensitive field in the entries a sort
// comparator receives") by meaning onto the servers whose fabric-scoped
// list holds fabric-sensitive fields: on a read that is not
// fabric-filtered, the list holds the entries of fabrics 2 and 1, the
// accessing fabric 1's entry is whole and fabric 2's carries no
// fabric-sensitive field — and for a session without a fabric no entry
// does (AccessControl.ts:347-349 at db021da0). The assertions are on the
// wire bytes of the dispatcher's read: each server returns every entry
// whole, the dispatcher scopes it ([readWire]).

// fabricIndexOnly is an entry as another fabric reads it when every field
// but FabricIndex is fabric-sensitive.
type fabricIndexOnly uint8

func (f fabricIndexOnly) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0xFE), uint64(f))
	_ = enc.EndContainer()
}

// wantWire holds got, a list on the wire, to the list of want.
func wantWire(t *testing.T, what string, got []byte, want ...spec.Encodable) {
	t.Helper()
	enc := tlv.NewEncoder()
	spec.List[spec.Encodable](want).EncodeTLV(enc, tlv.AnonymousTag())
	exp, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, exp) {
		t.Errorf("%s:\n got % X\nwant % X", what, got, exp)
	}
}

// TestParityMatterJS_MessagesEntryOfAnotherFabricWithoutSensitiveFields:
// every field of MessageStruct but FabricIndex is "S"
// (messages.element.ts:121-133), so another fabric's message reads as its
// FabricIndex alone.
func TestParityMatterJS_MessagesEntryOfAnotherFabricWithoutSensitiveFields(t *testing.T) {
	t.Parallel()
	m, _, _ := newMessages(t, 0)
	for _, f := range []uint8{2, 1} {
		if err := decodeAndInvoke(t, m, msgCtx(f, false), messagesdef.CmdPresentMessagesRequest, presentRequest(messageID(f), "of a fabric")); err != nil {
			t.Fatal(err)
		}
	}
	local, _ := m.MatterRead(messagesdef.AttrMessages)
	whole := local.(spec.List[messagesdef.MessageStruct])
	if len(whole) != 2 || whole[1].FabricIndex != 1 {
		t.Fatalf("local read %+v", whole)
	}
	wantWire(t, "unfiltered read by fabric 1", readWire(msgCtx(1, false), t, m, messagesdef.AttrMessages), fabricIndexOnly(2), whole[1])
	wantWire(t, "unfiltered read without a fabric", readWire(msgCtx(0, false), t, m, messagesdef.AttrMessages), fabricIndexOnly(2), fabricIndexOnly(1))
	wantWire(t, "filtered read by fabric 1", readWire(msgCtx(1, true), t, m, messagesdef.AttrMessages), whole[1])
}

// sceneInfoWire is a FabricSceneInfo entry on the wire; redacted leaves
// out the "S" fields CurrentScene, CurrentGroup and SceneValid
// (scenes-management.element.ts:179-186).
type sceneInfoWire struct {
	info     core.SceneInfoStruct
	redacted bool
}

func (s sceneInfoWire) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(s.info.SceneCount))
	if !s.redacted {
		enc.PutUint(tlv.ContextTag(1), uint64(s.info.CurrentScene))
		enc.PutUint(tlv.ContextTag(2), uint64(s.info.CurrentGroup))
		enc.PutBool(tlv.ContextTag(3), s.info.SceneValid)
	}
	enc.PutUint(tlv.ContextTag(4), uint64(s.info.RemainingCapacity))
	enc.PutUint(tlv.ContextTag(0xFE), uint64(s.info.FabricIndex))
	_ = enc.EndContainer()
}

// TestParityMatterJS_FabricSceneInfoOfAnotherFabricWithoutSensitiveFields:
// fabric 1 adds and recalls a scene; another fabric's entry reads with
// SceneCount, RemainingCapacity and FabricIndex only, and a read without a
// request sees both entries whole.
func TestParityMatterJS_FabricSceneInfoOfAnotherFabricWithoutSensitiveFields(t *testing.T) {
	t.Parallel()
	s, _ := newScenes(t, &sceneOnOff{}, nil)
	invoke(t, s, 0x00, core.AddSceneRequest{
		GroupID: 0x0101, SceneID: 7, SceneName: "Evening",
		ExtensionFieldSets: []core.ExtensionFieldSet{{ClusterID: 0x0006, AttributeValueList: []core.AttributeValuePair{{AttributeID: 0, Tag: 1, Unsigned: 1, Fields: 1}}}},
	})
	invoke(t, s, 0x05, core.RecallSceneRequest{GroupID: 0x0101, SceneID: 7})
	local, _ := s.MatterRead(0x0002)
	whole := local.([]core.SceneInfoStruct)
	if len(whole) != 2 || whole[0].FabricIndex != 1 || whole[0].CurrentScene != 7 || whole[0].CurrentGroup != 0x0101 || !whole[0].SceneValid {
		t.Fatalf("local read %+v", whole)
	}
	one, two := whole[0], whole[1]
	wantWire(t, "unfiltered read by fabric 2", readWire(msgCtx(2, false), t, s, 0x0002), sceneInfoWire{one, true}, sceneInfoWire{two, false})
	wantWire(t, "unfiltered read without a fabric", readWire(msgCtx(0, false), t, s, 0x0002), sceneInfoWire{one, true}, sceneInfoWire{two, true})
	wantWire(t, "filtered read by fabric 1", readWire(msgCtx(1, true), t, s, 0x0002), sceneInfoWire{one, false})
}

// TestParityMatterJS_RegisteredClientOfAnotherFabricWithoutSensitiveFields:
// CheckInNodeId, MonitoredSubject and ClientType are "S" and the entry
// holds no Key (icd-management.element.ts:145-152,
// IcdManagementServer.ts:572-577), so another fabric's registration reads
// as its FabricIndex alone — not as a registration of node 0.
func TestParityMatterJS_RegisteredClientOfAnotherFabricWithoutSensitiveFields(t *testing.T) {
	t.Parallel()
	s := newICDServer(t, core.ICDManagementConfig{
		IdleModeDuration: 4, MaximumCheckInBackoff: 4, ActiveModeDuration: 2000, ActiveModeThreshold: 1000,
		ClientsSupportedPerFabric: 2, Store: icdStore(t), Clock: inflight.WithGroup(clocktest.New(icdEpoch), inflight.New("icd-fs-test")),
	})
	for _, f := range []uint8{2, 1} {
		if _, err := s.MatterInvoke(icdInvokeCtx(f, privAdminister), icddef.CmdRegisterClient, icddef.RegisterClientRequest{
			CheckInNodeId: 0x10 * uint64(f), MonitoredSubject: 0x10 * uint64(f), Key: icdKey(f), ClientType: icddef.ClientTypeEphemeral,
		}); err != nil {
			t.Fatal(err)
		}
	}
	own := icddef.MonitoringRegistrationStruct{CheckInNodeId: 0x10, MonitoredSubject: 0x10, ClientType: icddef.ClientTypeEphemeral, FabricIndex: 1}
	wantWire(t, "unfiltered read by fabric 1", readWire(im.WithFabricFilter(context.Background(), false, 1), t, s, icddef.AttrRegisteredClients), fabricIndexOnly(2), own)
	wantWire(t, "unfiltered read without a fabric", readWire(im.WithFabricFilter(context.Background(), false, 0), t, s, icddef.AttrRegisteredClients), fabricIndexOnly(2), fabricIndexOnly(1))
	wantWire(t, "filtered read by fabric 1", readWire(im.WithFabricFilter(context.Background(), true, 1), t, s, icddef.AttrRegisteredClients), own)
}
