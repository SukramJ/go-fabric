// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"fmt"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// Groups (0x0004) command payloads (Matter §1.3.7, groups.element.ts).
// GroupsServer relaxes the model's "min 1" GroupId and "max 16" GroupName
// constraints on the requests so the server answers them with a
// ConstraintError status in its response; the decoders therefore accept
// any uint16 group id and any name, and leave the checks to
// [mattercore.Groups].

// groupCommandFieldsReader decodes the Groups (0x0004) and
// GroupKeyManagement (0x003F) command payloads; ok is false for a command
// it does not know, which the caller then salvages generically.
func groupCommandFieldsReader(path im.ConcreteCommandPath, dec *tlv.Decoder) (fields any, ok bool, err error) {
	switch {
	case path.Cluster == mattercore.GroupsClusterID && path.Command == 0x00:
		fields, err = decodeAddGroupRequest(dec)
	case path.Cluster == mattercore.GroupsClusterID && path.Command == 0x01:
		fields, err = decodeViewGroupRequest(dec)
	case path.Cluster == mattercore.GroupsClusterID && path.Command == 0x02:
		fields, err = decodeGetGroupMembershipRequest(dec)
	case path.Cluster == mattercore.GroupsClusterID && path.Command == 0x03:
		fields, err = decodeRemoveGroupRequest(dec)
	case path.Cluster == mattercore.GroupsClusterID && path.Command == 0x04: // RemoveAllGroups carries no fields
		err = decodeKeySetReadAllIndicesRequest(dec)
	case path.Cluster == mattercore.GroupsClusterID && path.Command == 0x05:
		fields, err = decodeAddGroupIfIdentifyingRequest(dec)
	case path.Cluster == 0x003F && path.Command == 0x00:
		fields, err = decodeKeySetWriteByDefinition(dec)
	case path.Cluster == 0x003F && path.Command == 0x01:
		fields, err = decodeKeySetReadRequest(dec)
	case path.Cluster == 0x003F && path.Command == 0x03:
		fields, err = decodeKeySetRemoveRequest(dec)
	case path.Cluster == 0x003F && path.Command == 0x04:
		err = decodeKeySetReadAllIndicesRequest(dec)
	default:
		return nil, false, nil
	}
	return fields, true, err
}

// decodeGroupIDAndName reads AddGroup / AddGroupIfIdentifying:
// [0] group-id GroupId, [1] string GroupName. The request passed its
// definition's check (heldByDefinition): both are present and of their
// type (R2-P6-test-0811).
func decodeGroupIDAndName(dec *tlv.Decoder, cmd string) (groupID uint16, name string, err error) {
	for {
		el, nerr := dec.Next()
		if nerr != nil {
			return 0, "", fmt.Errorf("%s: %w", cmd, nerr)
		}
		if el.IsEndContainer {
			return groupID, name, nil
		}
		if el.Tag.Kind == tlv.TagKindContext {
			switch el.Tag.Number {
			case 0:
				groupID = uint16(el.Uint) //nolint:gosec // the definition held the width
			case 1:
				name = el.String
			}
		}
		if err := skipValue(dec, el); err != nil {
			return 0, "", fmt.Errorf("%s: %w", cmd, err)
		}
	}
}

func decodeAddGroupRequest(dec *tlv.Decoder) (mattercore.AddGroupRequest, error) {
	id, name, err := decodeGroupIDAndName(dec, "AddGroup")
	return mattercore.AddGroupRequest{GroupID: id, GroupName: name}, err
}

func decodeAddGroupIfIdentifyingRequest(dec *tlv.Decoder) (mattercore.AddGroupIfIdentifyingRequest, error) {
	id, name, err := decodeGroupIDAndName(dec, "AddGroupIfIdentifying")
	return mattercore.AddGroupIfIdentifyingRequest{GroupID: id, GroupName: name}, err
}

func decodeViewGroupRequest(dec *tlv.Decoder) (mattercore.ViewGroupRequest, error) {
	id, err := decodeSingleUint16Request(dec, "ViewGroup")
	return mattercore.ViewGroupRequest{GroupID: id}, err
}

func decodeRemoveGroupRequest(dec *tlv.Decoder) (mattercore.RemoveGroupRequest, error) {
	id, err := decodeSingleUint16Request(dec, "RemoveGroup")
	return mattercore.RemoveGroupRequest{GroupID: id}, err
}

// decodeGetGroupMembershipRequest reads GetGroupMembership:
// [0] list<group-id> GroupList (may be empty). The request passed its
// definition's check (heldByDefinition): the list is present and holds
// group ids (R2-P6-test-0811).
func decodeGetGroupMembershipRequest(dec *tlv.Decoder) (mattercore.GetGroupMembershipRequest, error) {
	const cmd = "GetGroupMembership"
	var req mattercore.GetGroupMembershipRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("%s: %w", cmd, err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext || el.Tag.Number != 0 || !el.IsContainer {
			if err := skipValue(dec, el); err != nil {
				return req, fmt.Errorf("%s: %w", cmd, err)
			}
			continue
		}
		req.GroupList = []uint16{}
		for {
			item, err := dec.Next()
			if err != nil {
				return req, fmt.Errorf("%s: %w", cmd, err)
			}
			if item.IsEndContainer {
				break
			}
			req.GroupList = append(req.GroupList, uint16(item.Uint)) //nolint:gosec // the definition held the width
		}
	}
}

// encodeGroupsResponse writes the four Groups responses (Matter §1.3.7.7-10),
// in groups.element.ts field order. GetGroupMembershipResponse.Capacity is
// nullable on the wire and always carries a value here, as in matter.js.
// Returns false for a value that is no Groups response.
func encodeGroupsResponse(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	switch x := v.(type) {
	case mattercore.AddGroupResponse:
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.Status))
		enc.PutUint(tlv.ContextTag(1), uint64(x.GroupID))
		_ = enc.EndContainer()
	case mattercore.ViewGroupResponse:
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.Status))
		enc.PutUint(tlv.ContextTag(1), uint64(x.GroupID))
		enc.PutUTF8(tlv.ContextTag(2), x.GroupName)
		_ = enc.EndContainer()
	case mattercore.GetGroupMembershipResponse:
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.Capacity))
		enc.StartArray(tlv.ContextTag(1))
		for _, id := range x.GroupList {
			enc.PutUint(tlv.AnonymousTag(), uint64(id))
		}
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	case mattercore.RemoveGroupResponse:
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.Status))
		enc.PutUint(tlv.ContextTag(1), uint64(x.GroupID))
		_ = enc.EndContainer()
	default:
		return false
	}
	return true
}
