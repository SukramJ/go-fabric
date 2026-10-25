// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/SukramJ/go-fabric/tlv"
)

// The JSON codec turns a value of a generated definition into a [Node] and
// back, by walking its [Type]. It is the argument and output format of a
// command-line controller, and chip-tool's argument conventions are its
// reference (connectedhomeip at 6170af84,
// examples/chip-tool/commands/clusters/ComplexArgument.h — typed values —
// and CustomArgument.h — untyped values):
//
//   - an unsigned integer, enum or bitmap is a JSON number, or a string in
//     decimal or "0x" hex (ComplexArgument.h:60-90; an enum or bitmap is its
//     underlying integer, :105-133). Beyond chip-tool, an enum also takes
//     the name of one of its values, and a bitmap an object of member names
//     ({"onOff": true, "level": 3});
//   - a signed integer is a JSON number (:92-103); a float or double a JSON
//     number (:269-291); a boolean a JSON boolean (:293-303);
//   - a character string is a JSON string (:251-267);
//   - an octet string is a string: "hex:" followed by hex digits, "str:"
//     followed by the bytes as text, or bare hex digits (:196-249); beyond
//     chip-tool's typed parser, "base64:" followed by standard base64
//     (the prefix of examples/chip-tool/commands/common/CustomStringPrefix.h:28);
//   - a struct is an object by field name (chip-tool's generated Setup
//     functions, with EnsureMemberExist and EnsureNoMembersRemaining,
//     ComplexArgument.h:305-330): a mandatory field must be present, an
//     unknown one is refused, an optional one may be left out. The name
//     matches without regard to case; a decimal field id is accepted too,
//     as CustomArgument.h:134-139 takes it;
//   - a list is an array (ComplexArgument.h:161-194);
//   - null is a nullable value's null (:145-159);
//   - a value of KindAny (no definition) follows CustomArgument.h:47-111:
//     an object is a struct whose keys are context tags, an array an array,
//     a string an octet string with "hex:", a number with "u:", "s:", "f:"
//     or "d:", otherwise a character string; a whole number is unsigned
//     when not negative, signed otherwise; any other number a double.
//
// Decoding writes the same forms: integers as numbers, octet strings as
// "hex:…", structs as objects keyed by the camelCase field name (a field
// the definition does not name under its id), and an untyped value as
// CustomArgument would read it back.

// ErrJSON is returned for a JSON value that does not fit its type.
var ErrJSON = errors.New("spec: JSON value does not fit its type")

func jsonErr(path, format string, args ...any) error {
	if path == "" {
		path = "value"
	}
	return fmt.Errorf("%w: %s: "+format, append([]any{ErrJSON, path}, args...)...)
}

// AttributeField is the [Field] an attribute's value is encoded as.
func AttributeField(a *Attribute) *Field {
	return &Field{ID: a.ID, Name: a.Name, Type: a.Type, Conformance: a.Conformance, Quality: a.Quality, Constraint: a.Constraint}
}

// CommandField is the [Field] of a command's payload: a struct of its
// fields.
func CommandField(c *Command) *Field {
	return &Field{ID: c.ID, Name: c.Name, Type: Type{Name: c.Name, Kind: KindStruct, Struct: &Struct{Name: c.Name, Fields: c.Fields}}}
}

// EventField is the [Field] of an event's payload: a struct of its fields.
func EventField(e *Event) *Field {
	return &Field{ID: e.ID, Name: e.Name, Type: Type{Name: e.Name, Kind: KindStruct, Struct: &Struct{Name: e.Name, Fields: e.Fields}}}
}

// EncodeJSON encodes the JSON value raw as a value of field into a node
// with an anonymous tag. A nil field is an untyped value (KindAny).
func EncodeJSON(field *Field, raw json.RawMessage) (Node, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return Node{}, fmt.Errorf("%w: %w", ErrJSON, err)
	}
	if dec.More() {
		return Node{}, fmt.Errorf("%w: trailing data after the value", ErrJSON)
	}
	if field == nil {
		return encodeAny(tlv.AnonymousTag(), v, "")
	}
	return encodeValue(tlv.AnonymousTag(), field.Type, field.Quality.Nullable, v, "")
}

// DecodeJSON renders node as the JSON value of field. A nil field is an
// untyped value.
func DecodeJSON(field *Field, n Node) (json.RawMessage, error) {
	var b bytes.Buffer
	var err error
	if field == nil {
		err = writeAny(&b, n)
	} else {
		err = writeValue(&b, field.Type, n, "")
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b.Bytes()), nil
}

func bitsOf(t Type) int {
	if t.Bits == 0 {
		return 64
	}
	return t.Bits
}

func nullNode(tag tlv.Tag) Node {
	return Node{El: tlv.Element{Tag: tag, Type: tlv.TypeNull, IsNull: true}}
}

//nolint:gocyclo,cyclop,funlen // one case per Kind
func encodeValue(tag tlv.Tag, t Type, nullable bool, v any, path string) (Node, error) {
	if v == nil {
		if nullable || t.Kind == KindAny {
			return nullNode(tag), nil
		}
		return Node{}, jsonErr(path, "null for a value that is not nullable")
	}
	switch t.Kind {
	case KindBool:
		b, ok := v.(bool)
		if !ok {
			return Node{}, jsonErr(path, "want a boolean, got %v", v)
		}
		typ := tlv.TypeBoolFalse
		if b {
			typ = tlv.TypeBoolTrue
		}
		return Node{El: tlv.Element{Tag: tag, Type: typ, Bool: b}}, nil
	case KindUint, KindEnum, KindBitmap:
		u, err := encodeUnsigned(t, v, path)
		if err != nil {
			return Node{}, err
		}
		return uintNode(tag, u), nil
	case KindInt:
		num, ok := v.(json.Number)
		if !ok {
			return Node{}, jsonErr(path, "want a signed number, got %v", v)
		}
		i, err := strconv.ParseInt(num.String(), 10, bitsOf(t))
		if err != nil {
			return Node{}, jsonErr(path, "%s is not an int%d", num, bitsOf(t))
		}
		return Node{El: tlv.Element{Tag: tag, Type: tlv.TypeSignedInt8, Int: i}}, nil
	case KindFloat32, KindFloat64:
		num, ok := v.(json.Number)
		if !ok {
			return Node{}, jsonErr(path, "want a number, got %v", v)
		}
		bits, typ := 64, tlv.TypeFloat8
		if t.Kind == KindFloat32 {
			bits, typ = 32, tlv.TypeFloat4
		}
		f, err := strconv.ParseFloat(num.String(), bits)
		if err != nil {
			return Node{}, jsonErr(path, "%s is not a float%d", num, bits)
		}
		return Node{El: tlv.Element{Tag: tag, Type: typ, Float: f}}, nil
	case KindString:
		s, ok := v.(string)
		if !ok {
			return Node{}, jsonErr(path, "want a string, got %v", v)
		}
		if !utf8.ValidString(s) {
			return Node{}, jsonErr(path, "not UTF-8")
		}
		return Node{El: tlv.Element{Tag: tag, Type: tlv.TypeUTF8Str1, String: s}}, nil
	case KindBytes:
		s, ok := v.(string)
		if !ok {
			return Node{}, jsonErr(path, "want an octet string (\"hex:…\", \"base64:…\" or \"str:…\"), got %v", v)
		}
		b, err := octets(s, true)
		if err != nil {
			return Node{}, jsonErr(path, "%v", err)
		}
		return Node{El: tlv.Element{Tag: tag, Type: tlv.TypeOctetStr1, Octets: b}}, nil
	case KindList:
		return encodeList(tag, t, v, path)
	case KindStruct:
		m, ok := v.(map[string]any)
		if !ok {
			return Node{}, jsonErr(path, "want an object, got %v", v)
		}
		if t.Struct == nil {
			return encodeAny(tag, v, path)
		}
		return encodeStruct(tag, t.Struct, m, path)
	case KindAny:
	}
	return encodeAny(tag, v, path)
}

// encodeUnsigned is an unsigned integer, an enum (by number or value name)
// or a bitmap (by number or an object of member names).
func encodeUnsigned(t Type, v any, path string) (uint64, error) {
	switch {
	case t.Kind == KindEnum && t.Enum != nil:
		if s, ok := v.(string); ok {
			for i := range t.Enum.Values {
				if ev := &t.Enum.Values[i]; strings.EqualFold(ev.Name, s) {
					return ev.Value, nil
				}
			}
		}
	case t.Kind == KindBitmap && t.Bitmap != nil:
		if m, ok := v.(map[string]any); ok {
			return bitmapFromObject(t.Bitmap, m, path)
		}
	}
	return jsonUint(v, bitsOf(t), path)
}

func encodeList(tag tlv.Tag, t Type, v any, path string) (Node, error) {
	arr, ok := v.([]any)
	if !ok {
		return Node{}, jsonErr(path, "want an array, got %v", v)
	}
	n := Node{El: tlv.Element{Tag: tag, Type: tlv.TypeArray, IsContainer: true}}
	var entry Type
	entryNullable := false
	if t.Entry != nil {
		entry, entryNullable = t.Entry.Type, t.Entry.Quality.Nullable
	}
	for i, e := range arr {
		c, err := encodeValue(tlv.AnonymousTag(), entry, entryNullable, e, fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return Node{}, err
		}
		n.Children = append(n.Children, c)
	}
	return n, nil
}

func uintNode(tag tlv.Tag, u uint64) Node {
	return Node{El: tlv.Element{Tag: tag, Type: tlv.TypeUnsignedInt8, Uint: u}}
}

// jsonUint is ComplexArgument.h:60-90: a number, or a string in decimal or
// 0x hex.
func jsonUint(v any, bits int, path string) (uint64, error) {
	var s string
	switch x := v.(type) {
	case json.Number:
		s = x.String()
	case string:
		s = x
	default:
		return 0, jsonErr(path, "want an unsigned number, got %v", v)
	}
	base := 10
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		s, base = s[2:], 16
	}
	u, err := strconv.ParseUint(s, base, bits)
	if err != nil {
		return 0, jsonErr(path, "%v is not a uint%d", v, bits)
	}
	return u, nil
}

func bitmapFromObject(b *Bitmap, m map[string]any, path string) (uint64, error) {
	var out uint64
	for k, val := range m {
		var mem *BitmapMember
		for i := range b.Members {
			if strings.EqualFold(b.Members[i].Name, k) {
				mem = &b.Members[i]
				break
			}
		}
		if mem == nil {
			return 0, jsonErr(path+"."+k, "not a member of %s", b.Name)
		}
		var u uint64
		switch x := val.(type) {
		case bool:
			if x {
				u = 1
			}
		default:
			var err error
			if u, err = jsonUint(val, int(mem.Width), path+"."+k); err != nil {
				return 0, err
			}
		}
		out |= u << mem.Bit & mem.Mask()
	}
	return out, nil
}

// octets decodes an octet-string argument. typed selects the typed rule
// (ComplexArgument.h:196-249: "str:" text, "hex:" or bare hex) over the
// untyped one (CustomArgument.h:59-82: only "hex:" is octets).
func octets(s string, typed bool) ([]byte, error) {
	switch {
	case strings.HasPrefix(s, "str:"):
		return []byte(s[len("str:"):]), nil
	case strings.HasPrefix(s, "base64:"):
		return base64.StdEncoding.DecodeString(s[len("base64:"):])
	case strings.HasPrefix(s, "hex:"):
		return hex.DecodeString(s[len("hex:"):])
	case typed:
		return hex.DecodeString(s)
	}
	return nil, errors.New("spec: not an octet string")
}

func encodeStruct(tag tlv.Tag, st *Struct, m map[string]any, path string) (Node, error) {
	n := Node{El: tlv.Element{Tag: tag, Type: tlv.TypeStructure, IsContainer: true}}
	used := make(map[string]bool, len(m))
	for i := range st.Fields {
		f := &st.Fields[i]
		key, val, ok := memberOf(m, f)
		if !ok {
			// A fabric-scoped struct's FabricIndex (254) is the server's to
			// set on a write; chip-tool's encoder leaves it out then
			// (CustomArgument.h:292-294 — kIsFabricScoped).
			if f.Mandatory() && f.ID != fabricIndexFieldID {
				return Node{}, jsonErr(path, "%s is required. Should be provided as {%q: value}", f.Name, camel(f.Name))
			}
			continue
		}
		used[key] = true
		c, err := encodeValue(tlv.ContextTag(uint8(f.ID)), f.Type, f.Quality.Nullable, val, joinPath(path, f.Name)) //nolint:gosec // G115: a field id is a context tag, 0..254
		if err != nil {
			return Node{}, err
		}
		n.Children = append(n.Children, c)
	}
	for k := range m {
		if !used[k] {
			return Node{}, jsonErr(path, "Unexpected field name: %q", k)
		}
	}
	return n, nil
}

const fabricIndexFieldID = 0xFE

func joinPath(path, name string) string {
	if path == "" {
		return camel(name)
	}
	return path + "." + camel(name)
}

// memberOf finds f's member of m: by name without regard to case, or by
// decimal id.
func memberOf(m map[string]any, f *Field) (key string, value any, ok bool) {
	id := strconv.FormatUint(uint64(f.ID), 10)
	for k, v := range m {
		if strings.EqualFold(k, f.Name) || k == id {
			return k, v, true
		}
	}
	return "", nil, false
}

// encodeAny is CustomArgumentParser::Put (CustomArgument.h:47-111).
//
//nolint:cyclop // one case per JSON shape
func encodeAny(tag tlv.Tag, v any, path string) (Node, error) {
	switch x := v.(type) {
	case nil:
		return nullNode(tag), nil
	case bool:
		return encodeValue(tag, Type{Kind: KindBool}, false, x, path)
	case map[string]any:
		n := Node{El: tlv.Element{Tag: tag, Type: tlv.TypeStructure, IsContainer: true}}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			id, err := strconv.ParseUint(k, 0, 8)
			if err != nil {
				return Node{}, jsonErr(path, "member %q is not a context tag 0..255", k)
			}
			c, err := encodeAny(tlv.ContextTag(uint8(id)), x[k], path+"."+k)
			if err != nil {
				return Node{}, err
			}
			n.Children = append(n.Children, c)
		}
		return n, nil
	case []any:
		n := Node{El: tlv.Element{Tag: tag, Type: tlv.TypeArray, IsContainer: true}}
		for i, e := range x {
			c, err := encodeAny(tlv.AnonymousTag(), e, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return Node{}, err
			}
			n.Children = append(n.Children, c)
		}
		return n, nil
	case string:
		return encodeAnyString(tag, x, path)
	case json.Number:
		if u, err := strconv.ParseUint(x.String(), 10, 64); err == nil {
			return uintNode(tag, u), nil
		}
		if i, err := strconv.ParseInt(x.String(), 10, 64); err == nil {
			return Node{El: tlv.Element{Tag: tag, Type: tlv.TypeSignedInt8, Int: i}}, nil
		}
		f, err := x.Float64()
		if err != nil {
			return Node{}, jsonErr(path, "%s is not a number", x)
		}
		return Node{El: tlv.Element{Tag: tag, Type: tlv.TypeFloat8, Float: f}}, nil
	}
	return Node{}, jsonErr(path, "unsupported JSON value %v", v)
}

// encodeAnyString is CustomArgument.h:59-82 (the prefixes :32-36).
func encodeAnyString(tag tlv.Tag, s, path string) (Node, error) {
	switch {
	case strings.HasPrefix(s, "hex:"), strings.HasPrefix(s, "base64:"):
		b, err := octets(s, false)
		if err != nil {
			return Node{}, jsonErr(path, "%v", err)
		}
		return Node{El: tlv.Element{Tag: tag, Type: tlv.TypeOctetStr1, Octets: b}}, nil
	case strings.HasPrefix(s, "u:"):
		u, err := strconv.ParseUint(s[2:], 0, 64)
		if err != nil {
			return Node{}, jsonErr(path, "%q is not unsigned", s)
		}
		return uintNode(tag, u), nil
	case strings.HasPrefix(s, "s:"):
		i, err := strconv.ParseInt(s[2:], 0, 64)
		if err != nil {
			return Node{}, jsonErr(path, "%q is not signed", s)
		}
		return Node{El: tlv.Element{Tag: tag, Type: tlv.TypeSignedInt8, Int: i}}, nil
	case strings.HasPrefix(s, "f:"):
		f, err := strconv.ParseFloat(s[2:], 32)
		if err != nil {
			return Node{}, jsonErr(path, "%q is not a float", s)
		}
		return Node{El: tlv.Element{Tag: tag, Type: tlv.TypeFloat4, Float: f}}, nil
	case strings.HasPrefix(s, "d:"):
		f, err := strconv.ParseFloat(s[2:], 64)
		if err != nil {
			return Node{}, jsonErr(path, "%q is not a double", s)
		}
		return Node{El: tlv.Element{Tag: tag, Type: tlv.TypeFloat8, Float: f}}, nil
	}
	return Node{El: tlv.Element{Tag: tag, Type: tlv.TypeUTF8Str1, String: s}}, nil
}

func isNull(n Node) bool { return n.El.IsNull || n.El.Type == tlv.TypeNull }

func isUintType(t tlv.ElementType) bool {
	return t >= tlv.TypeUnsignedInt1 && t <= tlv.TypeUnsignedInt8
}

func isIntType(t tlv.ElementType) bool { return t <= tlv.TypeSignedInt8 }

func isStringType(t tlv.ElementType) bool { return t >= tlv.TypeUTF8Str1 && t <= tlv.TypeUTF8Str8 }

func isBytesType(t tlv.ElementType) bool { return t >= tlv.TypeOctetStr1 && t <= tlv.TypeOctetStr8 }

//nolint:gocyclo,cyclop // one case per Kind
func writeValue(b *bytes.Buffer, t Type, n Node, path string) error {
	if isNull(n) {
		b.WriteString("null")
		return nil
	}
	typ := n.El.Type
	switch t.Kind {
	case KindBool:
		if typ != tlv.TypeBoolFalse && typ != tlv.TypeBoolTrue {
			return jsonErr(path, "not a boolean (type 0x%02X)", typ)
		}
		b.WriteString(strconv.FormatBool(n.El.Bool))
	case KindUint, KindEnum, KindBitmap:
		if !isUintType(typ) {
			return jsonErr(path, "not unsigned (type 0x%02X)", typ)
		}
		b.WriteString(strconv.FormatUint(n.El.Uint, 10))
	case KindInt:
		if !isIntType(typ) {
			return jsonErr(path, "not signed (type 0x%02X)", typ)
		}
		b.WriteString(strconv.FormatInt(n.El.Int, 10))
	case KindFloat32, KindFloat64:
		if typ != tlv.TypeFloat4 && typ != tlv.TypeFloat8 {
			return jsonErr(path, "not a float (type 0x%02X)", typ)
		}
		writeFloat(b, n.El.Float, typ)
	case KindString:
		if !isStringType(typ) {
			return jsonErr(path, "not a string (type 0x%02X)", typ)
		}
		writeString(b, n.El.String)
	case KindBytes:
		if !isBytesType(typ) {
			return jsonErr(path, "not an octet string (type 0x%02X)", typ)
		}
		writeString(b, "hex:"+hex.EncodeToString(n.El.Octets))
	case KindList:
		if typ != tlv.TypeArray && typ != tlv.TypeList {
			return jsonErr(path, "not a list (type 0x%02X)", typ)
		}
		var entry Type
		if t.Entry != nil {
			entry = t.Entry.Type
		}
		b.WriteByte('[')
		for i, c := range n.Children {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeValue(b, entry, c, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case KindStruct:
		if typ != tlv.TypeStructure {
			return jsonErr(path, "not a struct (type 0x%02X)", typ)
		}
		if t.Struct == nil {
			return writeAny(b, n)
		}
		return writeStruct(b, t.Struct, n, path)
	default:
		return writeAny(b, n)
	}
	return nil
}

func writeFloat(b *bytes.Buffer, f float64, typ tlv.ElementType) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		// JSON has no NaN or infinity; chip-tool's parser takes neither.
		writeString(b, strconv.FormatFloat(f, 'g', -1, 64))
		return
	}
	bits := 64
	if typ == tlv.TypeFloat4 {
		bits = 32
	}
	b.WriteString(strconv.FormatFloat(f, 'g', -1, bits))
}

func writeString(b *bytes.Buffer, s string) {
	enc, _ := json.Marshal(s) // a string always marshals
	b.Write(enc)
}

func writeStruct(b *bytes.Buffer, st *Struct, n Node, path string) error {
	b.WriteByte('{')
	for i, c := range n.Children {
		if i > 0 {
			b.WriteByte(',')
		}
		var f *Field
		if c.El.Tag.Kind == tlv.TagKindContext {
			for fi := range st.Fields {
				if st.Fields[fi].ID == c.El.Tag.Number {
					f = &st.Fields[fi]
					break
				}
			}
		}
		if f == nil {
			writeString(b, tagKey(c.El.Tag))
			b.WriteByte(':')
			if err := writeAny(b, c); err != nil {
				return err
			}
			continue
		}
		writeString(b, camel(f.Name))
		b.WriteByte(':')
		if err := writeValue(b, f.Type, c, joinPath(path, f.Name)); err != nil {
			return err
		}
	}
	b.WriteByte('}')
	return nil
}

func tagKey(t tlv.Tag) string { return strconv.FormatUint(uint64(t.Number), 10) }

// writeAny renders an untyped node in the form encodeAny reads back:
// unsigned as a number, signed as "s:…" when not negative (a negative one
// as a number), floats as "f:…"/"d:…", octets as "hex:…", a struct as an
// object keyed by tag number.
func writeAny(b *bytes.Buffer, n Node) error {
	el := n.El
	switch {
	case isNull(n):
		b.WriteString("null")
	case el.Type == tlv.TypeStructure:
		b.WriteByte('{')
		for i, c := range n.Children {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, tagKey(c.El.Tag))
			b.WriteByte(':')
			if err := writeAny(b, c); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case el.IsContainer:
		b.WriteByte('[')
		for i, c := range n.Children {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeAny(b, c); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case el.Type == tlv.TypeBoolFalse || el.Type == tlv.TypeBoolTrue:
		b.WriteString(strconv.FormatBool(el.Bool))
	case isUintType(el.Type):
		b.WriteString(strconv.FormatUint(el.Uint, 10))
	case isIntType(el.Type):
		if el.Int < 0 {
			b.WriteString(strconv.FormatInt(el.Int, 10))
		} else {
			writeString(b, "s:"+strconv.FormatInt(el.Int, 10))
		}
	case el.Type == tlv.TypeFloat4:
		writeString(b, "f:"+strconv.FormatFloat(el.Float, 'g', -1, 32))
	case el.Type == tlv.TypeFloat8:
		writeString(b, "d:"+strconv.FormatFloat(el.Float, 'g', -1, 64))
	case isStringType(el.Type):
		writeString(b, el.String)
	case isBytesType(el.Type):
		writeString(b, "hex:"+hex.EncodeToString(el.Octets))
	default:
		return jsonErr("", "TLV type 0x%02X has no JSON form", el.Type)
	}
	return nil
}

// PropertyName is the camelCase property name of a model name, the name
// matter.js gives a field in its JavaScript form — camelize
// (packages/general/src/util/identifier-case.ts:21 at 862ad75f) as
// [DecodeJSON] keys a struct: "OnOff" is "onOff", "PIROccupiedToUnoccupiedDelay"
// "pirOccupiedToUnoccupiedDelay".
func PropertyName(name string) string { return camel(name) }

// GlobalAttributeType is the type of a global attribute (Matter Core
// §7.13: ClusterRevision, FeatureMap and the four id lists), whatever the
// cluster; false for another attribute id.
func GlobalAttributeType(attrID uint32) (Type, bool) { return globalType(attrID) }

// camel is the camelCase property name of a model name, as matter.js's
// camelize writes it: a leading acronym is lower-cased whole
// ("PIROccupiedToUnoccupiedDelay" is "pirOccupiedToUnoccupiedDelay").
func camel(name string) string {
	r := []rune(name)
	i := 0
	for i < len(r) && unicode.IsUpper(r[i]) {
		i++
	}
	switch {
	case i == 0:
		return name
	case i == 1 || i == len(r):
		// "FabricIndex" → "fabricIndex"; "ID" → "id".
	default:
		// "PIROccupied": the last capital starts the next word.
		if unicode.IsLower(r[i]) {
			i--
		}
	}
	for j := range i {
		r[j] = unicode.ToLower(r[j])
	}
	return string(r)
}
