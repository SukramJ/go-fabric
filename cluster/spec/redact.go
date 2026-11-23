// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import "thingfabric.dev/go-fabric/tlv"

// Secret redaction removes the values the model marks secret ([Field.Secret],
// [Attribute.Secret]) from a value before the module logs it. It mirrors
// matter.js's SecretRedaction namespace:
// matter.js packages/model/src/logic/SecretRedaction.ts:17-147 at b25ed960
// with one difference: matter.js turns it off when Logger.redactSecrets is
// false, by default true
// (matter.js packages/general/src/log/Logger.ts:80-88 at b25ed960);
// this module has no such switch, a marked secret never reaches its logs.
//
// The value is the TLV form, a [Node], walked by the model: a struct's
// members by field id (a child's context tag), a list's entries by
// [Type.Entry]. A Go value reaches it encoded and read back
// ([ReadElement]), so a server's own Go type is redacted by its wire form,
// not by the names of its Go fields.

// redactedText replaces each secret value.
// Mirrors matter.js packages/model/src/logic/SecretRedaction.ts:21 at b25ed960 (PLACEHOLDER).
const redactedText = "(redacted)"

// Redact returns n with every value model marks secret replaced by the
// text "(redacted)", a UTF-8 string under the replaced element's tag. A
// null stays null, whether secret or not; a member the model does not
// define stays as it is. n is not modified: the result is a copy at every
// struct or list that holds a secret, sharing the rest with n. Without a
// model, or without a secret in it ([ContainsSecret]), n comes back as it
// is.
//
// Redact does not fail closed on a shape the model does not describe: a
// list or struct that holds a secret, whose node is another container
// type than its model (a struct read as a TLV list, a list as a
// structure), comes back as it is, unredacted — matter.js's redactValue
// returns the value unchanged for an array model whose value is not an
// array and for an object model whose value is not a plain object
// (matter.js packages/model/src/logic/SecretRedaction.ts:107-108, :114-115
// at b25ed960). A caller that logs a value it did not encode by the model
// itself holds the shape to the model first (review II, R2-C2-sec-1303).
//
// Mirrors matter.js packages/model/src/logic/SecretRedaction.ts:32-37 at b25ed960
// (redact), without the Logger.redactSecrets switch.
func Redact(model *Field, n Node) Node {
	if model == nil || !ContainsSecret(model) {
		return n
	}
	return redactValue(model, n)
}

// RedactEntry is [Redact] for one entry of the list list describes, such
// as an entry a chunked list write appends: the entry is replaced as a
// whole when the list itself is secret and the entry is not null;
// otherwise it is redacted by the list's entry model.
//
// Mirrors matter.js packages/model/src/logic/SecretRedaction.ts:43-48 at b25ed960
// (redactEntry), without the Logger.redactSecrets switch.
func RedactEntry(list *Field, entry Node) Node {
	if list == nil {
		return entry
	}
	if list.Secret && !isNull(entry) {
		return redactedNode(entry.El.Tag)
	}
	return Redact(list.Type.Entry, entry)
}

// ContainsSecret reports whether model, or any member of it at any depth —
// a list's entry, a struct's fields — is secret. A self-referencing struct
// is searched once.
//
// Mirrors matter.js packages/model/src/logic/SecretRedaction.ts:66-89 at b25ed960
// (containsSecret, searchSecret), without its per-model cache, and
// matter.js packages/model/src/logic/SecretRedaction.ts:135-146 at b25ed960
// (childModelsOf).
func ContainsSecret(model *Field) bool {
	if model == nil {
		return false
	}
	return searchSecret(model, map[*Struct]bool{})
}

func searchSecret(f *Field, visiting map[*Struct]bool) bool {
	if f.Secret {
		return true
	}
	switch f.Type.Kind {
	case KindList:
		return f.Type.Entry != nil && searchSecret(f.Type.Entry, visiting)
	case KindStruct:
		s := f.Type.Struct
		if s == nil || visiting[s] {
			return false
		}
		visiting[s] = true
		for i := range s.Fields {
			if searchSecret(&s.Fields[i], visiting) {
				return true
			}
		}
	default:
	}
	return false
}

// redactValue redacts n by f: null first, then the mark, then the walk.
// Mirrors matter.js packages/model/src/logic/SecretRedaction.ts:91-122 at b25ed960
// (redactValue).
func redactValue(f *Field, n Node) Node {
	if isNull(n) {
		return n
	}
	if f.Secret {
		return redactedNode(n.El.Tag)
	}
	if !ContainsSecret(f) {
		return n
	}
	switch f.Type.Kind {
	case KindList:
		if f.Type.Entry == nil || n.El.Type != tlv.TypeArray {
			return n
		}
		out := Node{El: n.El, Children: make([]Node, len(n.Children))}
		for i, c := range n.Children {
			out.Children[i] = redactValue(f.Type.Entry, c)
		}
		return out
	case KindStruct:
		if f.Type.Struct == nil || n.El.Type != tlv.TypeStructure {
			return n
		}
		return redactMembers(f.Type.Struct, n)
	default:
		return n
	}
}

// redactMembers is a copy of the struct n with each member the model
// defines redacted by its model, matched by field id.
// Mirrors matter.js packages/model/src/logic/SecretRedaction.ts:124-133 at b25ed960
// (redactProperties); a member the model does not define is kept.
func redactMembers(s *Struct, n Node) Node {
	out := Node{El: n.El, Children: make([]Node, len(n.Children))}
	for i, c := range n.Children {
		out.Children[i] = c
		if c.El.Tag.Kind != tlv.TagKindContext {
			continue
		}
		for fi := range s.Fields {
			if s.Fields[fi].ID == c.El.Tag.Number {
				out.Children[i] = redactValue(&s.Fields[fi], c)
				break
			}
		}
	}
	return out
}

func redactedNode(tag tlv.Tag) Node {
	return Node{El: tlv.Element{Tag: tag, Type: tlv.TypeUTF8Str1, String: redactedText}}
}
