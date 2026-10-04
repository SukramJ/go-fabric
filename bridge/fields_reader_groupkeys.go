// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"fmt"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// fieldInvalidCommandError is the typed reject for a command payload that
// does not match the command's schema: a mandatory field is missing, or a
// field carries the wrong TLV type. matter.js decodes every request with
// the command's TlvSchema before the handler runs and answers a payload
// that does not match with InvalidCommand — a decode failure directly
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:
// #decodeWithSchema → StatusResponse.InvalidCommandError), a failed
// requestTlv.validate (ValidationMandatoryFieldMissingError /
// ValidationDatatypeMismatchError carry InvalidAction, which
// #invokeCommand rewrites to InvalidCommand for a ValidationError).
//
// consumed marks a reject found at the fields container's EndContainer
// (a missing mandatory field): the reader has read past it already, so
// the IM layer must not drain the container again.
type fieldInvalidCommandError struct {
	msg      string
	consumed bool
}

func (e fieldInvalidCommandError) Error() string { return e.msg }

// MatterStatusCode implements [im.StatusCodeError].
func (fieldInvalidCommandError) MatterStatusCode() im.StatusCode { return im.StatusInvalidCommand }

// FieldsContainerConsumed implements [im.FieldsContainerConsumed].
func (e fieldInvalidCommandError) FieldsContainerConsumed() bool { return e.consumed }

// isUnsignedInt reports whether el carries an unsigned integer.
func isUnsignedInt(el tlv.Element) bool {
	return el.Type >= tlv.TypeUnsignedInt1 && el.Type <= tlv.TypeUnsignedInt8
}

// isOctetString reports whether el carries an octet string.
func isOctetString(el tlv.Element) bool {
	return el.Type >= tlv.TypeOctetStr1 && el.Type <= tlv.TypeOctetStr8
}

// skipValue drains el when it opened a container, so a decoder that does
// not consume a field leaves the stream positioned at the next sibling.
func skipValue(dec *tlv.Decoder, el tlv.Element) error {
	if el.IsContainer {
		return drainContainer(dec)
	}
	return nil
}

// decodeUint16Field reads a mandatory uint16 command field, rejecting a
// non-integer as InvalidCommand and a wider value as ConstraintError.
func decodeUint16Field(cmd, field string, el tlv.Element) (uint16, error) {
	if !isUnsignedInt(el) {
		return 0, fieldInvalidCommandError{msg: fmt.Sprintf("%s: %s is not an unsigned integer (type=0x%02X)", cmd, field, el.Type)}
	}
	return fieldUint16(cmd, field, el)
}

// decodeSingleUint16Request reads a command whose only field is a
// mandatory uint16 at context tag 0 — KeySetRead and KeySetRemove
// (GroupKeySetID), Groups ViewGroup and RemoveGroup (GroupID).
func decodeSingleUint16Request(dec *tlv.Decoder, cmd, field string) (uint16, error) {
	var (
		v       uint16
		present bool
	)
	for {
		el, err := dec.Next()
		if err != nil {
			return 0, fmt.Errorf("%s: %w", cmd, err)
		}
		if el.IsEndContainer {
			if !present {
				return 0, fieldInvalidCommandError{msg: fmt.Sprintf("%s: mandatory field %s missing", cmd, field), consumed: true}
			}
			return v, nil
		}
		if el.Tag.Kind != tlv.TagKindContext || el.Tag.Number != 0 {
			if err := skipValue(dec, el); err != nil {
				return 0, fmt.Errorf("%s: %w", cmd, err)
			}
			continue
		}
		if v, err = decodeUint16Field(cmd, field, el); err != nil {
			if el.IsContainer {
				_ = drainContainer(dec)
			}
			return 0, err
		}
		present = true
	}
}

// decodeKeySetReadRequest reads GroupKeyManagement KeySetRead (Matter
// §11.2.7.2): [0] uint16 GroupKeySetID.
func decodeKeySetReadRequest(dec *tlv.Decoder) (mattercore.KeySetReadRequest, error) {
	id, err := decodeSingleUint16Request(dec, "KeySetRead", "GroupKeySetID")
	return mattercore.KeySetReadRequest{GroupKeySetID: id}, err
}

// decodeKeySetRemoveRequest reads GroupKeyManagement KeySetRemove (Matter
// §11.2.7.4): [0] uint16 GroupKeySetID.
func decodeKeySetRemoveRequest(dec *tlv.Decoder) (mattercore.KeySetRemoveRequest, error) {
	id, err := decodeSingleUint16Request(dec, "KeySetRemove", "GroupKeySetID")
	return mattercore.KeySetRemoveRequest{GroupKeySetID: id}, err
}

// decodeKeySetReadAllIndicesRequest drains KeySetReadAllIndices (Matter
// §11.2.7.5). Its one field, DoNotUse, has conformance X: there is
// nothing to read, and a stray field is ignored the way matter.js's
// schema decode ignores an unknown tag.
func decodeKeySetReadAllIndicesRequest(dec *tlv.Decoder) (any, error) {
	if err := drainContainer(dec); err != nil {
		return nil, fmt.Errorf("KeySetReadAllIndices: %w", err)
	}
	return nil, nil
}

// decodeKeySetWriteRequest reads GroupKeyManagement KeySetWrite (Matter
// §11.2.7.1): [0] GroupKeySetStruct GroupKeySet.
func decodeKeySetWriteRequest(dec *tlv.Decoder) (mattercore.KeySetWriteRequest, error) {
	var (
		req     mattercore.KeySetWriteRequest
		present bool
	)
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("KeySetWrite: %w", err)
		}
		if el.IsEndContainer {
			if !present {
				return req, fieldInvalidCommandError{msg: "KeySetWrite: mandatory field GroupKeySet missing", consumed: true}
			}
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext || el.Tag.Number != 0 {
			if err := skipValue(dec, el); err != nil {
				return req, fmt.Errorf("KeySetWrite: %w", err)
			}
			continue
		}
		if !el.IsContainer || el.Type != tlv.TypeStructure {
			_ = skipValue(dec, el)
			return req, fieldInvalidCommandError{msg: fmt.Sprintf("KeySetWrite: GroupKeySet is not a struct (type=0x%02X)", el.Type)}
		}
		gks, err := decodeGroupKeySetStruct(dec)
		if err != nil {
			return req, err
		}
		req.GroupKeySet = gks
		present = true
	}
}

// groupKeySetMandatory is the presence mask of GroupKeySetStruct fields
// 0-7, every one of them conformance M (fields 2-7 nullable, so a null is
// present). Field 8, GroupKeyMulticastPolicy, is O.
const groupKeySetMandatory uint16 = 0xFF

// decodeGroupKeySetStruct reads one GroupKeySetStruct (Matter §11.2.5.3,
// group-key-management.element.ts GroupKeySetStruct) from inside its
// container, consuming the closing EndContainer:
//
//	[0] uint16 GroupKeySetID
//	[1] enum8  GroupKeySecurityPolicy
//	[2] octstr EpochKey0        (nullable)
//	[3] epoch-us EpochStartTime0 (nullable)
//	[4] octstr EpochKey1        (nullable)
//	[5] epoch-us EpochStartTime1 (nullable)
//	[6] octstr EpochKey2        (nullable)
//	[7] epoch-us EpochStartTime2 (nullable)
//	[8] enum8  GroupKeyMulticastPolicy (optional)
//
// A null start time decodes to 0 and a null key to nil, the shape
// [mattercore.GroupKeySetStruct] uses for an unused slot. A key of the
// wrong length is not rejected here: its "16" constraint is the server's
// ConstraintError, as in matter.js.
func decodeGroupKeySetStruct(dec *tlv.Decoder) (mattercore.GroupKeySetStruct, error) { //nolint:gocognit,gocyclo // one case per struct field
	const cmd = "KeySetWrite.GroupKeySet"
	var (
		gks  mattercore.GroupKeySetStruct
		seen uint16
		rerr error
	)
	reject := func(msg string) {
		if rerr == nil {
			rerr = fieldInvalidCommandError{msg: cmd + ": " + msg}
		}
	}
	octets := func(el tlv.Element, name string) []byte {
		switch {
		case el.IsNull:
			return nil
		case isOctetString(el):
			return append([]byte(nil), el.Octets...)
		}
		reject(name + " is not an octet string")
		return nil
	}
	epoch := func(el tlv.Element, name string) uint64 {
		switch {
		case el.IsNull:
			return 0
		case isUnsignedInt(el):
			return el.Uint
		}
		reject(name + " is not an unsigned integer")
		return 0
	}
	for {
		el, err := dec.Next()
		if err != nil {
			return gks, fmt.Errorf("%s: %w", cmd, err)
		}
		if el.IsEndContainer {
			break
		}
		if el.IsContainer {
			if err := drainContainer(dec); err != nil {
				return gks, fmt.Errorf("%s: %w", cmd, err)
			}
			if el.Tag.Kind == tlv.TagKindContext && el.Tag.Number <= 8 {
				reject(fmt.Sprintf("field %d is a container", el.Tag.Number))
			}
			continue
		}
		if el.Tag.Kind != tlv.TagKindContext || el.Tag.Number > 8 {
			continue
		}
		seen |= 1 << el.Tag.Number
		switch el.Tag.Number {
		case 0:
			if !isUnsignedInt(el) {
				reject("GroupKeySetID is not an unsigned integer")
				continue
			}
			v, ferr := fieldUint16(cmd, "GroupKeySetID", el)
			if ferr != nil && rerr == nil {
				rerr = ferr
			}
			gks.GroupKeySetID = v
		case 1:
			if !isUnsignedInt(el) {
				reject("GroupKeySecurityPolicy is not an enum")
				continue
			}
			v, ferr := fieldUint8(cmd, "GroupKeySecurityPolicy", el)
			if ferr != nil && rerr == nil {
				rerr = ferr
			}
			gks.GroupKeySecurityPolicy = v
		case 2:
			gks.EpochKey0 = octets(el, "EpochKey0")
		case 3:
			gks.EpochStartTime0 = epoch(el, "EpochStartTime0")
		case 4:
			gks.EpochKey1 = octets(el, "EpochKey1")
		case 5:
			gks.EpochStartTime1 = epoch(el, "EpochStartTime1")
		case 6:
			gks.EpochKey2 = octets(el, "EpochKey2")
		case 7:
			gks.EpochStartTime2 = epoch(el, "EpochStartTime2")
		case 8:
			// GroupKeyMulticastPolicy: decoded for its type only. The
			// server ignores the value (matter.js 452d6f5c).
			if !isUnsignedInt(el) {
				reject("GroupKeyMulticastPolicy is not an enum")
				continue
			}
			gks.GroupKeyMulticastPolicy = uint8(el.Uint & 0xFF)
		}
	}
	if rerr != nil {
		return gks, rerr
	}
	if seen&groupKeySetMandatory != groupKeySetMandatory {
		return gks, fieldInvalidCommandError{msg: fmt.Sprintf("%s: mandatory field missing (present mask 0x%02X)", cmd, seen&groupKeySetMandatory)}
	}
	return gks, nil
}

// encodeGroupKeySetStruct writes a GroupKeySetStruct as KeySetReadResponse
// carries it (Matter §11.2.7.3): the epoch keys are always null — they
// never leave the node — an unused slot's start time is null, and
// GroupKeyMulticastPolicy (field 8) is reported while the model defines
// it. Mirrors matter.js GroupKeyManagementServer.ts:keySetRead and the
// field order of group-key-management.element.ts GroupKeySetStruct.
func encodeGroupKeySetStruct(enc *tlv.Encoder, tag tlv.Tag, gks mattercore.GroupKeySetStruct) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(gks.GroupKeySetID))
	enc.PutUint(tlv.ContextTag(1), uint64(gks.GroupKeySecurityPolicy))
	enc.PutNull(tlv.ContextTag(2))
	enc.PutUint(tlv.ContextTag(3), gks.EpochStartTime0)
	enc.PutNull(tlv.ContextTag(4))
	putNullableEpoch(enc, tlv.ContextTag(5), gks.EpochStartTime1)
	enc.PutNull(tlv.ContextTag(6))
	putNullableEpoch(enc, tlv.ContextTag(7), gks.EpochStartTime2)
	enc.PutUint(tlv.ContextTag(8), uint64(gks.GroupKeyMulticastPolicy))
	_ = enc.EndContainer()
}

// putNullableEpoch writes an epoch-us start time, or null for the zero
// value that stands for an unused slot.
func putNullableEpoch(enc *tlv.Encoder, tag tlv.Tag, v uint64) {
	if v == 0 {
		enc.PutNull(tag)
		return
	}
	enc.PutUint(tag, v)
}
