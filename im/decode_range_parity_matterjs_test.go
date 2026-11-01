// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// Path and header integers are type- and range-checked, never masked
// (review 2026-11 W2-wire-03). An out-of-range value is a ConstraintError
// as matter.js packages/types/src/tlv/TlvNumber.ts:151-155 throws it; a
// value of another TLV type is InvalidAction as chip
// src/app/MessageDef/AttributePathIB.cpp:220-275 answers it.

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

func TestDecodeRangeParity(t *testing.T) {
	t.Parallel()
	readRequest := func(endpoint ...byte) []byte {
		out := []byte{0x15, 0x36, 0x00, 0x17}
		out = append(out, endpoint...)
		return append(out, 0x24, 0x03, 0x1D, 0x24, 0x04, 0x03, 0x18, 0x18, 0x28, 0x03, 0x18)
	}
	cases := []struct {
		name   string
		decode func() error
		want   StatusCode
	}{
		{"(a) ReadRequest endpoint 65536 as uint32", func() error {
			_, err := UnmarshalReadRequestTLV(tlv.NewDecoder(readRequest(0x26, 0x02, 0x00, 0x00, 0x01, 0x00)))
			return err
		}, StatusConstraintError},
		{"(b) ReadRequest endpoint -1 as int8", func() error {
			_, err := UnmarshalReadRequestTLV(tlv.NewDecoder(readRequest(0x20, 0x02, 0xFF)))
			return err
		}, StatusInvalidAction},
		{"(c) TimedRequest 70000 ms as uint32", func() error {
			_, err := UnmarshalTimedRequestTLV(tlv.NewDecoder([]byte{0x15, 0x26, 0x00, 0x70, 0x11, 0x01, 0x00, 0x18}))
			return err
		}, StatusConstraintError},
		{"SubscribeRequest MinIntervalFloor 65536", func() error {
			_, err := UnmarshalSubscribeRequestTLV(tlv.NewDecoder([]byte{0x15, 0x28, 0x00, 0x26, 0x01, 0x00, 0x00, 0x01, 0x00, 0x24, 0x02, 0x3C, 0x28, 0x07, 0x18}))
			return err
		}, StatusConstraintError},
		{"CommandPathIB cluster as a string", func() error {
			_, err := UnmarshalCommandPathTLV(tlv.NewDecoder([]byte{0x37, 0x00, 0x24, 0x00, 0x01, 0x2C, 0x01, 0x01, 0x41, 0x24, 0x02, 0x00, 0x18}))
			return err
		}, StatusInvalidAction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.decode()
			var sce StatusCodeError
			if !errors.As(err, &sce) || sce.MatterStatusCode() != tc.want {
				t.Fatalf("decode error = %v, want a %v status error", err, tc.want)
			}
		})
	}

	t.Run("the two AttributePathIB decoders skip a foreign tag alike", func(t *testing.T) {
		t.Parallel()
		// List { anonymous uint8 7, endpoint 1, cluster 6 }
		raw := []byte{0x17, 0x04, 0x07, 0x24, 0x02, 0x01, 0x24, 0x03, 0x06, 0x18}
		p, err := UnmarshalAttributePathTLV(tlv.NewDecoder(raw))
		if err != nil || !p.HasEndpoint || p.Endpoint != 1 || p.Cluster != 6 {
			t.Fatalf("UnmarshalAttributePathTLV = %+v, %v; want endpoint 1 cluster 6", p, err)
		}
		dec := tlv.NewDecoder(raw)
		if _, err := dec.Next(); err != nil {
			t.Fatal(err)
		}
		q, err := readAttributePathFields(dec)
		if err != nil || q != p {
			t.Errorf("readAttributePathFields = %+v, %v; want %+v", q, err, p)
		}
	})
}
