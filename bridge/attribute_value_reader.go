// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"fmt"
	"strings"

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
	return primitiveAttributeValue(el, dec)
}

// overlongBeforeIS1 refuses a character string the decoder cut at its
// first IS1 when the string as it arrived exceeds the attribute's maximum
// length: matter.js decodes a write with the attribute's TlvString, which
// checks `value.length > maxLength` before it slices at the IS1
// (packages/types/src/tlv/TlvString.ts:57-69 at 862ad75f) and answers the
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
// not a structure, a string where a number belongs: the decoder's
// INVALID_COMMAND refusals that say "is not a …" (cluster/spec codec.go,
// decode_value.go) — is an [im.UndecodableValue], which ends the write
// interaction with FAILURE (owner decision 161, R2-P6-wire-1703). Every
// other refusal — a value outside its constraint, a struct missing a
// mandatory field (node:TestUserLabelServerParity pins CONSTRAINT_ERROR
// for it), a string that is not UTF-8 — is an [undecodedValue], answered
// CONSTRAINT_ERROR on its path.
func refusedValue(def *spec.Cluster, err error) any {
	if fe, ok := errors.AsType[*spec.FieldError](err); ok && fe.Status == im.StatusInvalidCommand && strings.Contains(fe.Msg, "is not a") {
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
		// the server's write check answers it, as any other such value
		// (matter.js packages/types/src/tlv/TlvArray.ts:77-78 at db021da0
		// throws for an array element with a tag).
		return im.AttributeValue{Value: undecodedValue{fmt.Errorf("%s: %w", def.Name, err)}}, nil
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
	v, err := primitiveAttributeValue(el, dec)
	if err != nil {
		return v, err
	}
	if im.IsUndecodable(v) {
		return v, nil
	}
	return im.AttributeValue{Value: []any{v.Value}}, nil
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
