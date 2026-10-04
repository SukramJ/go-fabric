// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// Groupcast (0x0065) and the AccessControl Auxiliary surface over the wire:
// each command sealed by a CASE controller, decoded by commandFieldsReader,
// served by the real cluster on a real store, its response and the
// attributes encoded by the bridge's writers.

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

const gcCluster uint32 = 0x0065

// joinGroupFields encodes a JoinGroup payload; nil pointers are left out.
func joinGroupFields(groupID uint16, endpoints []uint16, keySetID uint16, key []byte, useAux, replace *bool, policy *uint8) func(enc *tlv.Encoder) {
	return func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), uint64(groupID))
		enc.StartArray(tlv.ContextTag(1))
		for _, ep := range endpoints {
			enc.PutUint(tlv.AnonymousTag(), uint64(ep))
		}
		_ = enc.EndContainer()
		enc.PutUint(tlv.ContextTag(2), uint64(keySetID))
		if key != nil {
			enc.PutOctets(tlv.ContextTag(3), key)
		}
		if useAux != nil {
			enc.PutBool(tlv.ContextTag(4), *useAux)
		}
		if replace != nil {
			enc.PutBool(tlv.ContextTag(5), *replace)
		}
		if policy != nil {
			enc.PutUint(tlv.ContextTag(6), uint64(*policy))
		}
	}
}

func leaveGroupFields(groupID uint16, endpoints []uint16) func(enc *tlv.Encoder) {
	return func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), uint64(groupID))
		if endpoints != nil {
			enc.StartArray(tlv.ContextTag(1))
			for _, ep := range endpoints {
				enc.PutUint(tlv.AnonymousTag(), uint64(ep))
			}
			_ = enc.EndContainer()
		}
	}
}

func bptr(v bool) *bool    { return &v }
func u8ptr(v uint8) *uint8 { return &v }

// gcStatus invokes a Groupcast command and returns the status it
// answered with (Success for a response).
func (gh *groupsHarness) gcStatus(t *testing.T, cmd uint32, fields func(enc *tlv.Encoder)) im.StatusCode {
	t.Helper()
	_, _, status, isStatus := invokeResult(t, gh.invoke(0, gcCluster, cmd, fields))
	if !isStatus {
		return im.StatusSuccess
	}
	return status
}

func TestGroupcastCommandsOverTheWire(t *testing.T) {
	t.Parallel()
	gh := newGroupcastHarness(t, 2)
	lamps := gh.lampIDs()
	key := bytes.Repeat([]byte{0x3C}, 16)

	if st := gh.gcStatus(t, 0x00, joinGroupFields(0x0101, lamps, 0x0042, key, bptr(true), nil, u8ptr(0))); st != im.StatusSuccess {
		t.Fatalf("JoinGroup: %v", st)
	}
	membership := gh.readAttributeData(t, 0, gcCluster, 0x0000, true)
	if len(membership.Children) != 1 {
		t.Fatalf("Membership has %d entries, want 1", len(membership.Children))
	}
	m := membership.Children[0]
	if m.mustChild(t, 0).El.Uint != 0x0101 || len(m.mustChild(t, 1).Children) != 2 || m.mustChild(t, 2).El.Uint != 0x0042 ||
		!m.mustChild(t, 3).El.Bool || m.mustChild(t, 4).El.Uint != 0 || m.mustChild(t, 0xFE).El.Uint != uint64(gh.fabric) {
		t.Errorf("MembershipStruct = %+v", m)
	}
	if v := gh.readAttributeData(t, 0, gcCluster, 0x0003, true); v.El.Uint != 1 {
		t.Errorf("UsedMcastAddrCount = %d, want 1", v.El.Uint)
	}

	// The auxiliary entry, as AccessControl.AuxiliaryAcl serves it.
	aux := gh.readAttributeData(t, 0, 0x001F, 0x0007, true)
	if len(aux.Children) != 1 {
		t.Fatalf("AuxiliaryAcl has %d entries, want 1", len(aux.Children))
	}
	e := aux.Children[0]
	if e.mustChild(t, 1).El.Uint != 3 || e.mustChild(t, 2).El.Uint != 3 || e.mustChild(t, 3).Children[0].El.Uint != 0x0101 ||
		len(e.mustChild(t, 4).Children) != 2 || e.mustChild(t, 5).El.Uint != 1 {
		t.Errorf("AuxiliaryAcl entry = %+v, want Operate/Group/[0x0101]/two endpoints/Groupcast", e)
	}

	if st := gh.gcStatus(t, 0x03, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 0x0101)
		enc.PutUint(tlv.ContextTag(1), 0x0043)
		enc.PutOctets(tlv.ContextTag(2), key)
	}); st != im.StatusSuccess {
		t.Errorf("UpdateGroupKey: %v", st)
	}
	if st := gh.gcStatus(t, 0x04, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 0x0101)
		enc.PutBool(tlv.ContextTag(1), false)
	}); st != im.StatusSuccess {
		t.Errorf("ConfigureAuxiliaryAcl: %v", st)
	}
	if st := gh.gcStatus(t, 0x05, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 1)
		enc.PutUint(tlv.ContextTag(1), 10)
	}); st != im.StatusSuccess {
		t.Errorf("GroupcastTesting: %v", st)
	}
	if v := gh.readAttributeData(t, 0, gcCluster, 0x0004, false); v.El.Uint != uint64(gh.fabric) {
		t.Errorf("FabricUnderTest = %d", v.El.Uint)
	}

	// LeaveGroup answers LeaveGroupResponse (command 0x02).
	cmd, fields, _, isStatus := invokeResult(t, gh.invoke(0, gcCluster, 0x01, leaveGroupFields(0x0101, []uint16{lamps[0]})))
	if isStatus || cmd != 0x02 || fields.mustChild(t, 0).El.Uint != 0x0101 ||
		len(fields.mustChild(t, 1).Children) != 1 || fields.mustChild(t, 1).Children[0].El.Uint != uint64(lamps[0]) {
		t.Fatalf("LeaveGroupResponse = cmd 0x%02X %+v", cmd, fields)
	}
	cmd, fields, _, isStatus = invokeResult(t, gh.invoke(0, gcCluster, 0x01, leaveGroupFields(0, nil)))
	if isStatus || cmd != 0x02 || fields.mustChild(t, 0).El.Uint != 0 || len(fields.mustChild(t, 1).Children) != 0 {
		t.Fatalf("wildcard LeaveGroupResponse = cmd 0x%02X %+v", cmd, fields)
	}
	if len(gh.readAttributeData(t, 0, gcCluster, 0x0000, true).Children) != 0 {
		t.Error("Membership not empty after LeaveGroup 0")
	}
}

// TestGroupcastMalformedPayloadsOverTheWire: the type and presence checks
// of matter.js's TlvSchema decode answer InvalidCommand, an integer that
// overflows its type ConstraintError, and a model constraint the server's
// ConstraintError.
func TestGroupcastMalformedPayloadsOverTheWire(t *testing.T) {
	t.Parallel()
	gh := newGroupcastHarness(t, 1)
	for name, tc := range map[string]struct {
		cmd    uint32
		fields func(enc *tlv.Encoder)
		want   im.StatusCode
	}{
		"JoinGroup without KeySetId": {0x00, func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(0), 1)
			enc.StartArray(tlv.ContextTag(1))
			_ = enc.EndContainer()
		}, im.StatusInvalidCommand},
		"JoinGroup Endpoints not a list": {0x00, func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(0), 1)
			enc.PutUint(tlv.ContextTag(1), 2)
			enc.PutUint(tlv.ContextTag(2), 1)
		}, im.StatusInvalidCommand},
		"JoinGroup endpoint entry not an integer": {0x00, func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(0), 1)
			enc.StartArray(tlv.ContextTag(1))
			enc.PutUTF8(tlv.AnonymousTag(), "x")
			_ = enc.EndContainer()
			enc.PutUint(tlv.ContextTag(2), 1)
		}, im.StatusInvalidCommand},
		"JoinGroup GroupId overflow": {0x00, func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(0), 0x1_0000)
			enc.StartArray(tlv.ContextTag(1))
			enc.PutUint(tlv.AnonymousTag(), 2)
			_ = enc.EndContainer()
			enc.PutUint(tlv.ContextTag(2), 1)
		}, im.StatusConstraintError},
		"JoinGroup Key not octets": {0x00, func(enc *tlv.Encoder) {
			joinGroupFields(1, []uint16{2}, 1, nil, nil, nil, nil)(enc)
			enc.PutUTF8(tlv.ContextTag(3), "key")
		}, im.StatusInvalidCommand},
		"JoinGroup UseAuxiliaryAcl not a bool": {0x00, func(enc *tlv.Encoder) {
			joinGroupFields(1, []uint16{2}, 1, nil, nil, nil, nil)(enc)
			enc.PutUint(tlv.ContextTag(4), 1)
		}, im.StatusInvalidCommand},
		"JoinGroup McastAddrPolicy overflow": {0x00, func(enc *tlv.Encoder) {
			joinGroupFields(1, []uint16{2}, 1, nil, nil, nil, nil)(enc)
			enc.PutUint(tlv.ContextTag(6), 0x100)
		}, im.StatusConstraintError},
		"LeaveGroup without GroupId": {0x01, func(*tlv.Encoder) {}, im.StatusInvalidCommand},
		"LeaveGroup empty Endpoints": {0x01, leaveGroupFields(1, []uint16{}), im.StatusConstraintError},
		"UpdateGroupKey without KeySetId": {0x03, func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(0), 1)
		}, im.StatusInvalidCommand},
		"ConfigureAuxiliaryAcl without the flag": {0x04, func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(0), 1)
		}, im.StatusInvalidCommand},
		"GroupcastTesting without TestOperation": {0x05, func(*tlv.Encoder) {}, im.StatusInvalidCommand},
		"GroupcastTesting TestOperation not an integer": {0x05, func(enc *tlv.Encoder) {
			enc.PutBool(tlv.ContextTag(0), true)
		}, im.StatusInvalidCommand},
		"GroupcastTesting EnableSenderTesting": {0x05, func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(0), 2)
		}, im.StatusConstraintError},
		"GroupcastTesting DurationSeconds overflow": {0x05, func(enc *tlv.Encoder) {
			enc.PutUint(tlv.ContextTag(0), 1)
			enc.PutUint(tlv.ContextTag(1), 0x1_0000)
		}, im.StatusConstraintError},
	} {
		if got := gh.gcStatus(t, tc.cmd, tc.fields); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
	// A stray, unknown field is ignored as the schema decode ignores it.
	if st := gh.gcStatus(t, 0x05, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 0)
		enc.StartStruct(tlv.ContextTag(9))
		_ = enc.EndContainer()
	}); st != im.StatusSuccess {
		t.Errorf("unknown field: %v", st)
	}
}

// TestACLWriteWithAuxiliaryTypeOverTheWire: an ACL entry carrying
// AuxiliaryType is refused with Failure, as matter.js and CHIP refuse it.
func TestACLWriteWithAuxiliaryTypeOverTheWire(t *testing.T) {
	t.Parallel()
	gh := newGroupcastHarness(t, 1)
	status := gh.writeAttribute(0, 0x001F, 0x0000, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartArray(tag)
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(1), 5)
		enc.PutUint(tlv.ContextTag(2), 2)
		enc.StartArray(tlv.ContextTag(3))
		enc.PutUint(tlv.AnonymousTag(), harnessControllerNodeID)
		_ = enc.EndContainer()
		enc.PutNull(tlv.ContextTag(4))
		enc.PutUint(tlv.ContextTag(5), 1)
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	})
	if status != uint64(im.StatusFailure) {
		t.Fatalf("ACL write with AuxiliaryType: status 0x%02X, want Failure", status)
	}
}
