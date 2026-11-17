// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestSceneExtensionValueKeepsItsSign (R2-P6-wire-0449): the extension-
// field encoder writes a value under a signed tag (2, 4, 6, 8) as a signed
// integer — the decoder keeps a signed value's raw bits in Unsigned — and
// under an unsigned tag as an unsigned one.
func TestSceneExtensionValueKeepsItsSign(t *testing.T) {
	t.Parallel()
	for tag := uint8(1); tag <= 8; tag++ {
		v := int64(-5)
		pair := mattercore.AttributeValuePair{AttributeID: 0, Tag: tag, Unsigned: uint64(v), Fields: 1} //nolint:gosec // the raw bits
		if tag%2 == 1 {
			pair.Unsigned = 5
		}
		enc := tlv.NewEncoder()
		encodeExtensionFieldSetsTLV(enc, []mattercore.ExtensionFieldSet{{ClusterID: 6, AttributeValueList: []mattercore.AttributeValuePair{pair}}})
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		dec := tlv.NewDecoder(b)
		for {
			el, err := dec.Next()
			if err != nil {
				t.Fatalf("tag %d: value not found: %v", tag, err)
			}
			if el.Tag.Kind != tlv.TagKindContext || el.Tag.Number != uint32(tag) || el.IsContainer {
				continue
			}
			signed := el.Type >= tlv.TypeSignedInt1 && el.Type <= tlv.TypeSignedInt8
			switch {
			case tag%2 == 0 && (!signed || el.Int != -5):
				t.Errorf("tag %d: encoded type 0x%02X value %d/%d, want signed -5", tag, el.Type, el.Int, el.Uint)
			case tag%2 == 1 && (signed || el.Uint != 5):
				t.Errorf("tag %d: encoded type 0x%02X value %d, want unsigned 5", tag, el.Type, el.Uint)
			}
			break
		}
	}
}
