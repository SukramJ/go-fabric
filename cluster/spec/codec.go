// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec

import (
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The TLV codecs generated structs, command and event payloads are built
// from. A generated EncodeTLV is a straight sequence of Put* calls, a
// generated DecodeTLV one [DecodeFields] call over [Bind] specs, so all
// branching — and every rejection — lives here, written and tested once.
//
// They encode as matter.js's TlvOfModel does (packages/types/src/tlv/
// TlvOfModel.ts): struct fields in id order under context tags, integers
// at their smallest width, an optional field left out when absent, null as
// TLV null. Decoding rejects what TlvOfModel's schema rejects: a missing
// mandatory field or a field of the wrong TLV type is INVALID_COMMAND (the
// status the bridge answers a request its schema refuses), a number
// outside its type or its numeric constraint and a string, octet string or
// list outside its length constraint CONSTRAINT_ERROR (ValidationError's
// default code).

// Node is one decoded TLV element and, for a container, its children.
type Node struct {
	El       tlv.Element
	Children []Node
}

// ErrUnbalanced is returned when a container is not closed before the
// stream ends.
var ErrUnbalanced = errors.New("spec: TLV container not closed")

// ReadContainer reads the members of the container open opened — open
// already consumed from dec — through its EndContainer.
func ReadContainer(dec *tlv.Decoder, open tlv.Element) (Node, error) {
	n := Node{El: open}
	for {
		el, err := dec.Next()
		if err != nil {
			return n, fmt.Errorf("%w: %w", ErrUnbalanced, err)
		}
		if el.IsEndContainer {
			return n, nil
		}
		child := Node{El: el}
		if el.IsContainer {
			if child, err = ReadContainer(dec, el); err != nil {
				return n, err
			}
		}
		n.Children = append(n.Children, child)
	}
}

// ReadElement reads one complete element from dec.
func ReadElement(dec *tlv.Decoder) (Node, error) {
	el, err := dec.Next()
	if err != nil {
		return Node{}, err
	}
	if el.IsEndContainer {
		return Node{}, ErrUnbalanced
	}
	if !el.IsContainer {
		return Node{El: el}, nil
	}
	return ReadContainer(dec, el)
}

// FieldError rejects a payload with an IM status. The payload's container
// has been read completely when one is returned, so it also reports
// [im.FieldsContainerConsumed].
type FieldError struct {
	Status im.StatusCode
	Msg    string
}

// Error implements error.
func (e *FieldError) Error() string { return e.Msg }

// MatterStatusCode implements [im.StatusCodeError].
func (e *FieldError) MatterStatusCode() im.StatusCode { return e.Status }

// FieldsContainerConsumed implements [im.FieldsContainerConsumed].
func (*FieldError) FieldsContainerConsumed() bool { return true }

var _ im.FieldsContainerConsumed = (*FieldError)(nil)

func invalid(format string, args ...any) error {
	return &FieldError{Status: im.StatusInvalidCommand, Msg: fmt.Sprintf(format, args...)}
}

func outOfRange(format string, args ...any) error {
	return &FieldError{Status: im.StatusConstraintError, Msg: fmt.Sprintf(format, args...)}
}

// Decoder decodes one value from a node.
type Decoder[T any] func(Node) (T, error)

// Unsigned is the set of unsigned integer types, named ones included.
type Unsigned interface {
	~uint8 | ~uint16 | ~uint32 | ~uint64
}

// Signed is the set of signed integer types, named ones included.
type Signed interface {
	~int8 | ~int16 | ~int32 | ~int64
}

// DecodeUint decodes an unsigned integer of bits, enum or bitmap included.
func DecodeUint[T Unsigned](bits int) Decoder[T] {
	return DecodeUintIn[T](bits, 0, UintMax(bits, false))
}

// DecodeUintIn decodes an unsigned integer of bits bounded by a numeric
// constraint.
func DecodeUintIn[T Unsigned](bits int, lo, hi uint64) Decoder[T] {
	return func(n Node) (T, error) {
		el := n.El
		if el.Type < tlv.TypeUnsignedInt1 || el.Type > tlv.TypeUnsignedInt8 {
			return 0, invalid("value is not an unsigned integer (type 0x%02X)", el.Type)
		}
		if el.Uint > UintMax(bits, false) || el.Uint < lo || el.Uint > hi {
			return 0, outOfRange("%d outside %d..%d", el.Uint, lo, min(hi, UintMax(bits, false)))
		}
		return T(el.Uint), nil
	}
}

// DecodeInt decodes a signed integer of bits.
func DecodeInt[T Signed](bits int) Decoder[T] {
	lo, hi := IntRange(bits, false)
	return DecodeIntIn[T](bits, lo, hi)
}

// DecodeIntIn decodes a signed integer of bits bounded by a numeric constraint.
func DecodeIntIn[T Signed](bits int, lo, hi int64) Decoder[T] {
	tlo, thi := IntRange(bits, false)
	lo, hi = max(lo, tlo), min(hi, thi)
	return func(n Node) (T, error) {
		el := n.El
		if el.Type > tlv.TypeSignedInt8 {
			return 0, invalid("value is not a signed integer (type 0x%02X)", el.Type)
		}
		if el.Int < lo || el.Int > hi {
			return 0, outOfRange("%d outside %d..%d", el.Int, lo, hi)
		}
		return T(el.Int), nil
	}
}

// DecodeBool decodes a boolean.
func DecodeBool(n Node) (bool, error) {
	if n.El.Type != tlv.TypeBoolFalse && n.El.Type != tlv.TypeBoolTrue {
		return false, invalid("value is not a boolean (type 0x%02X)", n.El.Type)
	}
	return n.El.Bool, nil
}

// DecodeFloat32 decodes a single-precision float; a double that fits is
// accepted.
func DecodeFloat32(n Node) (float32, error) {
	f, err := DecodeFloat64(n)
	if err != nil {
		return 0, err
	}
	if math.Abs(f) > math.MaxFloat32 && !math.IsInf(f, 0) {
		return 0, outOfRange("%v outside single precision", f)
	}
	return float32(f), nil
}

// DecodeFloat64 decodes a double-precision float.
func DecodeFloat64(n Node) (float64, error) {
	if n.El.Type != tlv.TypeFloat4 && n.El.Type != tlv.TypeFloat8 {
		return 0, invalid("value is not a float (type 0x%02X)", n.El.Type)
	}
	return n.El.Float, nil
}

// DecodeString decodes a UTF-8 string.
func DecodeString(n Node) (string, error) { return DecodeStringIn(0, math.MaxInt)(n) }

// DecodeStringIn decodes a UTF-8 string whose length — UTF-16 code units, as
// matter.js's TlvString counts it — lies in lo..hi.
func DecodeStringIn(lo, hi int) Decoder[string] {
	return func(n Node) (string, error) {
		if n.El.Type < tlv.TypeUTF8Str1 || n.El.Type > tlv.TypeUTF8Str8 {
			return "", invalid("value is not a string (type 0x%02X)", n.El.Type)
		}
		if !utf8.ValidString(n.El.String) {
			return "", invalid("string is not valid UTF-8")
		}
		if l := StringLength(n.El.String); l < lo || l > hi {
			return "", outOfRange("string length %d outside %d..%d", l, lo, hi)
		}
		return n.El.String, nil
	}
}

// DecodeBytes decodes an octet string.
func DecodeBytes(n Node) ([]byte, error) { return DecodeBytesIn(0, math.MaxInt)(n) }

// DecodeBytesIn decodes an octet string whose length lies in lo..hi.
func DecodeBytesIn(lo, hi int) Decoder[[]byte] {
	return func(n Node) ([]byte, error) {
		if n.El.Type < tlv.TypeOctetStr1 || n.El.Type > tlv.TypeOctetStr8 {
			return nil, invalid("value is not an octet string (type 0x%02X)", n.El.Type)
		}
		if l := len(n.El.Octets); l < lo || l > hi {
			return nil, outOfRange("octet string length %d outside %d..%d", l, lo, hi)
		}
		return append([]byte(nil), n.El.Octets...), nil
	}
}

// DecodeStruct decodes a generated struct.
func DecodeStruct[T any, P interface {
	*T
	DecodeTLV(Node) error
}](n Node) (T, error) {
	var v T
	err := P(&v).DecodeTLV(n)
	return v, err
}

// DecodeList decodes a list.
func DecodeList[T any](entry Decoder[T]) Decoder[[]T] { return DecodeListIn(entry, 0, math.MaxInt) }

// DecodeListIn decodes a list whose entry count lies in lo..hi.
func DecodeListIn[T any](entry Decoder[T], lo, hi int) Decoder[[]T] {
	return func(n Node) ([]T, error) {
		if n.El.Type != tlv.TypeArray && n.El.Type != tlv.TypeList {
			return nil, invalid("value is not a list (type 0x%02X)", n.El.Type)
		}
		if l := len(n.Children); l < lo || l > hi {
			return nil, outOfRange("list length %d outside %d..%d", l, lo, hi)
		}
		out := make([]T, 0, len(n.Children))
		for i, c := range n.Children {
			v, err := entry(c)
			if err != nil {
				return nil, fmt.Errorf("entry %d: %w", i, err)
			}
			out = append(out, v)
		}
		return out, nil
	}
}

// DecodeOptional decodes a field that may be absent: present, it is decoded
// into a new value.
func DecodeOptional[T any](d Decoder[T]) Decoder[*T] {
	return func(n Node) (*T, error) {
		v, err := d(n)
		if err != nil {
			return nil, err
		}
		return &v, nil
	}
}

// Nullable is a value that may be null.
type Nullable[T any] struct {
	Value T
	Null  bool
}

// NullOf returns a null T.
func NullOf[T any]() Nullable[T] { return Nullable[T]{Null: true} }

// ValueOf returns a non-null T.
func ValueOf[T any](v T) Nullable[T] { return Nullable[T]{Value: v} }

// DecodeNullable decodes a nullable value: TLV null, or what d decodes.
func DecodeNullable[T any](d Decoder[T]) Decoder[Nullable[T]] {
	return func(n Node) (Nullable[T], error) {
		if n.El.Type == tlv.TypeNull {
			return NullOf[T](), nil
		}
		v, err := d(n)
		if err != nil {
			return Nullable[T]{}, err
		}
		return ValueOf(v), nil
	}
}

// FieldSpec is one field of a struct, command or event payload, bound to
// the destination it decodes into.
type FieldSpec struct {
	ID        uint32
	Name      string
	Mandatory bool
	decode    func(Node) error
}

// Bind binds a field to its destination and decoder.
func Bind[T any](id uint32, name string, mandatory bool, dst *T, d Decoder[T]) FieldSpec {
	return FieldSpec{ID: id, Name: name, Mandatory: mandatory, decode: func(n Node) error {
		v, err := d(n)
		if err != nil {
			return err
		}
		*dst = v
		return nil
	}}
}

// DecodeFields decodes a structure node into fields: each context-tagged
// member whose tag is a field id, members it does not know skipped, and a
// mandatory field that is absent rejected.
func DecodeFields(n Node, what string, fields ...FieldSpec) error {
	if n.El.Type != tlv.TypeStructure {
		return invalid("%s: payload is not a structure (type 0x%02X)", what, n.El.Type)
	}
	seen := make([]bool, len(fields))
	for _, c := range n.Children {
		if c.El.Tag.Kind != tlv.TagKindContext {
			continue
		}
		for i := range fields {
			if fields[i].ID != c.El.Tag.Number {
				continue
			}
			if err := fields[i].decode(c); err != nil {
				return wrapField(what, fields[i].Name, err)
			}
			seen[i] = true
		}
	}
	for i, f := range fields {
		if f.Mandatory && !seen[i] {
			return invalid("%s: mandatory field %s missing", what, f.Name)
		}
	}
	return nil
}

func wrapField(what, field string, err error) error {
	var fe *FieldError
	if errors.As(err, &fe) {
		return &FieldError{Status: fe.Status, Msg: fmt.Sprintf("%s.%s: %s", what, field, fe.Msg)}
	}
	return fmt.Errorf("%s.%s: %w", what, field, err)
}

// Encodable is a value that writes itself as TLV: every generated struct,
// command payload and event payload. The bridge's value and response
// writers encode one through it.
type Encodable interface {
	EncodeTLV(enc *tlv.Encoder, tag tlv.Tag)
}

// ResponsePayload is a generated response command payload; it names the
// response command it answers with, so the invoke response carries the
// response's command id rather than the request's.
type ResponsePayload interface {
	Encodable
	ResponseCommand() (clusterID, commandID uint32)
}

// List is a list of generated values that encodes as a TLV array — what a
// server returns for a list attribute of structs.
type List[T Encodable] []T

// EncodeTLV implements [Encodable].
func (l List[T]) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartArray(tag)
	for _, v := range l {
		v.EncodeTLV(enc, tlv.AnonymousTag())
	}
	_ = enc.EndContainer()
}

// Put encodes one value.
type Put[T any] func(*tlv.Encoder, tlv.Tag, T)

// PutUint encodes an unsigned integer, enum or bitmap at its smallest
// width.
func PutUint[T Unsigned](enc *tlv.Encoder, tag tlv.Tag, v T) { enc.PutUint(tag, uint64(v)) }

// PutInt encodes a signed integer at its smallest width.
func PutInt[T Signed](enc *tlv.Encoder, tag tlv.Tag, v T) { enc.PutInt(tag, int64(v)) }

// PutBool encodes a boolean.
func PutBool(enc *tlv.Encoder, tag tlv.Tag, v bool) { enc.PutBool(tag, v) }

// PutFloat32 encodes a single-precision float.
func PutFloat32(enc *tlv.Encoder, tag tlv.Tag, v float32) { enc.PutFloat32(tag, v) }

// PutFloat64 encodes a double-precision float.
func PutFloat64(enc *tlv.Encoder, tag tlv.Tag, v float64) { enc.PutFloat64(tag, v) }

// PutString encodes a UTF-8 string.
func PutString(enc *tlv.Encoder, tag tlv.Tag, v string) { enc.PutUTF8(tag, v) }

// PutBytes encodes an octet string.
func PutBytes(enc *tlv.Encoder, tag tlv.Tag, v []byte) { enc.PutOctets(tag, v) }

// PutStruct encodes a generated struct.
func PutStruct[T Encodable](enc *tlv.Encoder, tag tlv.Tag, v T) { v.EncodeTLV(enc, tag) }

// PutList returns the encoder of a list whose entries put encodes.
func PutList[T any](put Put[T]) Put[[]T] {
	return func(enc *tlv.Encoder, tag tlv.Tag, v []T) {
		enc.StartArray(tag)
		for _, e := range v {
			put(enc, tlv.AnonymousTag(), e)
		}
		_ = enc.EndContainer()
	}
}

// PutNullable returns the encoder of a nullable value.
func PutNullable[T any](put Put[T]) Put[Nullable[T]] {
	return func(enc *tlv.Encoder, tag tlv.Tag, v Nullable[T]) {
		if v.Null {
			enc.PutNull(tag)
			return
		}
		put(enc, tag, v.Value)
	}
}

// PutOptional encodes v when present and leaves the field out when nil.
func PutOptional[T any](enc *tlv.Encoder, tag tlv.Tag, v *T, put Put[T]) {
	if v != nil {
		put(enc, tag, *v)
	}
}
