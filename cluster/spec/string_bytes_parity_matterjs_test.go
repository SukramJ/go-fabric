// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestTlvStringBoundsInBytesParityMatterJS ports the bound cases of
// packages/types/test/tlv/TlvStringTest.ts at b25ed960 (#4995) over
// [spec.DecodeStringIn], the decoder a generated "max N" field reads with:
// a character string's minimum and maximum count UTF-8 bytes, also the
// length a string cut at its IS1 arrived with.
func TestTlvStringBoundsInBytesParityMatterJS(t *testing.T) {
	t.Parallel()
	str := func(t *testing.T, s string) spec.Node {
		t.Helper()
		enc := tlv.NewEncoder()
		enc.PutUTF8(tlv.AnonymousTag(), s)
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return wireNode(t, hex.EncodeToString(b))
	}
	// :98-102: "éé" then IS1 and "a" is six bytes before its IS1.
	t.Run("enforces maxLength in UTF-8 bytes on the string before Information Separator 1 truncation", func(t *testing.T) {
		t.Parallel()
		_, err := spec.DecodeStringIn(0, 4)(wireNode(t, "0c06c3a9c3a91f61"))
		wantStatus(t, "6 bytes before the IS1", err, im.StatusConstraintError)
		if err == nil || !strings.Contains(err.Error(), "6") {
			t.Errorf("err %v does not name the 6 bytes", err)
		}
	})
	// :132-137: 21 × "日" is 63 bytes, 22 × 66.
	t.Run("counts maxLength in UTF-8 bytes", func(t *testing.T) {
		t.Parallel()
		dec := spec.DecodeStringIn(0, 64)
		if _, err := dec(str(t, strings.Repeat("日", 21))); err != nil {
			t.Errorf("63 bytes refused: %v", err)
		}
		_, err := dec(str(t, strings.Repeat("日", 22)))
		wantStatus(t, "66 bytes", err, im.StatusConstraintError)
	})
	// :139-144: "éé" is four bytes, "éa" three.
	t.Run("counts minLength in UTF-8 bytes", func(t *testing.T) {
		t.Parallel()
		dec := spec.DecodeStringIn(4, 65_536)
		if _, err := dec(str(t, "éé")); err != nil {
			t.Errorf("4 bytes refused: %v", err)
		}
		_, err := dec(str(t, "éa"))
		wantStatus(t, "3 bytes", err, im.StatusConstraintError)
	})
}
