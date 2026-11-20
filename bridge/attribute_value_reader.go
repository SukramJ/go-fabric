// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"fmt"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	acldef "github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	gkmdef "github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// attributeValueReader is the [im.AttributeValueReader] the bridge
// plugs into [im.UnmarshalWriteRequestTLV]. It dispatches on
// (path.Cluster, path.Attribute) and converts the TLV element tree
// into the cluster-native Go value the cluster server expects in
// [contract.ClusterServer.MatterWrite].
//
// Apple Home's post-CommissioningComplete flow writes
// `AccessControl.ACL` (cluster 0x001F, attribute 0x0000) to install
// HomePod / AppleTV edge controllers as additional Administer subjects.
// Without this decoder the IM layer cannot expose the value to the
// AccessControl cluster server, the WriteResponse never lands, Apple
// times out after 10 s and tears the fabric down via RemoveFabric.
//
// Every writable list/struct attribute needs an explicit case here: the
// generic primitiveAttributeValue branch drains a container and yields a
// nil Value, so a structured write that falls through reaches the cluster
// server's MatterWrite as nil, fails its type assertion, and answers
// FAILURE. matter.js does not hand-maintain a switch — its write path
// decodes each value with the attribute's own TlvSchema
// (packages/protocol/src/action/server/AttributeWriteResponse.ts
// #decodeWithSchema), so every list/struct attribute is decodable by
// construction. Each case below mirrors the matching read-direction
// encoder in reply.go so the write path is symmetric with the read path.
//
// Add a switch case here when wiring a new writable cluster attribute
// that carries a structured value (primitives are handled by the
// generic primitiveAttributeValue branch).
func attributeValueReader(path im.ConcreteAttributePath, el tlv.Element, dec *tlv.Decoder) (im.AttributeValue, error) {
	if path.ListAppend {
		return listItemValue(path, el, dec)
	}
	if read := coreListWrite(path, el); read != nil {
		return read(el, dec, false)
	}
	switch {
	case path.Cluster == userlabeldef.ClusterID && path.Attribute == userlabeldef.AttrLabelList && el.IsContainer:
		return generatedAttributeValue(userlabeldef.Definition, path.Attribute, el, dec, false)
	case path.Cluster == bindingdef.ClusterID && path.Attribute == bindingdef.AttrBinding && el.IsContainer:
		// Binding (0x001E) is a list of TargetStruct; the generated
		// definition decodes it (brief AO, question 1).
		return generatedAttributeValue(bindingdef.Definition, path.Attribute, el, dec, false)
	case path.Cluster == otadef.ClusterID && path.Attribute == otadef.AttrDefaultOtaProviders && el.IsContainer:
		v, err := mattercore.DecodeDefaultOTAProvidersWrite(dec, el, false)
		return im.AttributeValue{Value: v}, err
	case path.Cluster == thermostatdef.ClusterID && path.Attribute == thermostatdef.AttrPresets && el.IsContainer:
		// Thermostat Presets (0x0201:0x0050) is a list of PresetStruct the
		// generated definition decodes (brief BL), as matter.js decodes it
		// with the attribute's TlvSchema.
		return generatedAttributeValue(thermostatdef.Definition, path.Attribute, el, dec, false)
	case path.Cluster == thermostatdef.ClusterID && path.Attribute == thermostatdef.AttrSchedules && el.IsContainer:
		// Thermostat Schedules (0x0201:0x0051), a list of ScheduleStruct
		// (MatterScheduleConfiguration, brief CO), decodes the same way.
		return generatedAttributeValue(thermostatdef.Definition, path.Attribute, el, dec, false)
	}
	// Any other structure or list of a cluster the generated definitions
	// know decodes with the attribute's type, as matter.js decodes every
	// write with its TlvSchema; the primitive reader below skips a
	// container, so such a write reached the server as null
	// (CameraAvStreamManagement's ViewportStruct, TC-AVSM-2.12 step 18).
	// Only an attribute with a generated decoder — a writable one; a
	// read-only list such as OperationalCredentials.TrustedRootCertificates
	// has none, and its write drains below and is answered UNSUPPORTED_WRITE
	// by the dispatcher (TC-ACE-2.2 writes every attribute).
	if el.IsContainer {
		if def := spec.Lookup(path.Cluster); def != nil {
			if attr := def.Attribute(path.Attribute); attr != nil && attr.Decode != nil {
				return generatedAttributeValue(def, path.Attribute, el, dec, false)
			}
		}
	}
	if v, over := overlongBeforeIS1(path, el); over {
		return v, nil
	}
	if wrong := scalarOfAnotherType(path, el); wrong != nil {
		// A value, not a decode error: the interaction ends where it
		// would be written (im.UndecodableValue).
		return im.AttributeValue{Value: im.UndecodableValue{Err: wrong}}, nil //nolint:nilerr // the refusal travels as the value
	}
	return primitiveAttributeValue(el, dec)
}

// scalarOfAnotherType is the refusal of a scalar written with another TLV
// type than its attribute's — a string, a boolean, a float or a signed
// integer to an unsigned attribute, and the reverse — by the attribute's
// definition and the strict decoders (class [spec.ErrWrongType]); nil for
// a value of the right type (its range is the definition's check on the
// path), a null to a nullable attribute ([nullToNonNullableScalar]), a container (decoded above or
// refused by [primitiveAttributeValue]) and an attribute no registered
// definition types. matter.js decodes a written value strictly — the
// integer relaxation is the client's alone — and throws for another type,
// which ends the write interaction (owner decision 161; TlvNumber.ts:124-131,
// TlvString.ts:58, TlvBoolean.ts:23 at db021da0, fable-read-6a-gate re-read §2).
func scalarOfAnotherType(path im.ConcreteAttributePath, el tlv.Element) error {
	if el.IsContainer {
		return nil
	}
	def := spec.Lookup(path.Cluster)
	if def == nil {
		return nil
	}
	a := def.Attribute(path.Attribute)
	if a == nil {
		return nil
	}
	if el.IsNull {
		return nullToNonNullableScalar(def, a)
	}
	n := spec.Node{El: el}
	var err error
	switch a.Type.Kind {
	case spec.KindBool:
		_, err = spec.DecodeBool(n)
	case spec.KindUint, spec.KindEnum, spec.KindBitmap:
		_, err = spec.DecodeUint[uint64](64)(n)
	case spec.KindInt:
		_, err = spec.DecodeInt[int64](64)(n)
	case spec.KindFloat32, spec.KindFloat64:
		_, err = spec.DecodeFloat64(n)
	case spec.KindString:
		_, err = spec.DecodeString(n)
	case spec.KindBytes:
		_, err = spec.DecodeBytes(n)
	default:
		return nil
	}
	if errors.Is(err, spec.ErrWrongType) {
		return fmt.Errorf("%s.%s: %w", def.Name, a.Name, err)
	}
	return nil
}

// nullToNonNullableScalar is the refusal of a null written to a scalar
// attribute that is not nullable (owner decision 170, R4-1): matter.js
// decodes the write with the attribute's TLV schema, which is nullable
// only when the attribute is (packages/types/src/tlv/TlvOfModel.ts:234-236,
// :242-244 at db021da0), and its number, boolean and string codecs throw
// on another TLV type (TlvNumber.ts:124-131, TlvBoolean.ts:22-23,
// TlvString.ts:57-58) before any validation — a throw that is no status
// ends the write interaction with FAILURE
// (packages/protocol/src/action/server/AttributeWriteResponse.ts:429,
// :452-460; fable-6a-w3 H3). It was answered CONSTRAINT_ERROR on its
// path. A null to a nullable attribute, and to a list or a struct, is the
// definition's check on the path.
func nullToNonNullableScalar(def *spec.Cluster, a *spec.Attribute) error {
	if a.Quality.Nullable {
		return nil
	}
	switch a.Type.Kind {
	case spec.KindBool, spec.KindUint, spec.KindEnum, spec.KindBitmap, spec.KindInt,
		spec.KindFloat32, spec.KindFloat64, spec.KindString, spec.KindBytes:
		return fmt.Errorf("%s.%s: null to an attribute that is not nullable: %w", def.Name, a.Name, spec.ErrWrongType)
	case spec.KindAny, spec.KindStruct, spec.KindList:
		// A list or a struct: the definition's check on the path.
	}
	return nil
}

// overlongBeforeIS1 refuses a character string the decoder cut at its
// first IS1 when the string as it arrived exceeds the attribute's maximum
// length: matter.js decodes a write with the attribute's TlvString, which
// checks `value.length > maxLength` before it slices at the IS1
// (packages/types/src/tlv/TlvString.ts:57-69 at db021da0) and answers the
// ValidationOutOfBoundsError CONSTRAINT_ERROR. The value is an
// [undecodedValue], which the server's write check answers
// CONSTRAINT_ERROR. Attributes of a cluster no registered definition
// describes, and strings that fit, pass through truncated.
func overlongBeforeIS1(path im.ConcreteAttributePath, el tlv.Element) (im.AttributeValue, bool) {
	if el.RawLength == 0 || el.Type < tlv.TypeUTF8Str1 || el.Type > tlv.TypeUTF8Str8 {
		return im.AttributeValue{}, false
	}
	def := spec.Lookup(path.Cluster)
	if def == nil {
		return im.AttributeValue{}, false
	}
	a := def.Attribute(path.Attribute)
	if a == nil || !a.Constraint.Max.IsInt() || int64(el.RawLength) <= a.Constraint.Max.Int {
		return im.AttributeValue{}, false
	}
	return im.AttributeValue{Value: undecodedValue{fmt.Errorf("bridge: %s: %s is %d long before its IS1, max %d",
		def.Name, a.Name, el.RawLength, a.Constraint.Max.Int)}}, true
}

// undecodedValue stands in for a write value of the attribute's type its
// generated decoder refused for another reason than the type — a label
// longer than its "max 16", a struct missing a mandatory field. It is
// neither a list nor a struct with the attribute's fields, so the
// definition's check answers it CONSTRAINT_ERROR on its path
// (spec.CheckWritten, spec.Instance.ValidateWrite), and the rest of the
// write request goes on. A value of another type than the attribute's is
// an [im.UndecodableValue] instead ([refusedValue], owner decision 161):
// matter.js AttributeWriteResponse.ts:429, :452-459 and
// InteractionMessenger.ts:425-459: a value the attribute's schema cannot
// decode (TlvNumber.ts:124-131, TlvObject.ts:205-207) is not a per-path
// status; the write interaction is answered StatusResponse FAILURE.
type undecodedValue struct{ err error }

// refusedValue is the write value of a decode the definition refused. An
// element of another type than the attribute's — a list element that is
// not a structure, a string where a number belongs: the decoder's refusals
// of class [spec.ErrWrongType] — is an [im.UndecodableValue], which ends the write
// interaction with FAILURE (owner decision 161, R2-P6-wire-1703). Every
// other refusal — a value outside its constraint, a struct missing a
// mandatory field (node:TestUserLabelServerParity pins CONSTRAINT_ERROR
// for it), a string that is not UTF-8 — is an [undecodedValue], answered
// CONSTRAINT_ERROR on its path.
func refusedValue(def *spec.Cluster, err error) any {
	if errors.Is(err, spec.ErrWrongType) {
		return im.UndecodableValue{Err: fmt.Errorf("%s: %w", def.Name, err)}
	}
	return undecodedValue{err}
}

// generatedAttributeValue decodes a structured write value with the
// attribute's generated decoder (cluster/spec/<cluster>), as matter.js
// decodes every write with the attribute's own schema. el is the value's
// container opener, already consumed. For a list-append write (item) el
// opens one entry, which is decoded as a one-element list for the
// dispatcher to append.
func generatedAttributeValue(def *spec.Cluster, attrID uint32, el tlv.Element, dec *tlv.Decoder, item bool) (im.AttributeValue, error) {
	n, err := spec.ReadContainer(dec, el)
	if errors.Is(err, spec.ErrArrayElementTag) {
		// A value its schema does not decode, read through to its end:
		// undecodable, the write interaction ends with FAILURE (owner
		// decision 161), as matter.js's TlvArray throws for an array
		// element with a tag (packages/types/src/tlv/TlvArray.ts:78 at
		// b25ed960, the class of G3).
		return im.AttributeValue{Value: im.UndecodableValue{Err: fmt.Errorf("%s: %w", def.Name, err)}}, nil
	}
	if err != nil {
		return im.AttributeValue{}, fmt.Errorf("%s: %w", def.Name, err)
	}
	if item {
		n = spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}, Children: []spec.Node{n}}
	}
	attr := def.Attribute(attrID)
	if attr == nil || attr.Decode == nil {
		// Never a panic on wire input: the caller checks, this guards it.
		return im.AttributeValue{}, fmt.Errorf("bridge: %s attribute 0x%04X has no generated decoder", def.Name, attrID)
	}
	if attr.Access.Fabric == "F" {
		stampFabricPlaceholder(&n)
	}
	v, derr := attr.Decode(n)
	if derr != nil {
		v = refusedValue(def, derr)
	}
	return im.AttributeValue{Value: v}, nil
}

// The AccessControl.ACL, .Extension and GroupKeyManagement.GroupKeyMap
// writes decode through their generated definitions, as Binding's does,
// and convert to the types the cluster/core servers take: an integer
// wider than its field, a field of another type and a list element that
// is not a struct are refused whole (an [undecodedValue]) — never cut to
// width, read as 0 or dropped (R2-P6-acl-0441, R2-P6-wire-1703). The
// FabricIndex is the placeholder [generatedAttributeValue] stamps; the
// dispatcher assigns the accessing fabric.

// coreListWrite is the decoder of a container written to one of the three
// lists above, nil for any other path or value.
func coreListWrite(path im.ConcreteAttributePath, el tlv.Element) func(tlv.Element, *tlv.Decoder, bool) (im.AttributeValue, error) {
	if !el.IsContainer {
		return nil
	}
	switch {
	case path.Cluster == acldef.ClusterID && path.Attribute == acldef.AttrAcl:
		return aclWriteValue
	case path.Cluster == acldef.ClusterID && path.Attribute == acldef.AttrExtension:
		return extensionWriteValue
	case path.Cluster == gkmdef.ClusterID && path.Attribute == gkmdef.AttrGroupKeyMap:
		return groupKeyMapWriteValue
	}
	return nil
}

// aclWriteValue decodes an ACL write: the whole list, or for a list-append
// write (item) one entry.
func aclWriteValue(el tlv.Element, dec *tlv.Decoder, item bool) (im.AttributeValue, error) {
	v, err := generatedAttributeValue(acldef.Definition, acldef.AttrAcl, el, dec, item)
	if l, ok := v.Value.(spec.List[acldef.AccessControlEntryStruct]); ok && err == nil {
		out := make([]mattercore.AccessControlEntryStruct, 0, len(l))
		for _, e := range l {
			out = append(out, aclEntryFromDef(e))
		}
		v.Value = out
	}
	return v, err
}

// aclEntryFromDef is the cluster/core form of a decoded ACL entry. An
// empty subject or target list is nil, as a null one: both match every
// subject or target.
func aclEntryFromDef(e acldef.AccessControlEntryStruct) mattercore.AccessControlEntryStruct {
	out := mattercore.AccessControlEntryStruct{
		Privilege:   uint8(e.Privilege),
		AuthMode:    uint8(e.AuthMode),
		FabricIndex: e.FabricIndex,
	}
	if !e.Subjects.Null && len(e.Subjects.Value) > 0 {
		out.Subjects = append([]uint64(nil), e.Subjects.Value...)
	}
	if !e.Targets.Null && len(e.Targets.Value) > 0 {
		out.Targets = make([]mattercore.ACLTargetStruct, 0, len(e.Targets.Value))
		for _, t := range e.Targets.Value {
			var ct mattercore.ACLTargetStruct
			if !t.Cluster.Null {
				ct.Cluster = &t.Cluster.Value
			}
			if !t.Endpoint.Null {
				ct.Endpoint = &t.Endpoint.Value
			}
			if !t.DeviceType.Null {
				ct.DeviceType = &t.DeviceType.Value
			}
			out.Targets = append(out.Targets, ct)
		}
	}
	if e.AuxiliaryType != nil {
		a := uint8(*e.AuxiliaryType)
		out.AuxiliaryType = &a
	}
	return out
}

// extensionWriteValue decodes an AccessControl.Extension write.
func extensionWriteValue(el tlv.Element, dec *tlv.Decoder, item bool) (im.AttributeValue, error) {
	v, err := generatedAttributeValue(acldef.Definition, acldef.AttrExtension, el, dec, item)
	if l, ok := v.Value.(spec.List[acldef.AccessControlExtensionStruct]); ok && err == nil {
		out := make([]mattercore.AccessControlExtensionEntry, 0, len(l))
		for _, e := range l {
			out = append(out, mattercore.AccessControlExtensionEntry{Data: append([]byte(nil), e.Data...), FabricIndex: e.FabricIndex})
		}
		v.Value = out
	}
	return v, err
}

// groupKeyMapWriteValue decodes a GroupKeyManagement.GroupKeyMap write.
func groupKeyMapWriteValue(el tlv.Element, dec *tlv.Decoder, item bool) (im.AttributeValue, error) {
	v, err := generatedAttributeValue(gkmdef.Definition, gkmdef.AttrGroupKeyMap, el, dec, item)
	if l, ok := v.Value.(spec.List[gkmdef.GroupKeyMapStruct]); ok && err == nil {
		out := make([]mattercore.GroupKeyMapStruct, 0, len(l))
		for _, e := range l {
			out = append(out, mattercore.GroupKeyMapStruct{GroupID: e.GroupId, GroupKeySetID: e.GroupKeySetId, FabricIndex: e.FabricIndex})
		}
		v.Value = out
	}
	return v, err
}

// primitiveAttributeValue extracts a primitive Go value from a
// non-container TLV element. A container is drained and answered with an
// [undecodedValue]: without a cluster-aware decoder the bridge cannot
// surface its structure, and a value that is neither null nor anything
// was a written null to the dispatcher — a structure written to a
// nullable scalar cleared it with Success (R2-P6-wire-0442). The write
// interaction ends there with FAILURE ([im.UndecodableValue], owner
// decision 161).
func primitiveAttributeValue(el tlv.Element, dec *tlv.Decoder) (im.AttributeValue, error) {
	if el.IsNull {
		return im.AttributeValue{IsNull: true}, nil
	}
	if el.IsContainer {
		if err := skipContainerTLV(dec); err != nil {
			return im.AttributeValue{}, err
		}
		return im.AttributeValue{Value: im.UndecodableValue{Err: fmt.Errorf("bridge: a %s written where the attribute has no structured decoder", containerKind(el))}}, nil
	}
	switch el.Type {
	case tlv.TypeBoolTrue:
		return im.AttributeValue{Value: true}, nil
	case tlv.TypeBoolFalse:
		return im.AttributeValue{Value: false}, nil
	case tlv.TypeUTF8Str1, tlv.TypeUTF8Str2, tlv.TypeUTF8Str4, tlv.TypeUTF8Str8:
		// UTF-8 strings are decoded into el.String (octet strings use
		// el.Octets); reading el.Octets here dropped every UTF-8 attribute
		// write — e.g. a NodeLabel — to the empty string.
		return im.AttributeValue{Value: el.String}, nil
	case tlv.TypeOctetStr1, tlv.TypeOctetStr2, tlv.TypeOctetStr4, tlv.TypeOctetStr8:
		return im.AttributeValue{Value: append([]byte(nil), el.Octets...)}, nil
	case tlv.TypeSignedInt1, tlv.TypeSignedInt2, tlv.TypeSignedInt4, tlv.TypeSignedInt8:
		// Signed integers carry their sign-extended value in el.Int.
		// Returning el.Uint (the old fall-through) dropped every signed
		// attribute write — e.g. a Thermostat setpoint — to 0, because the
		// decoder only fills el.Int for signed types. The cluster server
		// narrows int64 to its native width.
		return im.AttributeValue{Value: el.Int}, nil
	case tlv.TypeFloat4, tlv.TypeFloat8:
		return im.AttributeValue{Value: el.Float}, nil
	default:
		// Unsigned integers / enums surface as uint64; the cluster server
		// narrows to its native width (e.g. enum8 SystemMode → uint8).
		return im.AttributeValue{Value: el.Uint}, nil
	}
}

// skipContainerTLV mirrors im.skipContainer for the bridge package.
// Caller has just consumed a container opener; this drains until the
// matching EndContainer.
func skipContainerTLV(dec *tlv.Decoder) error {
	depth := 1
	for depth > 0 {
		el, err := dec.Next()
		if err != nil {
			return err
		}
		if el.IsContainer {
			depth++
		}
		if el.IsEndContainer {
			depth--
		}
	}
	return nil
}

// listItemValue decodes the value of a list-append write (null ListIndex):
// one element of the list attribute, returned as a one-element list for the
// dispatcher to append (endpoint.TopologyDispatcher.Write). A controller
// writes a list too large for one message as a replace followed by such
// appends (Matter §10.6.4.3.1); chip's TestOnlyWriteAttributeWithLegacyList
// and matter.js's AttributeWriteResponse both produce and accept them.
func listItemValue(path im.ConcreteAttributePath, el tlv.Element, dec *tlv.Decoder) (im.AttributeValue, error) {
	if read := coreListWrite(path, el); read != nil {
		return read(el, dec, true)
	}
	switch {
	case path.Cluster == userlabeldef.ClusterID && path.Attribute == userlabeldef.AttrLabelList && el.IsContainer:
		return generatedAttributeValue(userlabeldef.Definition, path.Attribute, el, dec, true)
	case path.Cluster == bindingdef.ClusterID && path.Attribute == bindingdef.AttrBinding && el.IsContainer:
		// The appended entry in the element type of the Binding server's
		// own list (mattercore.TargetStruct, Binding.MatterReadFiltered):
		// the dispatcher appends it to that list (appendListValue), as
		// matter.js applies a ListIndex-null write to the current value
		// (AttributeWriteResponse.ts). The generated decoder judged it.
		v, err := generatedAttributeValue(bindingdef.Definition, path.Attribute, el, dec, true)
		if l, ok := v.Value.(spec.List[bindingdef.TargetStruct]); ok && err == nil {
			out := make([]mattercore.TargetStruct, 0, len(l))
			for _, t := range l {
				out = append(out, mattercore.TargetFromDef(t))
			}
			v.Value = out
		}
		return v, err
	case path.Cluster == otadef.ClusterID && path.Attribute == otadef.AttrDefaultOtaProviders && el.IsContainer:
		v, err := mattercore.DecodeDefaultOTAProvidersWrite(dec, el, true)
		return im.AttributeValue{Value: v}, err
	case path.Cluster == thermostatdef.ClusterID && path.Attribute == thermostatdef.AttrPresets && el.IsContainer:
		return generatedAttributeValue(thermostatdef.Definition, path.Attribute, el, dec, true)
	case path.Cluster == thermostatdef.ClusterID && path.Attribute == thermostatdef.AttrSchedules && el.IsContainer:
		return generatedAttributeValue(thermostatdef.Definition, path.Attribute, el, dec, true)
	}
	if v, ok := definedListItem(path, el, dec); ok {
		return v, nil
	}
	v, err := primitiveAttributeValue(el, dec)
	if err != nil {
		return v, err
	}
	if im.IsUndecodable(v) {
		return v, nil
	}
	return im.AttributeValue{Value: []any{v.Value}}, nil
}

// definedListItem decodes an appended entry of a list a registered
// definition decodes ([spec.Attribute.Decode]): the entry as a one-entry
// list through the attribute's own decoder, so it comes out in the type
// the definition decodes the whole list into — the type a REPLACE of the
// list stored, which the dispatcher's append requires — and an entry the
// list's entry type refuses is refused. ok is false for an attribute no
// registered definition decodes as a list, and for a null entry, which
// keep the primitive reader. A structured entry goes through
// [generatedAttributeValue], as the replace of such a list does.
//
// Mirrors matter.js packages/protocol/src/action/server/AttributeWriteResponse.ts:446-460
// at b25ed960 (writeValue: an ADD is decoded with the list schema's
// elementSchema, :455, and pushed onto the list, :460).
func definedListItem(path im.ConcreteAttributePath, el tlv.Element, dec *tlv.Decoder) (im.AttributeValue, bool) {
	if el.IsNull {
		return im.AttributeValue{}, false
	}
	def := spec.Lookup(path.Cluster)
	if def == nil {
		return im.AttributeValue{}, false
	}
	attr := def.Attribute(path.Attribute)
	if attr == nil || attr.Decode == nil || attr.Type.Kind != spec.KindList {
		return im.AttributeValue{}, false
	}
	if el.IsContainer {
		v, err := generatedAttributeValue(def, path.Attribute, el, dec, true)
		if err != nil {
			return im.AttributeValue{Value: im.UndecodableValue{Err: err}}, true
		}
		return v, true
	}
	n := spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}, Children: []spec.Node{{El: el}}}
	v, err := attr.Decode(n)
	if err != nil {
		return im.AttributeValue{Value: refusedValue(def, err)}, true
	}
	return im.AttributeValue{Value: v}, true
}

// stampFabricPlaceholder sets the FabricIndex field (context tag 254) of
// every struct in a fabric-scoped list write to 1 before the generated
// decoder judges it. A writer's fabric index is noise the server replaces
// with the accessing fabric — matter.js AttributeWriteResponse.ts:461-467
// (the accessing fabric is assigned before validation) — but the
// generated decoder requires 1..254, so a client that encodes 0 (the
// generated encoders do) would be refused at the codec.
func stampFabricPlaceholder(n *spec.Node) {
	fabricTag := tlv.ContextTag(254)
	placeholder := spec.Node{El: tlv.Element{Tag: fabricTag, Type: tlv.TypeUnsignedInt1, Uint: 1}}
	for i := range n.Children {
		entry := &n.Children[i]
		if !entry.El.IsContainer {
			continue
		}
		found := false
		for j := range entry.Children {
			if entry.Children[j].El.Tag == fabricTag {
				entry.Children[j] = placeholder
				found = true
			}
		}
		if !found {
			entry.Children = append(entry.Children, placeholder)
		}
	}
}

// containerKind names el's container type for an error text.
func containerKind(el tlv.Element) string {
	switch el.Type {
	case tlv.TypeStructure:
		return "structure"
	case tlv.TypeArray:
		return "array"
	default:
		return "list"
	}
}
