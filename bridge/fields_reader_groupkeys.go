// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"fmt"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	gkmdef "github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
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

// decodeKeySetReadAllIndicesRequest drains a command without fields:
// KeySetReadAllIndices (Matter §11.2.7.5, whose one field DoNotUse has
// conformance X) and Groups RemoveAllGroups. A stray field is ignored the
// way matter.js's schema decode ignores an unknown tag.
func decodeKeySetReadAllIndicesRequest(dec *tlv.Decoder) error {
	if err := drainContainer(dec); err != nil {
		return fmt.Errorf("fieldless command: %w", err)
	}
	return nil
}

// decodeKeySetWriteByDefinition reads GroupKeyManagement KeySetWrite into
// the generated request, whose nullable start times say whether a start
// time was null or 0: the module's [mattercore.GroupKeySetStruct] reads
// both as 0, and the server refuses a null epoch key with a start time
// that is not null (R2-P6-par-1013, the wire half). dec is positioned
// inside the fields structure [heldByDefinition] already checked against
// this definition.
func decodeKeySetWriteByDefinition(dec *tlv.Decoder) (any, error) {
	n, err := spec.ReadContainer(dec, tlv.Element{Type: tlv.TypeStructure, IsContainer: true})
	if err != nil {
		return nil, fmt.Errorf("KeySetWrite: %w", err)
	}
	cmd := gkmdef.Definition.Command(gkmdef.CmdKeySetWrite, spec.Request)
	if cmd == nil || cmd.Decode == nil {
		return nil, errors.New("KeySetWrite: no generated decoder")
	}
	return cmd.DecodeRequest(n)
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
