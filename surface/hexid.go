// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// The JSON forms of an id: a lower-case hex string with the 0x prefix,
// four digits for a device type or a cluster id and eight for an
// attribute, command or event id, a feature's mask and a FeatureMap.
// Decoding also accepts the decimal number the first snapshot carried.
type (
	id16 uint32
	id32 uint32
	// kindID is an element's id, as wide as its [Kind] says.
	kindID struct {
		v    uint32
		wide bool
	}
)

func (v id16) MarshalJSON() ([]byte, error) { return hexJSON(uint32(v), 4), nil }
func (v id32) MarshalJSON() ([]byte, error) { return hexJSON(uint32(v), 8), nil }

func (v kindID) MarshalJSON() ([]byte, error) {
	if v.wide {
		return hexJSON(v.v, 8), nil
	}
	return hexJSON(v.v, 4), nil
}

func (v *id16) UnmarshalJSON(b []byte) error   { return unmarshalID((*uint32)(v), b) }
func (v *id32) UnmarshalJSON(b []byte) error   { return unmarshalID((*uint32)(v), b) }
func (v *kindID) UnmarshalJSON(b []byte) error { return unmarshalID(&v.v, b) }

// idOf is the JSON form of an element's id of kind k.
func idOf(k Kind, id uint32) kindID {
	return kindID{v: id, wide: k != KindDeviceType && k != KindCluster}
}

func hexJSON(v uint32, width int) []byte {
	return []byte(fmt.Sprintf(`"0x%0*x"`, width, v))
}

// unmarshalID reads "0x…" (hex, either case) or a decimal number.
func unmarshalID(dst *uint32, b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		h, ok := strings.CutPrefix(s, "0x")
		if !ok {
			return fmt.Errorf("surface: id %q has no 0x prefix", s)
		}
		v, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return fmt.Errorf("surface: id %q: %w", s, err)
		}
		*dst = uint32(v)
		return nil
	}
	v, err := strconv.ParseUint(string(b), 10, 32)
	if err != nil {
		return fmt.Errorf("surface: id %s: %w", b, err)
	}
	*dst = uint32(v)
	return nil
}

// ids32 and plain32 convert an id list to its JSON form and back.
func ids32(ids []uint32) []id32 {
	out := make([]id32, len(ids))
	for i, id := range ids {
		out[i] = id32(id)
	}
	return out
}

func plain32(ids []id32) []uint32 {
	if ids == nil {
		return nil
	}
	out := make([]uint32, len(ids))
	for i, id := range ids {
		out[i] = uint32(id)
	}
	return out
}
