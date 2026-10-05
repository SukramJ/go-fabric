// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"fmt"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// Groupcast (0x0065) command payloads (core§11.27.7, groupcast.element.ts).
// The decoders check what matter.js's TlvSchema decode checks — the TLV
// type of every field (InvalidCommand) and that a mandatory field is there
// (InvalidCommand), an integer that overflows its type (ConstraintError) —
// and leave the model constraints (GroupId and KeySetId "min 1", Key "16",
// the enum values, LeaveGroup's "1 to 20" endpoints, DurationSeconds "10
// to 1200") to [mattercore.Groupcast], which answers them with the same
// ConstraintError matter.js's validation does.

// groupcastFieldsReader decodes the Groupcast command payloads; ok is false
// for a command it does not know.
func groupcastFieldsReader(path im.ConcreteCommandPath, dec *tlv.Decoder) (fields any, ok bool, err error) {
	switch path.Command {
	case 0x00:
		fields, err = decodeJoinGroupRequest(dec)
	case 0x01:
		fields, err = decodeLeaveGroupRequest(dec)
	case 0x03:
		fields, err = decodeUpdateGroupKeyRequest(dec)
	case 0x04:
		fields, err = decodeConfigureAuxiliaryACLRequest(dec)
	case 0x05:
		fields, err = decodeGroupcastTestingRequest(dec)
	default:
		return nil, false, nil
	}
	return fields, true, err
}

// fieldDecoder handles one context-tagged field; a container field must
// consume its container, also when it fails.
type fieldDecoder func(el tlv.Element) error

// walkCommandFields walks a command's fields: each context tag with a
// decoder is handed to it, every other element is skipped, and a missing
// mandatory tag answers InvalidCommand once the container ends.
func walkCommandFields(dec *tlv.Decoder, cmd string, fields map[uint64]fieldDecoder, mandatory ...uint64) error {
	seen := make(map[uint64]bool, len(fields))
	for {
		el, err := dec.Next()
		if err != nil {
			return fmt.Errorf("%s: %w", cmd, err)
		}
		if el.IsEndContainer {
			for _, tag := range mandatory {
				if !seen[tag] {
					return fieldInvalidCommandError{msg: fmt.Sprintf("%s: mandatory field %d missing", cmd, tag), consumed: true}
				}
			}
			return nil
		}
		fd, known := fields[uint64(el.Tag.Number)]
		if el.Tag.Kind != tlv.TagKindContext || !known {
			if err := skipValue(dec, el); err != nil {
				return fmt.Errorf("%s: %w", cmd, err)
			}
			continue
		}
		if err := fd(el); err != nil {
			return err
		}
		seen[uint64(el.Tag.Number)] = true
	}
}

// uint16Field decodes a mandatory or optional uint16 field into dst.
func uint16Field(dec *tlv.Decoder, cmd, name string, dst *uint16) fieldDecoder {
	return func(el tlv.Element) error {
		v, err := decodeUint16Field(cmd, name, el)
		if err != nil {
			_ = skipValue(dec, el)
			return err
		}
		*dst = v
		return nil
	}
}

// enum8Field decodes an enum8 field into dst.
func enum8Field(dec *tlv.Decoder, cmd, name string, dst *uint8) fieldDecoder {
	return func(el tlv.Element) error {
		if !isUnsignedInt(el) {
			_ = skipValue(dec, el)
			return fieldInvalidCommandError{msg: fmt.Sprintf("%s: %s is not an unsigned integer (type=0x%02X)", cmd, name, el.Type)}
		}
		v, err := fieldUint8(cmd, name, el)
		if err != nil {
			return err
		}
		*dst = v
		return nil
	}
}

// boolField decodes a bool field into dst.
func boolField(dec *tlv.Decoder, cmd, name string, dst *bool) fieldDecoder {
	return func(el tlv.Element) error {
		if el.Type != tlv.TypeBoolFalse && el.Type != tlv.TypeBoolTrue {
			_ = skipValue(dec, el)
			return fieldInvalidCommandError{msg: fmt.Sprintf("%s: %s is not a boolean (type=0x%02X)", cmd, name, el.Type)}
		}
		*dst = el.Bool
		return nil
	}
}

// octetsField decodes an octet string field into dst (never nil once set).
func octetsField(dec *tlv.Decoder, cmd, name string, dst *[]byte) fieldDecoder {
	return func(el tlv.Element) error {
		if !isOctetString(el) {
			_ = skipValue(dec, el)
			return fieldInvalidCommandError{msg: fmt.Sprintf("%s: %s is not an octet string (type=0x%02X)", cmd, name, el.Type)}
		}
		*dst = append([]byte{}, el.Octets...)
		return nil
	}
}

// endpointListField decodes a list<endpoint-no> field into dst (never nil
// once set), consuming the list also when an entry is malformed.
func endpointListField(dec *tlv.Decoder, cmd, name string, dst *[]uint16) fieldDecoder {
	return func(el tlv.Element) error {
		if !el.IsContainer || el.Type != tlv.TypeArray {
			_ = skipValue(dec, el)
			return fieldInvalidCommandError{msg: fmt.Sprintf("%s: %s is not a list (type=0x%02X)", cmd, name, el.Type)}
		}
		out := []uint16{}
		for {
			item, err := dec.Next()
			if err != nil {
				return fmt.Errorf("%s: %w", cmd, err)
			}
			if item.IsEndContainer {
				*dst = out
				return nil
			}
			ep, ferr := decodeUint16Field(cmd, name+" entry", item)
			if ferr != nil {
				_ = skipValue(dec, item)
				if err := drainContainer(dec); err != nil {
					return fmt.Errorf("%s: %w", cmd, err)
				}
				return ferr
			}
			out = append(out, ep)
		}
	}
}

// decodeJoinGroupRequest reads JoinGroup: [0] GroupId, [1] Endpoints,
// [2] KeySetId (mandatory); [3] Key, [4] UseAuxiliaryAcl,
// [5] ReplaceEndpoints, [6] McastAddrPolicy (optional).
func decodeJoinGroupRequest(dec *tlv.Decoder) (mattercore.JoinGroupRequest, error) {
	const cmd = "JoinGroup"
	var (
		req             mattercore.JoinGroupRequest
		useAux, replace bool
		policy          uint8
		hasAux, hasRepl bool
		hasPolicy       bool
		auxDec, replDec = boolField(dec, cmd, "UseAuxiliaryAcl", &useAux), boolField(dec, cmd, "ReplaceEndpoints", &replace)
		policyDec       = enum8Field(dec, cmd, "McastAddrPolicy", &policy)
	)
	err := walkCommandFields(dec, cmd, map[uint64]fieldDecoder{
		0: uint16Field(dec, cmd, "GroupId", &req.GroupID),
		1: endpointListField(dec, cmd, "Endpoints", &req.Endpoints),
		2: uint16Field(dec, cmd, "KeySetId", &req.KeySetID),
		3: octetsField(dec, cmd, "Key", &req.Key),
		4: func(el tlv.Element) error { hasAux = true; return auxDec(el) },
		5: func(el tlv.Element) error { hasRepl = true; return replDec(el) },
		6: func(el tlv.Element) error { hasPolicy = true; return policyDec(el) },
	}, 0, 1, 2)
	if hasAux {
		req.UseAuxiliaryACL = &useAux
	}
	if hasRepl {
		req.ReplaceEndpoints = &replace
	}
	if hasPolicy {
		req.McastAddrPolicy = &policy
	}
	return req, err
}

// decodeLeaveGroupRequest reads LeaveGroup: [0] GroupId (mandatory),
// [1] Endpoints (optional).
func decodeLeaveGroupRequest(dec *tlv.Decoder) (mattercore.LeaveGroupRequest, error) {
	const cmd = "LeaveGroup"
	var req mattercore.LeaveGroupRequest
	epDec := endpointListField(dec, cmd, "Endpoints", &req.Endpoints)
	err := walkCommandFields(dec, cmd, map[uint64]fieldDecoder{
		0: uint16Field(dec, cmd, "GroupId", &req.GroupID),
		1: func(el tlv.Element) error { req.HasEndpoints = true; return epDec(el) },
	}, 0)
	return req, err
}

// decodeUpdateGroupKeyRequest reads UpdateGroupKey: [0] GroupId,
// [1] KeySetId (mandatory), [2] Key (optional).
func decodeUpdateGroupKeyRequest(dec *tlv.Decoder) (mattercore.UpdateGroupKeyRequest, error) {
	const cmd = "UpdateGroupKey"
	var req mattercore.UpdateGroupKeyRequest
	err := walkCommandFields(dec, cmd, map[uint64]fieldDecoder{
		0: uint16Field(dec, cmd, "GroupId", &req.GroupID),
		1: uint16Field(dec, cmd, "KeySetId", &req.KeySetID),
		2: octetsField(dec, cmd, "Key", &req.Key),
	}, 0, 1)
	return req, err
}

// decodeConfigureAuxiliaryACLRequest reads ConfigureAuxiliaryAcl:
// [0] GroupId, [1] UseAuxiliaryAcl (both mandatory).
func decodeConfigureAuxiliaryACLRequest(dec *tlv.Decoder) (mattercore.ConfigureAuxiliaryACLRequest, error) {
	const cmd = "ConfigureAuxiliaryAcl"
	var req mattercore.ConfigureAuxiliaryACLRequest
	err := walkCommandFields(dec, cmd, map[uint64]fieldDecoder{
		0: uint16Field(dec, cmd, "GroupId", &req.GroupID),
		1: boolField(dec, cmd, "UseAuxiliaryAcl", &req.UseAuxiliaryACL),
	}, 0, 1)
	return req, err
}

// decodeGroupcastTestingRequest reads GroupcastTesting: [0] TestOperation
// (mandatory), [1] DurationSeconds (optional).
func decodeGroupcastTestingRequest(dec *tlv.Decoder) (mattercore.GroupcastTestingRequest, error) {
	const cmd = "GroupcastTesting"
	var (
		req      mattercore.GroupcastTestingRequest
		duration uint16
		has      bool
	)
	durDec := uint16Field(dec, cmd, "DurationSeconds", &duration)
	err := walkCommandFields(dec, cmd, map[uint64]fieldDecoder{
		0: enum8Field(dec, cmd, "TestOperation", &req.TestOperation),
		1: func(el tlv.Element) error { has = true; return durDec(el) },
	}, 0)
	if has {
		req.DurationSeconds = &duration
	}
	return req, err
}

// encodeGroupcastResponse writes LeaveGroupResponse (core§11.27.7.3):
// [0] GroupId, [1] Endpoints. Returns false for any other value.
func encodeGroupcastResponse(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	x, ok := v.(mattercore.LeaveGroupResponse)
	if !ok {
		return false
	}
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(x.GroupID))
	enc.StartArray(tlv.ContextTag(1))
	for _, ep := range x.Endpoints {
		enc.PutUint(tlv.AnonymousTag(), uint64(ep))
	}
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	return true
}
