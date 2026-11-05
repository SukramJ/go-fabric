// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

// TestDerCodecDecodeRefusalsParityMatterJS mirrors
// packages/general/test/codec/DerCodecTest.ts:102-130 at 862ad75f: the
// element shapes matter.js's DerCodec.decode refuses — empty data, a
// declared length past the buffer, a 4-byte long-form length with its
// high bit set, the BER indefinite-length marker DER forbids, a long-form
// length of 7 bytes and a truncated nested element. The Go reader of a
// DER certificate (DecodeDER, over encoding/asn1) refuses each as
// ErrMalformed and never reads past the input.
func TestDerCodecDecodeRefusalsParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, der string }{
		{"rejects empty data", ""},
		{"rejects data whose declared length exceeds the buffer", "301000ff"},
		{"rejects a long-form length whose high bit would make it negative", "048480000000"},
		{"rejects the indefinite-length encoding", "0480ffff"},
		{"rejects a long-form length longer than a safe integer", "0487ffffffffffffff"},
		{"rejects a truncated nested element", "30020208"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			der, err := hex.DecodeString(c.der)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeDER(der); !errors.Is(err, ErrMalformed) {
				t.Fatalf("DecodeDER(%s) = %v, want ErrMalformed", c.der, err)
			}
		})
	}
}

// TestDerCodecTimeParityMatterJS mirrors
// packages/general/test/codec/DerCodecTest.ts:84-90 at 862ad75f: a date
// before 2050 is a UTCTime, and the "no well-defined expiration" date
// (X520.NON_WELL_DEFINED_DATE, 99991231235959Z) a GeneralizedTime — the
// Validity encoding of a certificate's NotAfter of 0
// (matterEpochToTime).
func TestDerCodecTimeParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		at   time.Time
		want string
	}{
		{"encodes DateTime correctly", time.Date(2024, 4, 4, 15, 30, 45, 0, time.UTC), "170d3234303430343135333034355a"},
		{"encodes special non-well defined GeneralizedDate correctly", matterEpochToTime(0), "180f39393939313233313233353935395a"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := asn1.Marshal(c.at)
			if err != nil {
				t.Fatal(err)
			}
			if hex.EncodeToString(got) != c.want {
				t.Fatalf("Marshal = %x, want %s", got, c.want)
			}
		})
	}
}
