// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// Path and header integers are type- and range-checked, never masked
// (review 2026-11 W2-wire-03). An out-of-range value is a ConstraintError
// as matter.js packages/types/src/tlv/TlvNumber.ts:145-155 throws it
// (ValidationOutOfBoundsError, ValidationError.ts:16-29); a value of
// another TLV type, a signed integer included, is a Failure, as matter.js
// throws UnexpectedDataError for it (TlvNumber.ts:124-131), which carries
// no status, and the request's catch answers that with Status.Failure
// (packages/protocol/src/interaction/InteractionMessenger.ts:425-460 at
// db021da0).

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/tlv"
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
		}, StatusFailure},
		{"ReadRequest endpoint 1 as int8", func() error {
			_, err := UnmarshalReadRequestTLV(tlv.NewDecoder(readRequest(0x20, 0x02, 0x01)))
			return err
		}, StatusFailure},
		{"ReadRequest endpoint as a boolean", func() error {
			_, err := UnmarshalReadRequestTLV(tlv.NewDecoder(readRequest(0x28, 0x02)))
			return err
		}, StatusFailure},
		{"TimedRequest timeout as int16", func() error {
			_, err := UnmarshalTimedRequestTLV(tlv.NewDecoder([]byte{0x15, 0x21, 0x00, 0xE8, 0x03, 0x18}))
			return err
		}, StatusFailure},
		{"SubscribeRequest MaxIntervalCeiling as a string", func() error {
			_, err := UnmarshalSubscribeRequestTLV(tlv.NewDecoder([]byte{0x15, 0x28, 0x00, 0x24, 0x01, 0x00, 0x2C, 0x02, 0x01, 0x41, 0x28, 0x07, 0x18}))
			return err
		}, StatusFailure},
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
		}, StatusFailure},
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

	t.Run("the two AttributePathIB decoders refuse a foreign tag alike", func(t *testing.T) {
		t.Parallel()
		// List { anonymous uint8 7, endpoint 1, cluster 6 }: a tagged
		// list refuses a non-context member (TlvObject.ts:205-214 at
		// b25ed960; review II, R2-P6-wire-0504, Fable's read of sub
		// round 1, H-5).
		raw := []byte{0x17, 0x04, 0x07, 0x24, 0x02, 0x01, 0x24, 0x03, 0x06, 0x18}
		if p, err := UnmarshalAttributePathTLV(tlv.NewDecoder(raw)); err == nil {
			t.Fatalf("UnmarshalAttributePathTLV = %+v; want the path refused", p)
		}
		dec := tlv.NewDecoder(raw)
		if _, err := dec.Next(); err != nil {
			t.Fatal(err)
		}
		if q, err := readAttributePathFields(dec); err == nil {
			t.Errorf("readAttributePathFields = %+v; want the path refused", q)
		}
	})
}
