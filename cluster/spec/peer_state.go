// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"errors"

	"github.com/SukramJ/go-fabric/tlv"
)

// ErrUntyped is the error of [DecodeAttribute] for a value without a type
// to decode it by: an attribute of a named or unknown cluster (no
// definition), or one the definition does not know.
var ErrUntyped = errors.New("spec: attribute has no type")

// Values are a peer's values of one cluster as it reported them: the TLV
// of each attribute, by id. A controller's client image of the cluster
// (client.Cluster) is one, the values of one cluster of a report
// (client.Report.Values) another, [AttributeValues] a third.
type Values interface {
	// Value returns the attribute's value as the peer reported it; false
	// when the values hold none.
	Value(attrID uint32) (Node, bool)
}

// AttributeValues are [Values] held in a map.
type AttributeValues map[uint32]Node

// Value implements [Values].
func (v AttributeValues) Value(attrID uint32) (Node, bool) {
	n, ok := v[attrID]
	return n, ok
}

// DecodeAttribute decodes a reported value of attribute attrID of the
// cluster def defines: a global attribute by its own type, whatever the
// cluster (def may be nil); otherwise through the definition's
// [Attribute].Decode when it has one (a struct, a list of structs: the
// generated type), else [DecodeValue] (a scalar at its widest Go type).
// TLV null is nil. A value with no type — def nil, or an attribute def
// does not know — is n itself with [ErrUntyped].
//
// It is the decode matter.js applies to every reported attribute as it
// arrives, by the attribute's schema, an unknown one as unknown
// (packages/protocol/src/action/client/InputChunk.ts:291-302 at 862ad75f,
// decodeAttributeGroup; globals by their cluster-independent schema,
// :58-74).
func DecodeAttribute(def *Cluster, attrID uint32, n Node) (any, error) {
	if t, ok := globalType(attrID); ok {
		return DecodeValue(t, n)
	}
	var a *Attribute
	if def != nil {
		a = def.Attribute(attrID)
	}
	if a == nil {
		return n, ErrUntyped
	}
	if n.El.Type == tlv.TypeNull {
		return nil, nil
	}
	if a.Decode != nil {
		return a.Decode(n)
	}
	return DecodeValue(a.Type, n)
}

// globalType is a global attribute's type (Matter Core §7.13).
func globalType(attrID uint32) (Type, bool) {
	idList := func(bits int) Type {
		return Type{Name: "list", Kind: KindList, Entry: &Field{Type: Type{Kind: KindUint, Bits: bits}}}
	}
	switch attrID {
	case AttrClusterRevision:
		return Type{Name: "ClusterRevision", Kind: KindUint, Bits: 16}, true
	case AttrFeatureMap:
		return Type{Name: "FeatureMap", Kind: KindBitmap, Bits: 32}, true
	case AttrAttributeList, AttrAcceptedCommandList, AttrGeneratedCommandList, AttrEventList:
		return idList(32), true
	}
	return Type{}, false
}

// StateFrom returns the generated State S of a peer's values of the
// cluster def defines: each attribute the values hold decoded by def
// ([DecodeAttribute]) and converted to its field's type as a stored value
// is for [StateOf] — the one generated path ([StateReader]) a device's
// Get and a controller's read share. absent lists, in State's field
// order, the attributes of S the values do not hold, or hold in a form
// that does not decode; their fields stay at the zero value. A value
// that decodes but does not fit its field also leaves the field at zero
// and is not listed. nil values are all absent.
//
// matter.js types a peer's cluster with the same generated cluster
// behavior a server uses (packages/node/src/node/client/PeerBehavior.ts:42-63
// at 862ad75f, a "known" shape), its state filled from the decoded
// report values (InputChunk.ts:291-302, ReadResult.ts:49-55 value).
// Each generated package calls it as <cluster>def.StateFrom.
// fabric:reachable:reason="generic: every generated server_gen.go calls an instantiation of it, and RTA records the instantiation, not the generic declaration"
func StateFrom[S any, P interface {
	*S
	StateReader
}](def *Cluster, values Values) (state S, absent []uint32) {
	P(&state).FromAttributeValues(func(attrID uint32) (any, bool) {
		if values == nil {
			absent = append(absent, attrID)
			return nil, false
		}
		n, ok := values.Value(attrID)
		if !ok {
			absent = append(absent, attrID)
			return nil, false
		}
		v, err := DecodeAttribute(def, attrID, n)
		if err != nil {
			absent = append(absent, attrID)
			return nil, false
		}
		return v, true
	})
	return state, absent
}
