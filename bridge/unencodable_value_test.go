// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// hostOwnValue is a host server's own struct without EncodeTLV.
type hostOwnValue struct{ A int }

// TestUnencodableValueIsLoggedOnce (R2-P6-err-0447): a value the attribute
// writer cannot encode is still served as null (a non-empty []any as an
// empty list), and the writer says so once per Go type at Warn.
func TestUnencodableValueIsLoggedOnce(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	for range 3 {
		enc := tlv.NewEncoder()
		defaultAttributeValueWriter(enc, tlv.AnonymousTag(), im.AttributeValue{Value: hostOwnValue{A: 1}})
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if el, err := tlv.NewDecoder(b).Next(); err != nil || !el.IsNull {
			t.Fatalf("served %X, want null", b)
		}
	}
	if n := strings.Count(buf.String(), "bridge.hostOwnValue"); n != 1 {
		t.Errorf("logged %d times, want once:\n%s", n, buf.String())
	}
}
