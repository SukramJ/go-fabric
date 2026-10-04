// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"fmt"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/tlv"
)

// Groups (0x0004) command payloads (Matter §1.3.7, groups.element.ts).
// GroupsServer relaxes the model's "min 1" GroupId and "max 16" GroupName
// constraints on the requests so the server answers them with a
// ConstraintError status in its response; the decoders therefore accept
// any uint16 group id and any name, and leave the checks to
// [mattercore.Groups].

// decodeGroupIDAndName reads AddGroup / AddGroupIfIdentifying:
// [0] group-id GroupId, [1] string GroupName — both mandatory.
func decodeGroupIDAndName(dec *tlv.Decoder, cmd string) (groupID uint16, name string, err error) {
	var haveID, haveName bool
	for {
		el, nerr := dec.Next()
		if nerr != nil {
			return 0, "", fmt.Errorf("%s: %w", cmd, nerr)
		}
		if el.IsEndContainer {
			if !haveID || !haveName {
				return 0, "", fieldInvalidCommandError{msg: cmd + ": mandatory field missing", consumed: true}
			}
			return groupID, name, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			if err := skipValue(dec, el); err != nil {
				return 0, "", fmt.Errorf("%s: %w", cmd, err)
			}
			continue
		}
		switch el.Tag.Number {
		case 0:
			if groupID, err = decodeUint16Field(cmd, "GroupId", el); err != nil {
				_ = skipValue(dec, el)
				return 0, "", err
			}
			haveID = true
		case 1:
			if el.Type < tlv.TypeUTF8Str1 || el.Type > tlv.TypeUTF8Str8 {
				_ = skipValue(dec, el)
				return 0, "", fieldInvalidCommandError{msg: fmt.Sprintf("%s: GroupName is not a string (type=0x%02X)", cmd, el.Type)}
			}
			name, haveName = el.String, true
		default:
			if err := skipValue(dec, el); err != nil {
				return 0, "", fmt.Errorf("%s: %w", cmd, err)
			}
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
	id, err := decodeSingleUint16Request(dec, "ViewGroup", "GroupId")
	return mattercore.ViewGroupRequest{GroupID: id}, err
}

func decodeRemoveGroupRequest(dec *tlv.Decoder) (mattercore.RemoveGroupRequest, error) {
	id, err := decodeSingleUint16Request(dec, "RemoveGroup", "GroupId")
	return mattercore.RemoveGroupRequest{GroupID: id}, err
}

// decodeGetGroupMembershipRequest reads GetGroupMembership:
// [0] list<group-id> GroupList (mandatory, may be empty).
func decodeGetGroupMembershipRequest(dec *tlv.Decoder) (mattercore.GetGroupMembershipRequest, error) {
	const cmd = "GetGroupMembership"
	var (
		req     mattercore.GetGroupMembershipRequest
		present bool
	)
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("%s: %w", cmd, err)
		}
		if el.IsEndContainer {
			if !present {
				return req, fieldInvalidCommandError{msg: cmd + ": mandatory field GroupList missing", consumed: true}
			}
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext || el.Tag.Number != 0 {
			if err := skipValue(dec, el); err != nil {
				return req, fmt.Errorf("%s: %w", cmd, err)
			}
			continue
		}
		if !el.IsContainer || el.Type != tlv.TypeArray {
			_ = skipValue(dec, el)
			return req, fieldInvalidCommandError{msg: fmt.Sprintf("%s: GroupList is not a list (type=0x%02X)", cmd, el.Type)}
		}
		present = true
		req.GroupList = []uint16{}
		for {
			item, err := dec.Next()
			if err != nil {
				return req, fmt.Errorf("%s: %w", cmd, err)
			}
			if item.IsEndContainer {
				break
			}
			id, ferr := decodeUint16Field(cmd, "GroupList entry", item)
			if ferr != nil {
				_ = skipValue(dec, item)
				if err := drainContainer(dec); err != nil {
					return req, fmt.Errorf("%s: %w", cmd, err)
				}
				return req, ferr
			}
			req.GroupList = append(req.GroupList, id)
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
