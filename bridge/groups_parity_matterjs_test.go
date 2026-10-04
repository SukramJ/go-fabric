// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// Wire parity for the group command payloads against matter.js's own
// encoder. Fixture master: testdata/group-wire-fixtures.json, produced by
// notes/parity/matter/generate-group-fixtures.ts (`wire`) from
// TlvOfModel(command) — the schema matter.js's interaction server decodes
// requests and encodes responses with
// (packages/node/src/node/integration/ProtocolService.ts:addCluster).
//
// Requests run matter.js bytes through commandFieldsReader and compare the
// decoded struct with the fixture; responses run the fixture through
// defaultCommandFieldsWriter and compare bytes.

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"slices"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

//go:embed testdata/group-wire-fixtures.json
var groupWireFixturesJSON []byte

type groupWireFixture struct {
	Label    string          `json:"label"`
	Cluster  uint32          `json:"cluster"`
	Command  string          `json:"command"`
	Fixture  json.RawMessage `json:"fixture"`
	BytesHex string          `json:"bytesHex"`
}

type keySetFixture struct {
	GroupKeySetID           uint16  `json:"groupKeySetId"`
	GroupKeySecurityPolicy  uint8   `json:"groupKeySecurityPolicy"`
	EpochKey0               *string `json:"epochKey0"`
	EpochStartTime0         *uint64 `json:"epochStartTime0"`
	EpochKey1               *string `json:"epochKey1"`
	EpochStartTime1         *uint64 `json:"epochStartTime1"`
	EpochKey2               *string `json:"epochKey2"`
	EpochStartTime2         *uint64 `json:"epochStartTime2"`
	GroupKeyMulticastPolicy *uint8  `json:"groupKeyMulticastPolicy"`
}

func (f keySetFixture) toStruct(t *testing.T) mattercore.GroupKeySetStruct {
	t.Helper()
	key := func(s *string) []byte {
		if s == nil {
			return nil
		}
		b, err := hex.DecodeString(*s)
		if err != nil {
			t.Fatalf("fixture key: %v", err)
		}
		return b
	}
	start := func(v *uint64) uint64 {
		if v == nil {
			return 0
		}
		return *v
	}
	gks := mattercore.GroupKeySetStruct{
		GroupKeySetID: f.GroupKeySetID, GroupKeySecurityPolicy: f.GroupKeySecurityPolicy,
		EpochKey0: key(f.EpochKey0), EpochStartTime0: start(f.EpochStartTime0),
		EpochKey1: key(f.EpochKey1), EpochStartTime1: start(f.EpochStartTime1),
		EpochKey2: key(f.EpochKey2), EpochStartTime2: start(f.EpochStartTime2),
	}
	if f.GroupKeyMulticastPolicy != nil {
		gks.GroupKeyMulticastPolicy = *f.GroupKeyMulticastPolicy
	}
	return gks
}

func loadGroupWireFixtures(t *testing.T) []groupWireFixture {
	t.Helper()
	var out []groupWireFixture
	if err := json.Unmarshal(groupWireFixturesJSON, &out); err != nil {
		t.Fatalf("parse group-wire-fixtures.json: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("group-wire-fixtures.json is empty")
	}
	return out
}

// encodeCommandFields runs v through the production command-fields writer.
func encodeCommandFields(t *testing.T, v any) string {
	t.Helper()
	enc := tlv.NewEncoder()
	defaultCommandFieldsWriter(enc, tlv.AnonymousTag(), v)
	b, err := enc.Bytes()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return hex.EncodeToString(b)
}

// decodeCommandFields runs matter.js request bytes through the production
// command-fields reader.
func decodeCommandFields(t *testing.T, cluster, command uint32, raw string) any {
	t.Helper()
	b, err := hex.DecodeString(raw)
	if err != nil {
		t.Fatalf("fixture bytes: %v", err)
	}
	dec := tlv.NewDecoder(b)
	open, err := dec.Next()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, err := commandFieldsReader(im.ConcreteCommandPath{Cluster: cluster, Command: command}, dec, open)
	if err != nil {
		t.Fatalf("commandFieldsReader: %v", err)
	}
	return got
}

func TestGroupCommandPayloadsMatchMatterJS(t *testing.T) {
	t.Parallel()
	for _, f := range loadGroupWireFixtures(t) {
		t.Run(f.Label, func(t *testing.T) {
			t.Parallel()
			switch f.Command {
			case "KeySetWrite":
				var fx keySetFixture
				if err := json.Unmarshal(f.Fixture, &fx); err != nil {
					t.Fatal(err)
				}
				got, ok := decodeCommandFields(t, 0x003F, 0x00, f.BytesHex).(mattercore.KeySetWriteRequest)
				if !ok {
					t.Fatalf("decoded %T, want KeySetWriteRequest", got)
				}
				want := fx.toStruct(t)
				if g := got.GroupKeySet; g.GroupKeySetID != want.GroupKeySetID || g.GroupKeySecurityPolicy != want.GroupKeySecurityPolicy ||
					!slices.Equal(g.EpochKey0, want.EpochKey0) || g.EpochStartTime0 != want.EpochStartTime0 ||
					!slices.Equal(g.EpochKey1, want.EpochKey1) || g.EpochStartTime1 != want.EpochStartTime1 ||
					!slices.Equal(g.EpochKey2, want.EpochKey2) || g.EpochStartTime2 != want.EpochStartTime2 ||
					g.GroupKeyMulticastPolicy != want.GroupKeyMulticastPolicy {
					t.Fatalf("decoded %+v, want %+v", g, want)
				}
			case "KeySetReadResponse":
				var fx keySetFixture
				if err := json.Unmarshal(f.Fixture, &fx); err != nil {
					t.Fatal(err)
				}
				if got := encodeCommandFields(t, mattercore.KeySetReadResponse{GroupKeySet: fx.toStruct(t)}); got != f.BytesHex {
					t.Fatalf("encoded %s\n  matter.js %s", got, f.BytesHex)
				}
			case "KeySetReadAllIndicesResponse":
				var fx struct {
					GroupKeySetIDs []uint16 `json:"groupKeySetIds"`
				}
				if err := json.Unmarshal(f.Fixture, &fx); err != nil {
					t.Fatal(err)
				}
				if got := encodeCommandFields(t, mattercore.KeySetReadAllIndicesResponse{GroupKeySetIDs: fx.GroupKeySetIDs}); got != f.BytesHex {
					t.Fatalf("encoded %s\n  matter.js %s", got, f.BytesHex)
				}
			default:
				checkGroupsFixture(t, f)
			}
		})
	}
}

// groupsFixtureFields is the union of the Groups payload fields.
type groupsFixtureFields struct {
	GroupID   uint16   `json:"groupId"`
	GroupName string   `json:"groupName"`
	GroupList []uint16 `json:"groupList"`
	Status    uint8    `json:"status"`
	Capacity  uint8    `json:"capacity"`
}

// checkGroupsFixture covers the Groups (0x0004) payloads: requests decode
// to the fixture's fields, responses encode to matter.js's bytes.
func checkGroupsFixture(t *testing.T, f groupWireFixture) {
	t.Helper()
	var fx groupsFixtureFields
	if err := json.Unmarshal(f.Fixture, &fx); err != nil {
		t.Fatal(err)
	}
	decoded := func(cmd uint32) any { return decodeCommandFields(t, mattercore.GroupsClusterID, cmd, f.BytesHex) }
	encoded := func(v any) {
		t.Helper()
		if got := encodeCommandFields(t, v); got != f.BytesHex {
			t.Fatalf("encoded %s\n  matter.js %s", got, f.BytesHex)
		}
	}
	status := im.StatusCode(fx.Status)
	switch f.Command {
	case "AddGroup":
		if got := decoded(0x00); got != (mattercore.AddGroupRequest{GroupID: fx.GroupID, GroupName: fx.GroupName}) {
			t.Fatalf("decoded %+v", got)
		}
	case "AddGroupIfIdentifying":
		if got := decoded(0x05); got != (mattercore.AddGroupIfIdentifyingRequest{GroupID: fx.GroupID, GroupName: fx.GroupName}) {
			t.Fatalf("decoded %+v", got)
		}
	case "ViewGroup":
		if got := decoded(0x01); got != (mattercore.ViewGroupRequest{GroupID: fx.GroupID}) {
			t.Fatalf("decoded %+v", got)
		}
	case "RemoveGroup":
		if got := decoded(0x03); got != (mattercore.RemoveGroupRequest{GroupID: fx.GroupID}) {
			t.Fatalf("decoded %+v", got)
		}
	case "GetGroupMembership":
		got, ok := decoded(0x02).(mattercore.GetGroupMembershipRequest)
		if !ok || !slices.Equal(got.GroupList, fx.GroupList) || got.GroupList == nil {
			t.Fatalf("decoded %+v", got)
		}
	case "AddGroupResponse":
		encoded(mattercore.AddGroupResponse{Status: status, GroupID: fx.GroupID})
	case "ViewGroupResponse":
		encoded(mattercore.ViewGroupResponse{Status: status, GroupID: fx.GroupID, GroupName: fx.GroupName})
	case "GetGroupMembershipResponse":
		encoded(mattercore.GetGroupMembershipResponse{Capacity: fx.Capacity, GroupList: fx.GroupList})
	case "RemoveGroupResponse":
		encoded(mattercore.RemoveGroupResponse{Status: status, GroupID: fx.GroupID})
	default:
		t.Fatalf("fixture %s: no parity check for command %s", f.Label, f.Command)
	}
}
