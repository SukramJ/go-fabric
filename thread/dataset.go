// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

// OperationalDataset is a decoded Thread operational dataset. Each named
// field is one MeshCoP TLV; nil is a TLV the dataset does not carry (an
// empty, non-nil slice is a zero-length TLV). TLVs without a field survive
// in UnknownTLVs; Raw is a copy of the decoded bytes.
//
// A dataset from [DecodeDataset] also keeps the decoded TLVs, so
// [OperationalDataset.Encode] reproduces the source bytes while no field
// moved.
//
// Mirrors matter.js packages/protocol/src/thread/OperationalDataset.ts:15-42
// (OperationalDataset); the replay state the WeakMap ORIGINAL_TLVS holds
// there (:105-110) is the unexported field.
type OperationalDataset struct {
	// Channel is the channel number; the channel page of the three-byte
	// form is not kept.
	Channel          *uint16
	PANID            *uint16
	ExtPANID         []byte
	NetworkName      *string
	PSKc             []byte
	NetworkKey       []byte
	MeshLocalPrefix  []byte
	SecurityPolicy   *SecurityPolicy
	ActiveTimestamp  []byte
	PendingTimestamp []byte
	DelayTimer       *uint32
	ChannelMask      []byte
	UnknownTLVs      []TLV
	Raw              []byte

	original []TLV
}

// DecodeDataset decodes a MeshCoP TLV stream. A stream that does not walk
// fails with [ErrThreadTLV], a known TLV of the wrong size with
// [ErrThreadDataset]. The dataset shares no memory with blob.
//
// Mirrors matter.js packages/protocol/src/thread/OperationalDataset.ts:53-69
// (OperationalDataset.decode), for bytes.
func DecodeDataset(blob []byte) (*OperationalDataset, error) {
	entries, err := WalkTLVs(blob)
	if err != nil {
		return nil, err
	}
	ds := &OperationalDataset{UnknownTLVs: []TLV{}, Raw: append([]byte{}, blob...), original: entries}
	for _, e := range entries {
		if err := applyKnownTLV(ds, e); err != nil {
			return nil, err
		}
	}
	return ds, nil
}

// DecodeDatasetHex decodes a dataset given as hex, whitespace anywhere
// ignored. Hex that does not parse fails with [ErrThreadDataset].
//
// Mirrors matter.js packages/protocol/src/thread/OperationalDataset.ts:53-54
// (OperationalDataset.decode), for a string: whitespace is the set of
// JavaScript's `\s`.
func DecodeDatasetHex(s string) (*OperationalDataset, error) {
	b, err := hex.DecodeString(strings.Map(func(r rune) rune {
		if isJSSpace(r) {
			return -1
		}
		return r
	}, s))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrThreadDataset, err)
	}
	return DecodeDataset(b)
}

// Encode writes the dataset as a MeshCoP TLV stream. A dataset from
// [DecodeDataset] replays: the decoded TLVs in their order, each known TLV
// in its source bytes while its field still holds the decoded value and in
// canonical form once it moved; a cleared field drops its TLV, an unknown
// TLV no longer in UnknownTLVs is dropped, and a field set that the source
// did not carry follows, canonical, in type order. A dataset built in
// memory encodes canonically: the known TLVs in type order, then
// UnknownTLVs. A value over 0xFFFF bytes fails with [ErrInvalidTLV].
//
// Mirrors matter.js packages/protocol/src/thread/OperationalDataset.ts:83-89
// (OperationalDataset.encode), encodeWithReplay (:182-221) and
// canonicalEntries (:225-236).
func (ds *OperationalDataset) Encode() ([]byte, error) {
	if ds.original != nil {
		return ds.encodeWithReplay()
	}
	out := []TLV{}
	for _, k := range knownTLVs {
		if v, set := k.encode(ds); set {
			out = append(out, TLV{Type: k.typ, Value: v})
		}
	}
	for _, u := range ds.UnknownTLVs {
		out = append(out, TLV{Type: u.Type, Value: append([]byte{}, u.Value...)})
	}
	return EncodeTLVs(out)
}

func (ds *OperationalDataset) encodeWithReplay() ([]byte, error) {
	seen := map[TLVType]bool{}
	cursor := map[TLVType]int{}
	out := []TLV{}
	scratch := &OperationalDataset{}
	for _, orig := range ds.original {
		if k, ok := knownTLVByType[orig.Type]; ok {
			seen[orig.Type] = true
			canonical, set := k.encode(ds)
			if !set {
				continue
			}
			// Compare values, not bytes, so a non-canonical form replays.
			if err := applyKnownTLV(scratch, orig); err != nil {
				return nil, err
			}
			if k.equal(ds, scratch) {
				out = append(out, orig)
			} else {
				out = append(out, TLV{Type: orig.Type, Value: canonical})
			}
			continue
		}
		match := -1
		for i := cursor[orig.Type]; i < len(ds.UnknownTLVs); i++ {
			if u := ds.UnknownTLVs[i]; u.Type == orig.Type && bytes.Equal(u.Value, orig.Value) {
				match = i
				break
			}
		}
		if match < 0 {
			continue
		}
		cursor[orig.Type] = match + 1
		out = append(out, orig)
	}
	for _, k := range knownTLVs {
		if seen[k.typ] {
			continue
		}
		if v, set := k.encode(ds); set {
			out = append(out, TLV{Type: k.typ, Value: v})
		}
	}
	return EncodeTLVs(out)
}

// Redact is a shallow copy of ds without PSKc and NetworkKey, the input
// untouched. The copy carries no replay state, so it encodes canonically;
// Raw stays, the source bytes and so the secrets with it, as in matter.js.
//
// Mirrors matter.js packages/protocol/src/thread/OperationalDataset.ts:100-102
// (OperationalDataset.redact): the spread copy there is not in the
// ORIGINAL_TLVS WeakMap.
func (ds *OperationalDataset) Redact() *OperationalDataset {
	c := *ds
	c.PSKc, c.NetworkKey, c.original = nil, nil, nil
	return &c
}

// applyKnownTLV sets the field of one TLV, or appends it to UnknownTLVs.
//
// Mirrors matter.js packages/protocol/src/thread/OperationalDataset.ts:112-180
// (applyKnownTlvToDataset).
func applyKnownTLV(ds *OperationalDataset, e TLV) error {
	v := e.Value
	fixed := func(name string, n int) error {
		if len(v) != n {
			return fmt.Errorf("%w: %s TLV must be %d bytes, got %d", ErrThreadDataset, name, n, len(v))
		}
		return nil
	}
	switch e.Type {
	case TLVChannel:
		ch, err := decodeChannel(v)
		if err != nil {
			return err
		}
		ds.Channel = &ch
	case TLVPANID:
		if err := fixed("PANID", 2); err != nil {
			return err
		}
		id := binary.BigEndian.Uint16(v)
		ds.PANID = &id
	case TLVExtPANID:
		if err := fixed("EXTPANID", 8); err != nil {
			return err
		}
		ds.ExtPANID = clone(v)
	case TLVNetworkName:
		name := decodeUTF8(v)
		ds.NetworkName = &name
	case TLVPSKc:
		ds.PSKc = clone(v)
	case TLVNetworkKey:
		if err := fixed("NETWORK_KEY", 16); err != nil {
			return err
		}
		ds.NetworkKey = clone(v)
	case TLVMeshLocalPrefix:
		if err := fixed("MESH_LOCAL_PREFIX", 8); err != nil {
			return err
		}
		ds.MeshLocalPrefix = clone(v)
	case TLVSecurityPolicy:
		p, err := DecodeSecurityPolicy(v)
		if err != nil {
			return err
		}
		ds.SecurityPolicy = &p
	case TLVActiveTimestamp:
		if err := fixed("ACTIVE_TIMESTAMP", 8); err != nil {
			return err
		}
		ds.ActiveTimestamp = clone(v)
	case TLVPendingTimestamp:
		if err := fixed("PENDING_TIMESTAMP", 8); err != nil {
			return err
		}
		ds.PendingTimestamp = clone(v)
	case TLVDelayTimer:
		if err := fixed("DELAY_TIMER", 4); err != nil {
			return err
		}
		t := binary.BigEndian.Uint32(v)
		ds.DelayTimer = &t
	case TLVChannelMask:
		ds.ChannelMask = clone(v)
	default:
		ds.UnknownTLVs = append(ds.UnknownTLVs, TLV{Type: e.Type, Value: clone(v)})
	}
	return nil
}

// decodeChannel accepts the spec's three-byte CHANNEL (page:1, channel:2)
// and the one-byte form real OpenThread datasets carry.
//
// Mirrors matter.js packages/protocol/src/thread/OperationalDataset.ts:314-327
// (decodeChannel).
func decodeChannel(v []byte) (uint16, error) {
	switch len(v) {
	case 1:
		return uint16(v[0]), nil
	case 3:
		return binary.BigEndian.Uint16(v[1:]), nil
	}
	return 0, fmt.Errorf("%w: CHANNEL TLV must be 1 or 3 bytes, got %d", ErrThreadDataset, len(v))
}

// knownTLV is the canonical encoding and the replay comparison of one
// named field.
//
// Mirrors matter.js packages/protocol/src/thread/OperationalDataset.ts:242-270
// (KnownTlvHandler, handler).
type knownTLV struct {
	typ TLVType
	// encode is the canonical value of the field; set is false when the
	// field is unset.
	encode func(ds *OperationalDataset) (v []byte, set bool)
	// equal is whether the field holds the same value in both.
	equal func(a, b *OperationalDataset) bool
}

func valueTLV[T comparable](typ TLVType, field func(*OperationalDataset) *T, enc func(T) []byte) knownTLV {
	return knownTLV{
		typ: typ,
		encode: func(ds *OperationalDataset) ([]byte, bool) {
			if p := field(ds); p != nil {
				return enc(*p), true
			}
			return nil, false
		},
		equal: func(a, b *OperationalDataset) bool {
			pa, pb := field(a), field(b)
			if pa == nil || pb == nil {
				return pa == nil && pb == nil
			}
			return *pa == *pb
		},
	}
}

func bytesTLV(typ TLVType, field func(*OperationalDataset) []byte) knownTLV {
	return knownTLV{
		typ: typ,
		encode: func(ds *OperationalDataset) ([]byte, bool) {
			v := field(ds)
			return clone(v), v != nil
		},
		equal: func(a, b *OperationalDataset) bool {
			va, vb := field(a), field(b)
			if va == nil || vb == nil {
				return va == nil && vb == nil
			}
			return bytes.Equal(va, vb)
		},
	}
}

// knownTLVs are the named fields in type order.
//
// Mirrors matter.js packages/protocol/src/thread/OperationalDataset.ts:278-312
// (KNOWN_TLV_HANDLERS): CHANNEL always in the three-byte form, page 0.
var knownTLVs = []knownTLV{
	valueTLV(TLVChannel, func(ds *OperationalDataset) *uint16 { return ds.Channel },
		func(v uint16) []byte { return binary.BigEndian.AppendUint16([]byte{0x00}, v) }),
	valueTLV(TLVPANID, func(ds *OperationalDataset) *uint16 { return ds.PANID },
		func(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }),
	bytesTLV(TLVExtPANID, func(ds *OperationalDataset) []byte { return ds.ExtPANID }),
	valueTLV(TLVNetworkName, func(ds *OperationalDataset) *string { return ds.NetworkName },
		func(v string) []byte { return []byte(v) }),
	bytesTLV(TLVPSKc, func(ds *OperationalDataset) []byte { return ds.PSKc }),
	bytesTLV(TLVNetworkKey, func(ds *OperationalDataset) []byte { return ds.NetworkKey }),
	bytesTLV(TLVMeshLocalPrefix, func(ds *OperationalDataset) []byte { return ds.MeshLocalPrefix }),
	valueTLV(TLVSecurityPolicy, func(ds *OperationalDataset) *SecurityPolicy { return ds.SecurityPolicy },
		SecurityPolicy.Encode),
	bytesTLV(TLVActiveTimestamp, func(ds *OperationalDataset) []byte { return ds.ActiveTimestamp }),
	bytesTLV(TLVPendingTimestamp, func(ds *OperationalDataset) []byte { return ds.PendingTimestamp }),
	valueTLV(TLVDelayTimer, func(ds *OperationalDataset) *uint32 { return ds.DelayTimer },
		func(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }),
	bytesTLV(TLVChannelMask, func(ds *OperationalDataset) []byte { return ds.ChannelMask }),
}

var knownTLVByType = func() map[TLVType]knownTLV {
	out := make(map[TLVType]knownTLV, len(knownTLVs))
	for _, k := range knownTLVs {
		out[k.typ] = k
	}
	return out
}()

// clone copies b, nil staying nil.
func clone(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte{}, b...)
}

// isJSSpace is JavaScript's `\s`: WhiteSpace and LineTerminator
// (ECMA-262 §12.2, §12.3).
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// decodeUTF8 is `new TextDecoder("utf-8").decode`: a leading BOM dropped,
// every maximal ill-formed subsequence replaced by U+FFFD (the WHATWG
// Encoding Standard's UTF-8 decoder, which Go's per-byte replacement is
// not).
func decodeUTF8(b []byte) string {
	b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	var sb strings.Builder
	var cp rune
	needed, seen := 0, 0
	lower, upper := byte(0x80), byte(0xbf)
	for i := 0; i < len(b); {
		c := b[i]
		if needed == 0 {
			i++
			switch {
			case c <= 0x7f:
				sb.WriteByte(c)
			case c >= 0xc2 && c <= 0xdf:
				needed, cp = 1, rune(c&0x1f)
			case c >= 0xe0 && c <= 0xef:
				switch c {
				case 0xe0:
					lower = 0xa0
				case 0xed:
					upper = 0x9f
				}
				needed, cp = 2, rune(c&0x0f)
			case c >= 0xf0 && c <= 0xf4:
				switch c {
				case 0xf0:
					lower = 0x90
				case 0xf4:
					upper = 0x8f
				}
				needed, cp = 3, rune(c&0x07)
			default:
				sb.WriteRune('\uFFFD')
			}
			continue
		}
		if c < lower || c > upper {
			// The byte is not consumed: it starts the next sequence.
			needed, seen, cp = 0, 0, 0
			lower, upper = 0x80, 0xbf
			sb.WriteRune('\uFFFD')
			continue
		}
		lower, upper = 0x80, 0xbf
		cp = cp<<6 | rune(c&0x3f)
		seen++
		i++
		if seen == needed {
			sb.WriteRune(cp)
			needed, seen, cp = 0, 0, 0
		}
	}
	if needed != 0 {
		sb.WriteRune('\uFFFD')
	}
	return sb.String()
}
